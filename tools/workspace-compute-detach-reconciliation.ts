import { createHmac, createHash } from "node:crypto";
import { canonicalJson, validateCloudCandidateReceipt } from "./cloud-candidate-receipt.ts";
import { workspaceComputeBindingSql, decodeWorkspaceComputeBinding, fabricComputeBindingSql, decodeFabricComputeBinding } from "./workspace-compute-readback-binding.ts";
import { computeDestroyJournalSql, decodeComputeDestroyJournal } from "./workspace-compute-destroy-journal.ts";

export const COMPUTE_DETACH_RECONCILIATION_CONFIRMATION = "RECONCILE_ORIGINAL_PREPAID_DETACH";
const ACTION = "detach_prepaid_compute_allocation";
const TOKEN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$(?![\s\S])/;
const HASH = /^[a-f0-9]{64}$(?![\s\S])/;
const fail = (): never => { throw new Error("workspace_compute_detach_reconciliation_refused"); };
const digest = (domain: string, value: string) => `sha256:${createHash("sha256").update(`${domain}\0${value}`).digest("hex")}`;
export type DetachReconciliationAccess = {
  select(owner: "workspace" | "fabric" | "fabric_domain", sql: string): Promise<string[]>;
  withFabric<T>(image: { indexRef: string; indexDigest: string; platforms: Array<{platform: string; digest: string}> },
    consume: (session: {post(path: string, body: string, signCapability: (key: string) => Record<string,string>): Promise<{status: number; body: unknown}>}) => Promise<T>): Promise<{pin: unknown; value: T}>;
};

// An explicit operator intent is separate from the original uncertain dispatch.
// Cloud resolves every target from the original Workspace and Fabric journals;
// the protected Instance adapter only transports the one signed owner command.
export async function runWorkspaceComputeDetachReconciliation(input: {
  candidate: unknown; workspaceId: string; workspaceOperationId: string; authorizationReference: string; confirmation: string;
}, access: DetachReconciliationAccess) {
  if (!input || Object.keys(input).sort().join() !== ["candidate","workspaceId","workspaceOperationId","authorizationReference","confirmation"].sort().join() ||
      input.confirmation !== COMPUTE_DETACH_RECONCILIATION_CONFIRMATION || !TOKEN.test(input.authorizationReference)) fail();
  const candidate = validateCloudCandidateReceipt(input.candidate);
  const workspace = decodeWorkspaceComputeBinding(await access.select("workspace",workspaceComputeBindingSql(input.workspaceId,input.workspaceOperationId)),input.workspaceId,input.workspaceOperationId);
  const binding = decodeFabricComputeBinding(await access.select("fabric",fabricComputeBindingSql(workspace)), workspace);
  const result = await access.withFabric(candidate.cloudImage, async session => {
    const rows = await access.select("fabric_domain",computeDestroyJournalSql(binding.computeId));
    const journal = decodeComputeDestroyJournal(rows,{computeId:binding.computeId,workspaceId:binding.workspaceId});
    if (journal.status !== "observed" || !journal.destroyOperationIdDigest) fail();
    const raw = rows.map(row => JSON.parse(row) as Record<string,unknown>);
    const destroy = raw.filter(row => row.action === "destroy_compute_allocation");
    const stages = raw.filter(row => row.action === ACTION);
    if (destroy.length===0 || stages.length!==2 || new Set(stages.map(row=>row.operationId)).size!==1) fail();
    const started = stages.find(row=>row.status==="started"), failed = stages.find(row=>row.status==="failed");
    const original = destroy[0];
    if (!started || !failed || typeof original.operationId!=="string" || !TOKEN.test(original.operationId) ||
        digest("cloud-launch-readback:operation:v1",original.operationId)!==journal.destroyOperationIdDigest ||
        stages.some(row=>row.originalDestroyOperationId!==original.operationId || row.accountId!==original.accountId || row.workspaceId!==binding.workspaceId || row.resourceId!==binding.computeId ||
          row.requestHash!==started.requestHash || row.operationId!==started.operationId || row.startedAt!==started.startedAt) ||
        typeof original.accountId!=="string" || !TOKEN.test(original.accountId) || typeof started.operationId!=="string" || !TOKEN.test(started.operationId) ||
        typeof started.requestHash!=="string" || !HASH.test(started.requestHash) || !failed.finishedAt ||
        !/^(?:compute_prepaid_detach_dispatch_unconfirmed|compute_prepaid_retirement_unconfirmed)(?::(?:AuthFailure|InvalidParameterValue|UnsupportedOperation|RequestLimitExceeded|InternalError|ResourceNotFound))?$/u.test(String(failed.errorCode))) fail();
    const command = { accountId:original.accountId, workspaceId:binding.workspaceId, destroyOperationId:original.operationId,
      detachOperationId:started.operationId,detachRequestHash:started.requestHash,authorizationReference:input.authorizationReference,confirmation:input.confirmation };
    const body=canonicalJson(command);
    const path=`/fabric/compute-allocations/${binding.computeId}/reconcile-prepaid-detach`;
    const response=await session.post(path,body,key=>{
      if (typeof key!=="string" || key.length<32) fail();
      const claims={version:1,caller:"operator",accountId:command.accountId,workspaceId:command.workspaceId,resourceKind:"compute_allocation",resourceId:binding.computeId,
        action:"reconcile_prepaid_compute_detachment",operationId:command.authorizationReference,expiresAt:Math.floor(Date.now()/1000)+60,
        bodySha256:createHash("sha256").update(body).digest("hex")};
      const payload=Buffer.from(JSON.stringify(claims)).toString("base64url");
      const signature=createHmac("sha256",key).update(payload).digest("base64url");
      return {"content-type":"application/json","Idempotency-Key":command.authorizationReference,"X-OPL-Fabric-Capability":`${payload}.${signature}`};
    });
    if (response.status!==202 || !response.body || typeof response.body!=="object" || Array.isArray(response.body)) fail();
    const out=response.body as Record<string,unknown>;
    if (Object.keys(out).sort().join()!==["operationId","dispatchAttempted","machineAbsent"].sort().join() || typeof out.operationId!=="string" || typeof out.dispatchAttempted!=="boolean" || typeof out.machineAbsent!=="boolean") fail();
    if (out.operationId !== "" && !TOKEN.test(out.operationId) || out.dispatchAttempted && out.operationId === "" || !out.dispatchAttempted && !out.machineAbsent) fail();
    return {operationId:out.operationId,dispatchAttempted:out.dispatchAttempted,machineAbsent:out.machineAbsent};
  });
  return result.value;
}
