/*
 * Real native qualification against the retained CP-only owner-API fixture.
 * The Go test provides real CP/Ledger HTTP processes over isolated PostgreSQL,
 * with synthetic Sub2API/Fabric boundaries. The real orchestrator must refuse
 * the missing CloudIdentity session before any business mutation. Candidate,
 * J0 and provider reachability are explicit test fixtures, never READY evidence.
 */

import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";

import { createQualificationRequest } from "../../../../../tools/workspace-qualification-contract.ts";
import { runWorkspaceQualification } from "../../../../../tools/workspace-qualification-orchestrator.ts";

const SOURCE_SHA = "a".repeat(40);
const SOURCE_TREE = "b".repeat(40);

function syntheticCandidateReceipt() {
  return {
    schemaVersion: 2,
    kind: "opl_cloud_candidate",
    product: { repository: "gaofeng21cn/one-person-lab-cloud", sha: SOURCE_SHA, tree: SOURCE_TREE },
    cloudImage: {
      repository: "ghcr.io/gaofeng21cn/one-person-lab-cloud",
      indexRef: `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:${"1".repeat(64)}`,
      indexDigest: `sha256:${"1".repeat(64)}`,
      revision: SOURCE_SHA,
      platforms: [
        { platform: "linux/amd64", digest: `sha256:${"2".repeat(64)}` },
        { platform: "linux/arm64", digest: `sha256:${"3".repeat(64)}` },
      ],
    },
    assets: [
      "compose.yaml",
      "compose.deployment-platform-owned.yaml",
      "compose.deployment-managed-tke.yaml",
      "compose.deployment-customer-owned.yaml",
      "compose.fabric-local-docker.yaml",
      "compose.fabric-tencent-tke.yaml",
      "compose.local-workspace.yaml",
      "opl-cloud.env.example",
      "opl-cloud-owner-topology.json",
    ].map((name) => ({ name, sha256: "4".repeat(64) })),
    provenance: {
      workflowRepository: "gaofeng21cn/one-person-lab-cloud",
      workflowSha: SOURCE_SHA,
      workflowRunId: "1",
      workflowRunAttempt: "1",
    },
  };
}

function syntheticJ0ReadyReceipt(provider) {
  return {
    schemaVersion: 1,
    kind: "opl.workspace-qualification.j0-ready.v2",
    status: "READY",
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, cloudImageDigest: syntheticCandidateReceipt().cloudImage.indexDigest, clean: true },
    authority: { mode: "live", class: "sandbox", dedicated: true, confirmed: true },
    provider,
    failed: 0,
    skipped: 0,
  };
}

function required(name) {
  const value = String(process.env[name] || "").trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}

/*
 * The owner-API transport handed to the orchestrator by `openBusinessScope`.
 * It pins every request to the one origin the Go test started, adds the
 * session and CSRF header the Control Plane's protected routes require, and
 * returns the `{ status, payload }` shape the orchestrator reads.
 */
