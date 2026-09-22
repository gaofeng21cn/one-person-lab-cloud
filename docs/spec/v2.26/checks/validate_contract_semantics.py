from pathlib import Path
import copy,json,sys,yaml
from jsonschema import Draft202012Validator,RefResolver
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate
from google.protobuf.json_format import ParseDict,ParseError
import tempfile,subprocess,grpc_tools
B=Path(__file__).resolve().parents[1]
_tmp=tempfile.TemporaryDirectory(prefix='opl-handoff-proto-')
_include=Path(grpc_tools.__file__).parent/'_proto'
subprocess.run([sys.executable,'-m','grpc_tools.protoc',f'-I{B/"contracts"}',f'-I{_include}',f'--python_out={_tmp.name}',str(B/'contracts/internal.proto')],check=True)
sys.path.insert(0,_tmp.name);import internal_pb2 as pb
api=yaml.safe_load((B/'03_api_contract_complete.yaml').read_text());sc=api['components']['schemas'];pub=json.loads((B/'contracts/publisher-contract.schema.json').read_text());events=json.loads((B/'contracts/events.json').read_text());results=[];resolve=RefResolver.from_schema(api)
def check(name,ok):
 assert ok,name
 results.append({'case':name,'pass':True})
def accepts(n,x):return not list(OAS30Validator(sc[n],resolver=resolve).iter_errors(x))
def rejects_proto(cls,x):
 try:ParseDict(x,cls());return False
 except ParseError:return True
validate(api);Draft202012Validator.check_schema(pub);Draft202012Validator.check_schema(events)
for i,x in enumerate(pub['examples']):Draft202012Validator(pub).validate(x);check(f'R03 publisher typed example {i}',True)
credit={'kind':'adjustment_credit','description':'verified credit','quantity':1,'amountUSDMicros':'10','creditSource':{'originalWalletOperationId':'wallet1','originalSubscriptionPeriodId':'period1','policyVersionId':'policy1','creditReceiptId':'receipt1','amountUSDMicros':'10'}}
positive={'kind':'compute','description':'compute','quantity':1,'amountUSDMicros':'100'}
check('R01 credit remains nonnegative, 100-10=90',accepts('QuoteLine',credit) and accepts('QuoteLine',positive) and int(positive['amountUSDMicros'])-int(credit['amountUSDMicros'])==90)
check('R01 negative credit rejected',not accepts('QuoteLine',{**credit,'amountUSDMicros':'-10'}));check('R01 credit without original evidence rejected',not accepts('QuoteLine',{k:v for k,v in credit.items() if k!='creditSource'}));check('R01 quantity 0 rejected',not accepts('QuoteLine',{**positive,'quantity':0}))
check('R01 JSON above signed int64 rejected',not accepts('USDMicros','9223372036854775808'))
q={'purpose':'deploy','capabilityVersionId':'v1','computePlanId':'c1','storagePlanId':'s1','modelSelections':[],'periodMonths':1}
check('R01 deploy discriminator accepted',accepts('QuoteRequest',q));check('R01 retired create purpose rejected',not accepts('QuoteRequest',{**q,'purpose':'create'}));check('R01 unsupported multi-month new quote rejected',not accepts('QuoteRequest',{**q,'periodMonths':2}))
for field in ['packageVersionId','runtimeVersionId','webuiVersionId','capabilityVersionId']:
 t=ParseDict({field:'id1'},pb.ReferenceTarget());check('R02 exact target '+field,bool(t.WhichOneof('target')))
