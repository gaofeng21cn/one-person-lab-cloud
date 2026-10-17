/*
 * The Local-Docker adapter over the shared business qualification seam.
 *
 * The orchestrator owns the flow and the meaning of its result. Its adapter
 * boundary is three reachability operations: open a business scope, close it,
 * and read deployment health. This module is the `local-docker` implementation
 * of that boundary over the Compose stack `tools/local-workspace-qualification.ts`
 * already builds, and the only place in the Cloud qualification source path
 * where the local provider's identity is named.
 *
 * What is deliberately not here: a Docker client, a compose command, a
 * hard-coded service list, a decoder for an owner business fact, or any receipt
 * field. The Compose stack, its installation inputs, its credentials and its
 * provider readback channel belong to the installation, so they are injected
 * below as a typed boundary. The orchestrator reads the business evidence
 * (account, wallet, quote, purchase, charge, Workspace and Runtime) through
 * Cloud's own owner APIs; this adapter supplies the transport that makes those
 * reads possible and proves it put that transport back. An adapter that could
 * answer any of those business questions would be able to invent a qualified
 * result, so the surface here cannot express one.
 *
 * Nor does this module hold a credential. The boundary's `login` operation
 * performs the login flow the existing harness already owns
 * (`login()` in `tools/local-workspace-qualification.ts`) with credentials that
 * never leave that boundary, and returns the resulting session. The adapter
 * validates and caches that session, and nothing here can read an email, a
 * password, a token or a provider secret.
 *
 * Replay is handled by identity, not by remembered provider state. The
 * orchestrator derives one operation key per Candidate, tenant, product and
 * provider, and passes the same key to every operation here. Repeated open or
 * close calls for that key reuse the first result instead of asking the local
 * stack to do the work twice, so a redelivery of the same qualification cannot
 * create a second reachability binding. A failed open is not cached: the stack
 * may recover, and the orchestrator's operation identity still names the same
 * purchase order.
 */

import { hasExactKeys } from "./workspace-qualification-contract.ts";

export const LOCAL_DOCKER_PROVIDER = "local-docker";

// The local provider operations the installation boundary must supply. They are
// reachability operations only; none of them can report a purchase, a charge, a
// Workspace, a Runtime or a Key.
export const LOCAL_DOCKER_BOUNDARY_OPERATIONS = Object.freeze([
  "up", "down", "readProviderHealth", "login",
]);

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const shaPattern = /^[0-9a-f]{40}$/;
// Compose project names are lowercase letters, digits, dashes and underscores.
const projectPattern = /^[a-z0-9][a-z0-9_-]*$/;
// The shared contract's own derived operation key (qualificationIdempotencyKey).
const operationKeyPattern = /^opl-qualification:[0-9a-f]{64}$/;
const providerStates = Object.freeze(["ready", "degraded", "absent", "unknown"]);

const DEPLOYMENT_KEYS = Object.freeze([
  "candidateReceiptDigest", "composeProject", "composeFiles", "platformOrigin", "accountDigest", "sourceTree",
]);

/*
 * The approved Local-Docker deployment inputs this adapter binds to.
 *
 * These are facts about the local installation the Candidate was deployed into,
 * not about the qualification. They stay on this side of the seam: the receipt
 * carries the provider name and the Candidate identity, never the Compose
 * project, the Compose files or the loopback origin.
 *
 * `composeFiles` lists the exact Compose files the stack is assembled from. The
 * real Local-Docker qualification builds five of them (base, deployment
 * overlay, Fabric provider overlay, Local-Workspace overlay and qualification
 * overlay), so a single-file binding would describe a stack the harness never
 * starts.
 *
 * `accountDigest` and `sourceTree` bind the stack to the tenant and the source
 * bytes the qualification declared. A stack started for another account or
 * built from another tree is a different installation, and this adapter
 * refuses to drive it.
 */
export type LocalDockerDeployment = {
  candidateReceiptDigest: string;
  composeProject: string;
  composeFiles: readonly string[];
  platformOrigin: string;
  accountDigest: string;
  sourceTree: string;
};

// The provider identity every readback has to repeat. A result that names a
// different Compose project belongs to another local stack.
export type LocalDockerProviderIdentity = {
  provider: "local-docker";
  composeProject: string;
};

// The owner-API transport the orchestrator already uses. `origin` is where the
// local stack serves its Cloud owner APIs; the adapter refuses a transport
// bound to any other origin.
export type LocalDockerOwnerTransport = {
  origin: string;
  http: {
    json(path: string, init?: Record<string, unknown>, auth?: unknown): Promise<{ status?: number; payload?: any }>;
    request(path: string, init?: Record<string, unknown>, auth?: unknown): Promise<{ response?: { status?: number }; payload?: any }>;
  };
  auth: { cookie: string; csrf: string };
};

