# OPL Workspace

Owner: `one-person-lab-cloud`
Purpose: `workspace_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target product reference; implementation and
readiness come from Workspace source, tests, status, and instance readback.

OPL Workspace is an account-owned isolated environment for running an admitted
application with stable access, resource entitlement and data bindings. OPL App
is the default workbench application; other OCI applications may supply their
own UI, API or worker behavior. The
[application boundary](architecture.md#workspace-application-boundary) owns the
runtime, access and data design.

## User Model

| Term | Meaning |
| --- | --- |
| OPL Workspace | One independently addressable, account-owned application environment |
| Application deployment | The Workspace's selected immutable application revision, potentially containing several cooperating services |
| Workspace Storage | Independently bound persistent application data; temporary process state is declared separately |

One user account may own zero or more independent OPL Workspaces. The product
does not impose a fixed count limit; each creation is admitted independently by
balance, provider capacity, quota, and policy. Workspace state is keyed by
`workspace_id`, never by an account singleton.

## MVP Boundary

The current implemented carrier is an OPL App/WebUI container created and
managed through the real product chain:

```text
Console -> Control Plane -> Workspace launcher/provider -> local Docker
```

Core completion requires create, authoritative readback, access, and delete on
a supported Linux Docker host. Compose startup of the Cloud control services
does not satisfy this boundary. The Local-Docker provider exists, but one
exact-current clean-host product journey remains open; see
[current capability](status.md) and the current [P0 gaps](roadmap.md).

## Workspace Product Flow

```text
open the account Workspace list
-> provision compute/storage or select an existing provisioned Workspace
-> administrator selects registry/repository/image and checks resource fit
-> administrator supplies configuration, Secret bindings and optional restore source
-> provision or bind services and data; restore only when explicitly requested
-> verify application readiness and open its entry under the chosen access policy
-> update the application, inspect status, or manage the Workspace lifecycle
```

A provisioned Workspace may have no installed application. Provisioning fulfills
compute/storage and records its own result; later installation, replacement and
rollback use independent deployment operations on those existing resources.
The initial application scope is zero or one deployment per Workspace, including
its supporting services. An authorized administrator selects the registry,
repository and image version, then supplies the declared startup, configuration,
mounts and exposure policy. Customers use the assigned application; account
ownership alone does not grant distribution permission. Each Workspace keeps
its own revision and stable data bindings; updates reuse persistent data, while
other Workspaces retain their own versions and data. Uploading an OCI image
does not automatically deploy it, update existing Workspaces or restore data.

## Workspace Contents

Console exposes application identity, deployment/data progress, resource use,
access and owner evidence. The chosen application owns its internal experience.
When OPL App is selected, it can show projects, task sessions, files, Agent
Instances, artifacts, reviews and continuation entries. Framework supplies its
package state and actions from owner descriptors and fresh native-carrier
readback. Other applications need not implement that workbench model.

Workspace may expose a **Publish to OPL Serve** action after the package,
entrypoint, policy and owner gates are satisfied. The action calls Serve owner
surfaces, which create canonical Service, Revision, Deployment, endpoint, and
traffic state.

## Responsibility Boundary

- Package owners control identity, capabilities, entrypoints and exact
  publication revisions.
- Configured native carriers control physical install, update, remove and fresh
  installed/callable readback; Framework delegates and aggregates.
- Control Plane owns account availability, quota and policy across the
  account's Workspace collection; Console presents and calls those capabilities.
- Serve owns Agent Service publication, immutable revisions and external endpoints.
- Fabric binds and runs compute, storage, environment and connector resources.
- Gateway supplies AI access when the application requests that binding.
- Ledger records receipt and opaque provenance refs; the selected application
  retains its business state, including OPL App projects, artifacts, reviews and
  continuation when that application is selected.
- The selected application owns its UI, business data formats and restore
  validation. Cloud governs its deployment and Workspace access.
- Console presents Workspace operations and dispatches owner actions.

A Workspace URL opens the selected application under its exposure policy;
visitors may be anonymous or use application-owned/optional platform login.
Management still requires role-specific Cloud authorization. An Agent Service endpoint
has its separate publication lifecycle for external consumers. Both collections have zero-to-many account cardinality and
separate product identities and lifecycles.

Package availability, resource availability and domain readiness are different
states. Workspace must display their owner and next action rather than collapse
them into a single ready flag.