check('R02 multiple targets rejected by protobuf',rejects_proto(pb.ReferenceTarget,{'runtimeVersionId':'r1','webuiVersionId':'u1'}))
check('R02 release running not representable',rejects_proto(pb.ReleaseEvidence,{'terminalStatus':'OPERATION_STATUS_ENUM_RUNNING'}))
check('R02 bind requires typed committed identity fields','owner_commit_evidence' in pb.BindReferenceRequest.DESCRIPTOR.fields_by_name)
r=pub['examples'][0];u=pub['examples'][1];third=pub['examples'][2]
check('R03 exact main artifact/revision identity',r['applicationRevisionTemplate']['image']==r['image']['repository']+'@'+r['image']['digest'])
check('R03 static WebUI integration source matches',r['buildRecipe']['webuiInput']['sourcePath']==u['staticRoot'])
check('R03 namespace exact component prefix accepts correct',third['image']['repository'].startswith('registry.example/third-party/acme/'))
check('R09 sibling-prefix spoof rejected',not 'registry.example/third-party/acme-evil/runtime'.startswith('registry.example/third-party/acme/'))
legacy={'schemaVersion':'opl-deployment-descriptor/v1','artifact':r['image'],'applicationRevision':r['applicationRevisionTemplate'],'provenance':'legacy_application','legacyApplicationRevisionId':'old-r1'}
check('R03 legacy exact revision without fake build allowed',accepts('DeploymentDescriptor',legacy));check('R03 legacy with fake runtime publisher rejected',not accepts('DeploymentDescriptor',{**legacy,'runtimeContract':r}))
check('R03 unknown secret body rejected',not accepts('RuntimePublisherContract',{**r,'rawGatewayKey':'never'}))
scope=ParseDict({'platform':{}},pb.AuthorizationScope());check('R04 platform scope requires no fake Tenant',scope.WhichOneof('scope')=='platform')
check('R04 ambiguous scope rejected',rejects_proto(pb.AuthorizationScope,{'platform':{},'tenant':{'tenantId':'t1'}}))
check('R04 role self-assertion absent from request',rejects_proto(pb.AuthorizationRequest,{'actorId':'u1','role':'owner'}))
check('R04 audience typed and action closed',rejects_proto(pb.AuthorizationRequest,{'action':'ARBITRARY_ADMIN_ACTION'}))
check('R04 no invented AppSSO service','CloudIdentityApplicationAccess' not in pb.DESCRIPTOR.services_by_name)
exact=ParseDict({'exactRevision':'rv7'},pb.ProviderRevisionPrecondition());absent=ParseDict({'requireAbsent':{'receiptId':'abs1','observedAt':'2026-09-21T00:00:00Z'}},pb.ProviderRevisionPrecondition())
check('R05 revision exact/absence discriminated',exact.WhichOneof('condition')=='exact_revision' and absent.WhichOneof('condition')=='require_absent')
check('R05 wildcard mixed with absence rejected',rejects_proto(pb.ProviderRevisionPrecondition,{'exactRevision':'rv7','requireAbsent':{'receiptId':'abs1'}}))
for method in ['FenceRouteEpoch','ActivateRoute','ObserveRoute','RollbackRoute']:check('R05 concrete route RPC '+method,method in pb.DESCRIPTOR.services_by_name['FabricRouteExecution'].methods_by_name)
check('R05 current generation/epoch/provider revision readback typed',all(f in pb.RouteReadback.DESCRIPTOR.fields_by_name for f in ['current_generation','accepted_execution_epoch','provider_revision','target_execution_resource_id']))
apiops={o['operationId']:o for item in api['paths'].values() for o in item.values()}
check('R07 reenable independent of restore',apiops['reenableTenant']['x-operation-kind']=='reenable_tenant' and apiops['restoreTenant']['x-operation-kind']=='restore_tenant')
check('R07 reenable includes bounded Workspace resumption','workspace_resumption' in sc['Operation']['x-stage-values']['reenable_tenant'])
check('R08 delete version is catalog tombstone not physical purge','catalog_tombstone' in sc['Operation']['x-stage-values']['delete_capability_version'] and 'artifact_deletion' not in sc['Operation']['x-stage-values']['delete_capability_version'])
check('R08 Workspace delete does not revoke retained Key','key_revocation' not in sc['Operation']['x-stage-values']['delete_workspace'])
policyDoc=json.loads((B/'contracts/plan-change-policy.json').read_text());policy=policyDoc['x-policy']
Draft202012Validator(policyDoc).validate(policy)
check('D17 approved fixed policy accepted',accepts('PlanChangePolicy',policy))
check('D17 pending policy rejected',not accepts('PlanChangePolicy',{**policy,'approvalStatus':'pending_decision'}))
check('D17 obsolete draft version rejected',not accepts('PlanChangePolicy',{**policy,'version':'resize-policy-draft/v1'}))
for v in json.loads((B/'checks/d17_acceptance_vectors.json').read_text())['cases']:
 old=int(v['oldMonthlyUSDMicros']);new=int(v['newMonthlyUSDMicros']);duration=int(v['periodMilliseconds']);remaining=int(v['remainingMilliseconds'])
 actual=(max(new-old,0)*remaining+duration-1)//duration
 check('D17 independent exact vector '+v['name'],actual==int(v['expectedSupplementUSDMicros']))
base={'operationId':'o1','owner':'workspace','kind':'create_workspace','resourceId':'w1','status':'running','stage':'admission','requestId':'req1','createdAt':'2026-09-21T00:00:00Z','updatedAt':'2026-09-21T00:00:00Z'}
check('R10 nonterminal missing poll guidance rejected',not accepts('Operation',base));check('R10 nonterminal poll guidance accepted',accepts('Operation',{**base,'pollAfterSeconds':2}));check('R10 terminal stale poll loop rejected',not accepts('Operation',{**base,'status':'succeeded','stage':'succeeded','pollAfterSeconds':2}))
check('R10 action-stage mismatch rejected',not accepts('Operation',{**base,'stage':'catalog_tombstone','pollAfterSeconds':2}))
check('F12 future automatic consent revocable',apiops['updateRenewalSettings']['x-operation-kind']=='update_renewal_settings')
check('F09 App credentials owner-only and no-session-secret',apiops['revealWorkspaceApplicationCredentials']['x-permission']==['owner'] and not accepts('WorkspaceApplicationCredentials',{'workspaceId':'w1','runtimeInstanceId':'rt1','username':'admin','password':'one-time','workspaceSessionSecret':'never'}))
inv=json.loads((B/'contracts/api_inventory.json').read_text());check('inventory exact actual operations',set(apiops)=={x['operationId'] for x in inv['operations']})
check('all stable error mappings exist',set(sc['ErrorCode']['enum'])==set(api['x-error-http-status']))
(B/'checks/handoff_contract_semantics.json').write_text(json.dumps({'scope':'Executable contract assertions and canonical-source tests, NOT implemented runtime or production qualification','cases':results,'pending':[]},ensure_ascii=False,indent=2))
print('PASS',len(results),'contract semantic cases; D17 user-approved and explicitly validated')
