/*
 * The provider-neutral business qualification orchestrator.
 *
 * One flow, two providers. Local-Docker and Tencent-TKE differ in how a
 * Workspace is made reachable and in how that reachability is torn down, and
 * in nothing else about the business question: the same Candidate, the same
 * customer, the same product, the same purchase, the same charge, the same
 * proof. That shared part is this module, and it is the only part that decides
 * what a qualification result means.
 *
 * What is deliberately not here: any provider SDK, any cluster or container
 * mutation, any local-filesystem assumption, any second database, and any
 * second authority over a wallet, a Key or an account. An adapter supplies a
 * business transport and the ability to close what it opened; the flow below is
 * what both providers run.
 *
 * Replay is handled by derivation rather than by stored state. The operation
 * identity is a function of the Candidate, the tenant, the product and the
 * provider, so a redelivery of the same qualification addresses the same launch
 * order the first delivery created. Before issuing anything the orchestrator
 * asks the platform what it already knows about that order: if the order is
 * there it is resumed, not reissued. A response lost after the platform accepted
 * a command is the same case seen once more and is answered the same way. No
 * path here issues a second purchase, and so none creates a second Workspace,
 * charge, refund or managed Key.
 */

import {
  QUALIFICATION_BUSINESS_CHECKS,
  QUALIFICATION_BUSINESS_RECEIPT_KIND,
  QUALIFICATION_MUTATION_BOUNDS,
  QUALIFICATION_TERMINAL_REASONS,
  QUALIFICATION_UNCOVERED_SCOPE,
  createQualificationRequest,
  qualificationWorkspaceName,
  validateBusinessQualificationReceipt,
} from "./workspace-qualification-contract.ts";
import { createHash } from "node:crypto";

import {
  launchOperationId, launchWorkspaceId, priceWorkspaceLaunch, submitWorkspaceLaunch, waitForLaunch,
} from "./workspace-purchase-core.ts";

/*
 * The adapter boundary.
 *
 * Three operations, all of them about reachability rather than about the
 * product: make a running business origin available, report whether the host
 * carrying it is healthy, and put back what was opened. Every business fact the
 * result depends on is read by the orchestrator itself over Cloud's own owner
 * APIs, so a provider cannot assert that a purchase settled or that a Key
 * exists. Deployment health is asked for and recorded, and is never allowed to
 * move the terminal state.
 */
export const QUALIFICATION_ADAPTER_METHODS = Object.freeze([
  "openBusinessScope", "closeBusinessScope", "readDeploymentHealth",
]);

const emptyMutations = () => Object.fromEntries(QUALIFICATION_MUTATION_BOUNDS.map((name) => [name, 0]));
const emptyChecks = () => Object.fromEntries(QUALIFICATION_BUSINESS_CHECKS.map((name) => [name, false]));
const digestOf = (value) => `sha256:${createHash("sha256").update(value).digest("hex")}`;

function adapterError(code) {
  const error = new Error(code);
  error.code = code;
  return error;
}

/*
 * The adapter is checked before it is used. An adapter that is missing an
 * operation, or that declares a different provider than the request did, is
 * refused here rather than discovered halfway through a purchase.
 */
export function assertQualificationAdapter(adapter, declaredProvider) {
  if (!adapter || typeof adapter !== "object") throw adapterError("qualification_adapter_invalid");
  for (const name of QUALIFICATION_ADAPTER_METHODS) {
    if (typeof adapter[name] !== "function") throw adapterError("qualification_adapter_invalid");
  }
  const provider = String(adapter.provider || "").trim();
  if (!provider || provider !== String(declaredProvider || "").trim()) {
    throw adapterError("qualification_adapter_provider_mismatch");
  }
  return Object.freeze({ provider });
}

// The Control Plane reads its own values out of a `data` envelope and names the
// authority they came from. An answer without that provenance is not an owner
// readback, so it is refused rather than read as one.
function ownerData(payload, expectedSource = "sub2api") {
  if (!payload || payload.source !== expectedSource || payload.available !== true ||
      !["available", "empty"].includes(payload.status)) {
    throw adapterError("owner_readback_unavailable");
  }
  return payload.data;
}

