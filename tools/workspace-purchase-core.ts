import { createHash } from "node:crypto";

/*
 * The Workspace purchase sequence on the Workspace owner route, the quote
 * policy every buyer shares, and nothing else.
 *
 * This is the invariant that every buyer shares. The Console drives it from a
 * browser, the local qualification harness drives it against a stack it built
 * itself, and a deployed buyer has to drive it against a running product. The
 * sequence is the same in all three, so it lives here once and is parameterized
 * by the origin its caller hands it.
 *
 * Beyond the sequence this module owns the two facts every buyer has to agree
 * with the platform on: the quote policy, and the operation identity. Neither
 * is a per-profile choice. A buyer that priced a different resource, package or
 * currency would act on a price that names something it never declared, and a
 * buyer that invented an operation identity would refuse the platform's own
 * correct answer.
 *
 * What is deliberately NOT here: provisioning an account, cleaning up
 * afterwards, or asserting anything about the delivered result. Those differ per
 * authority profile, so each caller keeps its own and composes this one.
 *
 * The sequence is the Workspace owner's own. The owner prices a deploy quote
 * that freezes exactly one application source beside the compute and storage
 * plan, and the order is admitted against that quote. The platform refuses a
 * client that supplies a size or a price, so the command body carries the
 * Workspace name, the quote it is buying and nothing else. Exactly one order is
 * issued per call, however long the poll runs.
 *
 * The owner mints the operation identity and the Workspace identity. A buyer
 * that derived its own would be reproducing an interop contract that no longer
 * exists, and it would have to refuse a correct answer whenever the owner
 * changed how it mints one. The buyer announces the idempotency identity it
 * will use instead, and the owner's own idempotency record decides whether a
 * repeated command is a replay of this order or a new one.
 */

const ORDER_POLL_ATTEMPTS = 180;
// The owner publishes how long to wait before the next read. It is bounded, so a
// buyer that cannot read a terminal state stops rather than polling forever.
const ORDER_POLL_MIN_SECONDS = 1;
const ORDER_POLL_MAX_SECONDS = 300;
const PRICE_MICROS = /^[1-9][0-9]*$/;

// The owner's published operation states. A delivery is done only when the owner
// says so; the rest are either in progress or a breakpoint the owner published,
// and neither may be read as an end.
const ORDER_SUCCEEDED = "succeeded";
const ORDER_STOPPED = ["needs_attention", "failed", "cancelled"];
const ORDER_IN_FLIGHT = ["accepted", "running", "awaiting_confirmation"];

export const WORKSPACE_QUOTE_ROUTE = "/api/v2/quotes";
export const WORKSPACE_ORDER_ROUTE = "/api/v2/workspaces";
export const WORKSPACE_OPERATION_ROUTE = "/api/v2/operations/workspace";

// The header the owner reads a caller's request identity from. The owner stores
// it on the operation it mints and publishes it on that operation, so it is the
// one way a buyer can tell its own order from one an earlier delivery admitted.
export const ORDER_REQUEST_ID_HEADER = "x-opl-request-id";

const defaultWait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));

/*
 * A failure that still names the order it belongs to.
 *
 * The owner mints the operation identity and the Workspace identity before it
 * knows whether the delivery will succeed. A delivery that stops afterwards
 * still left an order behind, and a recovery artifact has to name it. So the
 * failure carries the identities instead of losing them, and keeps the
 * original failure as its cause.
 */
function orderError(message, { operationId = "", workspaceId = "", quoteId = "" } = {}, cause) {
  const error = new Error(message);
  error.operationId = operationId;
  error.workspaceId = workspaceId;
  error.quoteId = quoteId;
  if (cause) error.cause = cause;
  return error;
}

/*
 * The Control Plane's own identifier derivation, byte for byte.
 *
 * The retained Control Plane mints an account identifier from a SHA-1 over the
 * parts it names, writing a zero byte after every part including the last, and
 * taking the first eighteen hex characters. The account is still a retained
 * Control Plane fact, so the rule stays here for the buyers that have to agree
 * with it. The Workspace owner mints its own identities and no buyer derives
 * them.
 */
