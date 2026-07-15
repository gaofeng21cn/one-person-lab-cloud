# OPL Agent Lifecycle In Cloud

OPL Cloud can expose standard OPL Agents in App/Workspace and publish them
through OPL Serve without owning a second agent package platform. Agent design,
package lifecycle, account policy, service publication, resource binding,
execution and evidence remain separate responsibilities.

```text
Agent design / domain source
-> Agent Package candidate
-> OPL Packages validated manifest
-> install transaction + package lock + lifecycle receipt
-> optional Console account availability policy
-> App / Workspace Agent Instance -> Agent Run
or
-> Service Entrypoint Contract
-> OPL Serve Agent Service -> immutable Agent Revision -> Deployment
-> OPL Runway Invocation / Session
-> Fabric/provider resource binding
-> Ledger receipt refs
```

## Lifecycle Objects

| Object | Meaning | Owner |
| --- | --- | --- |
| Agent design | Goal, boundary, stages, inputs, outputs, review rules and authority functions | OMA / domain owner |
| Agent Package | Versioned distributable candidate and its owner source | Package-owning repo |
| Package manifest and lock | Validated identity, digest, dependencies, installed state and lifecycle commit point | OPL Packages |
| Account availability policy | Which package refs the account Workspace may use | OPL Console |
| Resource binding | Compute, storage, environment and connector bindings for one instance or run | OPL Fabric |
| Agent Instance | A package ref exposed in App or Workspace with explicit permissions and resources | OPL App / Workspace |
| Agent Run | One execution with output and review refs | Runtime owner; Ledger records refs |
| Service Entrypoint | Portable action/stage, I/O, event, permission, side-effect and data-policy declaration | Package/domain owner contract |
| Agent Service | Stable publisher-owned external service identity | OPL Serve |
| Agent Revision | Immutable package digest, entrypoint, configuration, policy and provider refs | OPL Serve references owner truths |
| Deployment | Desired revision set, environment, endpoint and traffic policy | OPL Serve |
| Invocation / Session | Bounded request or stateful event sequence against an exact revision | OPL Runway; Serve projects status |

## Product Flow

1. OMA or another domain owner produces an Agent Package candidate.
2. OPL Packages validates its manifest, dependency closure and trust boundary.
3. OPL Packages performs install/update/rollback/repair as a recoverable
   transaction and writes the package lock plus lifecycle receipt.
4. App or Workspace can expose the exact package as an Agent Instance for
   workbench use.
5. A publishable package may additionally declare a Service Entrypoint Contract.
6. Serve creates a stable Agent Service and immutable Revision referencing the
   exact package digest. It does not copy or mutate package state.
7. Console applies account service, quota, budget, data and retention policy.
8. Serve deploys the revision behind its Agent Edge. Runway owns Invocation and
   Session execution and selects an approved provider adapter.
9. Fabric consumes package, revision and policy refs to bind approved compute,
   storage, environments, secrets, network and connectors.
10. Ledger records package, service, deployment, invocation/session, resource,
    output, review and continuation refs without becoming package, service or
    domain truth.

## Failure And Repair

Package download, source replacement, dependency preparation, uninstall,
rollback and repair remain inside the Framework package transaction. A failed
Fabric binding is a resource failure; a denied Console policy is an
account-policy result; neither is permission to rewrite package state. A failed
provider session routes through Runway; a failed deployment routes through
Serve. Neither may be repaired by changing an immutable Revision in place.

## Authority Boundary

- Package present, lock current or resource binding successful does not mean
  the domain Agent is ready or its output is professionally valid.
- Console policy approval does not approve package bytes or domain quality.
- Service publication or endpoint allocation does not prove a deployable or
  production-ready Agent Service.
- Fabric execution success does not produce an owner verdict.
- Ledger receipt presence does not replace package, resource or domain truth.
