import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { createServer, request as httpRequest } from "node:http";
import https from "node:https";
import { syncBuiltinESMExports } from "node:module";
import { candidateReceiptDigest } from "../../tools/cloud-candidate-receipt.ts";
import { isPublicReadbackAddress, runCloudBusinessReadback, validateCloudBusinessReadbackBinding, validateCloudBusinessReadbackReceipt } from "../../tools/cloud-business-readback.ts";

const digest=(s:string)=>`sha256:${s.repeat(64)}`;
const sha="a".repeat(40), instanceSha="f".repeat(40);
const now=new Date().toISOString(), future=new Date(Date.now()+3600000).toISOString();
const jsonResponse=(body:unknown)=>new Response(JSON.stringify(body),{headers:{"content-type":"application/json"}});
async function inputs() {
  const contract=JSON.parse(await readFile("packages/contracts/opl-cloud-candidate-receipt-contract.json","utf8"));
  const candidate={schemaVersion:2,kind:"opl_cloud_candidate",product:{repository:"gaofeng21cn/one-person-lab-cloud",sha,tree:"b".repeat(40)},cloudImage:{repository:"ghcr.io/gaofeng21cn/one-person-lab-cloud",indexRef:`ghcr.io/gaofeng21cn/one-person-lab-cloud@${digest("c")}`,indexDigest:digest("c"),revision:sha,platforms:[{platform:"linux/amd64",digest:digest("d")},{platform:"linux/arm64",digest:digest("e")}]},assets:contract.candidateReceiptV2.portableAssets.map((name:string)=>({name,sha256:"1".repeat(64)})),provenance:{workflowRepository:"gaofeng21cn/one-person-lab-cloud",workflowSha:sha,workflowRunId:"11",workflowRunAttempt:"1"}};
  const installation={schemaVersion:2,operationMode:"deployment_verification",environment:"production",status:"deployment_verified",stage:"complete",errorCode:"none",candidateReceiptDigest:candidateReceiptDigest(candidate),productSha:sha,productTree:candidate.product.tree,cloudImageRef:candidate.cloudImage.indexRef,cloudImageDigest:digest("c"),cloudImageRevision:sha,cloudRuntimeChildDigest:digest("d")};
  const installationRun={id:22,run_attempt:1,repository:{full_name:"gaofeng21cn/opl-instance-medopl"},path:".github/workflows/deployment-verify.yml",event:"workflow_dispatch",head_branch:"main",head_sha:instanceSha,status:"completed",conclusion:"success"};
  const run={...installationRun,id:33,run_attempt:2,repository:{full_name:"gaofeng21cn/one-person-lab-cloud"},path:".github/workflows/cloud-business-readback.yml",head_sha:sha,actor:{login:"RenDeHuang"},triggering_actor:{login:"RenDeHuang"}};
  const environment={OPL_QUALIFICATION_ORIGIN:"https://cloud.example.com",OPL_QUALIFICATION_CUSTOMER_EMAIL:"sensitive@example.com",OPL_QUALIFICATION_CUSTOMER_PASSWORD:"never-output-password",GITHUB_REPOSITORY:run.repository.full_name,GITHUB_SHA:sha,GITHUB_RUN_ID:"33",GITHUB_RUN_ATTEMPT:"2",GITHUB_ACTOR:"RenDeHuang",GITHUB_TRIGGERING_ACTOR:"RenDeHuang"};
  return {candidate,installationText:JSON.stringify(installation)+"\n",installationRun,run,environment};
}
function network(patch:Record<string,(init:RequestInit)=>Response|Promise<Response>>={}) {
  const calls:{path:string;init:RequestInit}[]=[]; let revoked=false;
  const json=(body:unknown,cookies:string[]=[])=>{const headers=new Headers({"content-type":"application/json"});for(const c of cookies)headers.append("set-cookie",c);return new Response(JSON.stringify(body),{headers});};
  const session={actorId:"actor-sensitive",displayName:"Sensitive Customer",csrfToken:"secret-session-csrf",expiresAt:future,permissions:["getWallet"]};
  const fetch=async(url:string,init:RequestInit)=>{
    const path=new URL(url).pathname+new URL(url).search; calls.push({path,init});
    assert.equal(init.redirect,"error"); assert.ok(init.signal); assert.equal(new URL(url).origin,"https://cloud.example.com");
    if(patch[path])return patch[path](init);
    const headers=new Headers(init.headers);
    if(path==="/api/v2/auth/context")return json({csrfToken:"secret-context",expiresAt:future},["opl_login_context=challenge; Secure; HttpOnly; Path=/api/v2/auth","opl_login_csrf=challenge-hash; Secure; HttpOnly; Path=/api/v2/auth"]);
    if(path==="/api/v2/auth/login"){
      assert.equal(init.method,"POST");assert.equal(headers.get("origin"),"https://cloud.example.com");assert.equal(headers.get("x-csrf-token"),"secret-context");assert.ok(headers.get("idempotency-key"));
      assert.match(headers.get("cookie")!,/opl_login_context=challenge/);assert.deepEqual(JSON.parse(init.body as string),{username:"sensitive@example.com",password:"never-output-password"});
      return json(session,["opl_session=secret-session-cookie; Path=/; HttpOnly; Secure"]);
    }
    assert.match(headers.get("cookie")!,/opl_session=secret-session-cookie/);
    if(path==="/api/v2/auth/session")return revoked?new Response(null,{status:401}):json(session);
    if(path==="/api/v2/auth/external-bindings")return json({actorId:session.actorId,sub2apiUserId:"101",registryHost:"registry.example.com",registryNamespace:"namespace",repository:"customer-repository",status:"active",verification:"confirmed"});
    if(path==="/api/v2/wallet")return json({source:"gateway",status:"available",currency:"USD",balanceUSDMicros:"0",fetchedAt:now});
    if(path==="/api/v2/usage/summary?period=month")return json({totalRequests:"17",totalInputTokens:"170",totalOutputTokens:"34",totalTokens:"204",totalActualCostUSDMicros:"125001"});
    if(path==="/api/v2/legacy-billing/receipts?limit=20")return json({source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[{receiptId:"receipt-1",type:"billing.workspace.monthly",status:"confirmed",workspaceId:"workspace-1",createdAt:now,currency:"USD",chargeUsdMicros:"12500000"}],nextCursor:"",hasMore:false}});
    if(path==="/api/v2/announcements?page=1&pageSize=20")return json({source:"control-plane",available:true,status:"empty",fetchedAt:now,data:{items:[],page:1,pageSize:20,total:0}});
    if(path==="/api/v2/auth/logout") {assert.equal(init.method,"POST");assert.equal(headers.get("x-csrf-token"),session.csrfToken);revoked=true;return new Response(null,{status:204});}
    throw new Error("unexpected request");
  };
  return {fetch,calls,json,session};
}
test("customer producer executes challenge login and public reads, cleans up and binds exact artifacts",async()=>{
  const input=await inputs(), n=network();const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
  assert.equal(receipt.status,"customer_reads_verified"); assert.ok(Object.values(receipt.checks).every(v=>v===true));
  assert.equal(receipt.checks.usage,true); assert.equal(receipt.checks.feeReceipts,true);
  assert.deepEqual(n.calls.map(v=>v.path),["/api/v2/auth/context","/api/v2/auth/login","/api/v2/auth/session","/api/v2/auth/external-bindings","/api/v2/wallet","/api/v2/usage/summary?period=month","/api/v2/legacy-billing/receipts?limit=20","/api/v2/announcements?page=1&pageSize=20","/api/v2/auth/logout","/api/v2/auth/session"]);
  const receiptText=JSON.stringify(receipt)+"\n";
  assert.doesNotMatch(receiptText,/sensitive|Sensitive|password|secret-|customer-repository|101|example\.com/);
  assert.deepEqual(validateCloudBusinessReadbackBinding({...input,receiptText}),{receiptDigest:`sha256:${createHash("sha256").update(receiptText).digest("hex")}`,installationReceiptDigest:receipt.installation.receiptDigest,authoritySha:sha,runId:"33",runAttempt:"2",status:"customer_reads_verified"});
});
test("read failures and mismatched actors block, with original session cleanup",async()=>{
  for(const endpoint of ["/api/v2/auth/session","/api/v2/auth/external-bindings","/api/v2/wallet","/api/v2/announcements?page=1&pageSize=20"]){
    const input=await inputs();let count=0;const n=network({[endpoint]:()=>{count++;return new Response(JSON.stringify({actorId:"foreign-actor",csrfToken:"secret-session-csrf",expiresAt:future}),{headers:{"content-type":"application/json"}});}});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});assert.equal(receipt.status,"blocked");assert.ok(n.calls.some(v=>v.path==="/api/v2/auth/logout"));assert.ok(count>0);
    assert.throws(()=>validateCloudBusinessReadbackBinding({...input,receiptText:JSON.stringify(receipt)}));
  }
});
test("redirect, oversized and transport failures never leak response or errors and still logout",async()=>{
  for(const response of [()=>new Response(null,{status:302,headers:{location:"http://127.0.0.1/credentials"}}),()=>new Response("x".repeat(1024*1024+1)),()=>{throw new Error("secret-password-sensitive@example.com");}]){
    const input=await inputs(),n=network({"/api/v2/wallet":response});const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});assert.equal(receipt.status,"blocked");assert.equal(receipt.checks.logout,true);assert.equal(receipt.checks.sessionRevoked,true);assert.doesNotMatch(JSON.stringify(receipt),/secret-password|sensitive@|credentials/);
  }
});
test("malformed login JSON still revokes issued session, using session readback for cleanup CSRF",async()=>{
  const input=await inputs();const n=network({"/api/v2/auth/login":()=>new Response("{",{headers:{"content-type":"application/json","set-cookie":"opl_session=secret-session-cookie; Secure; HttpOnly"}})});
  const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});assert.equal(receipt.status,"blocked");assert.equal(receipt.checks.logout,true);assert.equal(receipt.checks.sessionRevoked,true);
});
test("logout failure or a still-live original session blocks successful customer reads",async()=>{
  for(const patch of [{"/api/v2/auth/logout":()=>new Response(null,{status:503})},{"/api/v2/auth/logout":()=>new Response(null,{status:204})}]){
    const input=await inputs(),n=network(patch);const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});assert.equal(receipt.status,"blocked");assert.equal(receipt.errorCode,"session_cleanup_failed");assert.equal(receipt.checks.usage,true);assert.equal(receipt.checks.feeReceipts,true);assert.equal(receipt.checks.announcements,true);
  }
});
test("private origins and altered workflow identity fail before networking",async()=>{
  for(const patch of [{OPL_QUALIFICATION_ORIGIN:"http://cloud.example.com"},{OPL_QUALIFICATION_ORIGIN:"https://127.0.0.1"},{OPL_QUALIFICATION_ORIGIN:"https://10.0.0.1"},{OPL_QUALIFICATION_ORIGIN:"https://user:password@cloud.example.com"},{OPL_QUALIFICATION_ORIGIN:"https://cloud.example.com/path"},{GITHUB_TRIGGERING_ACTOR:"outsider"},{GITHUB_REPOSITORY:"someone/fork"}]){
    const input=await inputs();let calls=0;await assert.rejects(runCloudBusinessReadback(input,{environment:{...input.environment,...patch},fetch:async()=>{calls++;throw Error("must not run");}}),/^Error: cloud_business_readback_invalid$/);assert.equal(calls,0);
  }
  for(const ip of ["127.0.0.1","10.1.2.3","169.254.169.254","100.64.0.1","::1","::ffff:127.0.0.1","fc00::1","fe80::1","2001:db8::1","2002:7f00:1::1"])assert.equal(isPublicReadbackAddress(ip),false,ip);
  for(const ip of ["8.8.8.8","1.1.1.1","2606:4700:4700::1111"])assert.equal(isPublicReadbackAddress(ip),true,ip);
});
test("a session rejected with 401 on the usage or fee route cannot verify the customer journey",async()=>{
  // The pre-#840 shape read only wallet and announcements, so a fixture that
  // rejected the usage-summary and fee-record routes with 401 was never called
  // and the producer still claimed customer_reads_verified. Both reads now run in
  // the same session. Each 401 deliberately carries a body that would satisfy the
  // route's shape check, so only the status gate can block it: a producer that
  // parsed a 401 body instead of rejecting the status would verify the journey.
  const rejectedBody:Record<string,unknown>={"/api/v2/usage/summary?period=month":{totalRequests:"17",totalInputTokens:"170",totalOutputTokens:"34",totalTokens:"204",totalActualCostUSDMicros:"125001"},"/api/v2/legacy-billing/receipts?limit=20":{source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[{receiptId:"receipt-1",type:"billing.workspace.monthly",status:"confirmed",workspaceId:"workspace-1",createdAt:now,currency:"USD",chargeUsdMicros:"12500000"}],nextCursor:"",hasMore:false}}};
  for(const [endpoint,body] of Object.entries(rejectedBody)){
    const input=await inputs();
    const n=network({[endpoint]:()=>new Response(JSON.stringify(body),{status:401,headers:{"content-type":"application/json"}})});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
    assert.equal(receipt.status,"blocked");assert.equal(receipt.errorCode,"customer_readback_failed");
    assert.ok(n.calls.some(v=>v.path===endpoint),endpoint);
    assert.equal(receipt.checks.logout,true);assert.equal(receipt.checks.sessionRevoked,true);
    assert.throws(()=>validateCloudBusinessReadbackBinding({...input,receiptText:JSON.stringify(receipt)}));
  }
});
test("a 200 answer that is not the route's declared shape also blocks the journey",async()=>{
  // A 200 error body (or any other shape drift) must not be mistaken for a read,
  // and this case is kept separate from the 401 case so neither gate hides the
  // other: the status gate is exercised above, the shape gate here.
  for(const endpoint of ["/api/v2/usage/summary?period=month","/api/v2/legacy-billing/receipts?limit=20"]){
    const input=await inputs();
    const n=network({[endpoint]:()=>jsonResponse({code:"UNAUTHENTICATED",message:"session required",requestId:"request-1"})});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
    assert.equal(receipt.status,"blocked");assert.equal(receipt.errorCode,"customer_readback_failed");
    assert.ok(n.calls.some(v=>v.path===endpoint),endpoint);
    assert.equal(receipt.checks.logout,true);assert.equal(receipt.checks.sessionRevoked,true);
  }
});
test("a fee-record 404 answer is not a read and blocks the journey",async()=>{
  // The BFF writes one generic owner-not-found for a customer with no original
  // Account and for a missing or older internal route, so a 404 cannot prove the
  // fee route works. Both the BFF's actual fallback code and a bare NOT_FOUND
  // body must block; the Console would render either as an error.
  for(const code of ["owner_request_failed","NOT_FOUND"]){
    const input=await inputs();
    const n=network({"/api/v2/legacy-billing/receipts?limit=20":()=>new Response(JSON.stringify({code,message:"publisher request could not be completed",requestId:"request-1"}),{status:404,headers:{"content-type":"application/json"}})});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
    assert.equal(receipt.status,"blocked");assert.equal(receipt.errorCode,"customer_readback_failed");
    assert.equal(receipt.checks.feeReceipts,false);
    assert.ok(n.calls.some(v=>v.path==="/api/v2/legacy-billing/receipts?limit=20"));
  }
});
test("fee records must come from the ledger owner with canonical money and one identity per row",async()=>{
  for(const body of [
    {source:"control_plane",status:"available",available:true,fetchedAt:now,data:{receipts:[],nextCursor:"",hasMore:false}},
    {source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[{receiptId:"a",type:"billing.workspace.monthly",status:"confirmed"},{receiptId:"a",type:"billing.workspace.monthly",status:"confirmed"}],nextCursor:"",hasMore:false}},
    {source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[{receiptId:"a",type:"billing.workspace.monthly",status:"confirmed",chargeUsdMicros:12500000}],nextCursor:"",hasMore:false}},
    {source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[{receiptId:"a",type:"billing.workspace.monthly",status:"confirmed",totalUsdMicros:"9223372036854775808"}],nextCursor:"",hasMore:false}},
    {source:"ledger",status:"available",available:true,fetchedAt:now,data:{receipts:[],nextCursor:"",hasMore:true}},
  ]){
    const input=await inputs(),n=network({"/api/v2/legacy-billing/receipts?limit=20":()=>jsonResponse(body)});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
    assert.equal(receipt.status,"blocked");assert.ok(n.calls.some(v=>v.path==="/api/v2/legacy-billing/receipts?limit=20"));
  }
});
test("the usage summary must carry canonical non-negative int64 strings",async()=>{
  const usage=(patch:Record<string,unknown>)=>({totalRequests:"17",totalInputTokens:"170",totalOutputTokens:"34",totalTokens:"204",totalActualCostUSDMicros:"125001",...patch});
  for(const body of [
    usage({totalRequests:17}),
    usage({totalActualCostUSDMicros:"1.25"}),
    usage({totalInputTokens:"017"}),
    usage({totalOutputTokens:"-1"}),
    usage({totalTokens:"9223372036854775808"}),
  ]){
    const input=await inputs(),n=network({"/api/v2/usage/summary?period=month":()=>jsonResponse(body)});
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
    assert.equal(receipt.status,"blocked");assert.ok(n.calls.some(v=>v.path==="/api/v2/usage/summary?period=month"));
  }
});
test("binding refuses wrong run provenance, Candidate identity, installation bytes and receipt claims",async()=>{
  const input=await inputs(),n=network();const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});const receiptText=JSON.stringify(receipt);
  for(const patch of [{status:"in_progress"},{conclusion:"failure"},{head_branch:"feature"},{head_sha:"0".repeat(40)},{run_attempt:1},{id:44},{event:"pull_request"},{path:".github/workflows/other.yml"},{repository:{full_name:"someone/fork"}},{actor:{login:"outsider"}},{triggering_actor:{login:"outsider"}}])assert.throws(()=>validateCloudBusinessReadbackBinding({...input,receiptText,run:{...input.run,...patch}}));
  for(const patch of [{head_sha:"0".repeat(40)},{run_attempt:2},{path:".github/workflows/deploy.yml"},{conclusion:"failure"},{repository:{full_name:"someone/fork"}}])assert.throws(()=>validateCloudBusinessReadbackBinding({...input,receiptText,installationRun:{...input.installationRun,...patch}}));
  assert.throws(()=>validateCloudBusinessReadbackBinding({...input,receiptText,installationText:JSON.stringify(JSON.parse(input.installationText),null,2)}));
  for(const patch of [{schemaVersion:1},{schemaVersion:3},{publicEndpointCandidateVerified:true},{installationBinding:"same_runtime_verified"},{fullBusinessQualification:true},{scope:"full_business"},{rawBody:"private"},{checks:{...receipt.checks,wallet:false}},{checks:{...receipt.checks,usage:false}},{checks:{...receipt.checks,feeReceipts:false}},{producer:{...receipt.producer,password:"private"}},{errorCode:"private-error"}])assert.throws(()=>validateCloudBusinessReadbackReceipt({...receipt,...patch}));
  const installation=JSON.parse(input.installationText);installation.cloudRuntimeChildDigest=digest("e");await assert.rejects(runCloudBusinessReadback({...input,installationText:JSON.stringify(installation)},{environment:input.environment,fetch:n.fetch}));
});