export function stableID(...parts) {
  const hash = createHash("sha1");
  for (const part of parts) {
    hash.update(String(part));
    hash.update(Buffer.from([0]));
  }
  return hash.digest("hex");
}

/*
 * What a buyer declares when it buys a Workspace.
 *
 * The owner prices exactly one application source beside the compute and
 * storage plan, so the declaration names all three. The default OPL App names
 * an approved Runtime Release; a built Agent names a ready CapabilityVersion.
 * Neither combination is fabricated to satisfy the other, and a declaration
 * that names neither is not a Workspace order.
 */
export function workspaceDeclaration({
  applicationSelection,
  computePlanId,
  storagePlanId,
  modelSelections = [],
  periodMonths = 1,
}) {
  const kind = String(applicationSelection?.kind || "");
  const runtimeVersionId = String(applicationSelection?.runtimeVersionId || "");
  const capabilityVersionId = String(applicationSelection?.capabilityVersionId || "");
  if (kind !== "opl_app" && kind !== "agent") {
    throw new Error("workspace declaration must name one application source");
  }
  if (kind === "opl_app" && !runtimeVersionId) {
    throw new Error("workspace declaration for the default OPL App must name a runtime version");
  }
  if (kind === "agent" && !capabilityVersionId) {
    throw new Error("workspace declaration for an agent must name a capability version");
  }
  if (kind === "opl_app" && capabilityVersionId) {
    throw new Error("workspace declaration must not name a capability version and a runtime version together");
  }
  if (kind === "agent" && runtimeVersionId) {
    throw new Error("workspace declaration must not name a capability version and a runtime version together");
  }
  if (!computePlanId || !storagePlanId) {
    throw new Error("workspace declaration must name a compute plan and a storage plan");
  }
  if (!Number.isInteger(periodMonths) || periodMonths < 1) {
    throw new Error("workspace declaration must name a positive period");
  }
  const selection = kind === "opl_app"
    ? { kind, runtimeVersionId }
    : { kind, capabilityVersionId };
  return { applicationSelection: selection, computePlanId, storagePlanId, modelSelections, periodMonths };
}

/*
 * The account email, normalized the way the platform normalizes it.
 *
 * The platform lowercases and trims an account email before it stores or
 * compares it, so two authorities that each report the customer's email
 * produce the same text here. That is how a buyer shows that two sessions
 * belong to one customer: the normalization is one rule, not two.
 *
 * An address that is not usable returns the empty string rather than a digest,
 * because a digest of an address nobody can check is worse than no answer.
 */
export function normalizedAccountEmail(email) {
  const normalized = String(email || "").trim().toLowerCase();
  if (!normalized || normalized.length > 320 || !normalized.includes("@")) return "";
  return normalized;
}

/*
 * The owner catalog, read in full.
 *
 * The catalog pages by cursor, so every page is read. A cursor that repeats
 * itself would page forever, so it is refused rather than followed.
 */
export async function listOwnerCatalogPages(http, auth, path) {
  const items = [];
  const seenCursors = new Set();
  let cursor = "";
  do {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    const page = (await http.json(`${path}?${query.toString()}`, {}, auth)).payload;
    if (!Array.isArray(page?.items)) throw new Error("owner catalog readback is invalid");
    items.push(...page.items);
    cursor = page?.nextCursor ? String(page.nextCursor) : "";
    if (cursor && seenCursors.has(cursor)) throw new Error("owner catalog readback is invalid");
    if (cursor) seenCursors.add(cursor);
  } while (cursor);
  return items;
}

/*
 * The one catalog entry a declaration names.
 *
 * The catalog publishes identifiers it mints, and the contract publishes them
 * as strings without a shape, so the entry is found by the identity the
 * declaration already carries rather than by a shape a buyer guessed.
 *
 * The catalog is not searched for a default. A buyer that took the first
 * approved release and the first available plans would buy something nobody
 * chose, and two runs of the same declared purchase could buy two different
 * things.
 */
export function findDeclaredCatalogEntry(items, id) {
  const wanted = String(id || "");
  if (!wanted) return null;
  return items.find((candidate) => String(candidate?.id || "") === wanted) || null;
}

