# OPL Workspace

Owner: `one-person-lab-cloud`
Purpose: `workspace_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target product reference; implementation and
readiness come from Workspace source, tests, status, and instance readback.

OPL Workspace is an account-owned isolated environment with stable access,
resource entitlement and data bindings. In the Agent delivery path, a Workspace
is the authorized target for one current Agent: it may have no Agent before
delivery, and at most one current Agent after delivery. OPL App is the default
workbench/runtime implementation; other OCI applications may supply their own
UI, API or worker behavior. The
[application boundary](architecture.md#workspace-application-boundary) owns the
runtime implementation, access and data design.

## User Model

| Term | Meaning |
| --- | --- |
| OPL Workspace | One independently addressable, account-owned application environment |
| Current Agent | The single Agent OCI currently delivered to this Workspace; Serve owns the delivery/current-Agent record |
| Application deployment | An application-specific deployment operation; for the Agent path, Serve owns its canonical delivery state |
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
-> for Agent delivery, user selects Package, WebUI and Runtime version in Serve
-> administrator supplies configuration, Secret bindings and optional restore source
-> Workspace authorizes the target and resource plan; Fabric provisions/binds resources
-> Serve delivers immutable OCI to the Workspace's current-Agent slot
-> Serve verifies delivery and routes API / Embed / Hosted UI to that same Agent
-> inspect Agent and Workspace status through their respective owner readbacks
```

A provisioned Workspace may have no Agent. Workspace owns its identity, members,
entitlements, resource plan, lifecycle and authorization to target it. Capability
owns uploaded Agent Package bytes and metadata even when upload begins in a Serve
screen. Build fixes the selected Package, WebUI and Runtime-version inputs and
produces an immutable OCI. Serve owns the delivery operation and the sole
authoritative current-Agent state for that Workspace; replacing the Agent is a
Serve delivery operation, not a second Workspace-owned deployment record.
Fabric provisions/binds compute, storage and network resources and owns their
resource readback. API, Embed and Hosted UI all address the same current Agent.
Historical deliveries may be retained for evidence or rollback, but they are
not simultaneous current Agents. OCI upload/build alone does not deploy it,
change a Workspace, or restore data.

## Workspace Contents

Console exposes application identity, deployment/data progress, resource use,
access and owner evidence. The chosen application owns its internal experience.
When OPL App is selected, it can show projects, task sessions, files, Agent
Instances, artifacts, reviews and continuation entries. Framework supplies its
package state and actions from owner descriptors and fresh native-carrier
readback. Other applications need not implement that workbench model.

The Workspace interface may offer **Deploy Agent with OPL Serve** once Package,
Runtime-version, policy and owner gates are satisfied. The action invokes Serve
with the target `workspace_id`; Serve owns delivery/current-Agent state, while
the Workspace interface displays that state by reading the Serve owner surface.

## Responsibility Boundary

- Capability owns Agent Package identity, uploaded bytes, metadata, capabilities,
  entrypoints and immutable Package versions.
- Build owns fixed Package/WebUI/Runtime inputs, Build operations and immutable
  OCI outputs.
- Runtime Control owns the approved Runtime-version catalog and exact references;
  the OPL App/Framework owner supplies the actual Runtime implementation.
- Serve owns one Agent delivery lifecycle and unique current-Agent state per
  Workspace, plus API/Embed/Hosted UI access and routing to that Agent.
- Workspace owns its identity, membership, lifecycle, entitlements, resource
  plan and target authorization; it does not write a duplicate Agent deployment
  record.
- Fabric provisions/binds compute, storage and network resources and provides
  authoritative resource readback; it does not own Agent OCI deployment.
- Control Plane or the target policy owner owns account availability, quota and
  policy across the Workspace collection; Console presents and calls those
  capabilities.
- Gateway supplies AI access when the application requests that binding.
- Ledger records receipt and opaque provenance refs; the selected application
  retains its business state, including OPL App projects, artifacts, reviews and
  continuation when that application is selected.
- The selected application/runtime owns its UI, business data formats and
  restore validation. Cloud governs Workspace access; Serve owns Agent delivery.
- Console presents Workspace operations and dispatches owner actions.

A Workspace URL opens the selected application under its exposure policy;
visitors may be anonymous or use application-owned/optional platform login.
Management still requires role-specific Cloud authorization. Serve's API,
Embed and Hosted UI are access modes for the Workspace's same current Agent,
not a separate Agent Service collection. A Workspace has at most one current
Agent; prior delivery records are history, not parallel current deployments.

Package availability, resource availability and domain readiness are different
states. Workspace must display their owner and next action rather than collapse
them into a single ready flag.
