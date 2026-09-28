import assert from "node:assert/strict";
import test from "node:test";

import { chromium, type Page, type Route } from "playwright";

import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer,
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";

interface RecordedRequest {
  method: string;
  path: string;
  body: Record<string, unknown>;
}

const session = {
  actorId: "user-customer",
  tenantId: "acct-1",
  csrfToken: "fixture-csrf",
  expiresAt: "2099-01-01T00:00:00Z",
};

const approvedRuntime = {
  id: "runtime-approved-1",
  name: "OPL App Runtime",
  versionLabel: "2026.09.1",
  artifactDigest:
    "sha256:1111111111111111111111111111111111111111111111111111111111111111",
  status: "approved",
  runtimeAbiVersion: "opl-runtime-abi-v1",
  packageFormatVersions: ["oma-package-v1"],
};

const revokedRuntime = {
  ...approvedRuntime,
  id: "runtime-revoked-1",
  versionLabel: "2026.08.1",
  status: "revoked",
};

const buildJob = {
  id: "build-1",
  operationId: "operation-build-1",
  packageVersionId: "package-version-1",
  runtimeVersionId: approvedRuntime.id,
  webuiVersionId: "webui-approved-1",
  status: "succeeded",
  stage: "succeeded",
  artifactDigest:
    "sha256:2222222222222222222222222222222222222222222222222222222222222222",
  resultCapabilityVersionId: "capability-version-1",
};

