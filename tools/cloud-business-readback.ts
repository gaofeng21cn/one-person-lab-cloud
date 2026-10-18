import { createHash, randomUUID } from "node:crypto";
import { lookup } from "node:dns";
import { readFile, writeFile } from "node:fs/promises";
import { request as httpsRequest } from "node:https";
import { BlockList, isIP } from "node:net";
import { pathToFileURL } from "node:url";
import { Readable } from "node:stream";
import { candidateReceiptDigest, canonicalJson, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";

const CLOUD = "gaofeng21cn/one-person-lab-cloud";
const INSTANCE = "gaofeng21cn/opl-instance-medopl";
const WORKFLOW = ".github/workflows/cloud-business-readback.yml";
const SHA = /^[a-f0-9]{40}$/;
const DIGEST = /^sha256:[a-f0-9]{64}$/;
const ID = /^[1-9][0-9]*$/;
const MAX_BYTES = 1024 * 1024;
// Derived from the current original-owner AnnouncementMaxResponseBytes in
// packages/contracts/go/announcements.go: escaped Unicode must remain legal.
const MAX_ANNOUNCEMENT_BYTES = 50 * ((160 + 20_000) * 6 + 2048) + 4096;
const TIMEOUT_MS = 10_000;
// The four customer reads #840 names are asserted in the same session the
// Console uses: wallet, usage summary, fee records and announcements. Each check
// proves the Cloud-owned route answered without a session rejection.
const CHECKS = ["login", "session", "externalBindings", "wallet", "usage", "feeReceipts", "announcements", "logout", "sessionRevoked"] as const;
type Check = typeof CHECKS[number];
type CandidateIdentity = { receiptDigest: string; productSha: string; productTree: string; cloudImageRef: string; cloudImageDigest: string; cloudImageRevision: string; cloudRuntimeChildDigest: string };
type Authority = { repository: string; authoritySha: string; runId: string; runAttempt: string };
// Version 2 adds the usage and fee-record checks to the schema-validated check
// set (7 -> 9). A version 1 receipt is refused rather than reinterpreted: its
// schema does not prove those two reads, which is the gap version 2 closes.
export type CloudBusinessReadbackReceipt = {
  schemaVersion: 2; kind: "opl_cloud_business_readback"; scope: "customer_reads";
  status: "customer_reads_verified" | "blocked";
  fullBusinessQualification: false; workspaceLifecycleVerified: false; keyLifecycleVerified: false;
  installationBinding: "receipt_association_only"; publicEndpointCandidateVerified: false;
  candidate: CandidateIdentity; installation: Authority & { receiptDigest: string };
  producer: Authority & { workflowPath: string; actor: string; triggeringActor: string };
  originDigest: string; startedAt: string; completedAt: string;
  checks: Record<Check, boolean>; errorCode: "none" | "customer_readback_failed" | "session_cleanup_failed";
};
function fail(): never { throw new Error("cloud_business_readback_invalid"); }
function obj(value: unknown): Record<string, any> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Record<string, any>;
}
function exact(value: unknown, keys: readonly string[]) {
  const out = obj(value);
  if (Object.keys(out).length !== keys.length || keys.some(k => !Object.hasOwn(out, k))) fail();
  return out;
}
const hash = (value: string) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
const text = (v: unknown): v is string => typeof v === "string" && v.length > 0 && v.length <= 1024;
// int64String accepts only the canonical public encoding of a non-negative int64:
// a decimal string, never a JSON number that would lose precision above 2^53.
const int64String = (v: unknown): v is string => typeof v === "string" && /^(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= 9223372036854775807n;
// Money fields are micro-USD integers; a stored refund amount is non-negative and
// a signed projection may carry the sign, so both spellings are accepted.
const signedMicros = (v: unknown): v is string => typeof v === "string" && /^-?(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= 9223372036854775807n && BigInt(v) >= -9223372036854775808n;
const date = (v: unknown): v is string => typeof v === "string" && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(v) && Number.isFinite(Date.parse(v));
function identity(value: unknown): CandidateIdentity {
  const candidate = validateCloudCandidateReceipt(value);
  const product = obj(candidate.product), image = obj(candidate.cloudImage);
  return { receiptDigest: candidateReceiptDigest(candidate), productSha: product.sha, productTree: product.tree,
    cloudImageRef: image.indexRef, cloudImageDigest: image.indexDigest, cloudImageRevision: image.revision,
    cloudRuntimeChildDigest: image.platforms.find((v: any) => v.platform === "linux/amd64").digest };
}
function authority(value: unknown, repository: string, path: string, completed: boolean, requireMainBranch: boolean): Authority {
  const r = obj(value);
  // A Cloud-owned producer is a replaceable evidence input, so its dispatch
  // branch must not be pinned to main; the receipt binds the exact source commit
  // instead. An Instance-owned run deploys only from canonical main, so it keeps
  // the main-dispatched requirement.
  if (obj(r.repository).full_name !== repository || r.path !== path || r.event !== "workflow_dispatch" ||
    (requireMainBranch && r.head_branch !== "main") || !SHA.test(r.head_sha) || !ID.test(String(r.id)) || !ID.test(String(r.run_attempt)) ||
    (completed && (r.status !== "completed" || r.conclusion !== "success"))) fail();
  return { repository, authoritySha: r.head_sha, runId: String(r.id), runAttempt: String(r.run_attempt) };
}
// The Instance native validator must run at its exact producing revision before
// this binding. This only associates the supplied receipt, never attests that
// the independently approved public origin currently serves these Candidate bytes.
function installationIdentity(candidate: unknown, installationText: string, installationRun: unknown) {
  let d: Record<string, any>;
  try { d = obj(JSON.parse(installationText)); } catch { fail(); }
  const expected = identity(candidate);
  const source = authority(installationRun, INSTANCE, ".github/workflows/deployment-verify.yml", true, true);
  if (d.schemaVersion !== 2 || d.status !== "deployment_verified" || d.operationMode !== "deployment_verification" ||
      d.environment !== "production" || d.stage !== "complete" || d.errorCode !== "none") fail();
  const { receiptDigest, ...fields } = expected;
  if (d.candidateReceiptDigest !== receiptDigest || Object.entries(fields).some(([k,v]) => d[k] !== v)) fail();
  return { ...source, receiptDigest: hash(installationText) };
}
// The protected business qualification producer consumes the same original
// Instance deployment evidence. Exporting the binding keeps the exact receipt
// and run provenance checks in this one validator instead of a second decoder.
export function validateCloudInstallationIdentity(candidate: unknown, installationText: string, installationRun: unknown) {
  return installationIdentity(candidate, installationText, installationRun);
}
export function validateCloudBusinessReadbackReceipt(value: unknown): CloudBusinessReadbackReceipt {
  const r = exact(value, ["schemaVersion", "kind", "scope", "status", "fullBusinessQualification", "workspaceLifecycleVerified", "keyLifecycleVerified", "installationBinding", "publicEndpointCandidateVerified", "candidate", "installation", "producer", "originDigest", "startedAt", "completedAt", "checks", "errorCode"]);
  const c = exact(r.candidate, ["receiptDigest", "productSha", "productTree", "cloudImageRef", "cloudImageDigest", "cloudImageRevision", "cloudRuntimeChildDigest"]);
  const i = exact(r.installation, ["repository", "authoritySha", "runId", "runAttempt", "receiptDigest"]);
  const p = exact(r.producer, ["repository", "authoritySha", "runId", "runAttempt", "workflowPath", "actor", "triggeringActor"]);
  const checks = exact(r.checks, CHECKS);
  if (r.schemaVersion !== 2 || r.kind !== "opl_cloud_business_readback" || r.scope !== "customer_reads" ||
    !["customer_reads_verified", "blocked"].includes(r.status) || r.fullBusinessQualification !== false ||
    r.workspaceLifecycleVerified !== false || r.keyLifecycleVerified !== false ||
    r.installationBinding !== "receipt_association_only" || r.publicEndpointCandidateVerified !== false || !DIGEST.test(r.originDigest) ||
    !date(r.startedAt) || !date(r.completedAt) || Date.parse(r.completedAt) < Date.parse(r.startedAt) ||
    !SHA.test(c.productSha) || !SHA.test(c.productTree) || c.cloudImageRevision !== c.productSha ||
    !DIGEST.test(c.receiptDigest) || !DIGEST.test(c.cloudImageDigest) || !DIGEST.test(c.cloudRuntimeChildDigest) ||
    c.cloudImageRef !== `ghcr.io/${CLOUD}@${c.cloudImageDigest}` || !DIGEST.test(i.receiptDigest) ||
    i.repository !== INSTANCE || p.repository !== CLOUD || p.workflowPath !== WORKFLOW ||
    [i,p].some(a => !SHA.test(a.authoritySha) || typeof a.runId !== "string" || !ID.test(a.runId) || typeof a.runAttempt !== "string" || !ID.test(a.runAttempt)) ||
    !["gaofeng21cn", "RenDeHuang"].includes(p.actor) || p.triggeringActor !== p.actor ||
    CHECKS.some(k => typeof checks[k] !== "boolean")) fail();
  const all = CHECKS.every(k => checks[k] === true);
  if ((r.status === "customer_reads_verified" && (!all || r.errorCode !== "none")) ||
      (r.status === "blocked" && (all || !["customer_readback_failed", "session_cleanup_failed"].includes(r.errorCode)))) fail();
  return r as CloudBusinessReadbackReceipt;
}
export function validateCloudBusinessReadbackBinding(input: {
  candidate: unknown; installationText: string; installationRun: unknown; receiptText: string; run: unknown;
}) {
  let receipt: CloudBusinessReadbackReceipt;
  try { receipt = validateCloudBusinessReadbackReceipt(JSON.parse(input.receiptText)); } catch { fail(); }
  const installation = installationIdentity(input.candidate, input.installationText, input.installationRun);
  const run = authority(input.run, CLOUD, WORKFLOW, true, false), raw = obj(input.run);
  if (receipt.status !== "customer_reads_verified" || canonicalJson(receipt.candidate) !== canonicalJson(identity(input.candidate)) ||
    canonicalJson(receipt.installation) !== canonicalJson(installation) ||
    Object.entries(run).some(([k,v]) => obj(receipt.producer)[k] !== v) ||
    obj(raw.actor).login !== receipt.producer.actor || obj(raw.triggering_actor).login !== receipt.producer.triggeringActor) fail();
  return { receiptDigest: hash(input.receiptText), installationReceiptDigest: installation.receiptDigest,
    authoritySha: run.authoritySha, runId: run.runId, runAttempt: run.runAttempt, status: receipt.status };
}

// All resolved addresses must be public; the validated address is passed directly
// to the TLS socket lookup so a second DNS lookup cannot rebind to a private host.
const blocked = new BlockList();
for (const [ip, bits] of [["0.0.0.0",8],["10.0.0.0",8],["100.64.0.0",10],["127.0.0.0",8],["169.254.0.0",16],["172.16.0.0",12],["192.0.0.0",24],["192.0.2.0",24],["192.168.0.0",16],["198.18.0.0",15],["198.51.100.0",24],["203.0.113.0",24],["224.0.0.0",3]] as const) blocked.addSubnet(ip,bits,"ipv4");
const global6 = new BlockList(); global6.addSubnet("2000::",3,"ipv6");
const blocked6 = new BlockList(); blocked6.addSubnet("2001::",23,"ipv6"); blocked6.addSubnet("2002::",16,"ipv6"); blocked6.addSubnet("2001:db8::",32,"ipv6"); blocked6.addSubnet("3fff::",20,"ipv6");
export function isPublicReadbackAddress(address: string) {
  return isIP(address) === 4 ? !blocked.check(address,"ipv4") : isIP(address) === 6 && global6.check(address,"ipv6") && !blocked6.check(address,"ipv6");
}
function publicOrigin(value: string) {
  const u = new URL(value);
  if (u.protocol !== "https:" || u.username || u.password || u.search || u.hash || u.pathname !== "/" ||
      (u.port && u.port !== "443") || !u.hostname.includes(".") ||
      (isIP(u.hostname.replace(/^\[|\]$/g,"")) && !isPublicReadbackAddress(u.hostname.replace(/^\[|\]$/g,""))) ||
      /(?:^|\.)(?:localhost|local|internal|invalid|test)$/.test(u.hostname)) fail();
  return u.origin;
}
type Fetch = (url: string, init: RequestInit) => Promise<Response>;
const publicFetch: Fetch = (url, init) => new Promise((resolve, reject) => {
  const req = httpsRequest(url, { method: init.method || "GET", headers: Object.fromEntries(new Headers(init.headers)), signal: init.signal || undefined,
    lookup(host, _opts, callback) {
      lookup(host, { all: true, verbatim: true }, (error, addresses) => {
        if (error || !addresses?.length || addresses.some(a => !isPublicReadbackAddress(a.address))) { callback(new Error("cloud_business_readback_network_rejected"), []); return; }
        // Node may request an all-address result for automatic family selection.
        if (typeof _opts === "object" && _opts.all) callback(null, addresses);
        else callback(null, addresses[0].address, addresses[0].family);
      });
    },
  }, res => {
    const headers = new Headers();
    for (let n = 0; n < res.rawHeaders.length; n += 2) headers.append(res.rawHeaders[n], res.rawHeaders[n + 1]);
    // Deliver headers before consuming the body. A session may already exist
    // when a truncated/oversized login body fails; finally must still revoke it.
    const body = [204, 304].includes(res.statusCode!) ? null : Readable.toWeb(res);
    resolve(new Response(body as ReadableStream<Uint8Array> | null, { status: res.statusCode, headers }));
  });
  req.on("error", reject); if (init.body) req.write(init.body); req.end();
});

export async function runCloudBusinessReadback(input: { candidate: unknown; installationText: string; installationRun: unknown }, dependencies: {
  environment?: Record<string, string | undefined>; fetch?: Fetch;
} = {}): Promise<CloudBusinessReadbackReceipt> {
  const env = dependencies.environment || process.env;
  // Configuration and identity failures happen before any network request.
  let origin: string, installation: CloudBusinessReadbackReceipt["installation"], candidate: CandidateIdentity;
  try { origin = publicOrigin(env.OPL_QUALIFICATION_ORIGIN || ""); candidate = identity(input.candidate); installation = installationIdentity(input.candidate,input.installationText,input.installationRun); } catch { fail(); }
  const producer = { repository: env.GITHUB_REPOSITORY || "", authoritySha: env.GITHUB_SHA || "", runId: env.GITHUB_RUN_ID || "", runAttempt: env.GITHUB_RUN_ATTEMPT || "", workflowPath: WORKFLOW, actor: env.GITHUB_ACTOR || "", triggeringActor: env.GITHUB_TRIGGERING_ACTOR || "" };
  const checks = Object.fromEntries(CHECKS.map(k => [k,false])) as Record<Check,boolean>;
  const receipt: CloudBusinessReadbackReceipt = { schemaVersion: 2, kind: "opl_cloud_business_readback", scope: "customer_reads", status: "blocked", fullBusinessQualification: false, workspaceLifecycleVerified: false, keyLifecycleVerified: false, installationBinding: "receipt_association_only", publicEndpointCandidateVerified: false, candidate, installation, producer, originDigest: hash(origin), startedAt: new Date().toISOString(), completedAt: new Date().toISOString(), checks, errorCode: "customer_readback_failed" };
  validateCloudBusinessReadbackReceipt(receipt);
  if (!text(env.OPL_QUALIFICATION_CUSTOMER_EMAIL) || !text(env.OPL_QUALIFICATION_CUSTOMER_PASSWORD)) fail();
  const transport = dependencies.fetch || publicFetch;
  const jar = new Map<string,string>();
  let csrf = "";
  const request = async (path: string, method = "GET", body?: unknown, token?: string, expected = 200) => {
    const maxBytes = path.startsWith("/api/v2/announcements?") ? MAX_ANNOUNCEMENT_BYTES : MAX_BYTES;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const timeout = new Promise<never>((_,reject) => { timer = setTimeout(() => { controller.abort(); reject(new Error("cloud_business_readback_timeout")); },TIMEOUT_MS); });
    try { return await Promise.race([timeout, (async () => {
      const headers = new Headers({ accept: "application/json" });
      if (jar.size) headers.set("Cookie", [...jar].map(([k,v]) => `${k}=${v}`).join("; "));
      if (method === "POST") { headers.set("Origin",origin); headers.set("X-CSRF-Token",token || ""); headers.set("Idempotency-Key",randomUUID()); }
      if (body !== undefined) headers.set("Content-Type","application/json");
      const response = await transport(origin+path, { method, headers, redirect: "error", signal: controller.signal, ...(body !== undefined ? {body: JSON.stringify(body)} : {}) });
      // Capture any issued session before decoding so malformed login JSON still
      // executes the logout cleanup in finally.
      for (const cookie of response.headers.getSetCookie()) {
        const match = /^(opl_login_context|opl_login_csrf|opl_session)=([^;]*)/.exec(cookie);
        if (match) { if (/^[A-Za-z0-9_\-.~%+/=]*$/.test(match[2]) && match[2].length <= 4096) jar.set(match[1],match[2]); else fail(); }
      }
      if (response.status !== expected || response.redirected || (response.url && new URL(response.url).origin !== origin)) fail();
      if (Number(response.headers.get("content-length")) > maxBytes) fail();
      if (expected === 204 || expected === 401) { await response.body?.cancel(); return null; }
      const reader = response.body?.getReader(); if (!reader) fail();
      let bytes=0; const chunks: Uint8Array[]=[];
      try { for (;;) { const item=await reader.read(); if (item.done) break; bytes+=item.value.length; if(bytes>maxBytes) fail(); chunks.push(item.value); } } finally { await reader.cancel(); }
      if (!(response.headers.get("content-type") || "").toLowerCase().startsWith("application/json")) fail();
      return obj(JSON.parse(Buffer.concat(chunks).toString("utf8")));
    })()]); } finally { clearTimeout(timer!); controller.abort(); }
  };
  try {
    const context = (await request("/api/v2/auth/context"))!;
    if (!text(context.csrfToken) || !date(context.expiresAt) || Date.parse(context.expiresAt) <= Date.now() || !jar.get("opl_login_context") || !jar.get("opl_login_csrf")) fail();
    const session = (await request("/api/v2/auth/login","POST", {username: env.OPL_QUALIFICATION_CUSTOMER_EMAIL,password: env.OPL_QUALIFICATION_CUSTOMER_PASSWORD}, context.csrfToken))!;
    if (text(session.csrfToken)) csrf=session.csrfToken;
    if (!jar.get("opl_session") || !text(session.actorId) || !csrf || !date(session.expiresAt) || Date.parse(session.expiresAt) <= Date.now()) fail();
    checks.login=true;
    const current=(await request("/api/v2/auth/session"))!;
    if (text(current.csrfToken)) csrf=current.csrfToken;
    if(current.actorId!==session.actorId || !csrf || !date(current.expiresAt) || Date.parse(current.expiresAt)<=Date.now()) fail();
    checks.session=true;
    const binding=(await request("/api/v2/auth/external-bindings"))!;
    if(binding.actorId!==current.actorId || (typeof binding.sub2apiUserId !== "string" || !ID.test(binding.sub2apiUserId)) || binding.status!=="active" || binding.verification!=="confirmed" || !text(binding.registryHost) || !text(binding.registryNamespace) || !text(binding.repository)) fail();
    checks.externalBindings=true;
    const wallet=(await request("/api/v2/wallet"))!;
    if(wallet.source!=="gateway" || wallet.status!=="available" || wallet.currency!=="USD" || typeof wallet.balanceUSDMicros!=="string" || !/^(0|[1-9][0-9]{0,18})$/.test(wallet.balanceUSDMicros) || BigInt(wallet.balanceUSDMicros)>9223372036854775807n || !date(wallet.fetchedAt)) fail();
    checks.wallet=true;
    // The usage summary is the Gateway owner's own aggregate. The public contract
    // encodes every count and the micro-USD cost as an int64 decimal string, so a
    // number or a fractional value is a contract break rather than a read. This
    // proves the route, the owner encoding and a non-negative aggregate; it does
    // not prove the totals belong to a particular user beyond the session scope,
    // and it does not assert that totalTokens equals the sum of the token fields
    // because the owner does not declare that relation.
    const usage=(await request("/api/v2/usage/summary?period=month"))!;
    for (const field of ["totalRequests","totalInputTokens","totalOutputTokens","totalTokens","totalActualCostUSDMicros"]) {
      if (!int64String(usage[field])) fail();
    }
    checks.usage=true;
    // The fee records stay with the original financial owner behind the Cloud
    // route, and the read must answer that owner's `ledger` envelope with a
    // bounded page and canonical micro-USD strings. The check does not prove
    // completeness or that every projected field (workspaceId, createdAt,
    // periodStart, ...) is present. A 404 is not accepted as a read: the BFF
    // writes one generic owner-not-found both for a customer with no original
    // Account and for a missing or older internal route (its client turns any
    // owner HTTP 404 into gRPC NotFound), so a 404 cannot show that the fee route
    // works, and the Console renders it as an error rather than an empty bill.
    const feePage=obj((await request("/api/v2/legacy-billing/receipts?limit=20"))!);
    if (feePage.source!=="ledger" || feePage.available!==true || !["available","empty"].includes(feePage.status) || !date(feePage.fetchedAt)) fail();
    const data=obj(feePage.data);
    if (!Array.isArray(data.receipts) || data.receipts.length>20 || typeof data.hasMore!=="boolean" || typeof data.nextCursor!=="string" || (data.hasMore && !data.nextCursor)) fail();
    const seen=new Set<string>();
    for (const item of data.receipts) {
      const receiptRow=obj(item);
      if (!text(receiptRow.receiptId) || seen.has(receiptRow.receiptId) || !text(receiptRow.type) || !text(receiptRow.status) || (receiptRow.currency!==undefined && receiptRow.currency!=="USD")) fail();
      seen.add(receiptRow.receiptId);
      for (const field of ["chargeUsdMicros","totalUsdMicros","refundUsdMicros"]) {
        if (Object.hasOwn(receiptRow,field) && !signedMicros(receiptRow[field])) fail();
      }
    }
    checks.feeReceipts=true;
    const announcements=(await request("/api/v2/announcements?page=1&pageSize=20"))!;
    const page=obj(announcements.data);
    if(announcements.source!=="control-plane" || announcements.available!==true || !["available","empty"].includes(announcements.status) || !date(announcements.fetchedAt) || page.page!==1 || page.pageSize!==20 || !Number.isSafeInteger(page.total) || page.total<0 || !Array.isArray(page.items) || page.items.length>20 || page.items.length>page.total || page.items.some((item: unknown) => { const a=obj(item); return !text(a.id) || !text(a.title) || typeof a.body!=="string" || a.status!=="published" || typeof a.read!=="boolean" || !date(a.createdAt) || !date(a.updatedAt); })) fail();
    checks.announcements=true;
  } catch { receipt.errorCode="customer_readback_failed"; }
  finally {
    if (jar.get("opl_session")) {
      try {
        if (!csrf) { const current=(await request("/api/v2/auth/session"))!; if(text(current.csrfToken)) csrf=current.csrfToken; }
        if(!csrf) fail();
        const original=jar.get("opl_session")!;
        await request("/api/v2/auth/logout","POST",undefined,csrf,204); checks.logout=true;
        // Check revocation with the original cookie, not an empty browser jar.
        jar.set("opl_session",original);
        await request("/api/v2/auth/session","GET",undefined,undefined,401); checks.sessionRevoked=true;
      } catch { receipt.errorCode="session_cleanup_failed"; }
    }
    jar.clear(); csrf="";
  }
  if(CHECKS.every(k=>checks[k])) { receipt.status="customer_reads_verified"; receipt.errorCode="none"; }
  receipt.completedAt=new Date().toISOString();
  return validateCloudBusinessReadbackReceipt(receipt);
}

export async function main(args=process.argv.slice(2)) {
  if(args[0]!=="run" || args.length!==9) fail();
  const options=new Map<string,string>();
  for(let n=1;n<args.length;n+=2) { if(!["--candidate","--installation","--installation-run","--output"].includes(args[n]) || options.has(args[n]) || !args[n+1]) fail(); options.set(args[n],args[n+1]); }
  let receipt: CloudBusinessReadbackReceipt;
  try { receipt=await runCloudBusinessReadback({candidate:JSON.parse(await readFile(options.get("--candidate")!,"utf8")),installationText:await readFile(options.get("--installation")!,"utf8"),installationRun:JSON.parse(await readFile(options.get("--installation-run")!,"utf8"))}); } catch { fail(); }
  await writeFile(options.get("--output")!,JSON.stringify(receipt,null,2)+"\n",{mode:0o600,flag:"wx"});
  if(receipt.status!=="customer_reads_verified") throw new Error("cloud_business_readback_blocked");
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) main().catch(()=> { console.error("cloud_business_readback_failed"); process.exitCode=1; });