/*
 * The price of the declared purchase, and whether the wallet can fund it.
 *
 * The owner prices the Workspace, so a buyer never computes a price. It has to
 * decide whether the quote it was handed names the thing it declared and
 * whether the wallet can fund it. A quote that does not name the declared
 * application source, the declared plans and the declared period is not a price
 * this purchase may act on, and a wallet that cannot answer is not a large
 * balance.
 *
 * The two questions are separate because they are asked at different times. The
 * quote has to name the declared thing whether or not the money is about to
 * move: a replay reports the price its order was admitted at, and that price
 * is only this purchase's price if it names the same thing. The wallet only
 * matters when money is about to move, because a wallet that has since been
 * spent is not a reason to refuse to report an order that already exists.
 */
export function quoteAmountUsdMicros({ quote, declaration }) {
  const amountUsdMicros = String(quote?.totalUSDMicros || "");
  const selection = declaration?.applicationSelection || {};
  const sameApplication = selection.kind === "opl_app"
    ? quote?.runtimeVersionId === selection.runtimeVersionId
    : quote?.capabilityVersionId === selection.capabilityVersionId;
  if (!sameApplication || quote?.purpose !== "deploy" ||
      quote?.computePlanId !== declaration?.computePlanId ||
      quote?.storagePlanId !== declaration?.storagePlanId ||
      quote?.periodMonths !== declaration?.periodMonths ||
      !PRICE_MICROS.test(amountUsdMicros)) {
    throw new Error("workspace quote is invalid");
  }
  return amountUsdMicros;
}

export function assertWalletFundsQuote(walletUsdMicros, amountUsdMicros) {
  const walletMicros = String(walletUsdMicros || "");
  if (!PRICE_MICROS.test(walletMicros) || BigInt(walletMicros) < BigInt(amountUsdMicros)) {
    throw new Error("workspace wallet cannot fund the quote");
  }
  return amountUsdMicros;
}

export function priceWorkspaceQuote({ quote, walletUsdMicros, declaration }) {
  const amountUsdMicros = quoteAmountUsdMicros({ quote, declaration });
  return assertWalletFundsQuote(walletUsdMicros, amountUsdMicros);
}

/*
 * Ask the owner to price the declared Workspace.
 *
 * The owner records the quote against the idempotency identity it was handed,
 * so a repeated quote with the same identity and the same declaration replays
 * the original quote instead of pricing a second one. That is what lets a buyer
 * whose order answer was lost re-issue the order against the same quote.
 */
export async function quoteWorkspaceOrder({ http, auth, declaration, idempotencyKey }) {
  const quote = (await http.json(WORKSPACE_QUOTE_ROUTE, {
    method: "POST", headers: { "idempotency-key": idempotencyKey },
    body: { purpose: "deploy", ...declaration },
  }, auth, [200, 201])).payload;
  if (!quote?.id || !quote?.status) {
    throw new Error("workspace quote identity is missing");
  }
  return quote;
}

/*
 * The request identity this delivery announces.
 *
 * The owner records the request that admitted an order on the operation it
 * mints, so a redelivery can tell its own order from the one it is replaying:
 * the operation names the request that created it. The buyer therefore
 * announces its own request identity on the order command, and a replayed order
 * answers with the request that created it rather than with this one. Nothing
 * here derives an identity the platform owns.
 *
 * The wrapper also records whether the command was admitted at all, because a
 * command that was admitted and whose answer was lost is not a command that was
 * refused, and the two have to be told apart: the first is a mutation this
 * delivery caused, the second is an outcome this delivery cannot report.
 */
export function announceRequest(http, requestId) {
  const admitted = { order: false, mine: false };
  return {
    ...http,
    admitted,
    async json(path, init = {}, auth = null, statuses = [200]) {
      const result = await http.json(path, {
        ...init,
        headers: { ...(init.headers || {}), [ORDER_REQUEST_ID_HEADER]: requestId },
      }, auth, statuses);
      if (path === WORKSPACE_ORDER_ROUTE && init?.method === "POST") {
        admitted.order = Boolean(result?.payload?.operationId);
        // The operation names the request that created it, so an operation
        // under this request is this delivery's own purchase and an operation
        // under another one is a replay.
        admitted.mine = admitted.order && String(result?.payload?.requestId || "") === requestId;
      }
      return result;
    },
  };
}

