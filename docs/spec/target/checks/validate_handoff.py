#!/usr/bin/env python3
"""Join evidence layers without upgrading a draft business decision into full READY."""
from pathlib import Path
from datetime import datetime,timezone
import hashlib,json,re,sys
root=Path(__file__).resolve().parents[1]
issues=[]
def need(ok,msg):
 if not ok:issues.append(msg)
def read(p):return json.loads((root/p).read_text())
static=read('checks/verification.json');need(static['result']=='passed','current specification checks not passed')
# Static receipt must bind exact current inputs, not a stale PASS message.
sr=read(static['report'])
for name,digest in sr['artifacts'].items():
 if name.startswith('checks/runs/') or name in ['checks/handoff_readiness.json']:continue
 path=root/name
 need(path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest()==digest,'static receipt stale: '+name)
proto=(root/'contracts/internal.proto').read_text();rpcs={}
for m in re.finditer(r'service\s+(\w+)\s*\{(.*?)\}',proto,re.S):
 for n in re.finditer(r'rpc\s+(\w+)\((\w+)\)\s+returns\s*\((\w+)\)',m[2]):rpcs[f'{m[1]}.{n[1]}']=(n[2],n[3])
api=read('contracts/api_inventory.json');ops={x['operationId'] for x in api['operations']}
db=read('contracts/db_inventory.json');tables={f"{t['schema']}.{t['name']}" for t in db['tables']}
flows=read('contracts/domain_flows.json')['flows'];need({f['featureId'] for f in flows}=={f'F{i:02d}' for i in range(1,18)},'domain flow coverage is not F01-F17')
for f in flows:
 for op in f['entryOperationIds']:need(op in ops,'flow REST operation missing: '+op)
 for step in f['steps']:
  need(step['rpc'] in rpcs,'flow RPC absent: '+step['rpc'])
  if step['rpc'] in rpcs:need(rpcs[step['rpc']]==(step['request'],step['response']),'flow RPC types stale: '+step['rpc'])
  for table in step['writes']:need(table in tables,'flow table absent: '+table)
  need(bool(step['completionEvidence']) and bool(step['onUnknownOrRejection']),'flow omitted completion/unknown semantics')
# Most recent actual API/protobuf -> database -> projection execution.
cross_paths=sorted((root/'checks/runs').glob('cross-domain-*.json'));need(bool(cross_paths),'no cross-domain receipt')
cross=json.loads(cross_paths[-1].read_text()) if cross_paths else {}
need(cross.get('passed') is True and cross.get('sourceUnchangedDuringRun') is True,'cross-domain execution did not pass exact input')
for name,digest in cross.get('sourceHashes',{}).items():need(hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'cross-domain source changed: '+name)
need(cross.get('cleanup',{}).get('containerAbsent') is True,'DB cleanup not confirmed')
# Canonical Go source, not only our duplicated schema.
go_paths=sorted((root/'checks/runs').glob('publisher-source-*.json'));need(bool(go_paths),'no canonical publisher validation')
go=json.loads(go_paths[-1].read_text()) if go_paths else {}
need(go.get('passed') is True,'canonical publisher Go check failed')
need(go.get('publisherSchemaHash')==hashlib.sha256((root/'contracts/publisher-contract.schema.json').read_bytes()).hexdigest(),'canonical publisher check stale')
semantic=read('checks/handoff_contract_semantics.json');need(all(c['pass'] for c in semantic['cases']),'contract semantic case failed')
ui=read('checks/ui_prototype_verification.json')
need(ui.get('passed') is True,'UI prototype evidence did not pass')
# UI receipt declares its own exact source hashes; no guesswork about which screenshot was tested.
for name,digest in ui.get('sourceHashes',{}).items():need(hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'UI source changed after verification: '+name)
need(bool(ui.get('sourceHashes')),'UI source hashes absent')
for file in ['11_ui_ux_design.md','12_product_spec.md','ui-prototype/index.html']:need((root/file).is_file(),'human handoff artifact absent: '+file)
need('CloudIdentityApplicationAccess' not in proto,'unrequested AppSSO service reintroduced')
policy=read('contracts/plan-change-policy.json')['x-policy']
need(policy['version']=='workspace-plan-change-v1' and policy['approvalStatus']=='approved','D17 fixed approved policy absent')
need(not semantic.get('pending'),'semantic checks still report pending policy')
d17=read('checks/d17_contract_results.json')
need(d17['policyVersion']=='workspace-plan-change-v1' and all(c['pass'] for c in d17['cases']),'D17 executable contract/numeric cases failed')
need('contracts/plan-change-policy.json' in cross['sourceHashes'] and '13_plan_change_policy.md' in cross['sourceHashes'],'cross-domain execution omitted final D17 owner/policy sources')
native_paths=sorted((root/'checks/runs').glob('d17-native-*.json'))
need(bool(native_paths),'native D17 precision evidence absent')
if native_paths:
 native=json.loads(native_paths[-1].read_text())
 need(native['passed'] and native['scriptHash']==hashlib.sha256((root/native['script']).read_bytes()).hexdigest(),'native D17 precision source changed or failed')
 need(native['policyHash']==hashlib.sha256((root/'contracts/plan-change-policy.json').read_bytes()).hexdigest(),'native D17 check policy binding changed')
devplan=read('checks/development_plan_validation.json')
need(devplan['passed'] is True,'development work-package coverage/DAG checks failed')
for name,digest in devplan['sourceHashes'].items():need(hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'development plan evidence stale: '+name)
decisions=read('checks/decision_status.json')['decisions'];pending=[d['id'] for d in decisions if d['status']!='accepted']
status='needs_correction' if issues else ('awaiting_business_decision' if pending else 'ready_for_implementation')
report={'schemaVersion':1,'timestamp':datetime.now(timezone.utc).isoformat(),'status':status,'technicalClosureVerified':not issues,'pendingUserDecisions':pending,'issues':issues,'scope':'development specification + interactive prototype + isolated contract/DB tests; NOT implemented services or production qualification','reviewFindings':{r:('pending_user' if r=='R06' and 'D17' in pending else ('evidence_failed' if issues else 'closed_in_spec')) for r in ['R01','R02','R03','R04','R05','R06','R07','R08','R09','R10']},'evidence':{'static':static['report'],'crossDomain':str(cross_paths[-1].relative_to(root)) if cross_paths else None,'publisherSource':str(go_paths[-1].relative_to(root)) if go_paths else None,'contractSemantics':'checks/handoff_contract_semantics.json','d17ContractSemantics':'checks/d17_contract_results.json','developmentPlan':'checks/development_plan_validation.json','ui':'checks/ui_prototype_verification.json'},'counts':{'features':len(flows),'typedFlowSteps':sum(len(f['steps']) for f in flows),'restOperations':len(ops),'crossDomainCases':len(cross.get('cases',[])),'contractCases':len(semantic['cases']),'d17ContractCases':len(d17['cases']),'implementationWorkPackages':devplan['counts']['workPackages']}}
(root/'checks/handoff_readiness.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False,indent=2))
# Pending explicit business approval is not a technical test failure, but never yields READY.
raise SystemExit(1 if issues else 0)
