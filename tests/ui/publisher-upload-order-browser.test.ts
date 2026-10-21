import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import type { WorkspaceOwnerOperationDTO } from "../../apps/console-ui/src/api/dtos.ts";
import type { PackageVersion } from "../../apps/console-ui/src/pages/PublisherPage.tsx";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";
import { startConsoleDemoServer } from "../../tools/start-console-demo.ts";

const archive = Buffer.from("504b0506000000000000000000000000000000000000", "hex");
const sha256 = (bytes: Buffer) => "sha256:" + createHash("sha256").update(bytes).digest("hex");
const digest = "sha256:" + "a".repeat(64);
const operation: WorkspaceOwnerOperationDTO = {
  operationId: "op-upload", owner: "capability", kind: "complete_upload", resourceId: "pv-upload",
  status: "succeeded", stage: "upload_verification", requestId: "request-upload",
  createdAt: "2026-10-19T00:00:00Z", updatedAt: "2026-10-19T00:00:00Z"
};
const uploaded: PackageVersion = {
  id: "pv-upload", packageId: "pkg-upload", versionLabel: "browser-v1", status: "uploaded",
  sha256: sha256(archive), sizeBytes: String(archive.length)
};
const build = {
  id: "build-upload", packageVersionId: uploaded.id, runtimeVersionId: "runtime-default", webuiVersionId: "webui-default",
  status: "succeeded", stage: "registering", artifactDigest: digest, resultCapabilityVersionId: "capv-upload"
};
function gate() {
  let release!: () => void;
  const promise = new Promise<void>((resolve) => { release = resolve; });
  return { promise, release };
}

