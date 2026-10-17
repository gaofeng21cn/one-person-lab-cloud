import { createHash } from "node:crypto";

/*
 * The Workspace purchase sequence, the quote policy every buyer shares, and the
 * platform's identifier derivation. Nothing else.
 *
 * This is the invariant that every buyer shares. The Console drives it from a
 * browser, the local qualification harness drives it against a stack it built
 * itself, and a deployed buyer has to drive it against a running product. The
 * sequence is the same in all three, so it lives here once and is parameterized
 * by the origin its caller hands it.
 *
 * Beyond the sequence this module owns the two facts every buyer has to agree
 * with the platform on: the quote policy, and the identifier derivation. Neither
 * is a per-profile choice. A buyer that priced a different resource, package or
 * currency would act on a price that names something it never declared, and a
 * buyer that derived an identity differently would refuse the platform's own
 * correct answer.
 *
 * What is deliberately NOT here: provisioning an account, cleaning up
 * afterwards, or asserting anything about the delivered result. Those differ per
 * authority profile, so each caller keeps its own and composes this one.
 *
 * The sequence is the Control Plane's own: the platform prices the launch and
 * refuses a client that supplies a size or a price, so the command body carries
 * the Workspace name, the declared package, the renewal choice and the
 * provisioning shape of a new customer Launch and nothing else. Exactly one
 * command is issued per call, however long the poll runs.
 */

const LAUNCH_POLL_ATTEMPTS = 180;
const LAUNCH_POLL_INTERVAL_MS = 1_000;
// The platform's own published terminal and stopping words. A delivery is done
// only when it says so; the rest are either in progress or a breakpoint the
// platform published, and neither may be read as an end.
const LAUNCH_SUCCEEDED = "succeeded";
const LAUNCH_SUCCEEDED_STAGE = "succeeded";
const LAUNCH_STOPPED = ["manual_review", "failed", "refunded"];
const PRICE_MICROS = /^[1-9][0-9]*$/;

const defaultWait = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));

/*
 * The platform's own identifier derivation, byte for byte.
 *
 * The Control Plane mints an operation and a Workspace id from a SHA-1 over the
 * parts it names, writing a zero byte after every part including the last, and
 * taking the first eighteen hex characters. This is the one place the derivation
 * is written; a test pins its output to the platform's own literals so a drift
 * fails here rather than as a buyer refusing a correct answer.
 */
export function stableID(...parts) {
  const hash = createHash("sha1");
  for (const part of parts) {
    hash.update(String(part));
    hash.update(Buffer.from([0]));
  }
  return hash.digest("hex");
}

// The identities the platform derives for one declared command. A buyer
// announces both before it issues anything, so an answer belonging to another
// order is refused instead of being followed.
export const launchOperationId = (accountID, idempotencyKey) => `workspace-launch-${stableID(accountID, idempotencyKey).slice(0, 18)}`;
export const launchWorkspaceId = (accountID, operationId) => `ws-${stableID("workspace-launch-v2", accountID, operationId).slice(0, 18)}`;

/*
 * One quote policy for every buyer.
 *
 * The platform prices the launch, so a buyer never computes a price. It has to
 * decide whether the quote it was handed names the thing it declared and whether
 * the wallet can fund it. A quote that does not name the declared resource type,
 * the declared package and USD is not a price this purchase may act on, and a
 * wallet that cannot answer is not a large balance.
 */
export function priceWorkspaceLaunch({ pricing, walletUsdMicros, packageId = "basic" }) {
  const amountUsdMicros = String(pricing?.totalChargeUsdMicros || "");
  if (pricing?.resourceType !== "workspace" || pricing?.packageId !== packageId || pricing?.currency !== "USD" ||
      !PRICE_MICROS.test(amountUsdMicros)) {
    throw new Error("workspace launch quote is invalid");
  }
  const walletMicros = String(walletUsdMicros || "");
  if (!PRICE_MICROS.test(walletMicros) || BigInt(walletMicros) < BigInt(amountUsdMicros)) {
    throw new Error("workspace launch wallet cannot fund the quote");
  }
  return amountUsdMicros;
}

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
 *
 * The command states the provisioning shape every new customer Launch uses:
 * `full` commits the default OPL application installation beside the
 * resource Launch. Omitting it is the platform's implicit default today, and
 * `resource_only` is the retained historical contract that a new Launch must
 * not send, so the shape is stated rather than left to that default.
 */
export async function submitWorkspaceLaunch({
  http,
  auth,
  name,
  packageId = "basic",
  autoRenew = false,
  provisioningMode = "full",
  idempotencyKey,
  expectedOperationId,
  expectedWorkspaceId,
  wait,
}) {
  const initial = (await http.json("/api/workspace-launches", {
    method: "POST", headers: { "idempotency-key": idempotencyKey },
    body: { name, packageId, autoRenew, provisioningMode }
  }, auth, [202])).payload;
  if (initial?.operationId !== expectedOperationId || initial?.workspaceId !== expectedWorkspaceId) {
    throw new Error("deterministic launch identity is invalid");
  }
  const launch = await waitForLaunch(http, expectedOperationId, auth, wait);
  const receiptId = String(launch?.receiptId || "");
  if (!receiptId) throw new Error("terminal launch receipt identity is missing");
  return { initial, launch, receiptId };
}