/*
 * The admitted order followed through its own declared stage order until it
 * reports a terminal success. A bounded window, and an order that never reaches
 * one is a refusal rather than an infinite poll.
 *
 * The owner publishes the interval between reads, so the buyer waits as long as
 * the owner asked and not by its own guess.
 *
 * The operation the owner publishes has to be the order the command admitted:
 * the same operation identity and the same Workspace. A readback that names
 * another Workspace is not this order, and following it would attribute another
 * purchase to this delivery.
 */
export async function waitForWorkspaceOrder(http, operationId, workspaceId, auth, wait = defaultWait) {
  for (let attempt = 0; attempt < ORDER_POLL_ATTEMPTS; attempt += 1) {
    const operation = (await http.json(`${WORKSPACE_OPERATION_ROUTE}/${encodeURIComponent(operationId)}`, {}, auth)).payload;
    if (String(operation?.operationId || "") !== operationId ||
        (workspaceId && String(operation?.resourceId || "") !== workspaceId)) {
      throw orderError("workspace order readback is not this order", { operationId, workspaceId });
    }
    const status = String(operation?.status || "");
    if (status === ORDER_SUCCEEDED) return operation;
    if (ORDER_STOPPED.includes(status)) {
      throw orderError(`Workspace order stopped at ${status}/${operation?.stage || "none"}/${operation?.errorCode || "none"}`,
        { operationId, workspaceId: String(operation?.resourceId || "") });
    }
    if (!ORDER_IN_FLIGHT.includes(status)) {
      throw orderError(`Workspace order reported an unknown status ${status}`, { operationId });
    }
    const seconds = operation?.pollAfterSeconds;
    const delay = Number.isInteger(seconds) && seconds >= ORDER_POLL_MIN_SECONDS && seconds <= ORDER_POLL_MAX_SECONDS
      ? seconds * 1000
      : 1000;
    await wait(delay);
  }
  throw new Error(`Workspace order did not reach succeeded within ${ORDER_POLL_ATTEMPTS} seconds`);
}

/*
 * Issue exactly one order and follow it to a terminal success.
 *
 * The owner mints the operation identity and the Workspace identity, so the
 * answer is authoritative and the buyer binds its own evidence to it. The order
 * is admitted against the quote the owner priced, so the quote identity and the
 * declaration it froze travel with it.
 *
 * The command is keyed by the buyer's own idempotency identity. The owner
 * records it against the request, so a repeated command with the same identity
 * and the same request replays this order rather than creating a second one.
 * That is the guarantee this route offers: not that a buyer never repeats a
 * command, but that a repeated command never becomes a second purchase.
 */
export async function submitWorkspaceOrder({
  http,
  auth,
  name,
  quote,
  idempotencyKey,
  wait,
}) {
  let order;
  try {
    order = (await http.json(WORKSPACE_ORDER_ROUTE, {
      method: "POST", headers: { "idempotency-key": idempotencyKey },
      body: { name, quoteId: quote.id, renewalMode: "manual" },
    }, auth, [202])).payload;
  } catch (cause) {
    throw orderError(cause?.message || "workspace order response is unavailable", { quoteId: String(quote.id) }, cause);
  }
  const operationId = String(order?.operationId || "");
  const workspaceId = String(order?.resourceId || "");
  if (!operationId || !workspaceId) {
    throw orderError("workspace order identity is missing", { quoteId: String(quote?.id || "") });
  }
  try {
    const operation = await waitForWorkspaceOrder(http, operationId, workspaceId, auth, wait);
    return { order, operation, operationId, workspaceId };
  } catch (error) {
    // The delivery stopped or could not be read, but the order exists and the
    // owner minted its identities. A recovery artifact names them, so they
    // travel with the failure instead of being lost with it.
    throw orderError(error?.message || "workspace order did not complete",
      { operationId, workspaceId, quoteId: String(quote?.id || "") }, error);
  }
}

