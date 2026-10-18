/*
 * The Tencent-TKE adapter over the shared business qualification seam.
 *
 * The orchestrator owns the flow and the meaning of its result. Its adapter
 * boundary is three reachability operations: open a business scope, close it,
 * and read deployment health. This module is the `tencent-tke` implementation
 * of that boundary, and the only place in the Cloud qualification source path
 * where the provider's identity is named.
 *
 * What is deliberately not here: a Tencent or Kubernetes client, a cluster or
 * container mutation, a provider resource name, a decoder for an owner business
 * fact, or any receipt field. Provider-specific operations stay behind the
 * Fabric/Instance boundary, which is injected below as a typed dependency. The
 * orchestrator reads the business evidence (account, quote, purchase, charge,
 * Workspace and Runtime) through Cloud's own owner APIs; this adapter supplies
 * the transport that makes those reads possible and proves it put the transport
 * back. An adapter that could answer any of those business questions would be
 * able to invent a qualified result, so the surface here cannot express one.
 *
 * The boundary is a separate object, not a module import, for the same reason
 * the orchestrator takes an adapter: the TKE operations run in the protected
 * environment, and a source test binds a fixture in their place. Every readback
 * has to name this exact provider, cluster and namespace; another deployment's
 * answer is refused rather than adopted.
 *
 * Replay is handled by identity, not by remembered provider state. The
 * orchestrator derives one operation key per Candidate, tenant, product and
 * provider, and passes the same key to every provider operation here. Repeated
 * open or close calls for that key reuse the first result instead of asking the
 * provider to do the work twice, so a redelivery of the same qualification
 * cannot create a second reachability binding. A failed open is not cached: the
 * provider may recover, and the orchestrator's operation identity still names
 * the same purchase order.
 */

export const TENCENT_TKE_PROVIDER = "tencent-tke";

// The hosted deployment modes that pair with the TKE provider. `customer_owned`
// is a retained support target with no new business flow, so a qualification
// does not admit it here.
export const TENCENT_TKE_DEPLOYMENT_MODES = Object.freeze(["managed_tke", "platform_owned"]);

// The provider operations the protected Fabric/Instance boundary must supply.
// They are reachability operations only; none of them can report a purchase, a
// charge, a Workspace, a Runtime or a Key.
export const TENCENT_TKE_BOUNDARY_OPERATIONS = Object.freeze([
  "establishReachability", "releaseReachability", "readProviderHealth",
]);

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const namespacePattern = /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/;
// The shared contract's own derived operation key (qualificationIdempotencyKey).
const operationKeyPattern = /^opl-qualification:[0-9a-f]{64}$/;
const providerStates = Object.freeze(["ready", "degraded", "absent", "unknown"]);

/*
 * The approved Instance deployment inputs this adapter binds to.
 *
 * These are facts about the installation the Candidate was deployed into, not
 * about the qualification. They stay on this side of the seam: the receipt
 * carries the provider name and the Candidate identity, never the cluster, the
 * namespace, the region or the owner origin.
 */
export type TencentTkeDeployment = {
  deploymentMode: "managed_tke" | "platform_owned";
  fabricProvider: "tencent-tke";
  candidateReceiptDigest: string;
  clusterId: string;
  region: string;
  namespace: string;
  ownerOrigin: string;
};

// The provider identity every readback has to repeat. A result that names a
// different cluster or namespace belongs to another installation.
export type TencentTkeProviderIdentity = {
  provider: "tencent-tke";
  clusterId: string;
  namespace: string;
};

// The owner-API transport the orchestrator already uses. `origin` is where the
// deployment's Cloud owner APIs are served; the adapter refuses a transport
// bound to any other origin.
export type TencentTkeOwnerTransport = {
  origin: string;
  http: {
    json(path: string, init?: Record<string, unknown>, auth?: unknown): Promise<{ status?: number; payload?: any }>;
    request(path: string, init?: Record<string, unknown>, auth?: unknown): Promise<{ response?: { status?: number }; payload?: any }>;
  };
  auth: object | string;
};