function walletMicros(payload) {
  const value = String(ownerData(payload)?.usdMicros || "");
  if (!/^(?:0|[1-9][0-9]*)$/.test(value)) throw adapterError("owner_readback_unavailable");
  return value;
}

/*
 * The identity fence.
 *
 * An adapter hands over a transport and a session. Nothing about those two says
 * which account the session belongs to, so before anything is priced or bought
 * the session is asked who it is and the answer has to be the account this run
 * declared. Without this a transport pointed at any other account would produce
 * a settled receipt for a purchase the request never named — the Customer's
 * identity would be whatever the adapter happened to be holding.
 *
 * This runs before the quote, so a mismatched session costs one read and no
 * money.
 */
async function bindAuthenticatedAccount(http, auth, declaredAccountId) {
  const me = ownerData((await http.json("/api/auth/me", {}, auth)).payload);
  if (me?.accountId !== declaredAccountId || me?.role !== "owner" || me?.status !== "active") {
    throw adapterError("account_binding_mismatch");
  }
  return true;
}

/*
 * The Workspace and its Runtime, read from their owners.
 *
 * A purchase receipt says what was billed, not what runs. The Workspace owner
 * says the Workspace exists, is openable and has a ready current application;
 * the Fabric runtime readback says a Runtime is actually running behind that
 * exact application on the exact entry URL. Reading both is what turns "we were
 * charged" into "there is something there to use".
 */
async function readDeliveredWorkspace(http, auth, workspaceId, applicationOperationId) {
  const page = ownerData((await http.json("/api/workspaces?page=1&pageSize=20", {}, auth)).payload, "control-plane");
  const workspace = Array.isArray(page?.items) ? page.items.find((candidate) => candidate?.id === workspaceId) : null;
  const application = workspace?.currentApplication;
  if (!workspace || !application || application.status !== "ready" ||
      !String(application.entryUrl || "").trim() || workspace.url !== application.entryUrl || workspace.openable !== true) {
    throw adapterError("workspace_not_ready");
  }
  const runtime = ownerData((await http.json(
    `/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-status`, {}, auth)).payload, "fabric");
  if (runtime?.workspaceId !== workspaceId || runtime?.ready !== true || runtime?.status !== "running" ||
      runtime?.url !== application.entryUrl ||
      runtime?.currentApplication?.operationId !== application.operationId ||
      !String(runtime?.runtimeId || "").trim() || !String(application.operationId || "").trim()) {
    throw adapterError("runtime_not_ready");
  }
  return { applicationOperationId: String(application.operationId) };
}

/*
 * The Ledger receipt for this exact purchase.
 *
 * This, not the wallet balance, is the evidence that a charge happened. The
 * receipt is the owner's record and it names the Workspace, the amount, and the
 * resources the platform says the purchase delivered; the launch it is read
 * against has to agree on the Workspace and on each of those resources. The
 * wallet is then checked against the receipt rather than used to infer it.
 */
async function readPurchaseChargeReceipt(http, auth, { receiptId, workspaceId, amountUsdMicros, launch }) {
  const receipt = ownerData((await http.json(
    `/api/billing/receipts/${encodeURIComponent(receiptId)}`, {}, auth)).payload, "ledger");
  if (receipt?.receiptId !== receiptId || receipt?.workspaceId !== workspaceId ||
      receipt?.type !== "billing.workspace_purchased.v1" || receipt?.status !== "completed" ||
      String(receipt?.totalUsdMicros || "") !== String(amountUsdMicros) ||
      !String(receipt?.chargeReference || "").trim()) {
    throw adapterError("charge_receipt_mismatch");
  }
  // A resource-only purchase delivers resources and nothing else. A Runtime or a
  // Workspace Key inside this receipt would mean the platform fulfilled more
  // than the operation declared, and the run would not know what it paid for.
  if (receipt?.fulfillment?.runtimeId !== undefined || receipt?.fulfillment?.workspaceApiKeyId !== undefined ||
      String(receipt?.fulfillment?.computeAllocationId || "") !== String(launch?.computeAllocationId || "") ||
      String(receipt?.fulfillment?.storageId || "") !== String(launch?.storageId || "") ||
      String(receipt?.fulfillment?.attachmentId || "") !== String(launch?.attachmentId || "")) {
    throw adapterError("charge_receipt_mismatch");
  }
  return String(receipt.chargeReference);
}

