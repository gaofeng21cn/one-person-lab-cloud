# OPL Agent Lifecycle In Cloud

Owner: `one-person-lab-cloud`
Purpose: `cloud_agent_lifecycle_reference`
State: `active_target_reference`
Machine boundary: Human-readable target cross-surface object and owner model.
Actual implementation and runtime evidence remain with the current owning source,
test, service, Runtime implementation, and domain surfaces; this target model
does not assert that the end-to-end flow already exists.

This is the target Cloud Agent delivery model, not a claim that the complete
flow is implemented. Agent design, Package ownership, Runtime release selection,
OCI build, Workspace authorization, Agent delivery, infrastructure resources,
execution and evidence remain separate responsibilities. OPL Serve is the
delivery product and owns the authoritative current-Agent delivery state for a
Workspace; it is not a second Agent Package or Runtime owner.

```text
Agent design / domain source
-> Agent Package upload begins in Serve UI
-> Capability validates and stores uploaded bytes + metadata as immutable Package version
-> user selects WebUI and an approved Runtime version from Runtime Control catalog
-> Build fixes Package + WebUI + Runtime refs/digests and produces immutable OCI
-> user requests delivery to a Workspace through Serve
-> Workspace authorizes its identity, member, entitlement, target and resource plan
-> Fabric provisions/binds infrastructure and returns authoritative resource readback
-> Serve delivery executor applies OCI and owns the single current-Agent state for Workspace
-> Runtime implementation supplied by OPL App/Framework runs OCI in Workspace
-> API / Embed / Hosted UI route to that same Workspace Agent
-> Agent invocation/session evidence refs -> Ledger where required
```

## Lifecycle Objects

| Object | Meaning | Owner |
| --- | --- | --- |
| Agent design | Goal, boundary, stages, inputs, outputs, review rules and authority functions | OMA / domain owner |
| Agent design | Goal, boundary, stages, inputs, outputs, review rules and authority functions | OMA / domain owner |
| Agent Package | Uploaded distributable bytes, identity, metadata and immutable versions | Capability |
| WebUI selection | Exact selected WebUI reference/version | Build input; WebUI remains with its owning source |
| Runtime release | Approved Runtime-version catalog and exact selectable references | Runtime Control; implementation/release belongs to OPL App/Framework owner |
| OCI Build | Fixed Package/WebUI/Runtime inputs, build operation, immutable OCI digest and evidence | Build |
| Workspace | Identity, membership, lifecycle, entitlement, resource plan and target authorization | Workspace |
| Agent delivery/current state | Delivery operation and unique current-Agent OCI for a Workspace | OPL Serve |
| Resource binding | Compute, storage and network resources and their current facts | OPL Fabric |
| Runtime execution | Loading/running OCI and execution observations | OPL App/Framework Runtime implementation; Serve owns delivery status, not Runtime internals |
| Invocation / Session | Bounded request or stateful sequence against the current Workspace Agent | Serving/runtime path; owner contract determines execution details |
| Evidence refs | Opaque provenance and receipt references | OPL Ledger |

## Product Flow

1. Agent design owner creates the package content and declared Skills/entrypoints.
2. User begins upload in Serve; Capability validates and persists the exact
   Package bytes, metadata, identity and immutable version. Serve UI is not the
   Package writer.
3. User selects the WebUI and Runtime version. Runtime Control supplies only
   approved Runtime-version references; the OPL App/Framework owner supplies
   the Runtime implementation.
4. Build consumes exact Package, WebUI and Runtime refs and emits an immutable
   OCI digest plus build evidence. OCI fixes those inputs; changing one requires
   a new Build output.
5. User requests delivery to a target Workspace. Workspace authorizes the
   target, member, entitlement and resource plan; it does not create another
   current-Agent deployment record.
6. Fabric provisions/binds approved infrastructure resources and returns
   resource facts. It does not install or own Agent OCI lifecycle.
7. Serve executes the delivery, reads the Agent result, and records the one
   authoritative current-Agent state for that Workspace. Replacement changes
   that current state; history may be retained for audit/rollback.
8. API, Embed and Hosted UI authenticate and route to the same current Agent.
   The Runtime implementation runs OCI and reports execution observations.
9. Ledger records required Package, Build, delivery, invocation/session,
   resource and output refs as opaque provenance, without becoming their Owner.

## Readiness Evidence

Package, Build, Runtime-version approval, Workspace authorization, Serve
delivery, Fabric resource readback, Runtime execution and domain quality are
separate owner facts. A readiness statement combines relevant owner readbacks;
no single Package, policy, delivery, resource, execution or receipt result
substitutes for the others. Existing OCI-to-Workspace evidence does not prove
the full upload -> Build -> delivery -> serving path.

## Failure And Repair

Capability handles Package upload/validation failures; Runtime Control handles
Runtime-version catalog/reference failures; Build handles OCI build failures;
Workspace handles target authorization/resource-plan failures; Fabric reports
infrastructure provisioning/binding failures; Serve reports delivery failures
and owns current-Agent state; the external OPL App/Framework Runtime
implementation reports execution failures; Console/policy owners report policy
decisions. Immutable Package or OCI changes create a new version/digest through
the corresponding owner path.