// `open` means the provider channel exists. It never means the product is
// ready: the orchestrator still has to read every business fact itself.
export type TencentTkeReachabilityReadback =
  | ({ state: "open"; transport: TencentTkeOwnerTransport } & TencentTkeProviderIdentity)
  | ({ state: "absent"; reason: string } & TencentTkeProviderIdentity)
  | ({ state: "unknown"; reason: string } & TencentTkeProviderIdentity);

// `released` and `absent` both prove nothing this operation opened is left
// behind. `unknown` proves nothing and cannot be read as restored.
export type TencentTkeReleaseReadback =
  | ({ state: "released" } & TencentTkeProviderIdentity)
  | ({ state: "absent" } & TencentTkeProviderIdentity)
  | ({ state: "unknown"; reason: string } & TencentTkeProviderIdentity);

// The host carrying the product. `ready`, `serviceReady` and `failedChecks`
// are the provider's own readiness facts, mapped to one shared health word and
// never to a qualification state.
export type TencentTkeHealthReadback = TencentTkeProviderIdentity & {
  state: "ready" | "degraded" | "absent" | "unknown";
  ready: boolean;
  serviceReady: boolean;
  failedChecks: string[];
};

/*
 * The Fabric/Instance boundary, implemented by the protected runner.
 *
 * It receives the deployment it must act on and the sealed qualification
 * request. It does not receive a receipt, a result slot or a way to write one.
 * `readProviderHealth` must use the provider's own readback channel rather than
 * the business scope: the orchestrator reads health after it closes the scope.
 */
export type TencentTkeProviderBoundary = TencentTkeProviderIdentity & {
  establishReachability(input: {
    deployment: TencentTkeDeployment;
    request: unknown;
    operationKey: string;
  }): Promise<TencentTkeReachabilityReadback>;
  releaseReachability(input: {
    deployment: TencentTkeDeployment;
    request: unknown;
    operationKey: string;
  }): Promise<TencentTkeReleaseReadback>;
  readProviderHealth(input: {
    deployment: TencentTkeDeployment;
    request: unknown;
  }): Promise<TencentTkeHealthReadback>;
};

function adapterError(code) {
  const error = new Error(code);
  error.code = code;
  return error;
}

function nonEmptyText(value, maxLength = 256) {
  const normalized = typeof value === "string" ? value.trim() : "";
  return normalized && normalized.length <= maxLength && !/[\s\u0000-\u001f\u007f]/.test(normalized) ? normalized : "";
}

// An owner origin is an absolute HTTP(S) origin with no credentials, path,
// query or fragment. It is compared by exact string after normalization, so a
// transport cannot be bound to a different installation by a lookalike URL.
function ownerOrigin(value) {
  let url;
  try {
    url = new URL(String(value || ""));
  } catch {
    return "";
  }
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password ||
      url.search || url.hash || url.pathname !== "/") {
    return "";
  }
  return url.origin;
}

/*
 * Validate and normalize the approved deployment inputs once, before anything
 * is opened. A deployment that names another provider or mode, a Candidate
 * digest that is not one, or an incomplete provider identity is refused here
 * rather than half-used later. The Candidate digest is compared with the
 * request before a scope opens.
 */
export function tencentTkeDeployment(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw adapterError("tencent_tke_deployment_invalid");
  }
  const deploymentMode = String(value.deploymentMode || "").trim();
  const fabricProvider = String(value.fabricProvider || "").trim();
  const candidateReceiptDigest = String(value.candidateReceiptDigest || "");
  const clusterId = nonEmptyText(value.clusterId);
  const region = nonEmptyText(value.region);
  const namespace = nonEmptyText(value.namespace);
  const origin = ownerOrigin(value.ownerOrigin);
  if (!TENCENT_TKE_DEPLOYMENT_MODES.includes(deploymentMode) ||
      fabricProvider !== TENCENT_TKE_PROVIDER ||
      !digestPattern.test(candidateReceiptDigest) ||
      !clusterId || !region || !origin ||
      !namespacePattern.test(namespace) || namespace.length > 63) {
    throw adapterError("tencent_tke_deployment_invalid");
  }
  return Object.freeze({
    deploymentMode,
    fabricProvider,
    candidateReceiptDigest,
    clusterId,
    region,
    namespace,
    ownerOrigin: origin,
  });
}

