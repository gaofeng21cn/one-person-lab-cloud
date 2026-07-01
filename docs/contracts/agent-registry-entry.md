# Agent Registry Entry

OPL Agent Registry records approved OPL-compatible agent packages that can be
instantiated from OPL App or OPL Workspace.

It is part of OPL Fabric and is managed through OPL Console policy.

## Registry Entry Shape

```json
{
  "agent_id": "agent.example",
  "package_name": "Example Agent",
  "package_version": "0.1.0",
  "blueprint_ref": "blueprint-ref",
  "package_ref": "package-ref",
  "owner": "owner-ref",
  "status": "approved",
  "capabilities": [],
  "required_connectors": [],
  "required_environment": "environment-ref",
  "required_compute": {
    "profile": "standard",
    "gpu": false
  },
  "required_storage": {
    "profile": "workspace-volume"
  },
  "review_gates": [],
  "allowed_teams": [],
  "allowed_workspaces": [],
  "quota_policy_ref": "quota-policy-ref",
  "ledger_policy_ref": "ledger-policy-ref"
}
```

## Lifecycle

```text
Agent Blueprint
-> Agent Package
-> Registry review
-> Console approval
-> Fabric resource binding
-> App or Workspace Agent Instance
-> Agent Run
-> Ledger receipt
```

## Status Values

- `draft`
- `reviewing`
- `approved`
- `suspended`
- `retired`

## Required Review

An approved registry entry should make these points clear:

- what the agent is for;
- which package version is approved;
- which teams and workspaces may use it;
- which connectors, compute, storage, and environment it needs;
- which reviewer gates apply;
- which Ledger receipt policy records each run.
