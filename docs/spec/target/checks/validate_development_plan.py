#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Check actionable work-package coverage without executing implementation, deployment or charges."""
from pathlib import Path
from datetime import datetime,timezone
from collections import Counter
import json,hashlib,re,sys
R=Path(__file__).resolve().parents[1]
plan=json.loads((R/'checks/development_plan.json').read_text())
api=json.loads((R/'contracts/api_inventory.json').read_text())['operations']
db=json.loads((R/'contracts/db_inventory.json').read_text())['tables']
ui=json.loads((R/'contracts/ui_inventory.json').read_text())['features']
W={w['id']:w for w in plan['workPackages']};errors=[]
def require(ok,message):
 if not ok:errors.append(message)
# Repository placement is an execution constraint, not a new business contract.
cloud=R.parents[2]
def resolve_path(value):
 p=Path(value)
 return p.resolve() if p.is_absolute() else (cloud/p).resolve()
roots={owner:resolve_path(path) for owner,path in plan['sourceRoots'].items()}
require(roots.get('cloud')==cloud,'Cloud root must be the containing checkout')
for owner,path in roots.items():
 if owner not in {'cloud','instance'}:
  require(path.is_relative_to(cloud) and path!=cloud,'Cloud owner escapes repository: '+owner)
require(roots.get('tenant')==roots.get('gateway'),'CloudIdentity must share the Gateway Integration module')
require('instance' in roots and not roots['instance'].is_relative_to(cloud),'Instance remains an external deployment owner')
require({t['owner'] for t in db}<=set(roots),'data owner lacks a declared work root')
require(len(W)==len(plan['workPackages']),'duplicate work package identity')
features={f'F{i:02d}' for i in range(1,18)}
require({x for w in W.values() for x in w['features']}==features,'F01-F17 coverage differs')
ops={o['operationId'] for o in api};primary=Counter(o for w in W.values() for o in w['apiPrimary'])
require(set(primary)==ops,'REST operation coverage differs')
require(all(count==1 for count in primary.values()),'REST operation has multiple primary implementation tasks')
consumed={o for w in W.values() for o in w['apiConsumes']}
require(consumed<=ops,'frontend task invented REST operation')
require(consumed==ops,'some REST operations have no frontend consumer task')
actual_tables={f"{t['schema']}.{t['name']}" for t in db}
require(set(plan['tablePrimary'])==actual_tables,'DB owner tables not fully assigned')
for table,wid in plan['tablePrimary'].items():require(wid in W and table in W[wid].get('tablesPrimary',[]),'table task mapping mismatch: '+table)
proto=(R/'contracts/internal.proto').read_text();rpcs=set()
for m in re.finditer(r'service\s+(\w+)\s*\{(.*?)\}',proto,re.S):
 for method in re.findall(r'rpc\s+(\w+)',m[2]):rpcs.add(m[1]+'.'+method)
require(set(plan['rpcImplementers'])==rpcs,'internal RPC implementation coverage differs')
for rpc,wids in plan['rpcImplementers'].items():
 require(bool(wids),'RPC has no implementer: '+rpc)
 for wid in wids:require(wid in W and rpc in W[wid].get('rpcImplements',[]),'RPC task mapping mismatch: '+rpc)
for wid,w in W.items():
 require(set(w['owners'])<=set(roots),wid+' references an undeclared owner work root')
 require(w['state']=='not_implemented',wid+' incorrectly claims work was implemented')
 for k in ['coordinator','owners','features','plannedWritePaths','deliverables','verification','acceptance']:require(bool(w.get(k)),wid+' missing '+k)
 for source in w['existingReadPaths']:require(resolve_path(source).exists(),wid+' falsely labels existing source: '+source)
 for path in w['plannedWritePaths']:
  resolved=resolve_path(path)
  require(resolved.is_relative_to(cloud) or ('instance' in w['owners'] and resolved.is_relative_to(roots['instance'])),wid+' write escapes authorized repository scope: '+path)
 for dep in set(w['startAfter']+w['acceptAfter']):require(dep in W and dep!=wid,wid+' has invalid dependency: '+dep)
