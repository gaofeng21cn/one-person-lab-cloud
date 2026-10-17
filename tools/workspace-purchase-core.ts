/*
 * The Workspace purchase sequence, and nothing else.
 *
 * This is the invariant that every buyer shares. The Console drives it from a
 * browser, the local qualification harness drives it against a stack it built
 * itself, and a deployed entry point drives it against a running product. The
 * sequence is the same in all three, so it lives here once and is parameterized
 * by the origin its caller hands it.
 *
 * What is deliberately NOT here: pricing a quote, deciding whether the wallet
 * can fund it, provisioning an account, cleaning up afterwards, or asserting
 * anything about the result. Those differ per authority profile, so each caller
 * keeps its own and composes this one.
 *
 * The sequence is the Control Plane's own: the platform prices the launch and
 * refuses a client that supplies a size or a price, so the command body carries
 * the Workspace name, the declared package and the renewal choice and nothing
 * else. Exactly one command is issued per call, however long the poll runs.
 */

const LAUNCH_POLL_ATTEMPTS = 180;
const LAUNCH_POLL_INTERVAL_MS = 1_000;
// The platform's own published terminal and stopping words. A delivery is done
// only when it says so; the rest are either in progress or a breakpoint the
// platform published, and neither may be read as an end.
const LAUNCH_SUCCEEDED = "succeeded";
const LAUNCH_SUCCEEDED_STAGE = "succeeded";
const LAUNCH_STOPPED = ["manual_review", "failed", "refunded"];

const defaultWait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));

// The admitted order followed through its own declared stage order until it
// reports a terminal success. A bounded window, and an order that never reaches
// one is a refusal rather than an infinite poll.
export async function waitForLaunch(http, operationId, auth, wait = defaultWait) {
  let launch;
  for (let attempt = 0; attempt < LAUNCH_POLL_ATTEMPTS; attempt += 1) {
    launch = (await http.json(`/api/workspace-launches/${encodeURIComponent(operationId)}`, {}, auth)).payload;
    if (launch?.status === LAUNCH_SUCCEEDED && launch?.phase === LAUNCH_SUCCEEDED_STAGE) return launch;
    if (LAUNCH_STOPPED.includes(String(launch?.status || ""))) {
      throw new Error(`Workspace launch stopped at ${launch.status}/${launch.phase}/${launch.errorCode || "none"}`);
    }
    await wait(LAUNCH_POLL_INTERVAL_MS);
  }
  throw new Error(`Workspace launch did not reach succeeded within ${LAUNCH_POLL_ATTEMPTS} seconds`);
}

/*
 * Issue exactly one launch command and follow it to a terminal success.
 *
 * The caller declares the identity the command must come back with, so an
 * answer belonging to some other order is refused instead of being polled. The
 * returned receipt identity is what the caller binds its own evidence to; this
 * function records it and never interprets it.
 */
export async function submitWorkspaceLaunch({
  http,
  auth,
  name,
  packageId = "basic",
  autoRenew = false,
  idempotencyKey,
  expectedOperationId,
  expectedWorkspaceId,
  wait,
}) {
  const initial = (await http.json("/api/workspace-launches", {
    method: "POST", headers: { "idempotency-key": idempotencyKey },
    body: { name, packageId, autoRenew }
  }, auth, [202])).payload;
  if (initial?.operationId !== expectedOperationId || initial?.workspaceId !== expectedWorkspaceId) {
    throw new Error("deterministic launch identity is invalid");
  }
  const launch = await waitForLaunch(http, expectedOperationId, auth, wait);
  const receiptId = String(launch?.receiptId || "");
  if (!receiptId) throw new Error("terminal launch receipt identity is missing");
  return { initial, launch, receiptId };
}