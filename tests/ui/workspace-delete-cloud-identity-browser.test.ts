import assert from "node:assert/strict";
import test from "node:test";

import type { Page } from "playwright";

import {
  CONSOLE_DEMO_CREDENTIALS,
  startConsoleDemoServer
} from "../../tools/start-console-demo.ts";
import { viteClientWithoutHmrTransport } from "../../tools/console-browser-qa.ts";
import { launchBrowser } from "../../tools/launch-browser.ts";

const workspaceId = "ws-1";
const workspaceName = "Cloud Delete Workspace";

const viewports = [
  { name: "desktop", width: 1280, height: 900 },
  { name: "mobile", width: 390, height: 844 }
] as const;

// The Workspace owner's deletion readback as the typed contract declares it.
interface OwnerDeletion {
  workspaceId: string;
  operationId: string;
  resourceDeletionStatus: "pending" | "confirmed" | "rejected" | "unknown";
  dataDeletionStatus: "pending" | "confirmed" | "rejected" | "unknown";
  refundStatus: "not_applicable" | "requested" | "confirmed" | "rejected" | "unknown";
  refundOperationId?: string;
  updatedAt: string;
}

interface OwnerWorkspace {
  id: string;
  name: string;
  status: string;
  deliveryModel: string;
  resourceReadiness: string;
  applicationAvailability: string;
  currentPeriodEnd: string;
  createdAt: string;
  updatedAt: string;
  version: string;
}

interface DeleteCommand {
  method: string;
  path: string;
  csrf: string | undefined;
  idempotencyKey: string | undefined;
  body: unknown;
}

interface CloudDeleteFixture {
  workspace: OwnerWorkspace;
  workspacePresent: boolean;
  deletion: OwnerDeletion | null;
  deleteCommands: DeleteCommand[];
  deletionReads: number;
  legacyDeleteCalls: string[];
  legacyDeletionReads: number;
  legacyListReads: number;
}

function ownerWorkspace(): OwnerWorkspace {
  return {
    id: workspaceId,
    name: workspaceName,
    status: "active",
    deliveryModel: "agent_saas",
    resourceReadiness: "ready",
    applicationAvailability: "available",
    currentPeriodEnd: "2026-10-27T00:00:00Z",
    createdAt: "2026-09-27T00:00:00Z",
    updatedAt: "2026-09-27T00:00:00Z",
    version: "1"
  };
}