# Both useful-start and integrated-acceptance edges must converge, no hidden dependency cycles.
remaining={wid:set(w['startAfter']+w['acceptAfter']) for wid,w in W.items()};order=[];waves=[]
while remaining:
 ready=sorted(wid for wid,deps in remaining.items() if not deps)
 if not ready:errors.append('cyclic handoff dependencies: '+str(remaining));break
 waves.append(ready);order.extend(ready)
 for wid in ready:remaining.pop(wid)
 for deps in remaining.values():deps.difference_update(ready)
# Accepted business slices are consumed by the work-window handoff, not a second
# workflow engine. Validate their dependencies independently of whole-W closure.
slices={item['id']:item for item in plan.get('executionSlices',[])}
require(bool(slices),'implementation slices are absent')
require(len(slices)==len(plan.get('executionSlices',[])),'duplicate slice identity')
windows={window['id'] for window in plan.get('windows',[])}
for key,item in slices.items():
 require(item.get('workPackage') in W and key.startswith(item['workPackage']+'.'),'slice is outside its work package: '+key)
 require(item.get('window') in windows,'unknown slice window: '+key)
 for field in ['writePaths','inputRefs','deliverable','verification','completionEvidence','onUnknown']:
  require(bool(item.get(field)),'slice omitted '+field+': '+key)
 for dep in item.get('startAfter',[]):require(dep in slices and dep!=key,'invalid slice dependency: '+key+' -> '+dep)
 for path in item.get('writePaths',[]):
  resolved=resolve_path(path)
  require(resolved.is_relative_to(cloud) or (item.get('window')=='instance' and resolved.is_relative_to(roots['instance'])),'slice write escapes owner boundary: '+key)
remaining_slices={key:set(item.get('startAfter',[])) for key,item in slices.items()}
slice_waves=[]
while remaining_slices:
 ready=sorted(key for key,deps in remaining_slices.items() if not deps)
 if not ready:errors.append('cyclic slice dependencies: '+str(remaining_slices));break
 slice_waves.append(ready)
 for key in ready:remaining_slices.pop(key)
 for deps in remaining_slices.values():deps.difference_update(ready)
def predecessors(key):
 result=set();queue=list(slices.get(key,{}).get('startAfter',[]))
 while queue:
  value=queue.pop()
  if value in result:continue
  result.add(value);queue.extend(slices.get(value,{}).get('startAfter',[]))
 return result
require(plan.get('contractMigration',{}).get('entrySlice')=='W01.application-contracts','contract migration entry is not explicit')
for key in ['W15.first-create','W29.default-tke','W29.agent-tke']:
 require(key in slices,'required business slice absent: '+key)
require({'W21.tenant-admission','W17.first-delivery','W04.payment-key','W05.receipts'}<=predecessors('W15.first-create'),'first-create lacks a real identity/payment/delivery/evidence producer')
require('W09.agent-build' not in predecessors('W29.default-tke'),'default App wrongly depends on a synthetic Agent Build')
require(not {'W07.webui-catalog','W08.cos-upload'} & predecessors('W29.default-tke'),'default App wrongly depends on optional Package or catalog UI')
require('W09.agent-build' in predecessors('W29.agent-tke'),'Agent TKE acceptance omitted real Build')
require(not any(key.startswith(('W11.','W25.','W28.','W30.')) for key in predecessors('W29.default-tke')),'TKE first-use is incorrectly gated by full Local or legacy migration')
# Four dispatch windows have disjoint current-contract preparations. Their
# existence never erases the W01 dependency for new default-App consumers.
preparations=plan.get('parallelPreparation',[])
require({p.get('window') for p in preparations}=={'artifacts','business','resources','delivery'} and len(preparations)==4,'four domain preparations must be assigned exactly once')
reserved=[]
for item in preparations:
 key=item.get('window','unknown')
 require(item.get('model')=='deepseek-v4.1-flash' and item.get('reasoningEffort')=='high','dispatch model differs: '+key)
 require(item.get('sharedContractGate')=='W01.application-contracts','preparation bypasses contract gate: '+key)
 for field in ['entryWork','commands','acceptance','evidencePath','stopBoundary']:
  require(bool(item.get(field)),'preparation omitted '+field+': '+key)
 for next_slice in item.get('nextSlices',[]):
  require(next_slice in slices and slices[next_slice]['window']==key,'preparation assigned foreign slice: '+key)
 for path in item.get('writePaths',[]):
  resolved=resolve_path(path)
  require(resolved.is_relative_to(cloud),'preparation write escapes Cloud: '+key)
  for denied in item.get('forbiddenWrites',[]):
   blocked=resolve_path(denied)
   require(not resolved.is_relative_to(blocked) and not blocked.is_relative_to(resolved),'preparation writes shared boundary: '+key)
  for other_owner,other in reserved:
   require(other_owner==key or not (resolved.is_relative_to(other) or other.is_relative_to(resolved)),'preparation write overlap: '+key+' / '+other_owner)
  reserved.append((key,resolved))