/*
 * The boundary is checked before it is used. It must implement all three
 * reachability operations and declare the same provider the request does, so a
 * Local-Docker boundary cannot be driven through a TKE qualification.
 */
function tencentTkeBoundary(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw adapterError("tencent_tke_boundary_invalid");
  }
  for (const name of TENCENT_TKE_BOUNDARY_OPERATIONS) {
    if (typeof value[name] !== "function") throw adapterError("tencent_tke_boundary_invalid");
  }
  if (String(value.provider || "").trim() !== TENCENT_TKE_PROVIDER) {
    throw adapterError("tencent_tke_boundary_invalid");
  }
  return value;
}

/*
 * The qualification request, reduced to the identity this adapter depends on.
 *
 * The operation key is the shared contract's own derivation. It names one
 * purchase order for one Candidate, tenant, product and provider, so caching by
 * it cannot merge two qualifications, and reusing it cannot create a second
 * purchase. The Candidate digest is also compared with the installation the
 * deployment binding names: a qualification may not run against a host that was
 * installed from different bytes.
 */
function qualificationOperationKey(request, deployment) {
  if (!request || typeof request !== "object" || Array.isArray(request)) {
    throw adapterError("qualification_request_invalid");
  }
  if (String(request.provider || "").trim() !== TENCENT_TKE_PROVIDER) {
    throw adapterError("tencent_tke_adapter_provider_mismatch");
  }
  const idempotencyKey = String(request.operation?.idempotencyKey || "").trim();
  const accountDigest = String(request.tenant?.accountDigest || "");
  const candidateReceiptDigest = String(request.candidate?.receiptDigest || "");
  if (!operationKeyPattern.test(idempotencyKey) ||
      !digestPattern.test(accountDigest) || !digestPattern.test(candidateReceiptDigest)) {
    throw adapterError("qualification_request_invalid");
  }
  if (candidateReceiptDigest !== deployment.candidateReceiptDigest) {
    throw adapterError("tencent_tke_deployment_mismatch");
  }
  return `${idempotencyKey}\n${candidateReceiptDigest}\n${accountDigest}`;
}

// A provider answer is usable only when it names this provider, cluster and
// namespace and carries one of the states the caller asked for.
function exactReadback(value, deployment, states) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  if (String(value.provider || "") !== TENCENT_TKE_PROVIDER ||
      String(value.clusterId || "") !== deployment.clusterId ||
      String(value.namespace || "") !== deployment.namespace) {
    return null;
  }
  return states.includes(String(value.state || "")) ? value : null;
}

// The transport is used exactly as the boundary produced it. Returning the
// boundary's own `http` and `auth` is what keeps this adapter from synthesizing
// an owner payload or a session: there is nothing here that can create one.
function ownerTransport(value, deployment) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  if (typeof value.http?.json !== "function" || typeof value.http?.request !== "function") return null;
  if (ownerOrigin(value.origin) !== deployment.ownerOrigin) return null;
  const auth = value.auth;
  const authPresent = typeof auth === "string"
    ? auth.trim().length > 0
    : Boolean(auth && typeof auth === "object" && !Array.isArray(auth) && Object.keys(auth).length > 0);
  return authPresent ? { http: value.http, auth } : null;
}

/*
 * Read the shared health word out of the provider's own readiness facts.
 *
 * The mapping only narrows: `healthy` needs a ready, serving host with no
 * failed check; a serving host with a failed check is `degraded`; absence,
 * unreadable answers, another provider's answer and contradictions are
 * `unknown`. Nothing here can turn into a qualification state, and in
 * particular a healthy host cannot make a failed business run pass.
 */
function deploymentHealth(readback, deployment) {
  const exact = exactReadback(readback, deployment, providerStates);
  if (!exact || typeof exact.ready !== "boolean" || typeof exact.serviceReady !== "boolean" ||
      !Array.isArray(exact.failedChecks) || !exact.failedChecks.every((check) => typeof check === "string")) {
    return "unknown";
  }
  if (exact.state === "ready" && exact.ready === true && exact.serviceReady === true && exact.failedChecks.length === 0) {
    return "healthy";
  }
  return exact.state !== "absent" && exact.state !== "unknown" && exact.serviceReady === true ? "degraded" : "unknown";
}