// `ready` means the Compose stack is up and serving. It never means the product
// is ready: the orchestrator still has to read every business fact itself.
export type LocalDockerStackReadback =
  | ({ state: "ready" } & LocalDockerProviderIdentity)
  | ({ state: "absent" | "unknown"; reason: string } & LocalDockerProviderIdentity);

// `authenticated` is the login flow's own success. It carries the session the
// orchestrator needs and nothing that says the session belongs to a customer;
// the owner API read in the orchestrator is what proves that.
export type LocalDockerSessionReadback =
  | ({ state: "authenticated"; transport: LocalDockerOwnerTransport } & LocalDockerProviderIdentity)
  | ({ state: "absent" | "unknown"; reason: string } & LocalDockerProviderIdentity);

// `released` and `absent` both prove nothing this operation opened is left
// behind. `unknown` proves nothing and cannot be read as restored.
export type LocalDockerReleaseReadback =
  | ({ state: "released" | "absent" } & LocalDockerProviderIdentity)
  | ({ state: "unknown"; reason: string } & LocalDockerProviderIdentity);

// The host carrying the product, in the provider's own words. `state` is the
// provider's summary; `running`, `serviceReady` and `failedChecks` are the
// facts behind it. They map to one shared health word and never to a
// qualification state.
export type LocalDockerHealthReadback = LocalDockerProviderIdentity & {
  state: "ready" | "degraded" | "absent" | "unknown";
  running: boolean;
  serviceReady: boolean;
  failedChecks: string[];
};

/*
 * The installation boundary, implemented by the local qualification runner.
 *
 * It receives the deployment it must act on and the sealed qualification
 * request. It does not receive a receipt, a result slot or a way to write one.
 * `readProviderHealth` must use the provider's own readback channel (the
 * Compose/Docker facts or the Fabric readiness readback) rather than a
 * successful Console page load: a page served from a degraded stack is not a
 * healthy host.
 */