async function fulfill(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function login(page: Page, origin: string) {
  const response = await page.request.post(`${origin}/api/auth/login`, {
    data: CONSOLE_DEMO_CREDENTIALS.customer,
  });
  assert.equal(response.ok(), true);
}

/**
 * This is a UI contract test only. Its fixture responses are not OMA, TCR,
 * Instance, Ledger, or production evidence.
 */
test(
  "Publisher reads/selects an approved Runtime Version and sends it to Build",
  { timeout: 60_000 },
  async () => {
    const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
    process.env.VITE_CONSOLE_IDENTITY = "cloud";
    let demo: Awaited<ReturnType<typeof startConsoleDemoServer>>;
    try {
      demo = await startConsoleDemoServer({ port: 0, log: false });
    } finally {
      if (previousIdentity === undefined)
        delete process.env.VITE_CONSOLE_IDENTITY;
      else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
    }

    const requests: RecordedRequest[] = [];
    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage({
        viewport: { width: 1280, height: 900 },
      });
      const origin = demo.origin;

      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (
          url.origin === origin ||
          url.protocol === "data:" ||
          url.protocol === "blob:"
        ) {
          if (url.origin === origin && url.pathname === "/@vite/client") {
            await route.fulfill({
              status: 200,
              contentType: "application/javascript",
              body: viteClientWithoutHmrTransport,
            });
            return;
          }
          await route.continue();
          return;
        }
        await route.abort("blockedbyclient");
      });

      await page.route("**/upload/upl-1/1", async (route) => {
        await route.fulfill({
          status: 200,
          headers: { etag: "etag-1" },
          body: "",
        });
      });

      await page.route("**/api/v2/**", async (route) => {
        const request = route.request();
        const url = new URL(request.url());
        const body = request.postDataJSON?.() || {};
        requests.push({ method: request.method(), path: url.pathname, body });

        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/auth/context"
        ) {
          await fulfill(route, { csrfToken: session.csrfToken });
          return;
        }
        if (
          request.method() === "POST" &&
          url.pathname === "/api/v2/auth/login"
        ) {
          await fulfill(route, {
            ...session,
            displayName: "Customer",
            permissions: [],
          });
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/auth/session"
        ) {
          await fulfill(route, {
            ...session,
            displayName: "Customer",
            permissions: [],
          });
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/namespaces"
        ) {
          await fulfill(route, {
            items: [{ id: "namespace-1", name: "OMA Publisher" }],
          });
          return;
        }
        if (request.method() === "GET" && url.pathname === "/api/v2/packages") {
          await fulfill(route, {
            items: [{ id: "package-1", name: "OMA Agent" }],
          });
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/catalog/runtime-versions"
        ) {
          await fulfill(route, { items: [approvedRuntime, revokedRuntime] });
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/catalog/webui-versions"
        ) {
          await fulfill(route, {
            items: [
              {
                id: "webui-approved-1",
                name: "Approved WebUI",
                versionLabel: "2026.09.1",
                status: "approved",
              },
            ],
          });
          return;
        }
        if (
          request.method() === "POST" &&
          url.pathname === "/api/v2/packages/package-1/uploads"
        ) {
          await fulfill(
            route,
            {
              id: "upl-1",
              packageVersionId: "package-version-1",
              partSizeBytes: 1024,
              completedParts: [],
            },
            201,
          );
          return;
        }
        if (
          request.method() === "POST" &&
          url.pathname === "/api/v2/uploads/upl-1/parts"
        ) {
          const checksum = String(body.sha256 || "");
          await fulfill(
            route,
            {
              method: "PUT",
              url: `${origin}/upload/upl-1/1`,
              contentType: "application/zip",
              requiredChecksumHeaderName: "x-opl-sha256",
              requiredChecksumHeaderValue: checksum,
            },
            201,
          );
          return;
        }
        if (
          request.method() === "POST" &&
          url.pathname === "/api/v2/uploads/upl-1/complete"
        ) {
          await fulfill(route, {
            id: "upl-1",
            packageVersionId: "package-version-1",
          });
          return;
        }
        if (request.method() === "POST" && url.pathname === "/api/v2/builds") {
          await fulfill(route, buildJob, 202);
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/builds/build-1"
        ) {
          await fulfill(route, buildJob);
          return;
        }
        if (
          request.method() === "GET" &&
          url.pathname === "/api/v2/capability-versions/capability-version-1"
        ) {
          await fulfill(route, {
            id: "capability-version-1",
            status: "ready",
            artifactDigest: buildJob.artifactDigest,
            deploymentDescriptorDigest:
              "sha256:3333333333333333333333333333333333333333333333333333333333333333",
          });
          return;
        }
        await fulfill(
          route,
          {
            error: `unexpected_publisher_route:${request.method()}:${url.pathname}`,
          },
          500,
        );
      });

      await login(page, origin);
      await page.goto(`${origin}/console/publisher/`, {
        waitUntil: "networkidle",
      });
      await page
        .locator(".publisher-page")
        .getByRole("heading", { name: "发布 Package", exact: true })
        .waitFor({ state: "visible" });

      assert.ok(
        requests.some(
          ({ method, path }) =>
            method === "GET" && path === "/api/v2/catalog/runtime-versions",
        ),
        "Publisher must read Runtime Versions from Runtime Control's approved catalog route",
      );

      const runtimeSelect = page.getByLabel("Runtime Version");
      assert.equal(
        await runtimeSelect.count(),
        1,
        "Publisher must expose a Runtime Version selector",
      );
      assert.deepEqual(
        await runtimeSelect.locator("option").evaluateAll((options) =>
          options.map((option) => ({
            value: (option as HTMLOptionElement).value,
            text: option.textContent,
            disabled: (option as HTMLOptionElement).disabled,
          })),
        ),
        [
          { value: "", text: "选择已批准的 Runtime Version", disabled: false },
          {
            value: approvedRuntime.id,
            text: `${approvedRuntime.name} · ${approvedRuntime.versionLabel}`,
            disabled: false,
          },
        ],
      );
      await runtimeSelect.selectOption(approvedRuntime.id);
      await page
        .getByLabel("Package", { exact: true })
        .selectOption("package-1");

      await page.getByLabel("版本名称").fill("oma-v1");
      await page.setInputFiles('input[type="file"]', {
        name: "oma-agent.zip",
        mimeType: "application/zip",
        buffer: Buffer.from("oma-package-content"),
      });
      const buildRequestPromise = page.waitForRequest(
        (request) =>
          request.method() === "POST" &&
          new URL(request.url()).pathname === "/api/v2/builds",
      );
      await page
        .getByRole("button", { name: "上传并构建 / 继续上传", exact: true })
        .click();

      const buildRequest = await buildRequestPromise;
      assert.deepEqual(buildRequest.postDataJSON(), {
        packageVersionId: "package-version-1",
        runtimeVersionId: approvedRuntime.id,
        webuiVersionId: "webui-approved-1",
      });
    } finally {
      await browser.close();
      await demo.close();
    }
  },
);
