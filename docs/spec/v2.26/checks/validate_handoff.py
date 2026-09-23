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
# Historical receipt proves its own earlier source revision. A later UI/contract
# change requires a new immutable, actually replayed receipt rather than editing it.
ui=read('checks/ui_prototype_verification.json')
need(ui.get('passed') is True,'UI prototype historical regression failed')
ui_stale=[name for name,digest in ui.get('sourceHashes',{}).items() if hashlib.sha256((root/name).read_bytes()).hexdigest()!=digest]
ui_evidence='checks/ui_prototype_verification.json'
if ui_stale:
 ui_paths=sorted((root/'checks/runs').glob('ui-contract-*.json'))
 need(bool(ui_paths),'UI prototype evidence does not bind current sources: '+', '.join(ui_stale))
 if ui_paths:
  latest=ui_paths[-1];new=json.loads(latest.read_text());ui_evidence=str(latest.relative_to(root))
  need(new.get('passed') is True,'current UI contract regression did not pass')
  need(all((root/name).is_file() and hashlib.sha256((root/name).read_bytes()).hexdigest()==digest for name,digest in new.get('sourceHashes',{}).items()) and set(ui.get('sourceHashes',{}))<=set(new.get('sourceHashes',{})),'current UI contract receipt missing exact source binding')
  need(new.get('historicalEvidence')=='checks/ui_prototype_verification.json' and new.get('prototypeSha256')==new.get('sourceHashes',{}).get('ui-prototype/index.html'),'current UI regression provenance or prototype bytes differ')
  need(len(new.get('changedContractCases',[]))==3 and all(case.get('passed') for case in new['changedContractCases']),'generic Operation routes were not checked on current OpenAPI')
  need(new.get('desktop',{}).get('passed') and len(new['desktop'].get('cases',[]))>=40 and all(case.get('passed') for case in new['desktop']['cases']),'current desktop UI replay absent/failed')
  need(len(new.get('mobile',[]))==2 and {x.get('viewport',{}).get('width') for x in new['mobile']}=={320,390} and all(x.get('passed') and len(x.get('cases',[]))>=9 and all(case.get('passed') for case in x['cases']) for x in new['mobile']),'current mobile UI replay absent/failed')
  need(new.get('externalNetworkRequests')==0,'offline prototype issued external network requests')
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
# A static schema PASS must not hide later field/owner reconciliation findings.
field_paths=sorted((root/'checks/runs').glob('domain-alignment-*.json'))
need(bool(field_paths),'no field-level domain alignment receipt')
if field_paths:
 field_report=json.loads(field_paths[-1].read_text())
 need(field_report.get('referenceCoveragePassed') is True,'domain field reference coverage failed')
 for name,digest in field_report.get('sourceHashes',{}).items():
  need((root/name).is_file() and hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,'domain alignment receipt stale: '+name)
 for finding in field_report.get('openContractFindings',[]):
  need(False,'open cross-owner contract gap '+finding['id']+': '+finding['reason'])
decisions=read('checks/decision_status.json')['decisions'];pending=[d['id'] for d in decisions if d['status']!='accepted']
status='needs_correction' if issues else ('awaiting_business_decision' if pending else 'ready_for_implementation')
report={'schemaVersion':1,'timestamp':datetime.now(timezone.utc).isoformat(),'status':status,'technicalClosureVerified':not issues,'pendingUserDecisions':pending,'issues':issues,'scope':'development specification + interactive prototype + isolated contract/DB tests; NOT implemented services or production qualification','reviewFindings':{r:('pending_user' if r=='R06' and 'D17' in pending else ('previously_closed_with_receipt; new_issue_separately_reported' if issues else 'closed_in_spec')) for r in ['R01','R02','R03','R04','R05','R06','R07','R08','R09','R10']},'evidence':{'static':static['report'],'crossDomain':str(cross_paths[-1].relative_to(root)) if cross_paths else None,'publisherSource':str(go_paths[-1].relative_to(root)) if go_paths else None,'contractSemantics':'checks/handoff_contract_semantics.json','d17ContractSemantics':'checks/d17_contract_results.json','developmentPlan':'checks/development_plan_validation.json','ui':ui_evidence,'historicalUiSourceDrift':ui_stale,'domainAlignment':str(field_paths[-1].relative_to(root)) if field_paths else None},'counts':{'features':len(flows),'typedFlowSteps':sum(len(f['steps']) for f in flows),'restOperations':len(ops),'crossDomainCases':len(cross.get('cases',[])),'contractCases':len(semantic['cases']),'d17ContractCases':len(d17['cases']),'implementationWorkPackages':devplan['counts']['workPackages']}}
(root/'checks/handoff_readiness.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(report,ensure_ascii=False,indent=2))
# Pending explicit business approval is not a technical test failure, but never yields READY.
raise SystemExit(1 if issues else 0)
