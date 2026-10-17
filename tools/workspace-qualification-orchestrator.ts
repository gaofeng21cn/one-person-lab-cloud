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
  DEPLOYMENT_HEALTH,
  QUALIFICATION_BUSINESS_CHECKS,
  QUALIFICATION_BUSINESS_RECEIPT_KIND,
  QUALIFICATION_BUSINESS_SCOPE,
  QUALIFICATION_MUTATION_BOUNDS,
  QUALIFICATION_TERMINAL_REASONS,
  QUALIFICATION_UNCOVERED_SCOPE,
  assertQualificationRequest,
  createQualificationRequest,
  hasExactKeys,
  qualificationMutationScope,
  qualificationProduct,
  qualificationTenantScope,
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

/*
 * The optional purchase seam.
 *
 * The default flow above owns the purchase itself because it is the shared
 * flow for every provider. A protected caller may instead hand over the merged
 * deployed buyer: the buyer performs the account-digest check and the purchase
 * through the shared core, and this orchestrator consumes only its bounded
 * result before continuing with the owner readbacks that produce delivery,
 * charge and receipt evidence. The seam is optional so Local-Docker and the
 * existing callers keep the current behavior.
 */
export type QualificationPurchaseCaller = Readonly<{
  http: {
    request: (...args: any[]) => Promise<any>;
    json: (...args: any[]) => Promise<any>;
  };
  auth: unknown;
  wait?: (milliseconds: number) => Promise<void>;
}>;

export type QualificationPurchaseResult = Readonly<{
  idempotencyKey: string;
  operationId: string;
  workspaceId: string;
  purchaseReceiptId: string;
  resumed: boolean;
  amountUsdMicros: string;
  purchases: number;
  candidateReceiptDigest: string;
  product: Readonly<{ resourceType: string; packageId: string; autoRenew: boolean }>;
}>;

export type QualificationPurchase = Readonly<{
  run: (caller: QualificationPurchaseCaller) => Promise<QualificationPurchaseResult>;
}>;

function assertQualificationPurchase(purchase) {
  if (purchase === undefined) return null;
  if (!purchase || typeof purchase !== "object" || typeof purchase.run !== "function") {
    throw adapterError("qualification_purchase_invalid");
  }
  return purchase;
}

function purchasedQualification(value, request, declaredProduct) {
  const product = value?.product;
  if (!value || typeof value !== "object" || Array.isArray(value) ||
      value.candidateReceiptDigest !== request.candidate.receiptDigest ||
      !hasExactKeys(product, ["resourceType", "packageId", "autoRenew"]) ||
      product.resourceType !== declaredProduct.resourceType ||
      product.packageId !== declaredProduct.packageId ||
      product.autoRenew !== declaredProduct.autoRenew ||
      typeof value.idempotencyKey !== "string" || !value.idempotencyKey.trim() ||
      typeof value.operationId !== "string" || !value.operationId.trim() ||
      typeof value.workspaceId !== "string" || !value.workspaceId.trim() ||
      typeof value.purchaseReceiptId !== "string" || !value.purchaseReceiptId.trim() ||
      typeof value.resumed !== "boolean" ||
      !/^[1-9][0-9]*$/.test(String(value.amountUsdMicros || "")) ||
      !Number.isSafeInteger(value.purchases) || ![0, 1].includes(value.purchases) ||
      value.resumed !== (value.purchases === 0) ||
      value.operationId !== launchOperationId(request.tenant.accountId, value.idempotencyKey) ||
      value.workspaceId !== launchWorkspaceId(request.tenant.accountId, value.operationId)) {
    throw adapterError("purchase_receipt_missing");
  }
  return value;
}

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
 * The Workspace, its committed application installation and its Runtime.
 *
 * A purchase receipt says what was billed, not what runs. The purchase commits
 * the default application installation beside the resource Launch; the platform
 * then progresses that installation asynchronously, so a read taken the moment
 * the Launch reports success would see a paid delivery that is not yet usable.
 * This module waits for the owner to report the current application ready,
 * resuming a stalled installation exactly once through the customer's own
 * continuation command, and only then reads the delivery.
 *
 * The Workspace list is paginated: a first page is not the shape of an account
 * that has bought more than one Workspace, and the Workspace this run bought may
 * no longer be on it.
 */