// Only the test transport rewrites the approved HTTPS origin onto an ephemeral
// local HTTP listener. Production keeps its public HTTPS and DNS restrictions.
test("producer traverses a real local HTTP server through injected transport",async()=>{
  const input=await inputs(), n=network();
  const server=createServer(async(req,res)=>{
    try {
      const chunks:Buffer[]=[];for await(const chunk of req)chunks.push(Buffer.from(chunk));
      const body=Buffer.concat(chunks).toString();
      const headers=new Headers();for(const [key,value] of Object.entries(req.headers))if(value!==undefined)headers.set(key,Array.isArray(value)?value.join("; "):value);
      const reply=await n.fetch(`https://cloud.example.com${req.url}`,{method:req.method,headers,redirect:"error",signal:new AbortController().signal,...(body?{body}:{})});
      res.statusCode=reply.status;for(const [k,v] of reply.headers)if(k!=="set-cookie")res.setHeader(k,v);
      if(reply.headers.getSetCookie().length)res.setHeader("set-cookie",reply.headers.getSetCookie());
      res.end(Buffer.from(await reply.arrayBuffer()));
    }catch{res.statusCode=500;res.end();}
  });
  await new Promise<void>(resolve=>server.listen(0,"127.0.0.1",resolve));
  try {
    const address=server.address();assert.ok(address&&typeof address!=="string");
    const receipt=await runCloudBusinessReadback(input,{environment:input.environment,fetch:(url,init)=>fetch(`http://127.0.0.1:${address.port}${new URL(url).pathname}${new URL(url).search}`,init).then(async response=>{
      // Preserve the logical public URL: this rewriting is test transport only.
      return new Response(response.status===204?null:await response.arrayBuffer(),{status:response.status,headers:response.headers});
    })});
    assert.equal(receipt.status,"customer_reads_verified");assert.equal(n.calls.length,10);assert.equal(receipt.publicEndpointCandidateVerified,false);assert.equal(receipt.installationBinding,"receipt_association_only");
  }finally{await new Promise<void>((resolve,reject)=>server.close(e=>e?reject(e):resolve()));}
});