test("Publisher requires successful complete and exact uploaded readback before Build", { timeout: 180_000 }, async (t) => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  const server = await startConsoleDemoServer({ port: 0, log: false });
  const browser = await launchBrowser({ headless: true });
  try {
    async function scenario(options: {
      complete?: unknown; version?: unknown; completeStatus?: number; versionStatus?: number;
      held?: boolean; succeeds?: boolean; resumeForeignUpload?: boolean;
    } = {}) {
      const context = await browser.newContext();
      const page = await context.newPage();
      page.setDefaultTimeout(10_000);
      const events: string[] = [];
      const unexpected: string[] = [];
      const errors: string[] = [];
      const builds: unknown[] = [];
      const partBodies: Buffer[] = [];
      const completeBodies: unknown[] = [];
      const completeEntered = gate(); const completeResponse = gate();
      const versionEntered = gate(); const versionResponse = gate();
      const buildEntered = gate(); const buildResponse = gate();
      let versionReads = 0;
      page.on("pageerror", (error) => errors.push(error.message));
      await context.route("**/*", async (route) => {
        const request = route.request();
        const url = new URL(request.url());
        const path = url.pathname;
        const method = request.method();
        const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
        if (url.origin !== server.origin) { unexpected.push(request.url()); return route.abort("blockedbyclient"); }
        if (path === "/@vite/client") return route.fulfill({ contentType: "application/javascript", body: viteClientWithoutHmrTransport });
        if (path.startsWith("/publisher-parts/")) {
          const bytes = request.postDataBuffer()!;
          partBodies.push(bytes);
          if (method !== "PUT" || request.headers()["x-opl-sha256"] !== sha256(bytes) || request.headers().cookie) unexpected.push(`${method} ${path}: invalid part authorization`);
          return route.fulfill({ status: 200, headers: { ETag: `part-${partBodies.length}` } });
        }
        if (!path.startsWith("/api/v2/")) return route.continue();
        if (["/api/v2/auth/context", "/api/v2/auth/login", "/api/v2/auth/session"].includes(path)) return json({
          actorId: "user-customer", tenantId: "tenant-upload", displayName: "Publisher", permissions: [],
          csrfToken: "csrf-upload", expiresAt: "2099-01-01T00:00:00Z"
        });
        // Login opens Overview before Publisher. Its unrelated customer reads
        // remain served by the local demo server, outside this publisher trace.
        if (method === "GET" && ["/api/v2/workspaces", "/api/v2/wallet", "/api/v2/usage/summary", "/api/v2/legacy-billing/receipts", "/api/v2/legacy-billing/workspace-settlements", "/api/v2/announcements"].includes(path)) return route.continue();
        if (path === "/api/v2/namespaces") return json({ items: [{ id: "ns-upload", name: "publisher" }] });
        if (path === "/api/v2/packages") return json({ items: [{ id: "pkg-upload", name: "browser-package" }] });
        if (path === "/api/v2/catalog/build-policy") return json({ runtimeVersionId: "runtime-default", defaultWebuiVersionId: "webui-default" });
        if (path === "/api/v2/catalog/runtime-versions") return json({ items: [
          { id: "runtime-first", status: "approved", artifactDigest: digest },
          { id: "runtime-default", status: "approved", artifactDigest: digest, defaultForNewBuilds: true }
        ] });
        if (path === "/api/v2/catalog/webui-versions") return json({ items: [
          { id: "webui-first", status: "approved", artifactDigest: digest },
          { id: "webui-default", status: "approved", artifactDigest: digest }
        ] });
        events.push(`${method} ${path}`);
        if (method === "POST" && path === "/api/v2/packages/pkg-upload/uploads") return json({
          id: "upload-1", packageVersionId: uploaded.id, partSizeBytes: "16", completedParts: []
        }, 201);
        if (method === "GET" && path === "/api/v2/uploads/upload-1" && options.resumeForeignUpload) return json({
          id: "upload-1", packageVersionId: "pv-foreign", partSizeBytes: "16", completedParts: []
        });
        if (method === "POST" && path === "/api/v2/uploads/upload-1/parts") {
          const part = request.postDataJSON();
          return json({ method: "PUT", url: `${server.origin}/publisher-parts/${part.partNumber}`, contentType: "application/octet-stream",
            requiredChecksumHeaderName: "x-opl-sha256", requiredChecksumHeaderValue: part.sha256 }, 201);
        }
        if (method === "POST" && path === "/api/v2/uploads/upload-1/complete") {
          completeBodies.push(request.postDataJSON()); completeEntered.release();
          if (options.held) await completeResponse.promise;
          return json(Object.hasOwn(options, "complete") ? options.complete : operation, options.completeStatus ?? 202);
        }
        if (method === "GET" && path === "/api/v2/package-versions/pv-upload") {
          versionReads += 1; versionEntered.release();
          if (options.held) await versionResponse.promise;
          return json(Object.hasOwn(options, "version") ? options.version : uploaded, options.versionStatus ?? 200);
        }
        if (method === "POST" && path === "/api/v2/builds") {
          builds.push(request.postDataJSON()); buildEntered.release();
          if (options.held) await buildResponse.promise;
          return json(build, 201);
        }
        if (method === "GET" && path === "/api/v2/builds/build-upload") return json(build);
        if (method === "GET" && path === "/api/v2/capability-versions/capv-upload") return json({
          id: "capv-upload", status: "ready", artifactDigest: digest, deploymentDescriptorDigest: digest
        });
        unexpected.push(`${method} ${path}`);
        return json({ message: "Unexpected publisher request" }, 500);
      });
      try {
        await page.goto(`${server.origin}/login`, { waitUntil: "domcontentloaded" });
        await page.getByLabel("邮箱").fill("publisher@example.test");
        await page.getByLabel("密码").fill("isolated-password");
        await page.getByRole("button", { name: "登录", exact: true }).click();
        await page.waitForURL(/\/console\/overview$/);
        await page.goto(`${server.origin}/console/publisher`, { waitUntil: "domcontentloaded" });
        await page.getByLabel("Package", { exact: true }).selectOption(uploaded.packageId);
        await page.getByLabel("版本名称").fill(uploaded.versionLabel);
        await page.getByLabel("ZIP 文件").setInputFiles({ name: "package.zip", mimeType: "application/zip", buffer: archive });
        assert.equal(await page.getByLabel("Runtime Release", { exact: true }).count(), 0);
        assert.equal(await page.getByLabel("WebUI", { exact: true }).count(), 0);
        const submit = page.getByRole("button", { name: "上传并构建 / 继续上传", exact: true });
        await submit.click();
        if (options.held) {
          await completeEntered.promise;
          assert.equal(versionReads, 0, "readback waits for complete Operation");
          assert.equal(builds.length, 0, "Build waits for complete Operation");
          completeResponse.release();
          await versionEntered.promise;
          assert.equal(builds.length, 0, "Build waits for exact PackageVersion response");
          assert.equal(await page.getByRole("heading", { name: "版本已就绪", exact: true }).count(), 0);
          versionResponse.release();
          await buildEntered.promise;
          await page.locator("details.publisher-technical-details > summary").click();
          const facts = page.getByRole("region", { name: "Package 版本读回", exact: true });
          await facts.getByText("uploaded", { exact: true }).waitFor();
          assert.ok((await facts.textContent())?.includes(uploaded.id));
          buildResponse.release();
        }
        const succeeds = options.held || options.succeeds || Object.keys(options).length === 0;
        if (succeeds) await page.getByRole("heading", { name: "版本已就绪", exact: true }).waitFor();
        else {
          await page.getByRole("alert").waitFor();
          await submit.waitFor({ state: "visible" });
          await page.waitForFunction(() => !(document.querySelector(".publisher-page button[type=submit]") as HTMLButtonElement).disabled);
          if (options.resumeForeignUpload) {
            await submit.click();
            await page.getByRole("alert").filter({ hasText: "上传会话与本次 Package 版本不一致" }).waitFor();
          }
          assert.equal(builds.length, 0, "unconfirmed or foreign upload must never create Build");
          assert.equal(await page.getByRole("heading", { name: "版本已就绪", exact: true }).count(), 0);
          assert.equal(await page.getByRole("region", { name: "Package 版本读回", exact: true, includeHidden: true }).count(), 0);
        }
        assert.deepEqual(unexpected, [], "all requests stay in the existing local API protocol; no polling fallback");
        assert.deepEqual(errors, []);
        assert.deepEqual(Buffer.concat(partBodies), archive, "actual page uploads the selected bytes through signed permits");
        assert.equal(completeBodies.length, 1);
        assert.deepEqual(completeBodies[0], { parts: [0, 16].map((offset, index) => {
          const bytes = archive.subarray(offset, offset + 16);
          return { partNumber: index + 1, etag: `part-${index + 1}`, sizeBytes: String(bytes.length), sha256: sha256(bytes) };
        }) });
        return { events, builds, versionReads };
      } finally {
        completeResponse.release(); versionResponse.release(); buildResponse.release();
        await context.close();
      }
    }

    await t.test("waits at both owner responses, shows uploaded before Build, and preserves platform defaults", async () => {
      const result = await scenario({ held: true });
      assert.deepEqual(result.builds, [{ packageVersionId: uploaded.id, runtimeVersionId: "runtime-default", webuiVersionId: "webui-default" }]);
      assert.equal(result.versionReads, 1);
      assert.deepEqual(result.events.filter((event) => /\/complete$|\/package-versions\/|\/builds$/.test(event)), [
        "POST /api/v2/uploads/upload-1/complete", "GET /api/v2/package-versions/pv-upload", "POST /api/v2/builds"
      ]);
    });
    await t.test("accepts explicit confirmed evidence as well as the owner's omitted optional observation", async () => {
      const result = await scenario({ complete: { ...operation, observationResult: "confirmed" }, succeeds: true });
      assert.equal(result.builds.length, 1);
    });
    for (const status of ["accepted", "running", "awaiting_confirmation", "failed", "needs_attention", "cancelled", "unknown", undefined]) {
      await t.test(`complete status ${String(status)} prevents readback and Build`, async () => {
        const result = await scenario({ complete: { ...operation, status } });
        assert.equal(result.versionReads, 0);
      });
    }
    for (const observationResult of ["rejected", "unknown"]) {
      await t.test(`complete observation ${observationResult} prevents readback and Build`, async () => {
        const result = await scenario({ complete: { ...operation, observationResult } });
        assert.equal(result.versionReads, 0);
      });
    }
    for (const [name, complete] of [
      ["foreign resource", { ...operation, resourceId: "pv-foreign" }],
      ["foreign owner", { ...operation, owner: "build" }],
      ["foreign kind", { ...operation, kind: "create_build" }],
      ["missing operation identity", { ...operation, operationId: "" }],
      ["missing Operation", null]
    ] as const) await t.test(`${name} prevents readback and Build`, async () => {
      assert.equal((await scenario({ complete })).versionReads, 0);
    });
    for (const status of ["upload_pending", "rejected", "unknown", undefined]) {
      await t.test(`PackageVersion status ${String(status)} prevents Build`, async () => {
        assert.equal((await scenario({ version: { ...uploaded, status } })).versionReads, 1);
      });
    }
    for (const [name, version] of [
      ["foreign version", { ...uploaded, id: "pv-foreign" }],
      ["foreign Package", { ...uploaded, packageId: "pkg-foreign" }],
      ["different file digest", { ...uploaded, sha256: digest }],
      ["different file size", { ...uploaded, sizeBytes: "999" }],
      ["different version label", { ...uploaded, versionLabel: "other-version" }],
      ["missing PackageVersion", null]
    ] as const) await t.test(`${name} prevents Build`, async () => {
      assert.equal((await scenario({ version })).versionReads, 1);
    });
    await t.test("HTTP complete rejection prevents Build", async () => {
      assert.equal((await scenario({ completeStatus: 422, complete: { message: "Upload rejected" } })).versionReads, 0);
    });
    await t.test("failed exact readback prevents Build without polling", async () => {
      assert.equal((await scenario({ versionStatus: 503, version: { message: "Version readback unavailable" } })).versionReads, 1);
    });
    await t.test("resumed upload cannot replace the saved PackageVersion with a foreign version", async () => {
      assert.equal((await scenario({ resumeForeignUpload: true, completeStatus: 503, complete: { message: "Complete unavailable" } })).versionReads, 0);
    });
  } finally {
    await browser.close();
    await server.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