/*
 * What does the platform already know about this order?
 *
 * This is the whole of the replay answer. It is asked before the first command
 * and again after a lost response, and it reads the order the deterministic
 * identity names rather than any order this process remembers, so a different
 * machine answering the same qualification reaches the same conclusion.
 *
 * An answer that is not about this Workspace is refused rather than followed.
 * Resuming whatever the platform returned would let one account's order be read
 * as another's, and the run would then bill and report against a Workspace it
 * never declared.
 */
async function readAdmittedLaunch(http, operationId, workspaceId, auth) {
  const result = await http.request(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth);
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200 || !result?.payload) throw adapterError("owner_readback_unavailable");
  if (result.payload.workspaceId !== undefined && result.payload.workspaceId !== workspaceId) {
    throw adapterError("account_binding_mismatch");
  }
  return result.payload;
}

/*
 * A polling failure is not one thing.
 *
 * The purchase core publishes two different refusals and they mean opposite
 * things to an owner: a launch that reached a status the platform itself
 * published as stopping has failed, while an order that was accepted and then
 * went quiet has not. The second is the case where this run already caused a
 * mutation and cannot say what became of it, so it must not be reported as a
 * failure of the product, and must not be reported as success either.
 */
function classifyLaunchFollowFailure(error, { purchaseRecorded }) {
  const message = String(error?.message || "");
  if (/Workspace launch stopped at/.test(message)) return "launch_stopped";
  return purchaseRecorded ? "mutation_unaccounted" : "response_lost";
}

/*
 * Run one business qualification and return its terminal state with the receipt
 * that states it.
 *
 * The result is a value, not an exception. A qualification that cannot prove
 * what it did is an outcome an owner has to be able to read, not a crash that
 * looks like a pass to whatever called it. Only a malformed request or a broken
 * adapter throws, because those are programming errors rather than outcomes.
 */