/*
 * Build the `tencent-tke` adapter the orchestrator drives.
 *
 * The returned object is the orchestrator's adapter shape and nothing more. It
 * owns three small facts for an operation key: whether the open was attempted,
 * the transport it may have received, and the close result. All three are keyed
 * by the shared operation identity, which is what makes a repeated open or
 * close idempotent without a stored qualification.
 */
export function createTencentTkeQualificationAdapter({ deployment, boundary } = {}) {
  const binding = tencentTkeDeployment(deployment);
  const provider = tencentTkeBoundary(boundary);
  const scopes = new Map();
  const attempts = new Map();
  const closings = new Map();

  const adapter = {
    provider: TENCENT_TKE_PROVIDER,

    // Open the provider channel as the request's tenant and return the owner
    // transport. Absence and an unreadable provider are both a blocked scope:
    // neither has read a business fact, so neither may become a product claim.
    //
    // The operation is recorded as attempted before the provider is asked,
    // because an open can fail after the provider has already created a
    // binding: a lost answer, an unknown state, a readback naming another
    // deployment or a malformed transport. The orchestrator closes every
    // attempted scope, including one whose open it never received, so the
    // attempt has to outlive any failure here.
    async openBusinessScope({ request } = {}) {
      const operationKey = qualificationOperationKey(request, binding);
      const opened = scopes.get(operationKey);
      if (opened) return opened;
      // A new attempt invalidates an earlier close result for the same key.
      closings.delete(operationKey);
      attempts.set(operationKey, request);
      let readback;
      try {
        readback = await provider.establishReachability({ deployment: binding, request, operationKey });
      } catch {
        throw adapterError("provider_scope_unavailable");
      }
      const exact = exactReadback(readback, binding, ["open", "absent", "unknown"]);
      if (!exact) throw adapterError("tencent_tke_readback_invalid");
      if (exact.state !== "open") throw adapterError("provider_scope_unavailable");
      const transport = ownerTransport(exact.transport, binding);
      if (!transport) throw adapterError("tencent_tke_transport_invalid");
      scopes.set(operationKey, transport);
      return transport;
    },

    // Put back what this operation attempted. `released` and `absent` both say
    // the provider readback found nothing left; `unknown`, a lost release
    // answer, or a readback naming another deployment cannot say that, so the
    // scope is reported unproven rather than assumed restored.
    //
    // The release runs for an attempted open even when this adapter never
    // received a usable transport: the provider may have opened a binding whose
    // answer was lost or refused, and this is the only party that can ask it to
    // put that binding back. A close with no attempt at all has nothing to
    // release and is honestly restored.
    async closeBusinessScope({ request } = {}) {
      const operationKey = qualificationOperationKey(request, binding);
      const closed = closings.get(operationKey);
      if (closed) return closed;
      const attemptedRequest = attempts.get(operationKey);
      if (attemptedRequest === undefined) return Object.freeze({ restored: true });
      attempts.delete(operationKey);
      // Drop the transport before releasing so a failed release cannot leave a
      // usable session held by this adapter.
      scopes.delete(operationKey);
      let readback;
      try {
        readback = await provider.releaseReachability({ deployment: binding, request: attemptedRequest, operationKey });
      } catch {
        readback = null;
      }
      const exact = exactReadback(readback, binding, ["released", "absent", "unknown"]);
      const closing = Object.freeze({ restored: exact?.state === "released" || exact?.state === "absent" });
      closings.set(operationKey, closing);
      return closing;
    },

    // Health is recorded beside the business result and never moves it. Every
    // failure to read it is `unknown`, including another provider's answer.
    async readDeploymentHealth({ request } = {}) {
      let readback;
      try {
        readback = await provider.readProviderHealth({ deployment: binding, request });
      } catch {
        return "unknown";
      }
      return deploymentHealth(readback, binding);
    },
  };

  return Object.freeze(adapter);
}