// installCloudDeleteFixture answers the Workspace owner's own routes and records
// every legacy Workspace route the page might still reach for. Any legacy delete
// or legacy deletion readback is answered with a loud failure so the assertion
// cannot pass by accident.
async function installCloudDeleteFixture(page: Page, origin: string) {
  const fixture: CloudDeleteFixture = {
    workspace: ownerWorkspace(),
    workspacePresent: true,
    deletion: null,
    deleteCommands: [],
    deletionReads: 0,
    legacyDeleteCalls: [],
    legacyDeletionReads: 0,
    legacyListReads: 0
  };
  const audit = { externalRequests: [] as string[], pageErrors: [] as string[], consoleErrors: [] as string[] };
  page.on("pageerror", (error) => audit.pageErrors.push(error.stack || error.message));
  page.on("console", (message) => {
    if (message.type() === "error") audit.consoleErrors.push(message.text());
  });
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin === origin && url.pathname === "/@vite/client") {
      await route.fulfill({ status: 200, contentType: "application/javascript", body: viteClientWithoutHmrTransport });
      return;
    }
    if (url.origin !== origin) {
      audit.externalRequests.push(`${request.method()} ${request.url()}`);
      await route.abort("blockedbyclient");
      return;
    }
    const path = url.pathname;
    if (path.startsWith("/api/v2/")) {
      if (path === "/api/v2/auth/session") {
        await route.fulfill({ json: { actorId: "user-customer", tenantId: "acct-1", displayName: "Customer", permissions: [], csrfToken: "fixture-csrf", expiresAt: "2099-01-01T00:00:00Z" } });
        return;
      }
      if (path === "/api/v2/workspaces" && request.method() === "GET") {
        await route.fulfill({ json: { items: fixture.workspacePresent ? [fixture.workspace] : [] } });
        return;
      }
      if (path === `/api/v2/workspaces/${workspaceId}/deletion`) {
        fixture.deletionReads += 1;
        if (!fixture.deletion) {
          await route.fulfill({ status: 404, json: { code: "NOT_FOUND", message: "Workspace deletion is not found", requestId: "request-delete-read" } });
          return;
        }
        await route.fulfill({ json: fixture.deletion });
        return;
      }
      if (path === `/api/v2/workspaces/${workspaceId}` && request.method() === "DELETE") {
        fixture.deleteCommands.push({
          method: request.method(),
          path,
          csrf: request.headers()["x-csrf-token"],
          idempotencyKey: request.headers()["idempotency-key"],
          body: request.postDataJSON()
        });
        fixture.deletion = {
          workspaceId,
          operationId: "op-delete-1",
          resourceDeletionStatus: "pending",
          dataDeletionStatus: "pending",
          refundStatus: "not_applicable",
          updatedAt: "2026-09-18T05:00:00Z"
        };
        await route.fulfill({
          status: 202,
          json: {
            operationId: "op-delete-1", owner: "workspace", kind: "delete_workspace", resourceId: workspaceId,
            status: "accepted", stage: "admission", requestId: "request-delete-1",
            createdAt: "2026-09-18T05:00:00Z", updatedAt: "2026-09-18T05:00:00Z", pollAfterSeconds: 2
          }
        });
        return;
      }
      if (path === `/api/v2/workspaces/${workspaceId}`) {
        await route.fulfill({ json: fixture.workspace });
        return;
      }
      if (path === `/api/v2/delivery/${workspaceId}`) {
        await route.fulfill({
          json: {
            workspaceId,
            workspace: { owner: "workspace", state: "active", details: {} },
            capabilityVersion: { owner: "capability", state: "ready", details: {} },
            build: { owner: "build", state: "succeeded", details: {} },
            serve: { owner: "serve", state: "ready", details: {} }
          }
        });
        return;
      }
      assert.fail(`unexpected BFF request: ${request.method()} ${path}`);
    }
    if (request.method() === "DELETE" && path === `/api/workspaces/${workspaceId}`) {
      fixture.legacyDeleteCalls.push(`${request.method()} ${path}`);
      await route.fulfill({ status: 405, json: { error: "legacy_workspace_delete_route" } });
      return;
    }
    if (path === `/api/workspaces/${workspaceId}/deletion`) {
      fixture.legacyDeletionReads += 1;
      await route.fulfill({ status: 404, json: { error: "legacy_workspace_deletion_route" } });
      return;
    }
    if (path === "/api/workspaces") {
      fixture.legacyListReads += 1;
      await route.fulfill({ status: 404, json: { error: "legacy_workspace_list_route" } });
      return;
    }
    await route.continue();
  });
  return { fixture, audit };
}

