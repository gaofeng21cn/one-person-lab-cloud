# OPL Cloud Product Matrix

OPL Cloud is the umbrella target architecture for One Person Lab cloud
infrastructure. The repository navigates the implementation family; it does not
claim that every listed service is deployed.

| Layer | Brand | Primary user value | Owner boundary |
| --- | --- | --- | --- |
| AI access | OPL Gateway | Model access, routing and usage | No package or domain truth |
| User workbench | OPL Workspace / OPL App | Continuous project, task, artifact and review experience | Displays owner projections |
| Agent serving | OPL Serve | Publish an exact Agent revision as an API, Embed, or Hosted UI | Service state only; no package or domain truth |
| Management | OPL Console | User account, billing, permissions, approval and managed-resource policy | No package mutation or execution |
| Resource substrate | OPL Fabric | Connect, Compute, Storage, Environments and execution adapters | Resource binding only |
| Evidence record | OPL Ledger | Receipt, provenance, review and continuation refs | Refs only; no owner verdict |
| Package lifecycle | OPL Packages | Manifest, digest, install, lock, update, rollback and repair | Framework-owned, not a Cloud registry |
| Professional work | Domain Agents | Strategy, quality judgment and delivery authority | Domain-owned |

## Agent Use And Service Publication

```text
Agent Package candidate
-> OPL Packages manifest / lock / lifecycle receipt
-> optional Console account availability policy
-> App or Workspace Agent Instance -> Agent Run
or
-> Service Entrypoint Contract
-> OPL Serve Agent Service -> immutable Revision -> Deployment
-> Invocation or Session
-> Ledger run/service refs
```

Console can allow an exact package ref for an account. Fabric can bind the
resources declared by that ref. App and Workspace can expose it for workbench
use. OPL Serve can publish an immutable service revision that references it.
Ledger can record either path. Only OPL Packages can change installed package
state.

## Local And Cloud Workbench

OPL App is the local workbench and OPL Workspace is the cloud workbench. They
share the same task, artifact, resource-plan, approval, execution, collection
and receipt concepts. Each user account owns one primary OPL Workspace. Console
governance starts only when account policy or managed Cloud resources apply.

User-provided local, SSH or HPC resources can follow the Fabric execution
contract without becoming Console-billed resources by default. Sensitive data
remains with its owner unless an explicit approved transfer says otherwise.

## Workspace And Service Identity

One account owns one primary OPL Workspace, but may publish multiple Agent
Services. A Service is an externally callable deployment resource, not another
Workspace or workbench. Its API, Embed, and Hosted UI surfaces terminate at OPL
Serve and cannot reuse a Workspace access URL or expose a sandbox directly.

## Currentness

This matrix defines product responsibility. Current implementation and
availability must be read from each service repo, contract, runtime health and
owner receipt. Package state must be read from `opl packages` and its package
lock, not from a Cloud catalog projection.