test("a stalled body hits the request deadline and still executes session cleanup",async(t)=>{
  t.mock.timers.enable({apis:["setTimeout"]});
  const input=await inputs();let arrived!:()=>void;const atWallet=new Promise<void>(resolve=>{arrived=resolve;});
  const n=network({"/api/v2/wallet":async(init)=>{arrived();return new Promise<Response>((_,reject)=>init.signal!.addEventListener("abort",()=>reject(new Error("private transport timeout")),{once:true}));}});
  const pending=runCloudBusinessReadback(input,{environment:input.environment,fetch:n.fetch});
  await atWallet;t.mock.timers.tick(10_000);const receipt=await pending;
  assert.equal(receipt.status,"blocked");assert.equal(receipt.checks.logout,true);assert.equal(receipt.checks.sessionRevoked,true);assert.doesNotMatch(JSON.stringify(receipt),/private transport/);
});


test("legal large announcement pages fit the owner response budget", async () => {
  const input = await inputs(), n = network();
  const response = n.json({ source: "control-plane", available: true, status: "available", fetchedAt: now,
    data: { page: 1, pageSize: 20, total: 20, items: Array.from({length: 20}, (_, index) => ({
      id: String(index), title: "公告", body: "中".repeat(20_000), status: "published", read: false, createdAt: now, updatedAt: now,
    })) } });
  assert.ok((await response.clone().arrayBuffer()).byteLength > 1024 * 1024);
  const receipt = await runCloudBusinessReadback(input, { environment: input.environment,
    fetch: network({"/api/v2/announcements?page=1&pageSize=20": () => response}).fetch });
  assert.equal(receipt.status, "customer_reads_verified");
});