test("cloud identity deletes a Workspace through its owner and never the legacy route", { timeout: 120_000 }, async () => {
  const previousIdentity = process.env.VITE_CONSOLE_IDENTITY;
  process.env.VITE_CONSOLE_IDENTITY = "cloud";
  let demo: Awaited<ReturnType<typeof startConsoleDemoServer>>;
  try {
    demo = await startConsoleDemoServer({ port: 0, log: false });
  } catch (error) {
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
    throw error;
  }
  const browser = await launchBrowser({ headless: true });
  try {
    for (const viewport of viewports) {
      const context = await browser.newContext({ viewport });
      const page = await context.newPage();
      const { fixture, audit } = await installCloudDeleteFixture(page, demo.origin);
      const session = await page.request.post(`${demo.origin}/api/auth/login`, { data: CONSOLE_DEMO_CREDENTIALS.customer });
      assert.equal(session.ok(), true);
      await page.goto(`${demo.origin}/console/workspaces/${workspaceId}`, { waitUntil: "networkidle" });

      // The owner's command is only submittable once the caller confirmed the
      // exact Workspace name and acknowledged the data destruction.
      const deletePanel = page.locator(".workspace-delete-panel");
      const submit = deletePanel.getByRole("button", { name: "删除工作空间", exact: true });
      await submit.waitFor({ state: "visible" });
      assert.equal(await submit.isDisabled(), true);
      const confirmationName = deletePanel.getByLabel("工作空间名称");
      await confirmationName.fill("Not The Workspace");
      await deletePanel.getByRole("checkbox").check();
      assert.equal(await submit.isDisabled(), true);
      await confirmationName.fill(workspaceName);
      assert.equal(await submit.isDisabled(), false);

      await submit.click();
      await page.getByText("正在删除工作空间", { exact: true }).waitFor({ state: "visible" });
      assert.equal(fixture.deleteCommands.length, 1);
      const command = fixture.deleteCommands[0];
      assert.equal(command.method, "DELETE");
      assert.equal(command.path, `/api/v2/workspaces/${workspaceId}`);
      assert.equal(command.csrf, "fixture-csrf");
      assert.match(command.idempotencyKey ?? "", /^workspace-delete:/);
      assert.deepEqual(command.body, { confirmationName: workspaceName, acknowledgeDataDestruction: true });

      // Resource cleanup and the refund are two separate facts: the resources
      // are still being deleted while this deletion is not due a refund.
      const progress = deletePanel.locator("[data-workspace-delete-progress]");
      await progress.getByText("正在删除资源与数据", { exact: true }).waitFor({ state: "visible" });
      await progress.getByText("本次删除无需退款", { exact: true }).waitFor({ state: "visible" });

      // The resources finish first: the Workspace is gone while the owner's
      // refund readback is still only requested — never inferred from deletion.
      fixture.deletion = {
        workspaceId,
        operationId: "op-delete-1",
        resourceDeletionStatus: "confirmed",
        dataDeletionStatus: "confirmed",
        refundStatus: "requested",
        refundOperationId: "wallet-operation-refund-1",
        updatedAt: "2026-09-18T05:02:00Z"
      };
      await deletePanel.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await progress.getByText("资源与数据已确认删除", { exact: true }).waitFor({ state: "visible" });
      await progress.getByText("退款处理中", { exact: true }).waitFor({ state: "visible" });
      await progress.getByText("wallet-operation-refund-1", { exact: true }).waitFor({ state: "visible" });

      // Reload reads the same operation back instead of submitting a second one.
      await page.reload({ waitUntil: "networkidle" });
      await progress.getByText("退款处理中", { exact: true }).waitFor({ state: "visible" });
      assert.equal(fixture.deleteCommands.length, 1);
      assert.ok(fixture.deletionReads >= 2, `deletion reads: ${fixture.deletionReads}`);

      // The authoritative absence readback is the owner's own list, and it is
      // what finally confirms the deletion before Console leaves the page.
      fixture.workspacePresent = false;
      await deletePanel.getByRole("button", { name: "刷新删除状态", exact: true }).click();
      await page.waitForURL((url) => url.pathname === "/console/workspaces");
      await page.getByText("Workspace 已删除", { exact: true }).waitFor({ state: "visible" });

      assert.deepEqual(fixture.legacyDeleteCalls, []);
      assert.equal(fixture.legacyDeletionReads, 0);
      assert.equal(fixture.legacyListReads, 0);
      assert.deepEqual(audit.externalRequests, []);
      assert.deepEqual(audit.pageErrors, []);
      // The only expected console error is the owner's own not-found readback
      // before this Workspace has a deletion operation.
      assert.ok(
        audit.consoleErrors.every((message) => message.includes("404")),
        `unexpected console errors: ${audit.consoleErrors.join(" | ")}`
      );
      await context.close();
    }
  } finally {
    await browser.close();
    await demo.close();
    if (previousIdentity === undefined) delete process.env.VITE_CONSOLE_IDENTITY;
    else process.env.VITE_CONSOLE_IDENTITY = previousIdentity;
  }
});
