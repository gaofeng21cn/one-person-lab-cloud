"""Compile the canonical API contract into the contract-derived runtime tables.

It emits two tables that must not drift from the SSOT and must not be
hand-maintained:

  1. the CloudIdentity authorization policy, and
  2. the Console BFF's HTTP success status per routed action.

Both are read from the same `03_api_contract_complete.yaml`, so a declared
permission or success code reaches the running guard without a second, hand-
written list.


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
# Owner surfaces CloudIdentity authorizes. The set is every owner whose operations
# are reachable through the BFF or whose work package has already switched a real
# caller to this authority; an owner is added once, wholesale, never per action.
owners={'capability','build','tenant','resource_catalog','serve','workspace'}
# Runtime Control policy rows live in this table because CloudIdentity is the one
# authorization owner; the x-owner below is the audience the decision names.
runtime={'listRuntimeVersions','registerRuntimeVersion','setRuntimeVersionStatus','getBuildRuntimePolicy','setBuildRuntimePolicy'}
# getWorkspaceAccess is the Serve access fact reachable through the BFF. Its
# operation is named on ServeProductService and Serve owns
# serve.agent_runtime_instances/serve.access_bindings, so the audience is serve
# even though the operation routes through the Workspace product path.
audience_overrides={'getWorkspaceAccess':'serve'}
session_only={'getLoginContext','login','logout','getSession'}
subject_bound={'acceptInvitation'}
lines=['// Code generated from the canonical API permissions; DO NOT EDIT.','package identity','import api "opl-cloud/packages/contracts/go/api"','type actionPolicy struct {owner api.OwnerEnum; roles []string}','var actions = map[api.AuthorizationActionEnum]actionPolicy{']
for methods in api['paths'].values():
 for x in methods.values():
  if not isinstance(x,dict) or x.get('x-owner') not in owners: continue
  action=x['operationId'];owner=x['x-owner']
  if action in session_only or action in subject_bound: continue
  if action in runtime:owner='runtime_control'
  if action in audience_overrides:owner=audience_overrides[action]
  roles=','.join('"'+v+'"' for v in x['x-permission'])
  lines.append(f'api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_{action.upper()}:{{api.OwnerEnum_OWNER_ENUM_{owner.upper()},[]string{{{roles}}}}},')
lines+=['}']
out=Path(__file__).with_name('policy_generated.go')
out.write_text('\n'.join(lines)+'\n')
# The generated tables are committed source, so emit them already formatted;
# otherwise a regeneration would show formatting churn unrelated to a change.
subprocess.run(['gofmt','-w',str(out)],check=True)

# The BFF guard answers with the status the contract declares for the operation,
# so an operation whose declared code is not the write default (202/204/200)
# cannot silently answer 201 instead.
bff=root/'apps/console-bff/internal/httpapi/status_generated.go'
status=['// Code generated from the canonical API success codes; DO NOT EDIT.','package httpapi','import api "opl-cloud/packages/contracts/go/api"','','// successStatus is the HTTP status the canonical contract declares for each routed','// operation. publisherRoute answers with this value, so a response code cannot be','// hand-maintained per route and cannot drift from the contract.','var successStatus = map[api.AuthorizationActionEnum]int{']
for methods in api['paths'].values():
 for x in methods.values():
  if not isinstance(x,dict) or 'operationId' not in x: continue
  codes=sorted(int(c) for c in x.get('responses',{}) if str(c).startswith('2'))
  if not codes: continue
  action=x['operationId']
  enum='api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_'+action.upper()
  status.append(f'{enum}: {min(codes)},')
status+=['}']
status+=['', '// errorStatus is the canonical public status for an owner-supplied business error.', 'var errorStatus = map[api.ErrorCodeEnum]int{']
for code, definition in api['x-error-http-status'].items():
 if definition['httpStatus'] is None: continue
 status.append(f'api.ErrorCodeEnum_ERROR_CODE_ENUM_{code}: {definition["httpStatus"]},')
status+=['}']
bff.write_text('\n'.join(status)+'\n')
subprocess.run(['gofmt','-w',str(bff)],check=True)
