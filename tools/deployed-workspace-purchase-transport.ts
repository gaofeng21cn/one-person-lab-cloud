/*
 * The local transport and the money gate for the deployed Workspace purchase.
 *
 * `deployed-workspace-purchase` is the only automated buyer that may reach a real
 * customer, and it deliberately holds no credential and no HTTP client: its
 * caller boundary expects an already-authenticated transport so that the login
 * and the secret live with whoever owns the session, never with the purchase
 * logic. That leaves exactly this hole. This module fills it, and nothing else.
 *
 * It fills it with what the repository already has. The HTTP shape the purchase
 * entry expects is `createHTTP`, and the login is `login`; both are the local
 * harness's, and the purchase entry already reads the harness's `sourceData`
 * envelope, so reusing them is following the established dependency rather than
 * opening a second one. A second HTTP implementation would be the real risk:
 * two of them drift, and the drift would show up as a purchase refusal in
 * production rather than as a failing test.
 *
 * There is no protected environment, no cross-repository evidence token and no
 * provider boundary module here, and there is deliberately no place to configure
 * one. All three answer a question this entry does not ask. A protected
 * environment gates who may trigger a spend on a hosted CI runner, and this runs
 * on the operator's own machine where the caller is the operator. An evidence
 * token exists to read another repository's Actions artifacts across a trust
 * boundary, and the candidate digest this run binds is handed to it on the same
 * command line. A provider boundary module owns TKE reachability for a run that
 * deploys a Candidate, and this run deploys nothing: it buys from a product that
 * is already deployed, which is why the purchase path never referenced one.
 *
 * What this module does add is the gate the three mechanisms were standing in
 * for. Money is separated from everything else by splitting the run in two. The
 * `quote` command spends nothing: it reads the account, reads the wallet and
 * asks the platform for a price, and it prints the exact amount. The `purchase`
 * command then requires the operator to pass that exact amount back as
 * `--authorize-amount-usd-micros`. An authorization is therefore a number
 * someone chose after seeing the number, not a flag someone typed without
 * reading anything. If the platform's price moved between the two commands the
 * gate refuses rather than spending a different amount than the one approved,
 * and because the purchase is addressed by a deterministic idempotency key, a
 * refusal costs one HTTP read and no money.
 *
 * Nothing here widens what the purchase entry already does. It buys one
 * Workspace, it does not refund, delete, provision an account or assert that the
 * delivered Workspace is good, and the receipt it writes is the purchase entry's
 * own receipt validated by its own validator.
 */

import { pathToFileURL } from "node:url";

import { createHTTP, login, sourceData } from "./local-workspace-qualification.ts";
import {
  deployedAccountIdentityDigest,
  deployedPurchaseRequestFromEnvironment,
  runDeployedWorkspacePurchase,
  validateDeployedPurchaseReceipt,
} from "./deployed-workspace-purchase.ts";

// The credential names. They are read from the caller's environment and never
// written, logged, echoed into a summary or placed in the receipt; the purchase
// entry's own receipt validator is what guarantees the receipt carries none of
// them, and this module never adds a field that could.
export const PURCHASE_CUSTOMER_EMAIL = "OPL_QUALIFICATION_USER_EMAIL";
export const PURCHASE_CUSTOMER_PASSWORD = "OPL_QUALIFICATION_USER_PASSWORD";
// The account id is a verification input, not a secret and not part of the
// purchase request: the request's declared keys are closed, and the account the
// session belongs to is something the platform answers, not something the caller
// gets to assert. It is passed to `login` so a session for another account is
// refused before a single route is read.
export const PURCHASE_ACCOUNT_ID = "OPL_DEPLOYED_PURCHASE_ACCOUNT_ID";

const POSITIVE_MICROS = /^[1-9][0-9]*$/;
const REQUEST_TIMEOUT_MS = 30_000;
export const PURCHASE_TRANSPORT_FAILED = "deployed_purchase_transport_failed";

function transportError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

const invalidInput = (message) => transportError("deployed_purchase_transport_invalid", message);

/*
 * The credentials, read by name and refused rather than defaulted.
 *
 * A buyer that defaulted an email would log in as whoever it could and price a
 * Workspace for that account instead of the declared one, so an absent
 * credential is an error here and not an empty string passed on.
 */
function purchaseCredentials(environment) {
  const email = String(environment[PURCHASE_CUSTOMER_EMAIL] || "").trim();
  const password = String(environment[PURCHASE_CUSTOMER_PASSWORD] || "");
  const accountId = String(environment[PURCHASE_ACCOUNT_ID] || "").trim();
  if (!email || !password || !accountId) {
    throw invalidInput("deployed purchase transport requires the customer email, password and account id");
  }
  return { email, password, accountId };
}

/*
 * Log in and hand back the caller the purchase entry expects.
 *
 * The origin is the declared request's own origin, taken by the entry's own
 * `assertDeployedPurchaseTarget` rather than parsed again here: a transport that
 * computed its own base could disagree with the target the request was refused
 * or accepted against, and the entry compares the two.
 */