export type LocalDockerProviderBoundary = LocalDockerProviderIdentity & {
  up(input: {
    deployment: LocalDockerDeployment;
    request: unknown;
    operationKey: string;
  }): Promise<LocalDockerStackReadback>;
  down(input: {
    deployment: LocalDockerDeployment;
    request: unknown;
    operationKey: string;
  }): Promise<LocalDockerReleaseReadback>;
  readProviderHealth(input: {
    deployment: LocalDockerDeployment;
    request: unknown;
  }): Promise<LocalDockerHealthReadback>;
  login(input: {
    deployment: LocalDockerDeployment;
    request: unknown;
    operationKey: string;
  }): Promise<LocalDockerSessionReadback>;
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

// A platform origin is an absolute HTTP(S) origin with no credentials, path,
// query or fragment. It is compared by exact string after normalization, so a
// transport cannot be pointed at a different installation by a lookalike URL.
function platformOrigin(value) {
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

// A Compose file is a repository-relative path to a YAML file. Absolute paths,
// traversal, query/fragment-like characters and non-Compose extensions are
// refused here rather than passed to the local stack.
function composeFile(value) {
  const file = typeof value === "string" ? value.trim() : "";
  if (!file || file.length > 256 || file.startsWith("/") || file.includes("\\") ||
      !/^[A-Za-z0-9._/-]+$/.test(file) || !/\.ya?ml$/.test(file)) {
    return "";
  }
  const parts = file.split("/");
  return parts.every((part) => part && part !== "." && part !== "..") ? file : "";
}

/*
 * Validate and normalize the approved deployment inputs once, before anything
 * is opened. The object is closed to exactly the keys above: an extra field is
 * refused rather than ignored, so a caller cannot smuggle an installation fact
 * (or a credential) through a binding this adapter is trusted to interpret. A
 * deployment that names another provider, an incomplete Compose stack, a
 * malformed origin or an invalid Candidate/tenant/source identity is refused
 * here rather than half-used later.
 */
export function localDockerDeployment(value) {
  if (!value || typeof value !== "object" || Array.isArray(value) || !hasExactKeys(value, DEPLOYMENT_KEYS)) {
    throw adapterError("local_docker_deployment_invalid");
  }
  const candidateReceiptDigest = String(value.candidateReceiptDigest || "");
  const accountDigest = String(value.accountDigest || "");
  const sourceTree = String(value.sourceTree || "");
  const composeProject = nonEmptyText(value.composeProject, 63);
  const origin = platformOrigin(value.platformOrigin);
  const files = Array.isArray(value.composeFiles) ? value.composeFiles.map(composeFile) : [];
  if (!digestPattern.test(candidateReceiptDigest) ||
      !digestPattern.test(accountDigest) ||
      !shaPattern.test(sourceTree) ||
      !projectPattern.test(composeProject) ||
      !origin ||
      files.length === 0 ||
      files.some((file) => !file) ||
      new Set(files).size !== files.length) {
    throw adapterError("local_docker_deployment_invalid");
  }
  return Object.freeze({
    candidateReceiptDigest,
    composeProject,
    composeFiles: Object.freeze(files),
    platformOrigin: origin,
    accountDigest,
    sourceTree,
  });
}

/*
 * The boundary is checked before it is used. It must implement all four local
 * installation operations and declare the same provider the request does, so a
 * Tencent boundary cannot be driven through a Local-Docker qualification.
 */
function localDockerBoundary(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw adapterError("local_docker_boundary_invalid");
  }
  for (const name of LOCAL_DOCKER_BOUNDARY_OPERATIONS) {
    if (typeof value[name] !== "function") throw adapterError("local_docker_boundary_invalid");
  }
  if (String(value.provider || "").trim() !== LOCAL_DOCKER_PROVIDER) {
    throw adapterError("local_docker_boundary_invalid");
  }
  return value;
}

/*
 * The qualification request, reduced to the identity this adapter depends on.
 *
 * The operation key is the shared contract's own derivation. It names one
 * purchase order for one Candidate, tenant, product and provider, so caching by
 * it cannot merge two qualifications, and reusing it cannot create a second
 * purchase. The Candidate digest, tenant digest and source tree are also
 * compared with the installation the deployment binding names: a qualification
 * may not run against a stack started for another tenant or built from
 * different bytes.
 */
function qualificationOperationKey(request, deployment) {
  if (!request || typeof request !== "object" || Array.isArray(request)) {
    throw adapterError("qualification_request_invalid");
  }
  if (String(request.provider || "").trim() !== LOCAL_DOCKER_PROVIDER) {
    throw adapterError("local_docker_adapter_provider_mismatch");
  }
  const idempotencyKey = String(request.operation?.idempotencyKey || "").trim();
  const accountDigest = String(request.tenant?.accountDigest || "");
  const candidateReceiptDigest = String(request.candidate?.receiptDigest || "");
  const sourceTree = String(request.source?.tree || "");
  if (!operationKeyPattern.test(idempotencyKey) ||
      !digestPattern.test(accountDigest) ||
      !digestPattern.test(candidateReceiptDigest) ||
      !shaPattern.test(sourceTree)) {
    throw adapterError("qualification_request_invalid");
  }
  if (candidateReceiptDigest !== deployment.candidateReceiptDigest ||
      accountDigest !== deployment.accountDigest ||
      sourceTree !== deployment.sourceTree) {
    throw adapterError("local_docker_deployment_mismatch");
  }
  return `${idempotencyKey}\n${candidateReceiptDigest}\n${accountDigest}`;
}

// A provider answer is usable only when it names this provider and this Compose
// project and carries one of the states the caller asked for.
function exactReadback(value, deployment, states) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  if (String(value.provider || "") !== LOCAL_DOCKER_PROVIDER ||
      String(value.composeProject || "") !== deployment.composeProject) {
    return null;
  }
  return states.includes(String(value.state || "")) ? value : null;
}

// The transport is used exactly as the boundary produced it, except that its
// origin and session are checked here. Returning only `http` and `auth` is what
// keeps this adapter from handing an installation fact to the business flow.
//
// The origin check is a declaration check, not proof of where requests go: the
// boundary constructs the HTTP client, so the local qualification runner must
// pin that client to `deployment.platformOrigin` rather than trust a redirect or
// a client it did not build.
function ownerTransport(value, deployment) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  if (typeof value.http?.json !== "function" || typeof value.http?.request !== "function") return null;
  if (platformOrigin(value.origin) !== deployment.platformOrigin) return null;
  const auth = value.auth;
  if (!auth || typeof auth !== "object" || Array.isArray(auth)) return null;
  if (typeof auth.cookie !== "string" || !auth.cookie.trim() ||
      typeof auth.csrf !== "string" || !auth.csrf.trim()) {
    return null;
  }
  return { http: value.http, auth };
}

