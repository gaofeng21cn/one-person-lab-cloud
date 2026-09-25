"""Compile current publisher permission metadata from the canonical API contract."""
from pathlib import Path
import yaml
root=Path(__file__).resolve().parents[3]
api=yaml.safe_load((root/'docs/spec/target/03_api_contract_complete.yaml').read_text())
lines=['// Code generated from the canonical API permissions; DO NOT EDIT.','package identity','import api "opl-cloud/packages/contracts/go/api"','type actionPolicy struct {owner api.OwnerEnum; roles []string}','var actions = map[api.AuthorizationActionEnum]actionPolicy{']
for methods in api['paths'].values():
 for x in methods.values():
  if not isinstance(x,dict) or x.get('x-owner') not in ('capability','build'): continue
  action=x['operationId'];owner=x['x-owner']
  if action in ('listRuntimeVersions','registerRuntimeVersion','setRuntimeVersionStatus','getBuildRuntimePolicy','setBuildRuntimePolicy'):owner='runtime_control'
  roles=','.join('"'+v+'"' for v in x['x-permission'])
  lines.append(f'api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_{action.upper()}:{{api.OwnerEnum_OWNER_ENUM_{owner.upper()},[]string{{{roles}}}}},')
lines+=['}']
Path(__file__).with_name('policy_generated.go').write_text('\n'.join(lines)+'\n')