export async function deployedPurchaseCaller(request, environment = process.env, dependencies = {}) {
  const { email, password, accountId } = purchaseCredentials(environment);
  const origin = new URL(String(request.baseURL || "")).origin;
  const create = dependencies.createHTTP || createHTTP;
  const openSession = dependencies.login || login;
  const http = create(origin);
  const auth = await openSession(http, email, password, accountId);
  return { origin, http, auth };
}

/*
 * Price the declared purchase without spending anything.
 *
 * This is a preflight of the purchase, not a cheaper version of it, so it
 * repeats the purchase entry's own identity fence: the account is read through
 * the same sub2api source envelope and refused unless its email digest is the
 * declared one. A quote for an account the purchase would have refused is not a
 * useful thing to show an operator before asking them to approve a number.
 *
 * It also re-reads the wallet, so the operator sees the balance the purchase
 * would have checked against rather than a price that may be unaffordable.
 */
export async function quoteDeployedWorkspacePurchase(caller, request) {
  const { http, auth } = caller;
  const me = sourceData((await http.request("/api/auth/me", {}, auth)).payload, "sub2api");
  const accountId = String(me?.accountId || "");
  if (!accountId || me?.role !== "owner" || me?.status !== "active") {
    throw transportError("deployed_purchase_account_unavailable", "could not read the declared customer account");
  }
  if (deployedAccountIdentityDigest(me?.email) !== request.accountEmailIdentityDigest) {
    throw transportError("deployed_purchase_account_mismatch", "declared customer account digest does not match the session");
  }
  const walletUsdMicros = String(sourceData((await http.request("/api/gateway/wallet", {}, auth)).payload, "sub2api")?.usdMicros || "");
  const pricing = (await http.json("/api/pricing/preview", {
    method: "POST", body: { resourceType: "workspace", packageId: request.packageId },
  }, auth)).payload;

  // A quote that does not name the declared thing is refused outright: there is
  // no number to show, because the number would be about something else.
  const amountUsdMicros = String(pricing?.totalChargeUsdMicros || "");
  if (pricing?.resourceType !== "workspace" || pricing?.packageId !== request.packageId ||
      pricing?.currency !== "USD" || !POSITIVE_MICROS.test(amountUsdMicros)) {
    throw transportError("deployed_purchase_quote_unavailable", "could not price the declared Workspace");
  }

  // An unaffordable quote is reported, not thrown. The shared core refuses a
  // buyer that cannot fund the quote, and the purchase path still does exactly
  // that; but this command spends nothing, and the operator's next question is
  // the number itself — whether to top the wallet up or buy a different package
  // is a decision only they can make. Throwing here would hide the very figure
  // the decision needs, and it would hide it on the one call that could have
  // told them.
  const walletReadable = /^(?:0|[1-9][0-9]*)$/.test(walletUsdMicros);
  const fundable = walletReadable && BigInt(walletUsdMicros) >= BigInt(amountUsdMicros);
  return { amountUsdMicros, walletUsdMicros, fundable, accountId, packageId: request.packageId, baseURL: request.baseURL };
}

/*
 * The authorization gate.
 *
 * The operator passes the exact micros amount they approved. It is compared to
 * the price the platform is quoting right now, so this is a check against the
 * live price and not against a number recorded earlier: an approval is an
 * approval of an amount, and an amount that has changed is not the approved one.
 * A refusal is deliberately silent about whether an order exists, because a
 * refusal here has spent nothing and there is nothing to reconcile.
 */
function assertAuthorizedAmount(quoted, authorized) {
  // An unaffordable quote is refused here, before the purchase entry is entered,
  // so the operator gets the price and the balance rather than the shared core's
  // refusal from inside a run that had already authenticated and read the order.
  if (!quoted.fundable) {
    throw transportError(
      "deployed_purchase_wallet_cannot_fund",
      `purchase refused: wallet holds ${quoted.walletUsdMicros} micros and the quote is ${quoted.amountUsdMicros} micros`,
    );
  }
  const approved = String(authorized || "").trim();
  if (!POSITIVE_MICROS.test(approved)) {
    throw invalidInput("purchase requires --authorize-amount-usd-micros as an exact micros amount");
  }
  if (approved !== quoted.amountUsdMicros) {
    throw transportError(
      "deployed_purchase_amount_not_authorized",
      `purchase refused: approved ${approved} micros does not match the quoted ${quoted.amountUsdMicros} micros`,
    );
  }
  return approved;
}

const QUOTE_OPTIONS = ["--output"];
const PURCHASE_OPTIONS = ["--output", "--authorize-amount-usd-micros"];

