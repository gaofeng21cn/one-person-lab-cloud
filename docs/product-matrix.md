# OPL Cloud Product Matrix

OPL Cloud is the umbrella target architecture for One Person Lab cloud
infrastructure. The repository navigates the implementation family; it does not
claim that every listed service is deployed.

| Layer | Brand | Primary user value | Owner boundary |
| --- | --- | --- | --- |
| AI access | OPL Gateway | Model access, routing and usage | No package or domain truth |
| User workbench | OPL Workspace / OPL App | Continuous project, task, artifact and review experience | Displays owner projections |
| Management | OPL Console | Organization, billing, permissions, approval and managed-resource policy | No package mutation or execution |
| Resource substrate | OPL Fabric | Connect, Compute, Storage, Environments and execution adapters | Resource binding only |
| Evidence record | OPL Ledger | Receipt, provenance, review and continuation refs | Refs only; no owner verdict |
| Package lifecycle | OPL Packages | Manifest, digest, install, lock, update, rollback and repair | Framework-owned, not a Cloud registry |
| Professional work | Domain Agents | Strategy, quality judgment and delivery authority | Domain-owned |

## Agent Use Across Products

```text
Agent Package candidate
-> OPL Packages manifest / lock / lifecycle receipt
-> optional Console organization availability policy
-> Fabric resource binding
-> App or Workspace Agent Instance
-> Ledger run refs
```

Console can allow an exact package ref for a team. Fabric can bind the resources
declared by that ref. App and Workspace can expose it. Ledger can record it.
Only OPL Packages can change installed package state.

## Local And Cloud Workbench

OPL App is the local workbench and OPL Workspace is the cloud workbench. They
share the same task, artifact, resource-plan, approval, execution, collection
and receipt concepts. Console governance starts only when organization policy
or managed Cloud resources apply.

User-provided local, SSH or HPC resources can follow the Fabric execution
contract without becoming Console-billed resources by default. Sensitive data
remains with its owner unless an explicit approved transfer says otherwise.

## Currentness

This matrix defines product responsibility. Current implementation and
availability must be read from each service repo, contract, runtime health and
owner receipt. Package state must be read from `opl packages` and its package
lock, not from a Cloud catalog projection.