/*
 * The J0 sandbox reachability qualification.
 *
 * A J0 READY receipt is a statement about the provider before any business
 * question is asked: a dedicated workspace scope can be opened for this exact
 * Candidate in the declared sandbox, the owner transport it returns belongs to
 * that deployment, the host carrying it answers healthy on the provider's own
 * readback, and the scope is put back. Fabric already owns exactly those
 * operations behind the Fabric/Instance boundary, so this function drives that
 * boundary instead of starting a second qualification flow. It buys nothing,
 * reads no business fact, and writes no receipt.
 *
 * Every check is recorded as passed, failed or not evaluated. A readback naming
 * another cluster or namespace, a transport bound to another origin, a health
 * answer that is not the provider's own `healthy`, or a release that cannot say
 * the scope is gone all fail. Only a step that could not be reached (an open
 * that never returned a readback leaves no transport to check) is left
 * unevaluated, and is counted as skipped rather than folded into a pass or a
 * failure. `failed` and `skipped` are therefore derived from what the provider
 * answered, not from the caller's expectation.
 */
export const TENCENT_TKE_SANDBOX_CHECKS = Object.freeze([
  "scope_opened", "transport_bound", "provider_healthy", "scope_released",
]);

const sandboxOperationKeyPattern = /^opl-j0-sandbox:[0-9a-f]{64}$/;

export type TencentTkeSandboxQualification = Readonly<{
  checks: Readonly<Record<string, boolean | null>>;
  failed: number;
  skipped: number;
  deploymentHealth: string;
  startedAt: string;
  completedAt: string;
}>;

export async function runTencentTkeSandboxQualification({ deployment, boundary, request, operationKey } = {}) {
  const binding = tencentTkeDeployment(deployment);
  const provider = tencentTkeBoundary(boundary);
  if (!sandboxOperationKeyPattern.test(String(operationKey || ""))) {
    throw adapterError("tencent_tke_sandbox_identity_invalid");
  }
  if (!request || typeof request !== "object" || Array.isArray(request)) {
    throw adapterError("tencent_tke_sandbox_request_invalid");
  }
  const startedAt = new Date().toISOString();
  const checks: Record<string, boolean | null> = {
    scope_opened: null,
    transport_bound: null,
    provider_healthy: null,
    scope_released: null,
  };
  let deploymentHealthWord = "unknown";

  // The open is attempted once. Its answer decides whether there is a transport
  // to inspect; a thrown open is a failed check, not a reason to stop before
  // the release, because the provider may have created the binding anyway.
  let opened = null;
  try {
    opened = exactReadback(
      await provider.establishReachability({ deployment: binding, request, operationKey }),
      binding,
      ["open", "absent", "unknown"],
    );
  } catch {
    opened = null;
  }
  checks.scope_opened = opened?.state === "open";
  if (checks.scope_opened) {
    checks.transport_bound = ownerTransport(opened.transport, binding) !== null;
  }

  // Health is asked for while the scope exists, as the business flow does, and
  // is recorded as its own check rather than as a qualification state.
  try {
    deploymentHealthWord = deploymentHealth(
      await provider.readProviderHealth({ deployment: binding, request }),
      binding,
    );
  } catch {
    deploymentHealthWord = "unknown";
  }
  checks.provider_healthy = deploymentHealthWord === "healthy";

  // The release runs for the attempt even when no usable readback came back:
  // this is the only party that can ask the provider to put back a binding whose
  // answer was lost. `released` and `absent` both say nothing is left behind.
  let released = null;
  try {
    released = exactReadback(
      await provider.releaseReachability({ deployment: binding, request, operationKey }),
      binding,
      ["released", "absent", "unknown"],
    );
  } catch {
    released = null;
  }
  checks.scope_released = released?.state === "released" || released?.state === "absent";

  let completedAt = new Date().toISOString();
  if (Date.parse(completedAt) < Date.parse(startedAt)) completedAt = startedAt;
  const failed = TENCENT_TKE_SANDBOX_CHECKS.filter((name) => checks[name] === false).length;
  const skipped = TENCENT_TKE_SANDBOX_CHECKS.filter((name) => checks[name] === null).length;
  return Object.freeze({
    checks: Object.freeze({ ...checks }),
    failed,
    skipped,
    deploymentHealth: deploymentHealthWord,
    startedAt,
    completedAt,
  });
}