/*
 * Read the shared health word out of the local provider's own readiness facts.
 *
 * The mapping only narrows. `healthy` needs the provider to report a ready,
 * running, serving stack with no failed check; anything the provider itself
 * calls ready or degraded while running is `degraded`; absence, an unreadable
 * answer, another stack's answer and contradictions are `unknown`. Nothing here
 * can turn into a qualification state, and in particular a healthy stack cannot
 * make a failed business run pass.
 */
function deploymentHealth(readback, deployment) {
  const exact = exactReadback(readback, deployment, providerStates);
  if (!exact || typeof exact.running !== "boolean" || typeof exact.serviceReady !== "boolean" ||
      !Array.isArray(exact.failedChecks) || !exact.failedChecks.every((check) => typeof check === "string")) {
    return "unknown";
  }
  if (exact.state === "ready" && exact.running === true && exact.serviceReady === true &&
      exact.failedChecks.length === 0) {
    return "healthy";
  }
  return (exact.state === "ready" || exact.state === "degraded") && exact.running === true ? "degraded" : "unknown";
}

/*
 * Build the `local-docker` adapter the orchestrator drives.
 *
 * The returned object is the orchestrator's adapter shape and nothing more. It
 * owns three small facts for an operation key: whether the open was attempted,
 * the transport it may have received, and the close result. All three are keyed
 * by the shared operation identity, which is what makes a repeated open or
 * close idempotent without a stored qualification.
 */
export function createLocalDockerQualificationAdapter({ deployment, boundary } = {}) {
  const binding = localDockerDeployment(deployment);
  const provider = localDockerBoundary(boundary);
  const scopes = new Map();
  const attempts = new Map();
  const closings = new Map();

  const adapter = {
    provider: LOCAL_DOCKER_PROVIDER,

    // Bring the local stack up as the request's tenant and return the
    // authenticated owner transport. Absence and an unreadable stack are both a
    // blocked scope: neither has read a business fact, so neither may become a
    // product claim.
    //
    // The operation is recorded as attempted before the stack is asked for
    // anything, because an open can fail after Compose has already started
    // containers: a lost answer, a stack that came up but does not report
    // ready, or a login that fails after `up` succeeded. The orchestrator closes
    // every attempted scope, including one whose open it never received, so the
    // attempt has to outlive any failure here.
    async openBusinessScope({ request } = {}) {
      const operationKey = qualificationOperationKey(request, binding);
      const opened = scopes.get(operationKey);
      if (opened) return opened;
      // A new attempt invalidates an earlier close result for the same key.
      closings.delete(operationKey);
      attempts.set(operationKey, request);
      let stack;
      try {
        stack = await provider.up({ deployment: binding, request, operationKey });
      } catch {
        throw adapterError("provider_scope_unavailable");
      }
      const exactStack = exactReadback(stack, binding, ["ready", "absent", "unknown"]);
      if (!exactStack) throw adapterError("local_docker_readback_invalid");
      if (exactStack.state !== "ready") throw adapterError("provider_scope_unavailable");
      let readback;
      try {
        readback = await provider.login({ deployment: binding, request, operationKey });
      } catch {
        throw adapterError("provider_scope_unavailable");
      }
      const exactSession = exactReadback(readback, binding, ["authenticated", "absent", "unknown"]);
      if (!exactSession) throw adapterError("local_docker_readback_invalid");
      if (exactSession.state !== "authenticated") throw adapterError("provider_scope_unavailable");
      const transport = ownerTransport(exactSession.transport, binding);
      if (!transport) throw adapterError("local_docker_transport_invalid");
      scopes.set(operationKey, transport);
      return transport;
    },

    // Put back what this operation attempted. `released` and `absent` both say
    // the stack readback found nothing left; `unknown`, a lost release answer
    // or a readback naming another Compose project cannot say that, so the
    // scope is reported unproven rather than assumed restored.
    //
    // The release runs for an attempted open even when this adapter never
    // received a usable transport: Compose may have started containers whose
    // answer was lost or refused, and this is the only party that can ask the
    // local stack to put them back. A close with no attempt at all has nothing
    // to release and is honestly restored.
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
        readback = await provider.down({ deployment: binding, request: attemptedRequest, operationKey });
      } catch {
        readback = null;
      }
      const exact = exactReadback(readback, binding, ["released", "absent", "unknown"]);
      const closing = Object.freeze({ restored: exact?.state === "released" || exact?.state === "absent" });
      closings.set(operationKey, closing);
      return closing;
    },

    // Health is recorded beside the business result and never moves it. Every
    // failure to read it is `unknown`, including another stack's answer. This
    // asks the installation boundary, not the Console page.
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
