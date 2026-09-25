"""Compile current permission metadata from the canonical API contract.

The table is the role/delegation policy AuthorizeAction enforces. It is generated
wholesale per served owner from the single canonical owner/action/permission
source, so a role rule can neither be hand-maintained twice nor hand-minimized
into a second policy. A row is policy, not a capability: authorization still
requires a live session, the current role and an actual RPC caller, so a row for
an operation this tree does not yet route is inert rather than an enabled feature.

Session-only operations (anonymous/authenticated) and the invitee-bound
acceptInvitation are not role rows: a live session together with the owner's own
object readback is what authorizes them, so they are handled explicitly and never
appear as a grantable tenant role.
"""
from pathlib import Path
import subprocess
import yaml
root=Path(__file__).resolve().parents[3]
api=yaml.safe_load((root/'docs/spec/target/03_api_contract_complete.yaml').read_text())
# Owner surfaces served by CloudIdentity's policy: the current owner processes and
# the ones whose work packages are switching their real callers to this authority.
owners={'capability','build','tenant','resource_catalog','serve'}
# Runtime Control policy rows live in this table because CloudIdentity is the one
# authorization owner; the x-owner below is the audience the decision names.
runtime={'listRuntimeVersions','registerRuntimeVersion','setRuntimeVersionStatus','getBuildRuntimePolicy','setBuildRuntimePolicy'}
session_only={'getLoginContext','login','logout','getSession'}
subject_bound={'acceptInvitation'}
lines=['// Code generated from the canonical API permissions; DO NOT EDIT.','package identity','import api "opl-cloud/packages/contracts/go/api"','type actionPolicy struct {owner api.OwnerEnum; roles []string}','var actions = map[api.AuthorizationActionEnum]actionPolicy{']
for methods in api['paths'].values():
 for x in methods.values():
  if not isinstance(x,dict) or x.get('x-owner') not in owners: continue
  action=x['operationId'];owner=x['x-owner']
  if action in session_only or action in subject_bound: continue
  if action in runtime:owner='runtime_control'
  roles=','.join('"'+v+'"' for v in x['x-permission'])
  lines.append(f'api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_{action.upper()}:{{api.OwnerEnum_OWNER_ENUM_{owner.upper()},[]string{{{roles}}}}},')
lines+=['}']
out=Path(__file__).with_name('policy_generated.go')
out.write_text('\n'.join(lines)+'\n')
# The generated table is committed source, so emit it already formatted; otherwise
# a regeneration would show formatting churn unrelated to a permission change.
subprocess.run(['gofmt','-w',str(out)],check=True)