// Read every owner page. A partial list cannot establish cardinality, and a
// repeated cursor or duplicate operation means this is not a coherent readback.
export async function listWorkspaceTransactions(http, auth, workspaceId) {
  const items = [];
  const seenIds = new Set();
  const seenCursors = new Set();
  let cursor = "";
  do {
    const query = new URLSearchParams({ limit: "100" });
    if (cursor) query.set("cursor", cursor);
    const page = (await http.json(`/api/v2/workspaces/${encodeURIComponent(workspaceId)}/transactions?${query}`, {}, auth)).payload;
    if (!Array.isArray(page?.items) || (page.nextCursor !== undefined && typeof page.nextCursor !== "string")) {
      throw new Error("Workspace transaction page is invalid");
    }
    for (const item of page.items) {
      if (!String(item?.id || "").trim() || item.workspaceId !== workspaceId || seenIds.has(item.id)) {
        throw new Error("Workspace transaction identity is invalid");
      }
      seenIds.add(item.id);
      items.push(item);
    }
    cursor = page.nextCursor || "";
    if (cursor && seenCursors.has(cursor)) throw new Error("Workspace transaction cursor repeated");
    if (cursor) seenCursors.add(cursor);
  } while (cursor);
  return items;
}

// Ledger already binds the funded charge to the accepted Workspace obligation.
// Consumers use that operation identity, never amount equality or wall clocks,
// to distinguish a purchase from another charge or renewal of the same Workspace.
export async function readWorkspacePurchaseEvidence(http, auth, {
  workspaceId, operationId, amountUsdMicros, quoteId = "", expectedReceiptId = "",
}) {
  if (!workspaceId || !operationId || !PRICE_MICROS.test(String(amountUsdMicros))) {
    throw new Error("Workspace purchase evidence binding is missing");
  }
  let transactions;
  try {
    transactions = await listWorkspaceTransactions(http, auth, workspaceId);
  } catch (cause) {
    throw Object.assign(new Error(`Workspace purchase evidence is unavailable: ${cause?.message || "owner read failed"}`, { cause }), { code: "purchase_receipt_missing" });
  }
  const charges = transactions.filter((item) => item.kind === "charge" && item.status === "confirmed" &&
    String(item.receiptId || "").trim());
  const matches = [];
  if (charges.length === 0) throw Object.assign(new Error("Workspace purchase evidence is missing"), { code: "purchase_receipt_missing" });
  for (const charge of charges) {
    let financial;
    try {
      financial = (await http.json(
        `/api/v2/workspaces/${encodeURIComponent(workspaceId)}/financial-receipts/${encodeURIComponent(charge.receiptId)}`, {}, auth,
      )).payload;
    } catch (cause) {
      throw Object.assign(new Error(`Workspace purchase evidence is unavailable: ${cause?.message || "owner read failed"}`, { cause }), { code: "purchase_receipt_missing" });
    }
    if (financial?.receipt?.operationId !== operationId) continue;
    if (Object.keys(financial).sort().join(",") !== "evidenceDigest,quote,receipt,walletOperation,workspaceId" ||
      financial.workspaceId !== workspaceId || financial.receipt.id !== charge.receiptId ||
      (expectedReceiptId && financial.receipt.id !== expectedReceiptId) ||
      financial.receipt.owner !== "workspace" || financial.receipt.kind !== "wallet_action" || financial.receipt.outcome !== "confirmed" ||
      financial.walletOperation?.id !== charge.id || financial.walletOperation?.workspaceId !== workspaceId ||
      financial.walletOperation?.receiptId !== charge.receiptId || financial.walletOperation?.kind !== "charge" ||
      financial.walletOperation?.status !== "confirmed" ||
      String(financial.walletOperation?.amountUSDMicros) !== String(amountUsdMicros) || String(charge.amountUSDMicros) !== String(amountUsdMicros) ||
      !String(financial.quote?.id || "").trim() || financial.quote?.status !== "accepted" ||
      (quoteId && financial.quote.id !== quoteId) || String(financial.quote?.totalUSDMicros) !== String(amountUsdMicros) ||
      !/^sha256:[a-f0-9]{64}$/.test(String(financial.evidenceDigest || ""))) {
      throw new Error("Workspace purchase evidence does not bind this order");
    }
    matches.push(financial);
  }
  if (matches.length !== 1) throw new Error("Workspace purchase evidence cardinality is invalid");
  return matches[0];
}
