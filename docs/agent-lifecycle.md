# OPL Agent Lifecycle In Cloud

OPL Cloud can expose standard OPL Agents without owning a second agent package
platform. Agent design, package lifecycle, organization policy, resource
binding, workbench use and run evidence remain separate responsibilities.

```text
Agent design / domain source
-> Agent Package candidate
-> OPL Packages validated manifest
-> install transaction + package lock + lifecycle receipt
-> optional Console organization availability policy
-> Fabric resource binding
-> App / Workspace Agent Instance
-> Agent Run
-> Ledger receipt refs
```

## Lifecycle Objects

| Object | Meaning | Owner |
| --- | --- | --- |
| Agent design | Goal, boundary, stages, inputs, outputs, review rules and authority functions | OMA / domain owner |
| Agent Package | Versioned distributable candidate and its owner source | Package-owning repo |
| Package manifest and lock | Validated identity, digest, dependencies, installed state and lifecycle commit point | OPL Packages |
| Organization availability policy | Which package refs a team, role or workspace may use | OPL Console |
| Resource binding | Compute, storage, environment and connector bindings for one instance or run | OPL Fabric |
| Agent Instance | A package ref exposed in App or Workspace with explicit permissions and resources | OPL App / Workspace |
| Agent Run | One execution with output and review refs | Runtime owner; Ledger records refs |

## Product Flow

1. OMA or another domain owner produces an Agent Package candidate.
2. OPL Packages validates its manifest, dependency closure and trust boundary.
3. OPL Packages performs install/update/rollback/repair as a recoverable
   transaction and writes the package lock plus lifecycle receipt.
4. When organization governance applies, Console decides whether that exact
   package ref is available to a team, role or workspace. It does not mutate
   the installation or lock.
5. Fabric consumes the package requirements and policy refs to bind approved
   compute, storage, environments and connectors. It does not register packages.
6. App or Workspace exposes the instance and Framework-owned package actions.
7. Ledger records run, package lock, resource, output, review and continuation
   refs without becoming package or domain truth.

## Failure And Repair

Package download, source replacement, dependency preparation, uninstall,
rollback and repair remain inside the Framework package transaction. A failed
Fabric binding is a resource failure; a denied Console policy is an
organization-policy result; neither is permission to rewrite package state.

## Authority Boundary

- Package present, lock current or resource binding successful does not mean
  the domain Agent is ready or its output is professionally valid.
- Console policy approval does not approve package bytes or domain quality.
- Fabric execution success does not produce an owner verdict.
- Ledger receipt presence does not replace package, resource or domain truth.
