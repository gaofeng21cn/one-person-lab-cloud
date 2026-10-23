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
 * Replay is handled by the owner's own idempotency record rather than by stored
 * state. The declared idempotency identity is a function of the Candidate, the
 * tenant, the product and the provider, so a redelivery of the same
 * qualification addresses the same order the first delivery created: the owner
 * prices the quote again under the same identity and admits the order again
 * under the same identity, and it replays the original order rather than
 * admitting a second one. The owner mints the operation and Workspace
 * identities, so this module never derives them; it announces the request that
 * is buying and reads the owner's own answer. A response lost after the owner
 * accepted a command is the same case seen once more and is answered the same
 * way. No path here issues a second purchase, and so none creates a second
 * Workspace, charge, refund or managed Key.
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
  validateBusinessQualificationReceipt,
} from "./workspace-qualification-contract.ts";
import { createHash, randomUUID } from "node:crypto";

import {
  announceRequest, findDeclaredCatalogEntry, listOwnerCatalogPages, normalizedAccountEmail,
  priceWorkspaceQuote, quoteWorkspaceOrder, stableID, submitWorkspaceOrder,
  workspaceDeclaration, WORKSPACE_OPERATION_ROUTE, WORKSPACE_ORDER_ROUTE,
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
 * flow for every provider. A protected caller may instead hand over a buyer of
 * its own: the buyer performs the account-digest check and the purchase
 * through the shared owner-route core, and this orchestrator consumes only its
 * bounded result before continuing with the owner readbacks that produce
 * delivery, charge and receipt evidence. The seam is optional so Local-Docker
 * and the existing callers keep the current behavior.
 *
 * The result names the identity space the order lives in. An order bought on
 * the Workspace owner route is read back from the owner, and a retained Control
 * Plane operation is still read and recovered through its own owner, so the
 * readback dispatches on the recorded space rather than on a global URL.
 */
export type QualificationPurchaseCaller = Readonly<{
  http: {
    request: (...args: any[]) => Promise<any>;
    json: (...args: any[]) => Promise<any>;
  };
  auth: unknown;
  wait?: (milliseconds: number) => Promise<void>;
}>;

export const QUALIFICATION_IDENTITY_SPACES = Object.freeze(["workspace_owner", "control_plane"]);

export type QualificationPurchaseResult = Readonly<{
  idempotencyKey: string;
  operationId: string;
  workspaceId: string;
  purchaseReceiptId: string;
  resumed: boolean;
  amountUsdMicros: string;
  purchases: number;
  candidateReceiptDigest: string;
  identitySpace: string;
  product: Readonly<{
    resourceType: string;
    applicationSelection: Readonly<{ kind: string; runtimeVersionId: string }>;
    computePlanId: string;
    storagePlanId: string;
    renewalMode: string;
  }>;
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
  // The identities are the ones the owner minted, so there is nothing here to
  // re-derive and compare against: the owner readback below is what proves
  // that this operation is this order's.
  //
  // The purchase names what it bought the way the owner route states it: a
  // Workspace, the application source and plan pair the owner priced beside
  // it, and the renewal mode it admitted. The owner route has no package, so
  // the declared package cannot be compared here; the renewal mode is what
  // says whether this run bought the period it declared.
  if (!value || typeof value !== "object" || Array.isArray(value) ||
      value.candidateReceiptDigest !== request.candidate.receiptDigest ||
      !hasExactKeys(product, ["resourceType", "applicationSelection", "computePlanId", "storagePlanId", "renewalMode"]) ||
      !hasExactKeys(product.applicationSelection, ["kind", "runtimeVersionId"]) ||
      product.resourceType !== declaredProduct.resourceType ||
      product.applicationSelection.kind !== declaredProduct.applicationSelection.kind ||
      product.applicationSelection.runtimeVersionId !== declaredProduct.applicationSelection.runtimeVersionId ||
      product.computePlanId !== declaredProduct.computePlanId ||
      product.storagePlanId !== declaredProduct.storagePlanId ||
      product.renewalMode !== (declaredProduct.autoRenew === true ? "automatic" : "manual") ||
      typeof value.idempotencyKey !== "string" || !value.idempotencyKey.trim() ||
      typeof value.operationId !== "string" || !value.operationId.trim() ||
      typeof value.workspaceId !== "string" || !value.workspaceId.trim() ||
      typeof value.purchaseReceiptId !== "string" ||
      typeof value.resumed !== "boolean" ||
      !QUALIFICATION_IDENTITY_SPACES.includes(String(value.identitySpace || "")) ||
      !/^[1-9][0-9]*$/.test(String(value.amountUsdMicros || "")) ||
      !Number.isSafeInteger(value.purchases) || ![0, 1].includes(value.purchases) ||
      value.resumed !== (value.purchases === 0)) {
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

/*
 * The wallet, read from the Gateway owner under the owner session.
 *
 * The owner route reads the balance under the session's own authorization, so
 * it is the balance the owner would spend. The retained Control Plane
 * projection answers for a different authority and a different session, and a
 * qualification that accounted its charges against it would be accounting
 * against a balance that is not the one the owner spends.
 */
function walletMicros(payload) {
  const readback = payload || {};
  if (readback.source !== "gateway" || readback.status !== "available" || readback.currency !== "USD") {
    throw adapterError("owner_readback_unavailable");
  }
  const value = String(readback.balanceUSDMicros || "");
  if (!/^(?:0|[1-9][0-9]*)$/.test(value)) throw adapterError("owner_readback_unavailable");
  return value;
}

/*
 * The Workspace owner route.
 *
 * A new purchase is priced and bought by the Workspace owner: the owner freezes
 * exactly one application source beside the compute and storage plan in a
 * deploy quote, admits the order against that quote, and mints the operation
 * and Workspace identities itself. A buyer that derived either identity would
 * be reproducing an interop contract the owner no longer has, and it would have
 * to refuse a correct answer whenever the owner changed how it mints one.
 *
 * The catalog is read to confirm the declared selection is one the owner still
 * offers: an approved Runtime Release for the default OPL App, and an available
 * compute and storage plan. The catalog is not searched for a default, because a
 * buyer that took the first approved release and the first available plans
 * would buy something nobody chose.
 */
const RUNTIME_VERSION_ROUTE = "/api/v2/catalog/runtime-versions";
const COMPUTE_PLAN_ROUTE = "/api/v2/catalog/compute-plans";
const STORAGE_PLAN_ROUTE = "/api/v2/catalog/storage-plans";
// The owner records the request that admitted an order on the operation it
// mints, so the buyer announces its own request identity on the command.
const ORDER_REQUEST_ID_HEADER = "x-opl-request-id";

async function readDeclaredCatalogEntry(http, auth, route, id, available) {
  let items = null;
  try {
    items = await listOwnerCatalogPages(http, auth, route);
  } catch {
    throw adapterError("owner_readback_unavailable");
  }
  const entry = findDeclaredCatalogEntry(items, id);
  // A catalog that does not offer the declared plan is not a price this run may
  // act on.
  if (!entry || !available(entry)) throw adapterError("quote_invalid");
  return entry;
}

export type OwnerPurchaseDeclaration = Readonly<{
  applicationSelection: Readonly<{ kind: string; runtimeVersionId: string }>;
  computePlanId: string;
  storagePlanId: string;
  modelSelections: ReadonlyArray<Readonly<{ slot: string; modelId: string }>>;
  periodMonths: number;
}>;

async function declareOwnerPurchase(http, auth, declared): Promise<OwnerPurchaseDeclaration> {
  const runtimeVersionId = String(declared?.applicationSelection?.runtimeVersionId || "");
  const computePlanId = String(declared?.computePlanId || "");
  const storagePlanId = String(declared?.storagePlanId || "");
  await readDeclaredCatalogEntry(http, auth, RUNTIME_VERSION_ROUTE, runtimeVersionId, (item) => item?.status === "approved");
  await readDeclaredCatalogEntry(http, auth, COMPUTE_PLAN_ROUTE, computePlanId, (item) => item?.availability === "available");
  await readDeclaredCatalogEntry(http, auth, STORAGE_PLAN_ROUTE, storagePlanId, (item) => item?.availability === "available");
  try {
    return workspaceDeclaration({
      applicationSelection: { kind: "opl_app", runtimeVersionId },
      computePlanId,
      storagePlanId,
      modelSelections: [],
      periodMonths: 1,
    }) as OwnerPurchaseDeclaration;
  } catch {
    throw adapterError("quote_invalid");
  }
}

/*
 * The Workspace name this run buys under.
 *
 * The owner takes the name from the command body, and the command is idempotent
 * on that body, so the name has to be a function of the declared purchase
 * rather than of the run: a redelivery has to name the same Workspace or the
 * owner refuses the replay as a different order. It is not an identity the
 * platform minted; the owner mints those.
 */
function qualificationPurchaseName(idempotencyKey) {
  return `Qualification ${stableID("qualification-workspace", idempotencyKey).slice(0, 18)}`;
}

/*
 * The request that is buying.
 *
 * The owner records the request that admitted an order on the operation it
 * mints, so a redelivery can tell its own order from the one it is replaying:
 * the operation names the request that created it. The buyer therefore
 * announces its own request identity on the order command, and a replayed order
 * answers with the request that created it rather than with this one. Nothing
 * here derives an identity the platform owns.
 */

/*
 * The owner's own answer about the order it admitted.
 *
 * The answer has to be the order that was asked for. An operation that comes
 * back under another identity, or naming another Workspace, is not this run's
 * order whatever else it says, and following it would attribute another
 * purchase to this request.
 */
async function readOwnerWorkspaceOperation(http, operationId, workspaceId, auth) {
  let result = null;
  try {
    result = await http.request(`${WORKSPACE_OPERATION_ROUTE}/${encodeURIComponent(operationId)}`, {}, auth);
  } catch {
    throw adapterError("owner_readback_unavailable");
  }
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200 || !result?.payload) throw adapterError("owner_readback_unavailable");
  const operation = result.payload;
  if (operation.operationId !== operationId || operation.owner !== "workspace" ||
      operation.kind !== "create_workspace" || operation.resourceId !== workspaceId) {
    throw adapterError("launch_identity_mismatch");
  }
  return operation;
}

/*
 * A polling failure is not one thing.
 *
 * The purchase core publishes two different refusals and they mean opposite
 * things to an owner: an order that reached a status the owner itself
 * published as stopping has failed, while an order that was accepted and then
 * went quiet has not. The second is the case where this run already caused a
 * mutation and cannot say what became of it, so it must not be reported as a
 * failure of the product, and must not be reported as success either.
 */
function classifyOwnerOrderFailure(error, { purchaseRecorded }) {
  const message = String(error?.message || "");
  if (/Workspace order stopped at/.test(message)) return "launch_stopped";
  // The owner published an operation that is not the order this command
  // admitted. The evidence does not bind to this purchase, so the run failed
  // rather than ending with an outcome nobody can account for.
  if (/workspace order readback is not this order/i.test(message)) return "launch_identity_mismatch";
  if (/Workspace order did not reach succeeded/.test(message)) return "mutation_unaccounted";
  return purchaseRecorded ? "mutation_unaccounted" : "response_lost";
}

export type OwnerPurchase = Readonly<{
  idempotencyKey: string;
  quoteId: string;
  operationId: string;
  workspaceId: string;
  amountUsdMicros: string;
  resumed: boolean;
  purchases: number;
  identitySpace: string;
  declaration: OwnerPurchaseDeclaration | null;
  status: string;
  stage: string;
}>;

/*
 * Buy one Workspace through the Workspace owner.
 *
 * The quote is priced by the owner under the declared idempotency identity, so
 * a redelivery replays the same quote, and the order is admitted against that
 * quote under the same identity, so a redelivery replays the same order
 * instead of creating a second purchase. The owner mints the operation and the
 * Workspace identity, and the evidence is bound to them.
 */
export async function buyWorkspaceOnOwnerRoute({
  http,
  auth,
  idempotencyKey,
  walletUsdMicros,
  wait,
  requestId,
  declared,
}: {
  http: { request: (...args: any[]) => Promise<any>; json: (...args: any[]) => Promise<any> };
  auth: unknown;
  idempotencyKey: string;
  walletUsdMicros: string;
  wait?: (milliseconds: number) => Promise<void>;
  requestId?: string;
  declared?: { applicationSelection?: { runtimeVersionId?: string }; computePlanId?: string; storagePlanId?: string };
}): Promise<OwnerPurchase> {
  const declaration = await declareOwnerPurchase(http, auth, declared);
  let quote = null;
  try {
    quote = await quoteWorkspaceOrder({ http, auth, declaration, idempotencyKey });
  } catch (error) {
    throw adapterError(/cannot fund/i.test(String(error?.message || "")) ? "wallet_cannot_fund" : "quote_invalid");
  }
  let amountUsdMicros = "";
  try {
    amountUsdMicros = priceWorkspaceQuote({ quote, walletUsdMicros, declaration });
  } catch (error) {
    throw adapterError(/cannot fund/i.test(String(error?.message || "")) ? "wallet_cannot_fund" : "quote_invalid");
  }
  // The request identity is announced once and used for every attempt of this
  // order, so a lost answer is recovered by the same request rather than by a
  // second one.
  const request = String(requestId || randomUUID());
  const name = qualificationPurchaseName(idempotencyKey);
  const transport = announceRequest(http, request);
  let order = null;
  try {
    order = await submitWorkspaceOrder({
      http: transport, auth, name, quote, idempotencyKey, wait,
    });
  } catch (error) {
    // The command may have been accepted and its answer lost. That is not a
    // second purchase, it is the same order, and the only question left is
    // whether the owner holds it. The same command under the same idempotency
    // identity replays that order rather than admitting another one.
    try {
      order = await submitWorkspaceOrder({
        http: transport, auth, name, quote, idempotencyKey, wait,
      });
    } catch {
      const failure = adapterError(classifyOwnerOrderFailure(error, { purchaseRecorded: transport.admitted.order }));
      // The order may have been admitted even though the run could not follow
      // it to its end. The mutation is this run's to report.
      (failure as { purchases?: number }).purchases = transport.admitted.mine ? 1 : 0;
      throw failure;
    }
  }
  const operation = order.operation || {};
  if (operation.owner !== "workspace" || operation.kind !== "create_workspace" ||
      operation.resourceId !== order.workspaceId) {
    throw adapterError("launch_identity_mismatch");
  }
  // The order is read back under the identity the owner minted, so the
  // evidence names the owner's own answer rather than the command's.
  const readback = await readOwnerWorkspaceOperation(http, order.operationId, order.workspaceId, auth);
  if (!readback) throw adapterError("purchase_receipt_missing");
  // The operation names the request that created it. An operation that names
  // another request was admitted by an earlier delivery, so this delivery
  // resumed it and bought nothing again.
  const resumed = String(order.order?.requestId || "") !== request;
  return {
    idempotencyKey,
    quoteId: String(quote.id || ""),
    operationId: order.operationId,
    workspaceId: order.workspaceId,
    amountUsdMicros,
    resumed,
    purchases: resumed ? 0 : 1,
    identitySpace: "workspace_owner",
    declaration,
    status: String(readback.status || operation.status || ""),
    stage: String(readback.stage || operation.stage || ""),
  };
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
  // The owner route authorizes every command against a CloudIdentity session,
  // and the identity owner publishes which customer that session belongs to. Its
  // actor is the Gateway user id and its display name is the Gateway email, so
  // they are the same two facts the Control Plane session answered with. A run
  // that could not show them to be one customer would be buying for an account
  // it never checked.
  const session = (await http.json("/api/v2/auth/session", {}, auth)).payload;
  const ownerEmail = normalizedAccountEmail(session?.displayName);
  const accountEmail = normalizedAccountEmail(me?.email);
  if (!ownerEmail || !accountEmail || ownerEmail !== accountEmail ||
      String(session?.actorId || "") !== String(me?.sub2apiUserId || "")) {
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
 * delivered. The order is now admitted by the Workspace owner, so the receipt
 * is no longer reachable through a launch identity this run would have derived:
 * it is found by the pair the owner minted. The retained Control Plane billing
 * read is asked for this account's receipts and the receipt that names this
 * operation and this Workspace is this purchase's.
 */
const BILLING_RECEIPT_PAGE_SIZE = 100;
const BILLING_RECEIPT_PAGE_LIMIT = 50;

async function findPurchaseChargeReceipt(http, auth, { workspaceId, operationId }) {
  const found = [];
  let cursor = "";
  for (let page = 0; page < BILLING_RECEIPT_PAGE_LIMIT; page += 1) {
    const query = cursor
      ? `?limit=${BILLING_RECEIPT_PAGE_SIZE}&cursor=${encodeURIComponent(cursor)}`
      : `?limit=${BILLING_RECEIPT_PAGE_SIZE}`;
    const payload = ownerData((await http.json(`/api/billing/receipts${query}`, {}, auth)).payload, "ledger");
    const receipts = Array.isArray(payload?.receipts) ? payload.receipts : [];
    for (const receipt of receipts) {
      // The receipt names its purchase by the Workspace it paid for or by the
      // order that caused it. Either one makes this receipt the candidate for
      // this purchase, and the read below is what says whether it really is:
      // a receipt that names another order is found and then refused, rather
      // than being passed over for another one that happens to fit.
      if (receipt?.workspaceId === workspaceId || receipt?.operationId === operationId) found.push(receipt);
    }
    const next = String(payload?.nextCursor || "");
    if (payload?.hasMore !== true || !next) break;
    cursor = next;
  }
  return found;
}

async function readPurchaseChargeReceipt(http, auth, { workspaceId, operationId, amountUsdMicros, expectedReceiptId }) {
  const found = await findPurchaseChargeReceipt(http, auth, { workspaceId, operationId });
  // A receipt that names this order is the only evidence that this order was
  // charged, and more than one of them is not an answer this run can read.
  if (found.length === 0) throw adapterError("purchase_receipt_missing");
  if (found.length > 1) throw adapterError("charge_receipt_mismatch");
  const receiptId = String(found[0]?.receiptId || "");
  // A buyer that already holds a receipt identity for this order has to be
  // holding this one.
  if (String(expectedReceiptId || "").trim() && String(expectedReceiptId) !== receiptId) {
    throw adapterError("launch_identity_mismatch");
  }
  const receipt = ownerData((await http.json(
    `/api/billing/receipts/${encodeURIComponent(receiptId)}`, {}, auth)).payload, "ledger");
  if (receipt?.receiptId !== receiptId || receipt?.workspaceId !== workspaceId ||
      receipt?.operationId !== operationId ||
      receipt?.type !== "billing.workspace_purchased.v1" || receipt?.status !== "completed" ||
      String(receipt?.totalUsdMicros || "") !== String(amountUsdMicros) ||
      !String(receipt?.chargeReference || "").trim()) {
    throw adapterError("charge_receipt_mismatch");
  }
  // A resource-only purchase delivers resources and nothing else. The receipt
  // has to name exactly those three resources: a Runtime or a Workspace Key
  // inside this receipt would mean the platform fulfilled more than the
  // operation declared, and the run would not know what it paid for.
  const delivered = ["computeAllocationId", "storageId", "attachmentId"];
  if (!hasExactKeys(receipt?.fulfillment, delivered) ||
      delivered.some((name) => !String(receipt.fulfillment[name] || "").trim())) {
    throw adapterError("charge_receipt_mismatch");
  }
  return { chargeReference: String(receipt.chargeReference), receiptId };
}

/*
 * The retained Control Plane order, for an operation that already exists.
 *
 * A new purchase is admitted by the Workspace owner and is read back from the
 * owner. An order that the retained Control Plane admitted before the cutover
 * keeps its own identity and its own owner, so it is still readable and still
 * recoverable through the route that created it. The readback dispatches on the
 * identity space the order was recorded in rather than on a global URL, and the
 * original order identity is never rewritten.
 *
 * When the platform names a Workspace and it is not this run's, the answer is
 * refused here rather than followed.
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
 * Following a retained Control Plane order to its own end.
 *
 * The retained route is read, never written: an operation that already exists
 * is followed under its own identity rather than read as finished or
 * reissued. The window is bounded, so an order that never reaches a terminal
 * state is a refusal rather than an infinite poll.
 */
const RETAINED_ORDER_POLL_ATTEMPTS = 180;
const RETAINED_ORDER_POLL_INTERVAL_MS = 1_000;
const RETAINED_ORDER_STOPPED = ["manual_review", "failed", "refunded"];

async function followRetainedOrder(http, operationId, workspaceId, auth, wait) {
  const pause = wait || defaultWait;
  for (let attempt = 0; attempt < RETAINED_ORDER_POLL_ATTEMPTS; attempt += 1) {
    const launch = await readAdmittedLaunch(http, operationId, workspaceId, auth);
    if (!launch) throw adapterError("purchase_receipt_missing");
    if (launch.status === "succeeded" && launch.phase === "succeeded") return launch;
    if (RETAINED_ORDER_STOPPED.includes(String(launch.status || ""))) {
      throw adapterError("launch_stopped");
    }
    await pause(RETAINED_ORDER_POLL_INTERVAL_MS);
  }
  throw adapterError("mutation_unaccounted");
}

/*
 * The order this run bought, read from the owner that owns it.
 *
 * An owner-route order is read from the Workspace owner, which reports the
 * owner's own vocabulary: a terminal success is `succeeded`, a stop is
 * `needs_attention`, `failed` or `cancelled`, and the rest are still in flight.
 * A retained Control Plane order is read from its own owner and reports the
 * Control Plane's own phase and status.
 */
async function readPurchaseOperation(http, { operationId, workspaceId, identitySpace }, auth, wait) {
  if (identitySpace === "control_plane") {
    const launch = await followRetainedOrder(http, operationId, workspaceId, auth, wait);
    return {
      status: String(launch.status || ""),
      stage: String(launch.phase || ""),
      receiptId: String(launch.receiptId || ""),
    };
  }
  const operation = await readOwnerWorkspaceOperation(http, operationId, workspaceId, auth);
  if (!operation) return null;
  if (operation.status !== "succeeded") {
    throw adapterError("launch_stopped");
  }
  return { status: String(operation.status || ""), stage: String(operation.stage || ""), receiptId: "" };
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
  const operation = {
    idempotencyKey: request.operation.idempotencyKey,
    operationId: "",
    workspaceId: "",
    purchaseReceiptId: "",
    resumed: false,
  };
  // The purchase evidence this run gathered, kept beside the receipt so a
  // caller that writes its own purchase artifact does not have to re-derive
  // anything the owner already said.
  let purchaseEvidence: OwnerPurchase | null = null;

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

      accounting.walletBeforeUsdMicros = walletMicros((await http.json("/api/v2/wallet", {}, auth)).payload);

      let quote = "";
      let identitySpace = "workspace_owner";

      if (purchaseHook) {
        // The protected caller hands over a buyer of its own. That buyer owns the
        // declared-account check and the purchase through the shared owner-route
        // core; this orchestrator consumes only its bounded result and then reads
        // the order and delivery through the same owner APIs as before. The buyer
        // has already decided whether this is a first purchase or a replay, so
        // the result carries the identities the rest of the run must use.
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
        identitySpace = purchased.identitySpace;
        accounting.quoteUsdMicros = purchased.amountUsdMicros;
        mutations.purchases = purchased.purchases;
        quote = purchased.amountUsdMicros;

        // The buyer waited for its own order. Read the same order under the
        // identity it returned, from the owner that owns that identity space,
        // so the charge and delivery evidence still comes from an owner rather
        // than from the buyer's receipt.
        const owned = await readPurchaseOperation(http, {
          operationId: operation.operationId, workspaceId: operation.workspaceId, identitySpace,
        }, auth, wait);
        if (!owned) throw adapterError("purchase_receipt_missing");
        purchaseEvidence = {
          idempotencyKey: operation.idempotencyKey,
          quoteId: "",
          operationId: operation.operationId,
          workspaceId: operation.workspaceId,
          amountUsdMicros: quote,
          resumed: operation.resumed,
          purchases: mutations.purchases,
          identitySpace,
          declaration: null,
          status: owned.status,
          stage: owned.stage,
        };

        checks.quote_priced = true;
        checks.purchase_issued = true;
      } else {
        // The Workspace owner prices and admits the order. The catalog is read
        // the way the Console reads it and the declaration is frozen into the
        // quote, so the owner prices exactly what this run asked for. The owner
        // mints the operation and the Workspace identity; this run binds its
        // evidence to them.
        let purchased;
        try {
          purchased = await buyWorkspaceOnOwnerRoute({
            http,
            auth,
            idempotencyKey: operation.idempotencyKey,
            walletUsdMicros: accounting.walletBeforeUsdMicros,
            wait,
            declared: declaredProduct,
          });
        } catch (error) {
          // An order that was admitted and then went quiet is a mutation this
          // run caused, and the receipt has to say so even though the run could
          // not follow the order to its end.
          const admitted = Number((error as { purchases?: number } | null)?.purchases ?? 0);
          if (Number.isInteger(admitted) && admitted > 0) mutations.purchases = admitted;
          throw error;
        }
        operation.operationId = purchased.operationId;
        operation.workspaceId = purchased.workspaceId;
        operation.resumed = purchased.resumed;
        quote = purchased.amountUsdMicros;
        accounting.quoteUsdMicros = quote;
        checks.quote_priced = true;
        mutations.purchases = purchased.purchases;
        checks.purchase_issued = true;
        purchaseEvidence = purchased;
      }

      // What was billed, and what is running. The Ledger receipt is the charge
      // evidence; the Workspace and Runtime readbacks are what say there is
      // something there to use. The receipt is bound to the operation and the
      // Workspace the owner minted, so it cannot be another purchase's charge.
      const charge = await readPurchaseChargeReceipt(http, auth, {
        workspaceId: operation.workspaceId,
        operationId: operation.operationId,
        amountUsdMicros: quote,
        expectedReceiptId: operation.purchaseReceiptId,
      });
      operation.purchaseReceiptId = charge.receiptId;
      checks.purchase_receipt_bound = true;
      checks.charge_receipted = true;
      accounting.chargeReferenceDigest = digestOf(charge.chargeReference);

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

      accounting.walletAfterUsdMicros = walletMicros((await http.json("/api/v2/wallet", {}, auth)).payload);
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
  return { state: receipt.state, reason: receipt.reason, receipt, purchase: purchaseEvidence };
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