const WORKSPACE_PAGE_SIZE = 20;
const WORKSPACE_PAGE_LIMIT = 50;
const APPLICATION_INSTALL_ATTEMPTS = 300;
const APPLICATION_INSTALL_INTERVAL_MS = 1_000;
const defaultWait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));

async function readWorkspaceOwnerItem(http, auth, workspaceId) {
  for (let page = 1; page <= WORKSPACE_PAGE_LIMIT; page += 1) {
    const payload = ownerData((await http.json(
      `/api/workspaces?page=${page}&pageSize=${WORKSPACE_PAGE_SIZE}`, {}, auth)).payload, "control-plane");
    const items = Array.isArray(payload?.items) ? payload.items : [];
    const workspace = items.find((candidate) => candidate?.id === workspaceId);
    if (workspace) return workspace;
    const total = Number(payload?.total);
    if (items.length < WORKSPACE_PAGE_SIZE) return null;
    if (Number.isFinite(total) && total > 0 && page * WORKSPACE_PAGE_SIZE >= total) return null;
  }
  return null;
}

async function waitForWorkspaceApplicationInstallation(http, auth, { workspaceId, operationId, wait }) {
  const pause = wait || defaultWait;
  let resumed = false;
  for (let attempt = 0; attempt < APPLICATION_INSTALL_ATTEMPTS; attempt += 1) {
    const workspace = await readWorkspaceOwnerItem(http, auth, workspaceId);
    if (!workspace) throw adapterError("workspace_not_ready");
    const installation = workspace.applicationInstallation || null;
    if (workspace.currentApplication?.status === "ready") return;
    // A stalled credential preparation is resumed exactly once through the
    // customer's own continuation command. The projection has to name the same
    // Workspace and the same installation this run observed; a continuation
    // for something else is not this delivery's.
    if (installation?.canResume === true && !resumed) {
      const projection = (await http.json(
        `/api/workspaces/${encodeURIComponent(workspaceId)}/application-installation/resume`,
        { method: "POST", headers: { "idempotency-key": `application-resume:${operationId}` }, body: {} },
        auth, [202],
      )).payload;
      if (projection?.workspaceId !== workspaceId ||
          (projection.applicationInstallation &&
            (projection.applicationInstallation.operationId !== installation.operationId ||
              projection.applicationInstallation.applicationId !== installation.applicationId ||
              projection.applicationInstallation.revision !== installation.revision))) {
        throw adapterError("workspace_not_ready");
      }
      resumed = true;
      continue;
    }
    // An installation the owner parks for review, and will not hand back through
    // the continuation above, is a product failure rather than a slow delivery.
    if (installation?.status === "manual_review") throw adapterError("application_installation_manual_review");
    await pause(APPLICATION_INSTALL_INTERVAL_MS);
  }
  throw adapterError("workspace_not_ready");
}

async function readDeliveredWorkspace(http, auth, workspaceId) {
  const workspace = await readWorkspaceOwnerItem(http, auth, workspaceId);
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
  // The platform derives `ready` from the application record alone, so it stays
  // true while a component is unhealthy. The component list is the only place
  // that says anything is actually serving, and an empty list says nothing was
  // observed at all.
  const components = Array.isArray(runtime?.checks) ? runtime.checks : [];
  if (components.length === 0 || components.some((component) => component?.ok !== true)) {
    throw adapterError("runtime_not_ready");
  }
  return { entryUrl: String(application.entryUrl), applicationOperationId: String(application.operationId), componentsObserved: components.length };
}