# The single serial successor consumes these preparations; it does not invent
# another work-package or claim that an accepted task is already deployed.
serial=plan.get('serialIntegration',{})
require(serial.get('model')=='deepseek-v4.1-flash' and serial.get('reasoningEffort')=='high','serial integration model differs')
for field in ['ownership','scope','authorization','auditReceipt','completion']:
 require(bool(serial.get(field)),'serial integration omitted '+field)
stages=serial.get('stages',[])
require(bool(stages) and [s.get('order') for s in stages]==list(range(1,len(stages)+1)),'serial integration order is not contiguous')
for stage in stages:
 for field in ['title','workPackages','action','accept']:
  require(bool(stage.get(field)),'serial stage omitted '+field)
 require(set(stage.get('workPackages',[]))<=set(W),'serial stage invented work package')

# Human document, machine plan and immutable product rules are linked, not independently reauthored.
text=(R/'14_implementation_work_packages.md').read_text()
for wid,w in W.items():
 require('### '+wid+' ' in text,'human task section missing: '+wid)
 for operation in w['apiPrimary']:require('`'+operation+'`' in text,'human task operation absent: '+operation)
require('W29' in W['W31']['startAfter'] and 'W30' not in W['W31']['startAfter'],'Product release and all-customer migration wrongly conflated')
require('08_delivery_checklist_per_role.md' in text or '08' in text,'role acceptance owner absent')
check_only='--check' in sys.argv[1:]
now=datetime.now(timezone.utc)
report={'schemaVersion':1,'timestamp':now.isoformat(),'passed':not errors,'scope':'implementation plan completeness/coverage/dependency verification only; no work package implemented','counts':{'workPackages':len(W),'features':len(features),'restOperations':len(ops),'tables':len(actual_tables),'internalRPCs':len(rpcs),'executionSlices':len(slices)},'sliceWaves':slice_waves,'topologicalOrder':order,'acceptanceDependencyWaves':waves,'errors':errors,'sourceHashes':{name:hashlib.sha256((R/name).read_bytes()).hexdigest() for name in ['00_master_index.md','01_domain_ownership_matrix.md','14_implementation_work_packages.md','checks/development_plan.json','checks/render_development_plan.py','checks/validate_development_plan.py','contracts/api_inventory.json','contracts/db_inventory.json','contracts/ui_inventory.json','contracts/internal.proto','03_api_contract_complete.yaml','12_product_spec.md']}}
if check_only:
 # Read-only gate: prove the checked-in projection still matches its live inputs without
 # mutating receipts or the validation snapshot. The trusted host decides where to persist evidence.
 print(json.dumps({'passed':report['passed'],'counts':report['counts'],'waves':waves,'sliceWaves':slice_waves,'errors':errors},ensure_ascii=False,indent=2))
 sys.exit(bool(errors))
receipt=R/'checks/runs'/('development-plan-'+now.strftime('%Y%m%dT%H%M%S%fZ')+'.json');receipt.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
(R/'checks/development_plan_validation.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'passed':report['passed'],'counts':report['counts'],'waves':waves,'sliceWaves':slice_waves,'errors':errors,'receipt':str(receipt)},ensure_ascii=False,indent=2))
sys.exit(bool(errors))