test("production transport captures a login session before a truncated body and revokes it", async t => {
  const input = await inputs(), n = network();
  const server = createServer(async (req, res) => {
    try {
      const chunks: Buffer[] = []; for await (const chunk of req) chunks.push(Buffer.from(chunk));
      if (req.url === "/api/v2/auth/login") {
        // The session exists once these headers have reached the client.
        res.writeHead(200, { "content-type": "application/json", "set-cookie": "opl_session=secret-session-cookie; Secure; HttpOnly", "content-length": "1000" });
        res.flushHeaders(); res.write("{"); setImmediate(() => res.destroy()); return;
      }
      const response = await n.fetch(`https://cloud.example.com${req.url}`, { method: req.method,
        headers: req.headers as Record<string,string>, redirect: "error", signal: new AbortController().signal,
        ...(chunks.length ? {body: Buffer.concat(chunks).toString()} : {}) });
      res.statusCode = response.status;
      for (const [key, value] of response.headers) if (key !== "set-cookie") res.setHeader(key, value);
      if (response.headers.getSetCookie().length) res.setHeader("set-cookie", response.headers.getSetCookie());
      res.end(Buffer.from(await response.arrayBuffer()));
    } catch { res.statusCode = 500; res.end(); }
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  const address = server.address(); assert.ok(address && typeof address !== "string");
  const original = https.request;
  // Replace only the socket destination; execute the actual publicFetch response
  // header/body logic. No production TLS or network restriction is disabled.
  t.mock.method(https, "request", (url: string, options: any, listener: any) => {
    const target = new URL(url);
    return httpRequest(`http://127.0.0.1:${address.port}${target.pathname}${target.search}`,
      { method: options.method, headers: options.headers, signal: options.signal }, listener);
  });
  syncBuiltinESMExports();
  try {
    const receipt = await runCloudBusinessReadback(input, { environment: input.environment });
    assert.equal(receipt.status, "blocked");
    assert.equal(receipt.checks.logout, true);
    assert.equal(receipt.checks.sessionRevoked, true);
  } finally {
    t.mock.restoreAll(); assert.equal(https.request, original); syncBuiltinESMExports();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
});