function parseOptions(args, allowed) {
  const options = new Map();
  for (let index = 1; index < args.length; index += 2) {
    const name = args[index];
    const value = args[index + 1];
    if (!(allowed as readonly string[]).includes(name) || options.has(name) ||
        typeof value !== "string" || value.trim() === "") {
      throw invalidInput(`deployed purchase arguments are invalid: ${String(name)}`);
    }
    options.set(name, value);
  }
  return options;
}

export function parseDeployedPurchaseTransportArgs(args) {
  const command = args[0];
  if (command !== "quote" && command !== "purchase") {
    throw invalidInput("expected the quote or purchase command");
  }
  const options = parseOptions(args, command === "quote" ? QUOTE_OPTIONS : PURCHASE_OPTIONS);
  const authorized = options.get("--authorize-amount-usd-micros");
  if (command === "purchase" && authorized === undefined) {
    throw invalidInput("purchase requires --authorize-amount-usd-micros");
  }
  if (command === "quote" && authorized !== undefined) {
    throw invalidInput("quote takes no --authorize-amount-usd-micros");
  }
  return Object.freeze({
    command,
    outputPath: options.get("--output") as string,
    authorizedAmountUsdMicros: authorized as string | undefined,
  });
}

/*
 * The entry point.
 *
 * `quote` returns the amount and the account it is for, and spends nothing.
 * `purchase` re-quotes, checks the operator's number against it, runs the
 * purchase entry once, and writes the receipt the entry itself produced after
 * the entry's own validator accepted it. The summary is the operator's record of
 * what happened and carries the amount, the two platform-minted identities and
 * the receipt digest; it carries no credential, session, raw account identifier
 * or access URL, which is the same bar the receipt validator holds the receipt
 * to.
 */
export async function runDeployedPurchaseTransport(input, environment = process.env, dependencies = {}) {
  const request = deployedPurchaseRequestFromEnvironment(environment);
  const caller = await deployedPurchaseCaller(request, environment, dependencies);
  const quote = await quoteDeployedWorkspacePurchase(caller, request);
  if (input.command === "quote") {
    return { command: "quote", spent: false, quote };
  }
  assertAuthorizedAmount(quote, input.authorizedAmountUsdMicros);
  const receipt = validateDeployedPurchaseReceipt(await runDeployedWorkspacePurchase(request, caller));
  if (input.outputPath && dependencies.writeReceipt) {
    await dependencies.writeReceipt(input.outputPath, receipt);
  }
  return { command: "purchase", spent: true, quote, receipt };
}

export function deployedPurchaseTransportSummary(result) {
  const summary = {
    command: result.command,
    spent: result.spent,
    baseURL: result.quote.baseURL,
    accountId: result.quote.accountId,
    packageId: result.quote.packageId,
    amountUsdMicros: result.quote.amountUsdMicros,
    walletUsdMicros: result.quote.walletUsdMicros,
    fundable: result.quote.fundable,
  };
  if (!result.receipt) return summary;
  return {
    ...summary,
    candidateReceiptDigest: result.receipt.candidate.receiptDigest,
    operationId: result.receipt.operation.operationId,
    workspaceId: result.receipt.operation.workspaceId,
    purchaseReceiptId: result.receipt.operation.purchaseReceiptId,
    resumed: result.receipt.operation.resumed,
    admittedAmountUsdMicros: result.receipt.purchase.amountUsdMicros,
    purchases: result.receipt.mutations.observed.purchases,
  };
}

export async function main(
  args: readonly string[] = process.argv.slice(2),
  environment: Record<string, string | undefined> = process.env,
  dependencies: Record<string, any> = {},
) {
  const input = parseDeployedPurchaseTransportArgs(args);
  const result = await runDeployedPurchaseTransport(input, environment, {
    ...dependencies,
    writeReceipt: dependencies.writeReceipt || defaultWriteReceipt,
  });
  process.stdout.write(`${JSON.stringify(deployedPurchaseTransportSummary(result))}\n`);
  if (result.command === "quote") {
    throw transportError(
      "deployed_purchase_awaiting_authorization",
      `quote is ${result.quote.amountUsdMicros} micros for account ${result.quote.accountId} (wallet ${result.quote.walletUsdMicros}, fundable ${result.quote.fundable}); nothing was spent`,
    );
  }
}

async function defaultWriteReceipt(outputPath, receipt) {
  const { writeFile } = await import("node:fs/promises");
  await writeFile(outputPath, `${JSON.stringify(receipt, null, 2)}\n`, { mode: 0o600 });
}

// The timeout the transport shares with the harness's own HTTP. It is named here
// so the value is visible to a reader of this module; `createHTTP` owns the
// behaviour.
export const PURCHASE_REQUEST_TIMEOUT_MS = REQUEST_TIMEOUT_MS;

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error: unknown) => {
    const code = String((error as { code?: string } | null)?.code || PURCHASE_TRANSPORT_FAILED);
    const message = String((error as { message?: string } | null)?.message || "deployed purchase transport failed");
    process.stderr.write(`${code}: ${message}\n`);
    process.exitCode = 1;
  });
}