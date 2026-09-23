# OPL Serve

Owner: `one-person-lab-cloud`
Purpose: `serve_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable target product definition.

OPL Serve is the planned OPL Cloud product for delivering a validated OPL Agent
to users. A Serve delivery targets a Workspace and makes that Workspace's one
current Agent available through API, Embed, and, when needed, an OPL-hosted user
interface. Serve owns the canonical delivery/deployment state for that Agent;
Workspace remains the target and authorization boundary, not a second writer of
Agent deployment state.

## Product Promise

An Agent builder can upload an Agent Package through the Serve product flow.
The upload UI is an entry point only: Capability validates and owns the uploaded
bytes, Package identity, metadata, and immutable Package version. The builder
then chooses a WebUI and an approved Runtime version. Build fixes those exact
inputs and produces an immutable OCI artifact. Serve delivers that OCI to the
selected Workspace.

External consumers can use that service in three ways:

| Mode | Consumer experience | Product boundary |
| --- | --- | --- |
| API | The publisher connects the Agent to an existing App, site, or backend | OPL Serve exposes authenticated invocation and session APIs |
| Embed | The publisher embeds an OPL-provided interaction component | The component uses the same public API and short-lived client credentials |
| Hosted UI | OPL hosts a branded task, report, workflow, or chat surface | The UI is an official API client, not a second execution path |

An account may own zero or more Workspaces, but each Workspace has at most one
current Agent delivered through Serve. Publishing or updating an Agent does not
create a parallel Agent Service beside the Workspace: it installs or replaces
the Agent in that Workspace's single current-Agent slot. Historical delivery
operations and revisions may be retained for audit and rollback without being
additional current Agents.

## Brand And Product Language

| Term | Meaning |
| --- | --- |
| OPL Serve | The user-visible Agent publishing and serving product |
| Agent Package | Immutable Agent bytes and descriptive metadata owned by Capability |
| Runtime version | An approved OPL App/Agent Runtime release reference, cataloged by Runtime Control |
| OCI artifact | Immutable Build output fixing Package, WebUI, and Runtime inputs by exact references/digests |
| Workspace Agent delivery | Serve-owned state delivering one OCI to one Workspace's current-Agent slot |
| Invocation | One bounded request to the Workspace's current Agent |
| Session | A stateful sequence of events against that same current Agent |
| API / Embed / Hosted UI | Different access surfaces to the same Workspace Agent, not separate deployments |

The preferred publisher action is **Deploy Agent to Workspace** (or the
equivalent Serve action in the product UI). External consumers primarily see
the publisher's Agent name and brand. A secondary `Hosted on OPL Cloud`
attribution may be shown. Model-provider attribution remains optional and must
follow that provider's branding rules.

## Publication Flow

```text
User chooses Package file and delivery Workspace in Serve UI
-> Capability validates upload and owns immutable Package version + metadata
-> user selects WebUI and approved Runtime version
-> Build fixes Package + WebUI + Runtime refs and produces immutable OCI digest
-> Serve creates a delivery operation for the target Workspace
-> Workspace authorizes target, member, entitlement, and resource-plan use
-> Fabric provisions/binds requested infrastructure resources and reads them back
-> Serve delivery executor applies the OCI to the Workspace Agent slot
-> Serve records authoritative delivery/current-Agent state and verifies outcome
-> API / Embed / Hosted UI access that same current Agent
-> Ledger receives evidence references where required
```

The uploader may begin in Serve, but Capability is the only Package writer.
Build owns build operations and OCI output; Runtime Control owns the approved
Runtime-version catalog and exact references, not Agent instances or deployment.
Workspace owns Workspace identity, membership, entitlements, resource plan, and
authorization to target that Workspace. Fabric owns infrastructure resource
provisioning, bindings, and resource readback. Serve owns the Agent-delivery
operation and the authoritative current-Agent deployment state for each
Workspace. Workspace must not maintain a competing authoritative current-Agent
record; its UI reads Serve-owned delivery state.

## Serving Architecture

```text
consumer App / site / Hosted UI
-> OPL Serve Agent Edge
-> Serve authentication, access policy, quota, rate limit and routing
-> the Workspace's one current Agent OCI running on its selected Runtime version
-> OPL Fabric-provisioned/bound compute, storage, and network resources
-> OPL Gateway / OPL Connect when the Agent requires those capabilities
-> outputs, events and artifacts
-> OPL Ledger receipt refs
```

The Agent Edge terminates public traffic and routes all supported access modes
to the same Workspace Agent. The exact OCI digest is immutable; changing the
Agent Package, WebUI, or Runtime version requires a new Build output and a Serve
delivery/replacement operation. A sandbox, container, or provider resource is
never exposed directly as the stable public endpoint.

Short actions may return synchronously within a bounded timeout. Long-running
work returns an `invocation_ref` and continues through event streaming, polling,
or signed Webhooks. Stateful interaction uses an explicit Session rather than
turning every request into an unrelated run.

## Hosted UI Boundary

Initial Hosted UI templates should cover repeatable Agent interaction patterns:

- Task: structured input, background progress, completion, and notification.
- Report: material upload, generated artifacts, review status, and delivery.
- Workflow: multi-stage progress, approval points, outputs, and history.
- Chat: stateful turns, streaming events, file exchange, and interruption.

Templates consume the public Serve API and Package-declared entrypoint
schemas. They may
project publisher name, logo, theme, domain, authentication mode, inputs,
outputs, events, artifacts, and support links. Public authentication and quota
policy apply to every template.

Browser clients use short-lived consumer credentials. A publisher's server-side
service key or provider credential must never be shipped to a browser template
or embed component.

## Ownership

| Concern | Owner |
| --- | --- |
| Agent design, professional behavior, Skills, quality and delivery authority | OMA / domain owner / Agent publisher as applicable |
| Uploaded Package bytes, Package identity, metadata and immutable Package versions | Capability |
| Approved OPL App/Agent Runtime versions and exact version references | Runtime Control; Runtime implementation/release remains with OPL App/Framework owner |
| Build inputs, build operation, immutable OCI digest and build evidence | Build |
| Workspace identity, membership, entitlement, resource plan and target authorization | Workspace |
| Per-Workspace Agent delivery operation and unique current-Agent deployment state | OPL Serve |
| API / Embed / Hosted UI access policy and routing to the current Agent | OPL Serve |
| Account-wide policy and financial governance | OPL Console / owning policy module |
| Compute, storage, network provisioning/binding and resource readback | OPL Fabric |
| Model access, provider policy, routing and model usage | OPL Gateway |
| External data and tool access | OPL Connect |
| Immutable evidence and opaque references (where the product requires receipts) | OPL Ledger |

## Provider Model

OPL Serve is provider-neutral. Its delivery executor uses the selected Workspace
runtime mechanism to apply an exact OCI artifact; the OCI contains the chosen
Agent Package, WebUI, and approved Runtime version. The Runtime implementation
is supplied by the external OPL App/Framework owner and is not a second Serve
runtime implementation. Fabric provisions and reports infrastructure resources;
it does not own Agent deployment or Runtime lifecycle. Add a provider adapter
only for a current, evidenced provider requirement.

Serve delivery, Workspace, Package, Build, and Runtime-version references remain
canonical across providers. Provider-specific resource identifiers are refs,
not alternate Agent or Runtime authorities.

## Data And Security Boundary

Each Package/Build input declares or references input/output schemas,
permissions, side effects, resource requirements, data classification,
retention, egress, cancellation, and
artifact policies. Serve and Console apply the publisher's account policy before
traffic reaches the current Workspace Agent. Workspace authorization and
Fabric resource facts remain owner-read; Serve does not copy them into competing
authoritative records.

Hosted execution does not imply that all data may leave its owner boundary.
Provider eligibility must be selected from the declared data policy. Sensitive
or regulated workloads may require an OPL-native, institution-owned, or otherwise
approved execution path even when another provider offers a managed sandbox.

## Commercial Boundary

The first commercial boundary bills the publisher account for service, model,
execution, storage, and managed connector usage.

## Currentness

This document describes the target product and ownership model, not proof that
the complete upload-to-serving flow is implemented. Current implementation
evidence belongs in [status](status.md); an accepted Serve implementation
outcome belongs in the [roadmap](roadmap.md). Existing OCI/Workspace runtime
operations, if present, do not by themselves prove the Package upload -> Build
-> Serve delivery -> API/Embed/Hosted UI end-to-end path.
