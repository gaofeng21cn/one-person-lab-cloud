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
roots={owner:Path(path).resolve() for owner,path in plan['sourceRoots'].items()}
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
 for source in w['existingReadPaths']:require(Path(source).exists(),wid+' falsely labels existing source: '+source)
 for path in w['plannedWritePaths']:
  require(Path(path).is_absolute(),wid+' has relative write path')
  resolved=Path(path).resolve()
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
# Human document, machine plan and immutable product rules are linked, not independently reauthored.
text=(R/'14_implementation_work_packages.md').read_text()
for wid,w in W.items():
 require('### '+wid+' ' in text,'human task section missing: '+wid)
 for operation in w['apiPrimary']:require('`'+operation+'`' in text,'human task operation absent: '+operation)
require('W29' in W['W31']['startAfter'] and 'W30' not in W['W31']['startAfter'],'Product release and all-customer migration wrongly conflated')
require('08_delivery_checklist_per_role.md' in text or '08' in text,'role acceptance owner absent')
now=datetime.now(timezone.utc)
report={'schemaVersion':1,'timestamp':now.isoformat(),'passed':not errors,'scope':'implementation plan completeness/coverage/dependency verification only; no work package implemented','counts':{'workPackages':len(W),'features':len(features),'restOperations':len(ops),'tables':len(actual_tables),'internalRPCs':len(rpcs)},'topologicalOrder':order,'acceptanceDependencyWaves':waves,'errors':errors,'sourceHashes':{name:hashlib.sha256((R/name).read_bytes()).hexdigest() for name in ['00_master_index.md','01_domain_ownership_matrix.md','14_implementation_work_packages.md','checks/development_plan.json','checks/render_development_plan.py','checks/validate_development_plan.py','contracts/api_inventory.json','contracts/db_inventory.json','contracts/ui_inventory.json','contracts/internal.proto']}}
receipt=R/'checks/runs'/('development-plan-'+now.strftime('%Y%m%dT%H%M%S%fZ')+'.json');receipt.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
(R/'checks/development_plan_validation.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'passed':report['passed'],'counts':report['counts'],'waves':waves,'errors':errors,'receipt':str(receipt)},ensure_ascii=False,indent=2))
sys.exit(bool(errors))