function ownerTransport(origin, auth) {
  let target;
  try {
    target = new URL(origin);
  } catch {
    throw new Error("owner API origin is invalid");
  }
  if (target.protocol !== "http:" && target.protocol !== "https:") {
    throw new Error("owner API origin must be HTTP(S)");
  }
  const send = target.protocol === "https:" ? httpsRequest : httpRequest;
  const requests = [];
  const responses = [];

  function execute(path, method, extraHeaders, body) {
    const url = new URL(path, target);
    if (url.origin !== target.origin) throw new Error("owner API transport escaped its origin");
    const headers = {
      cookie: auth.cookie,
      ...(method === "GET" || method === "HEAD" ? {} : { "x-opl-csrf": auth.csrf }),
      ...extraHeaders,
    };
    if (body !== undefined) headers["content-type"] = "application/json";
    requests.push(`${method} ${url.pathname}${url.search}`);
    const requestLabel = `${method} ${url.pathname}${url.search}`;
    return new Promise((resolve, reject) => {
      const request = send({
        protocol: url.protocol,
        hostname: url.hostname,
        port: url.port || undefined,
        path: url.pathname + url.search,
        method,
        headers,
        rejectUnauthorized: false,
        timeout: 15_000,
      }, (response) => {
        const chunks = [];
        response.on("data", (chunk) => chunks.push(chunk));
        response.on("end", () => {
          const text = Buffer.concat(chunks).toString("utf8");
          let payload = null;
          try {
            payload = text ? JSON.parse(text) : null;
          } catch {
            payload = null;
          }
          const status = Number(response.statusCode || 0);
          if (status < 200 || status >= 300) responses.push({ request: requestLabel, status, payload });
          resolve({ status, payload });
        });
      });
      request.on("timeout", () => request.destroy(new Error("owner API request timed out")));
      request.on("error", reject);
      if (body !== undefined) request.write(body);
      request.end();
    });
  }

  function normalizedHeaders(init) {
    const headers = {};
    for (const [name, value] of Object.entries(init?.headers || {})) headers[String(name).toLowerCase()] = String(value);
    return headers;
  }

  return {
    http: {
      // The transport is bound to the one session this run declared. The auth
      // value the orchestrator threads through is the same session by
      // construction, so only its presence was checked when the scope opened.
      async json(path, init = {}) {
        const body = init.body === undefined ? undefined : JSON.stringify(init.body);
        return execute(path, String(init.method || "GET").toUpperCase(), normalizedHeaders(init), body);
      },
      async request(path, init = {}) {
        const body = init.body === undefined ? undefined : JSON.stringify(init.body);
        const result = await execute(path, String(init.method || "GET").toUpperCase(), normalizedHeaders(init), body);
        return { response: { status: result.status }, payload: result.payload };
      },
    },
    requests,
    responses,
  };
}

async function main() {
  const origin = required("OPL_QUALIFICATION_OWNER_API_ORIGIN");
  const accountId = required("OPL_QUALIFICATION_OWNER_API_ACCOUNT_ID");
  const cookie = required("OPL_QUALIFICATION_OWNER_API_COOKIE");
  const csrf = required("OPL_QUALIFICATION_OWNER_API_CSRF");
  const provider = required("OPL_QUALIFICATION_OWNER_API_PROVIDER");
  const pollMilliseconds = Math.max(0, Math.min(50, Number(process.env.OPL_QUALIFICATION_OWNER_API_POLL_MS || 1) || 0));

  const request = createQualificationRequest({
    candidate: syntheticCandidateReceipt(),
    accountId,
    tenantId: "tenant-owner-api-declared",
    billingSubjectId: "payer-owner-api-declared",
    product: {
      resourceType: "workspace",
      applicationSelection: { kind: "opl_app", runtimeVersionId: "runtime-owner-api-declared" },
      computePlanId: "compute-owner-api-declared",
      storagePlanId: "storage-owner-api-declared",
      autoRenew: false,
    },
    provider,
    scope: { purchases: 1, charges: 1, refunds: 0, workspaceDeletions: 0, managedKeys: 0 },
    authority: { mode: "live", authorityClass: "sandbox", j0ReadyReceipt: syntheticJ0ReadyReceipt(provider) },
    source: { sha: SOURCE_SHA, tree: SOURCE_TREE, clean: true },
  });

  const transport = ownerTransport(origin, { cookie, csrf });
  // The declared fake adapter: reachability only. It returns the real owner-API
  // transport; every business answer still comes from the real owner routes.
  const adapter = Object.freeze({
    provider,
    async openBusinessScope() {
      return { http: transport.http, auth: { cookie, csrf } };
    },
    async closeBusinessScope() {
      return { restored: true };
    },
    async readDeploymentHealth() {
      return "healthy";
    },
  });

  const wait = (milliseconds) => new Promise((resolve) => setTimeout(resolve, Math.min(pollMilliseconds, milliseconds)));
  let result;
  try {
    result = await runWorkspaceQualification({ request, adapter }, { wait });
  } catch (error) {
    process.stderr.write(`${JSON.stringify({
      error: String(error?.stack || error?.message || error),
      requests: transport.requests,
      responses: transport.responses,
    })}\n`);
    process.exitCode = 1;
    return;
  }
  process.stdout.write(`${JSON.stringify({
    state: result.state,
    reason: result.reason,
    receipt: result.receipt,
    requests: transport.requests,
  })}\n`);
}

main().catch((error) => {
  process.stderr.write(`${String(error?.stack || error?.message || error)}\n`);
  process.exit(1);
});
