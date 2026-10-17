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
  createQualificationRequest,
  qualificationWorkspaceName,
  validateBusinessQualificationReceipt,
} from "./workspace-qualification-contract.ts";
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
 * What does the platform already know about this order?
 *
 * This is the whole of the replay answer. It is asked before the first command
 * and again after a lost response, and it reads the order the deterministic
 * identity names rather than any order this process remembers, so a different
 * machine answering the same qualification reaches the same conclusion.
 */
async function readAdmittedLaunch(http, operationId, auth) {
  const result = await http.request(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth);
  const status = Number(result?.response?.status || 0);
  if (status === 404) return null;
  if (status !== 200 || !result?.payload) throw adapterError("owner_readback_unavailable");
  return result.payload;
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
  const accounting = { quoteUsdMicros: null, walletBeforeUsdMicros: null, walletAfterUsdMicros: null };
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
      let launch = await readAdmittedLaunch(http, operation.operationId, auth);
      if (launch) {
        operation.resumed = true;
      } else {
        try {
          ({ launch } = await submitWorkspaceLaunch({
            http, auth, name: qualificationWorkspaceName(operation.operationId), packageId: request.product.packageId,
            autoRenew: request.product.autoRenew, idempotencyKey: operation.idempotencyKey,
            expectedOperationId: operation.operationId, expectedWorkspaceId: operation.workspaceId, wait,
          }));
        } catch {
          // The command may have been accepted and its answer lost. That is not
          // a second purchase, it is the same order, and the only question left
          // is whether the platform holds it.
          const recovered = await readAdmittedLaunch(http, operation.operationId, auth);
          if (!recovered) throw adapterError("response_lost");
          operation.resumed = true;
          launch = recovered;
        }
      }
      mutations.purchases = 1;
      checks.purchase_issued = true;

      // A resumed order that is not terminal yet is followed to its own end
      // under its own identity, rather than read as finished or reissued. The
      // window is the purchase core's own, so a stuck order is bounded even
      // though a lost response may cost one further follow.
      if (launch?.status !== "succeeded" || launch?.phase !== "succeeded") {
        try {
          launch = await waitForLaunch(http, operation.operationId, auth, wait);
        } catch {
          throw adapterError("launch_stopped");
        }
      }
      // A terminal launch that carries no purchase receipt identity has not
      // delivered a receipt to bind anything to. That is a binding failure
      // rather than an absent check, and it is reported as one.
      if (!String(launch?.receiptId || "").trim()) throw adapterError("receipt_binding_mismatch");
      operation.purchaseReceiptId = String(launch.receiptId);
      checks.receipt_bound = true;

      accounting.walletAfterUsdMicros = walletMicros((await http.json("/api/gateway/wallet", {}, auth)).payload);
      const moved = BigInt(accounting.walletBeforeUsdMicros) - BigInt(accounting.walletAfterUsdMicros);
      if (moved === BigInt(quote)) {
        mutations.charges = 1;
        checks.charge_settled = true;
      } else if (moved > BigInt(quote)) {
        // The owner readback proves more movement than this run's single
        // declared purchase accounts for. Something charged that this run
        // cannot place, which is an unaccounted mutation rather than a
        // mismatch between two numbers that are both known.
        throw adapterError("mutation_unaccounted");
      } else if (moved === 0n && operation.resumed && checks.receipt_bound) {
        // A redelivery of an order that already reached its terminal purchase
        // receipt bought nothing again, so the wallet correctly did not move.
        // That is the same settled purchase observed a second time, and the
        // zero is the evidence that replay cost nothing.
        mutations.charges = 0;
        checks.charge_settled = true;
      } else {
        throw adapterError("charge_readback_mismatch");
      }

      terminal = QUALIFICATION_BUSINESS_CHECKS.every((name) => checks[name])
        ? { state: "ready", reason: "none" }
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
    state: terminal.state,
    reason: terminal.reason,
    provider: request.provider,
    candidate: request.candidate,
    source: request.source,
    tenant: { accountDigest: request.tenant.accountDigest },
    product: request.product,
    authority: request.authority,
    operation,
    scope: { declared: { ...request.scope }, observed: mutations },
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