export async function runWorkspaceQualification({ request, adapter }, { wait } = {}) {
  if (!request?.operation?.idempotencyKey || !request?.scope || !request?.candidate) {
    throw adapterError("qualification_request_invalid");
  }
  assertQualificationAdapter(adapter, request.provider);

  const startedAt = new Date().toISOString();
  const mutations = emptyMutations();
  const checks = emptyChecks();
  const accounting = { quoteUsdMicros: null, walletBeforeUsdMicros: null, walletAfterUsdMicros: null, chargeReferenceDigest: "" };
  const operationId = launchOperationId(request.tenant.accountId, request.operation.idempotencyKey);
  const operation = {
    idempotencyKey: request.operation.idempotencyKey,
    operationId,
    workspaceId: launchWorkspaceId(request.tenant.accountId, operationId),
    purchaseReceiptId: "",
    resumed: false,
  };

  let scope = null;
  let deploymentHealth = "unknown";
  let cleanup = { performed: false, restored: false };
  let terminal;

  // A run already outside the envelope it declared must not begin. The bounds
  // are the request's own admission, checked before anything is issued, so
  // refusing here costs nothing and cannot be reached after a purchase.
  if (request.scope.purchases < 1 || request.scope.charges < 1) {
    terminal = { state: "blocked", reason: "scope_bound_exhausted" };
  }

  try {
    if (!terminal) {
      scope = await adapter.openBusinessScope({ request });
      if (!scope || typeof scope.http?.json !== "function" || typeof scope.http?.request !== "function" || !scope.auth) {
        throw adapterError("provider_scope_unavailable");
      }
      const { http, auth } = scope;

      // Who am I acting as. Asked before anything is priced or bought, so a
      // transport holding someone else's session costs one read and no money.
      checks.account_bound = await bindAuthenticatedAccount(http, auth, request.tenant.accountId);

      accounting.walletBeforeUsdMicros = walletMicros((await http.json("/api/gateway/wallet", {}, auth)).payload);

      // The quote policy is the shared one, so it is asked rather than
      // reimplemented. Its two published refusals mean different things to an
      // owner and are kept apart here instead of collapsing into one failure.
      const pricing = (await http.json("/api/pricing/preview", {
        method: "POST", body: { resourceType: request.product.resourceType, packageId: request.product.packageId },
      }, auth)).payload;
      let quote;
      try {
        quote = priceWorkspaceLaunch({ pricing, walletUsdMicros: accounting.walletBeforeUsdMicros, packageId: request.product.packageId });
      } catch (error) {
        throw adapterError(/wallet cannot fund/i.test(String(error?.message || ""))
          ? "wallet_cannot_fund"
          : "quote_invalid");
      }
      accounting.quoteUsdMicros = quote;
      checks.quote_priced = true;

      // Ask before issuing. A redelivery finds its own order here and resumes
      // it; a first delivery finds nothing and issues exactly one command.
      let launch = await readAdmittedLaunch(http, operation.operationId, operation.workspaceId, auth);
      if (launch) {
        operation.resumed = true;
      } else {
        try {
          ({ launch } = await submitWorkspaceLaunch({
            http, auth, name: qualificationWorkspaceName(operation.operationId), packageId: request.product.packageId,
            autoRenew: request.product.autoRenew, idempotencyKey: operation.idempotencyKey,
            expectedOperationId: operation.operationId, expectedWorkspaceId: operation.workspaceId, wait,
          }));
        } catch (error) {
          // The command may have been accepted and its answer lost. That is not
          // a second purchase, it is the same order, and the only question left
          // is whether the platform holds it.
          const recovered = await readAdmittedLaunch(http, operation.operationId, operation.workspaceId, auth);
          if (!recovered) throw adapterError(classifyLaunchFollowFailure(error, { purchaseRecorded: false }));
          operation.resumed = true;
          launch = recovered;
        }
      }
      mutations.purchases = 1;
      checks.purchase_issued = true;

      // A resumed order that is not terminal yet is followed to its own end
      // under its own identity, rather than read as finished or reissued. The
      // window is the purchase core's own, so a stuck order stays bounded.
      if (launch?.status !== "succeeded" || launch?.phase !== "succeeded") {
        try {
          launch = await waitForLaunch(http, operation.operationId, auth, wait);
        } catch (error) {
          // The order was admitted, so this run already caused a purchase. A
          // published stopping status is a failure of the product; anything
          // else means the outcome is unknown with a mutation outstanding.
          throw adapterError(classifyLaunchFollowFailure(error, { purchaseRecorded: true }));
        }
      }
      // A terminal launch that carries no purchase receipt identity has not
      // delivered a receipt to bind anything to.
      if (!String(launch?.receiptId || "").trim()) throw adapterError("purchase_receipt_missing");
      if (launch.workspaceId !== operation.workspaceId) throw adapterError("account_binding_mismatch");
      operation.purchaseReceiptId = String(launch.receiptId);
      checks.purchase_receipt_bound = true;

      // What was billed, and what is running. The Ledger receipt is the charge
      // evidence; the Workspace and Runtime readbacks are what say there is
      // something there to use.
      const chargeReference = await readPurchaseChargeReceipt(http, auth, {
        receiptId: operation.purchaseReceiptId, workspaceId: operation.workspaceId,
        amountUsdMicros: quote, launch,
      });
      checks.charge_receipted = true;
      accounting.chargeReferenceDigest = digestOf(chargeReference);

      await readDeliveredWorkspace(http, auth, operation.workspaceId, launch.receiptId);
      checks.workspace_ready = true;
      checks.runtime_ready = true;

      accounting.walletAfterUsdMicros = walletMicros((await http.json("/api/gateway/wallet", {}, auth)).payload);
      const moved = BigInt(accounting.walletBeforeUsdMicros) - BigInt(accounting.walletAfterUsdMicros);
      if (moved === BigInt(quote)) {
        mutations.charges = 1;
        checks.wallet_confirmed = true;
      } else if (moved > BigInt(quote)) {
        // The owner readback proves more movement than this run's single
        // declared purchase accounts for. Something charged that this run
        // cannot place, which is an unaccounted mutation rather than a
        // mismatch between two numbers that are both known.
        throw adapterError("mutation_unaccounted");
      } else if (moved === 0n && operation.resumed && checks.purchase_receipt_bound) {
        // A redelivery of an order that already reached its terminal purchase
        // receipt bought nothing again, so the wallet correctly did not move.
        // That is the same settled purchase observed a second time, and the
        // zero is the evidence that replay cost nothing.
        mutations.charges = 0;
        checks.wallet_confirmed = true;
      } else {
        throw adapterError("wallet_readback_mismatch");
      }

      terminal = QUALIFICATION_BUSINESS_CHECKS.every((name) => checks[name])
        ? { state: "settled", reason: "none" }
        : { state: "failed", reason: "owner_evidence_incomplete" };
    }
  } catch (error) {
    terminal = terminalFailure(error);
  } finally {
    // The scope is always closed, including after a failure: the adapter that
    // opened something is the only party that can put it back.
    if (scope) {
      try {
        cleanup = { performed: true, restored: (await adapter.closeBusinessScope({ request }))?.restored === true };
      } catch {
        cleanup = { performed: true, restored: false };
      }
      try {
        const health = String(await adapter.readDeploymentHealth({ request }) || "");
        if (["healthy", "degraded", "unknown"].includes(health)) deploymentHealth = health;
      } catch {
        deploymentHealth = "unknown";
      }
    }
  }

  // Cleanup is part of the qualification, not a courtesy after it. A run that
  // changed something and cannot show it put the scope back is not a failure of
  // the product; it is an unfinished operation an owner has to look at.
  if (cleanup.performed && !cleanup.restored && terminal.state !== "recovery-required") {
    terminal = { state: "recovery-required", reason: "cleanup_unproven" };
  }

  const receipt = validateBusinessQualificationReceipt({
    schemaVersion: 1,
    kind: QUALIFICATION_BUSINESS_RECEIPT_KIND,
    scope: "workspace_purchase",
    uncovered: { ...QUALIFICATION_UNCOVERED_SCOPE },
    state: terminal.state,
    reason: terminal.reason,
    provider: request.provider,
    candidate: request.candidate,
    source: request.source,
    tenant: { accountDigest: request.tenant.accountDigest },
    product: request.product,
    authority: request.authority,
    operation,
    mutations: { declared: { ...request.scope }, observed: mutations },
    checks,
    accounting,
    cleanup,
    deploymentHealth,
    startedAt,
    completedAt: new Date().toISOString(),
  });
  return { state: receipt.state, reason: receipt.reason, receipt };
}

/*
 * One place that turns an error into the terminal state it means.
 *
 * Without it every transport hiccup would arrive as the same untyped failure,
 * and the difference between "we could not find out" and "it changed
 * something" would be lost at exactly the point where an owner needs it. An
 * error nobody classified is reported as undetermined rather than guessed at.
 */
function terminalFailure(error) {
  const code = String(error?.code || "");
  for (const [state, reasons] of Object.entries(QUALIFICATION_TERMINAL_REASONS)) {
    if (reasons.includes(code)) return { state, reason: code };
  }
  return { state: "unknown", reason: "owner_readback_unavailable" };
}

export { createQualificationRequest };