/*
 * The Ledger receipt for this exact purchase.
 *
 * This, not the wallet balance, is the evidence that a charge happened. The
 * receipt is the owner's record and it names the operation that caused it, the
 * Workspace, the amount, and the resources the platform says the purchase
 * delivered; the launch it is read against has to agree on the Workspace and on
 * each of those resources. The wallet is then checked against the receipt rather
 * than used to infer it.
 *
 * The operation binding is what keeps the receipt from being any charge that
 * happens to share a Workspace and an amount. The Control Plane records the
 * purchase receipt with `requestId` equal to the launch operation identity and
 * projects that value into `operationId` (routes_billing.go), while
 * `<launchOperationId>:purchase-receipt` is the Ledger idempotency key that
 * creates the receipt and is not part of the projection. A receipt whose
 * operation id names another launch is another purchase.
 */
async function readPurchaseChargeReceipt(http, auth, { receiptId, workspaceId, amountUsdMicros, launch, operationId }) {
  const receipt = ownerData((await http.json(
    `/api/billing/receipts/${encodeURIComponent(receiptId)}`, {}, auth)).payload, "ledger");
  if (receipt?.receiptId !== receiptId || receipt?.workspaceId !== workspaceId ||
      receipt?.operationId !== operationId ||
      receipt?.type !== "billing.workspace_purchased.v1" || receipt?.status !== "completed" ||
      String(receipt?.totalUsdMicros || "") !== String(amountUsdMicros) ||
      !String(receipt?.chargeReference || "").trim()) {
    throw adapterError("charge_receipt_mismatch");
  }
  // A resource-only purchase delivers resources and nothing else. The launch has
  // to name every resource the receipt says was delivered, and the receipt has to
  // name exactly those three: a Runtime or a Workspace Key inside this receipt
  // would mean the platform fulfilled more than the operation declared, and the
  // run would not know what it paid for.
  const delivered = ["computeAllocationId", "storageId", "attachmentId"];
  if (!hasExactKeys(receipt?.fulfillment, delivered) ||
      delivered.some((name) => {
        const expected = String(launch?.[name] || "").trim();
        return !expected || String(receipt.fulfillment[name] || "") !== expected;
      })) {
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
 * When the platform names a Workspace and it is not this run's, the answer is
 * refused here rather than followed. When it names none, nothing is refused
 * here; the terminal launch is bound to the declared Workspace before the run
 * reports on it, so an answer that never names one cannot carry the run past
 * that point either.
 */
async function readAdmittedLaunch(http, operationId, workspaceId, auth) {
  const result = await http.request(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth);
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200 || !result?.payload) throw adapterError("owner_readback_unavailable");
  // The answer has to be the order that was asked for. A launch that comes back
  // under another operation identity, or naming another Workspace, is not this
  // run's order whatever else it says, and following it would attribute another
  // launch's purchase to this request.
  if (result.payload.operationId !== operationId) throw adapterError("launch_identity_mismatch");
  if (result.payload.workspaceId !== undefined && result.payload.workspaceId !== workspaceId) {
    throw adapterError("launch_identity_mismatch");
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
export async function runWorkspaceQualification({ request, adapter }, { wait, purchase } = {}) {
  // The request is the admission. Only an object this contract built is run: a
  // hand-assembled or copied request has the same fields but no brand, and no
  // check below could tell it from one.
  assertQualificationRequest(request);
  // The tenant digest is what the receipt publishes and the operation identity
  // is derived from. Re-derive it with the contract's own function so the fence,
  // the published digest and the operation identity cannot name different
  // accounts.
  if (qualificationTenantScope(request.tenant.accountId, request.candidate.receiptDigest).accountDigest !==
      request.tenant.accountDigest) {
    throw adapterError("qualification_request_invalid");
  }
  // Normalise the declared envelope and product before anything is priced or
  // bought. A scope that is missing a bound is refused here, not after a
  // purchase the run could not account for.
  const declaredScope = qualificationMutationScope(request.scope);
  const declaredProduct = qualificationProduct(request.product);
  assertQualificationAdapter(adapter, request.provider);
  const purchaseHook = assertQualificationPurchase(purchase);

  const startedAt = new Date().toISOString();
  const mutations = emptyMutations();
  const checks = emptyChecks();
  const accounting = { quoteUsdMicros: null, walletBeforeUsdMicros: null, walletAfterUsdMicros: null, chargeReferenceDigest: "" };
  const delivery = { workspaceEntryDigest: "", applicationOperationDigest: "", componentsObserved: 0 };
  const operationId = launchOperationId(request.tenant.accountId, request.operation.idempotencyKey);
  const operation = {
    idempotencyKey: request.operation.idempotencyKey,
    operationId,
    workspaceId: launchWorkspaceId(request.tenant.accountId, operationId),
    purchaseReceiptId: "",
    resumed: false,
  };

  let scope = null;
  let scopeAttempted = false;
  let deploymentHealth = "unknown";
  let cleanup = { performed: false, restored: false };
  let terminal;

  // A run already outside the envelope it declared must not begin. The bounds
  // are the request's own admission, checked before anything is issued, so
  // refusing here costs nothing and cannot be reached after a purchase.
  if (declaredScope.purchases < 1 || declaredScope.charges < 1) {
    terminal = { state: "blocked", reason: "scope_bound_exhausted" };
  }

  try {
    if (!terminal) {
      // Recorded before the attempt: an adapter that throws may still have
      // opened something, so cleanup has to run even when no scope came back.
      scopeAttempted = true;
      try {
        scope = await adapter.openBusinessScope({ request });
      } catch {
        throw adapterError("provider_scope_unavailable");
      }
      if (!scope || typeof scope.http?.json !== "function" || typeof scope.http?.request !== "function" || !scope.auth) {
        throw adapterError("provider_scope_unavailable");
      }
      const { http, auth } = scope;

      // Who am I acting as. Asked before anything is priced or bought, so a
      // transport holding someone else's session costs one read and no money.
      checks.account_bound = await bindAuthenticatedAccount(http, auth, request.tenant.accountId);

      accounting.walletBeforeUsdMicros = walletMicros((await http.json("/api/gateway/wallet", {}, auth)).payload);

      let quote = "";
      let launch: any = null;

      if (purchaseHook) {
        // The protected caller delegates the purchase to the deployed buyer.
        // That buyer owns the source-envelope account/wallet reads, the
        // declared-account digest check and the shared purchase core; this
        // orchestrator consumes only its bounded result and then reads the
        // order and delivery through the same owner APIs as before. The buyer
        // has already decided whether this is a first purchase or a replay, so
        // the result carries the operation identities the rest of the run must
        // use.
        const purchased = purchasedQualification(
          await purchaseHook.run({ http, auth, wait }),
          request,
          declaredProduct,
        );
        operation.idempotencyKey = purchased.idempotencyKey;
        operation.operationId = purchased.operationId;
        operation.workspaceId = purchased.workspaceId;
        operation.purchaseReceiptId = purchased.purchaseReceiptId;
        operation.resumed = purchased.resumed;
        accounting.quoteUsdMicros = purchased.amountUsdMicros;
        mutations.purchases = purchased.purchases;
        quote = purchased.amountUsdMicros;

        // The buyer waited for its own order. Read the same order under the
        // identity it returned so the charge and delivery evidence still comes
        // from the owner, not from the buyer's receipt.
        launch = await readAdmittedLaunch(http, operation.operationId, operation.workspaceId, auth);
        if (!launch) throw adapterError("purchase_receipt_missing");
        if (launch.status !== "succeeded" || launch.phase !== "succeeded") {
          try {
            launch = await waitForLaunch(http, operation.operationId, auth, wait);
          } catch (error) {
            throw adapterError(classifyLaunchFollowFailure(error, { purchaseRecorded: true }));
          }
        }
        if (launch.workspaceId !== operation.workspaceId) throw adapterError("launch_identity_mismatch");
        if (launch.provisioningMode !== "resource_only") throw adapterError("launch_identity_mismatch");
        if (!String(launch.receiptId || "").trim()) throw adapterError("purchase_receipt_missing");
        if (String(launch.receiptId) !== operation.purchaseReceiptId) throw adapterError("launch_identity_mismatch");

        checks.quote_priced = true;
        checks.purchase_issued = true;
        checks.purchase_receipt_bound = true;
      } else {
        // The quote policy is the shared one, so it is asked rather than
        // reimplemented. Its two published refusals mean different things to an
        // owner and are kept apart here instead of collapsing into one failure.
        const pricing = (await http.json("/api/pricing/preview", {
          method: "POST", body: { resourceType: declaredProduct.resourceType, packageId: declaredProduct.packageId },
        }, auth)).payload;
        try {
          quote = priceWorkspaceLaunch({ pricing, walletUsdMicros: accounting.walletBeforeUsdMicros, packageId: declaredProduct.packageId });
        } catch (error) {
          throw adapterError(/wallet cannot fund/i.test(String(error?.message || ""))
            ? "wallet_cannot_fund"
            : "quote_invalid");
        }
        accounting.quoteUsdMicros = quote;
        checks.quote_priced = true;

        // Ask before issuing. A redelivery finds its own order here and resumes
        // it; a first delivery finds nothing and issues exactly one command.
        launch = await readAdmittedLaunch(http, operation.operationId, operation.workspaceId, auth);
        if (launch) {
          operation.resumed = true;
        } else {
          try {
            ({ launch } = await submitWorkspaceLaunch({
              http, auth, name: qualificationWorkspaceName(operation.operationId), packageId: declaredProduct.packageId,
              autoRenew: declaredProduct.autoRenew, idempotencyKey: operation.idempotencyKey,
              expectedOperationId: operation.operationId, expectedWorkspaceId: operation.workspaceId, wait,
            }));
          } catch (error) {
            // The command may have been accepted and its answer lost. That is not
            // a second purchase, it is the same order, and the only question left
            // is whether the platform holds it. It is not a resume either: this
            // delivery issued the command, so whatever it caused is this run's to
            // account for.
            const recovered = await readAdmittedLaunch(http, operation.operationId, operation.workspaceId, auth);
            if (!recovered) throw adapterError(classifyLaunchFollowFailure(error, { purchaseRecorded: false }));
            launch = recovered;
          }
        }
        mutations.purchases = operation.resumed ? 0 : 1;
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
        // The terminal launch has to name the Workspace this run declared. A
        // launch that ended against another Workspace is another order, not this
        // run's delivery.
        if (launch.workspaceId !== operation.workspaceId) throw adapterError("launch_identity_mismatch");
        // The Launch must be the resource-only purchase this flow accounts for.
        // Anything else fulfils a different shape, so the exactly-three-resource
        // receipt rule below would be describing an operation that was not run.
        if (launch.provisioningMode !== "resource_only") throw adapterError("launch_identity_mismatch");
        operation.purchaseReceiptId = String(launch.receiptId);
        checks.purchase_receipt_bound = true;
      }

      // What was billed, and what is running. The Ledger receipt is the charge
      // evidence; the Workspace and Runtime readbacks are what say there is
      // something there to use.
      const chargeReference = await readPurchaseChargeReceipt(http, auth, {
        receiptId: operation.purchaseReceiptId, workspaceId: operation.workspaceId,
        amountUsdMicros: quote, launch, operationId: operation.operationId,
      });
      checks.charge_receipted = true;
      accounting.chargeReferenceDigest = digestOf(chargeReference);

      // The purchase commits the default application installation beside the
      // resource Launch, and the platform progresses it after the Launch reports
      // success. Reading the Workspace before it finishes would report a paid
      // delivery as a product failure, so wait for the owner's own readiness.
      await waitForWorkspaceApplicationInstallation(http, auth, {
        workspaceId: operation.workspaceId, operationId: operation.operationId, wait,
      });

      const delivered = await readDeliveredWorkspace(http, auth, operation.workspaceId);
      // Digests, not the entry URL or the application operation: the receipt
      // records that these readbacks happened without carrying the identity of
      // where the product is served.
      delivery.workspaceEntryDigest = digestOf(delivered.entryUrl);
      delivery.applicationOperationDigest = digestOf(delivered.applicationOperationId);
      delivery.componentsObserved = delivered.componentsObserved;
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
      } else if (moved === 0n && operation.resumed) {
        // A redelivery of an order that already reached its terminal purchase
        // receipt bought nothing again, so the wallet correctly did not move.
        // That is the same settled purchase observed a second time, and the
        // zero is the evidence that replay cost nothing.
        mutations.charges = 0;
        checks.wallet_confirmed = true;
      } else {
        throw adapterError("wallet_readback_mismatch");
      }

      // Every read this flow needs has now succeeded, so the terminal state is
      // settled and the contract decides whether the evidence supports it. A
      // failed state has to name a real check or read failure; there is no
      // "evidence was incomplete" failure left to invent here.
      terminal = { state: "settled", reason: "none" };
    }
  } catch (error) {
    terminal = terminalFailure(error);
  } finally {
    // Deployment health is read before the scope is closed: a closed scope cannot
    // answer for it, and a health read that fails must not prevent cleanup.
    //
    // The scope is always closed, including after a failure: the adapter that
    // opened something is the only party that can put it back. An attempt that
    // threw may still have opened something, so the attempt itself is what
    // triggers cleanup, not a returned scope object.
    if (scope || scopeAttempted) {
      try {
        const health = String(await adapter.readDeploymentHealth({ request }) || "");
        if (DEPLOYMENT_HEALTH.includes(health)) deploymentHealth = health;
      } catch {
        deploymentHealth = "unknown";
      }
      try {
        cleanup = { performed: true, restored: (await adapter.closeBusinessScope({ request }))?.restored === true };
      } catch {
        cleanup = { performed: true, restored: false };
      }
    }
  }

  // Cleanup is part of the qualification, not a courtesy after it. A run that
  // changed something and cannot show it put the scope back is not a failure of
  // the product; it is an unfinished operation an owner has to look at.
  if (cleanup.performed && !cleanup.restored && terminal.state !== "recovery-required") {
    terminal = { state: "recovery-required", reason: "cleanup_unproven" };
  }

  // A clock that went backwards must not invalidate evidence about a purchase
  // that already happened. The contract requires a completed instant at or after
  // the start; the run cannot have finished before it began, so a backdated
  // reading is clamped rather than allowed to throw after the money moved.
  let completedAt = new Date().toISOString();
  if (Date.parse(completedAt) < Date.parse(startedAt)) completedAt = startedAt;

  const evidence = {
    schemaVersion: 1,
    kind: QUALIFICATION_BUSINESS_RECEIPT_KIND,
    scope: QUALIFICATION_BUSINESS_SCOPE,
    uncovered: { ...QUALIFICATION_UNCOVERED_SCOPE },
    provider: request.provider,
    candidate: request.candidate,
    source: request.source,
    tenant: { accountDigest: request.tenant.accountDigest },
    product: declaredProduct,
    authority: request.authority,
    operation,
    mutations: { declared: { ...declaredScope }, observed: mutations },
    checks,
    accounting,
    delivery,
    cleanup,
    deploymentHealth,
    startedAt,
    completedAt,
  };
  let receipt;
  try {
    receipt = validateBusinessQualificationReceipt({ ...evidence, state: terminal.state, reason: terminal.reason });
  } catch (error) {
    if (error?.code !== "qualification_business_receipt_invalid" || terminal.state !== "settled") throw error;
    // The last read succeeded, so a purchase may really have happened, but the
    // contract will not read this evidence as settled. Returning a settled
    // receipt it rejects, or throwing after a real purchase, would both lose
    // that fact: the run changed something it cannot account for. The same
    // evidence is therefore reported under the recovery state. The fallback is
    // still validated, so malformed evidence throws here, which is a
    // programming error rather than an outcome.
    receipt = validateBusinessQualificationReceipt({
      ...evidence, state: "recovery-required", reason: "mutation_unaccounted",
    });
  }
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
    // `settled` is not something a thrown error can mean. Skipping it keeps an
    // unclassified error out of the passing state even when its code happens to
    // be the passing state's own reason name.
    if (state === "settled") continue;
    if (reasons.includes(code)) return { state, reason: code };
  }
  return { state: "unknown", reason: "owner_readback_unavailable" };
}

export { createQualificationRequest };