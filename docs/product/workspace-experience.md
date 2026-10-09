# Console Workspace Experience

This document owns the Workspace product capability boundary. The
[Console experience guide](console-experience-guide.md) describes durable user
outcomes without freezing navigation or visual implementation; APIs and domain
contracts own field-level facts and permissions.

## User Job

```text
register -> sign in -> observe zero balance -> receive administrator top-up
         -> list Workspaces -> select default OPL App or a built Agent and plans
         -> confirm one accepted quote -> resources and application progress
         -> inspect owner readiness -> open the delivered application
administrator: govern approved releases, Agent builds, configuration and data
```

The target public beta allows one customer to register one Account and create multiple
independent Workspaces after an administrator funds its Sub2API wallet. A new
Account starts at zero balance, and registration performs no purchase or Fabric
mutation. Each provisioned Workspace has its own launch identity, resources,
entitlement and purchase Receipt, with at most one current application selection.
The
[September 29 decision](../decisions.md#2026-09-29-default-opl-app-and-optional-agent-on-one-tencenttke-delivery-path)
owns the default-App/optional-Agent target. Retained Control Plane Launches and
resource-only orders keep their original contracts; their current callers and
the cloud-identity implementation are mapped in
[implementation architecture](../implementation-architecture.md#console-source-truth).

## Agent Upload, Build And Version Tasks

The customer supplies a standard OMA/OPL Agent Package, name and upload version,
then requests Build. Runtime/WebUI defaults are selected by the administrator's
approved effective policy, not a customer picker or the first catalog result.
Show uploading, building, deployable and a specific failure reason; retain
resumable operation identity and expose technical details only on demand.
Package origin is not a substitute for actual format/compatibility validation.

A Package/Runtime/WebUI change creates a new immutable Build/Agent version.
Show available updates, allow explicit rebuild from the retained Package, then
explicit deployment/switch. One Package upload may have several built artifacts;
do not collapse them into the first CapabilityVersion. Existing Builds retain
their exact inputs even after the default changes. Missing/incompatible active
inputs are explained without silent substitution or resource purchase.

A deployable version offers an entitled empty Workspace or the existing quote
flow for a new independent Workspace. New data is not automatically copied from
an old Workspace, and the old resources/order are not deleted or refunded.

## Application Selection Target

Workspace identity is independent of its selected application; the
[architecture boundary](../architecture.md#workspace-application-boundary)
owns that decision. Resource provisioning and deployment have independent
progress and success states. A provisioned empty Workspace is usable for later
installation; an application failure does not make the resource purchase fail.
New customer delivery uses the administrator-selected approved active Runtime
Release for default OPL App, or a customer-selected ready immutable Agent version. Default App uses its native UI and
does not create a Package, BuildJob or CapabilityVersion; independent WebUI
inputs belong to the administrator-selected custom Agent build policy. One confirmation must reach
an application that can actually be used, while resource and application
progress remain separate facts.

For retained administrator-managed application installation, the administrator
selects the target Workspace, registry connection,
repository and image/tag,
resolves the immutable version, then supplies startup/configuration/Secret
inputs, persistent mounts, exposure policy and optional data restoration.
Resource-fit preview precedes deployment onto the existing Workspace. One
application can include several private services. OPL App is the
default selection, and its Package/task controls appear only when applicable.

Registration of a TCR reference, restoration of data and replacement of an
application are distinct actions with distinct results. Selecting a revision
for one Workspace does not change other Workspaces or the installation default.
A new registry version or default change does not upgrade existing Workspaces.
Arbitrary registry/image installation remains an admin capability; account
ownership does not authorize it. The approved default-App/Agent selection does
not introduce customer self-service image browsing.
The preview explains data compatibility, any planned interruption and whether
additional capacity would require a separate purchase. Cloud management always
requires role-specific authorization. Visitor access follows the selected exposure:
IBD may allow anonymous use, OPL App may use its own password, and a private
entry may additionally require a Cloud account. Application login is not a
universal provisioning or deployment requirement.
The retained Control Plane source exposes the OPL App credential and image
controls described below; complete owner-backed maintenance is an
[open outcome](../roadmap.md#workspace-application-decoupling).

## Customer Version Switching And Cleanup

Customers can switch forward/backward among retained ready artifacts of their
same Tenant/Package identity. Default App Runtime and unrelated-application
replacement remain administrator-only. Preview names the current/target version,
existing resource fit, data compatibility and permitted downtime. Confirm once,
read the same operation after refresh, and show success only after current-version
and real application readiness/access readback. A rejected or unknown result is
not success, a new purchase, automatic expansion or permission to erase data.
Insufficient capacity links to another Workspace or a new explicit quote.

Keep three actions distinct:

| Action | Result and protected facts |
| --- | --- |
| Switch version | Changes only the named Workspace's Serve current application after authorization and compatibility/resource checks |
| Clean local version | Retires only unused inactive runtime artifacts/cache; retains cloud OCI, CBS business data, Packages, records and receipts; excludes shared/current/in-flight references |
| Delete cloud artifact | Explicitly deletes an unused exact Tenant OCI with reference checks; retains metadata/history/data but removes direct pull/rollback availability for that artifact |

A retained cloud OCI can be pulled again after local cleanup when compatible.
Local cleanup is not registry deletion, Workspace deletion, or proof that shared
layers immediately released disk bytes. Report actual observed absence and any
available measured space separately. Each Tenant has at most 50 distinct cloud
Agent artifact root digests; count aliases/platform children once and do not count
Package metadata or local copies. At capacity, show usage and ask for explicit
unused-artifact deletion; no automatic eviction. Publication/deletion are durable,
reference-safe owner actions and unknown results do not free quota.

## Owner Surface

The retained Console surface shows:

- live Sub2API USD balance;
- fixed Basic or Pro Workspace package price in USD;
- general Gateway Key create, enable/disable, delete, reveal/copy, and per-Key
  Usage readback;
- resource status, `paidThrough`, auto-renew, and manual-review state;
- Workspace access, billing receipts, messages, and account information.

The [experience guide](console-experience-guide.md) owns the customer task
hierarchy and navigation. The Support ticket surface is retired.

The Workspace access area answers, in one place and from owner readback: URL,
用户名, 密码 reveal/copy, and the corresponding Workspace Key reveal/copy. The
Workspace Key reuses `POST /api/gateway/keys/{keyId}/reveal`; it does not create a
second secret store or Key API. Console does not expose a Gateway base-address
card or link to the server-only Sub2API backend.

Console does not show raw request fingerprints, provider credentials, generic
Fabric/Ledger APIs, Sub2API admin operations, or internal identifiers in the
default customer layer. Useful source and diagnostic facts remain available in
closed `技术详情` disclosures where the current surface has a diagnostic need.

## Admin Surface

Operations sees account mappings, roles, wallet recharge/debit/business refund,
receipt and review evidence, reconciliation reports, readiness, announcements,
and only server-authorized reconciliation or recovery actions. Resource rows
show owner account/user, Workspace, resource type, package/spec, provider ID,
Zone, status, created and expiry times, last readback, and operation/Receipt
references. A missing owner source displays unavailable; Fabric or Ledger facts
are not copied into a new Control Plane truth table.

Keep the existing resource-list and Workspace-detail structure. Extend the
Workspace's administrator controls with application deployment, image/version
selection, data bindings, preview, progress and compatible rollback. Show
resource readiness independently of the selected application/version and its
availability: a provisioned Workspace can display “待部署”, and an application
failure does not make its delivered compute/storage failed. Each operation
shows its explicit target Workspace; changing a default does not imply a fleet
update. The customer surface additionally offers its authorized same-Agent
switching/cleanup tasks, not arbitrary-image installation.

Platform Runtime/WebUI import, validation/approval and active-default selection
belong in platform catalog management, separate from Workspace operations.
Import alone does not make a version the default. Reuse the current owning APIs
and publisher declarations; do not require repeated manual registration per
Workspace. Agent/OCI management defaults to ownership, version, build diagnostics,
digest and deployment relations, not private Package downloads or credentials.
Customer management initially offers view/admit/enable/disable. Disable blocks
new Cloud management commands without implicit app shutdown, subscription or
renewal cancellation, Gateway Key revocation, deletion or refund. In-flight
accepted operations can recover. Multi-member management is deferred.

## Purchase Confirmation

Workspace confirmation shows the selected package/spec, exact total USD charge,
current balance, entitlement period, and the compute/storage fulfillment included
in that total.

The operation is resumable. Read-only provider preflight failure or insufficient
balance stops before debit and before every Fabric write. Ambiguous external
results enter manual review.
Confirmed single charge activates the Workspace after fulfillment and emits one
purchase Receipt. Compute and storage never debit the customer independently.

## Workspace And Storage

A Workspace keeps stable identity and access while application and data
bindings have separate lifecycles. The retained Control Plane path uses one
independently owned StorageVolume and a current runtime pointer; the application target supports
explicit data bindings for its component services. On Tencent, retained data
uses Workspace-owned CBS through the declared application mounts. Compatible
image updates keep the existing data set and bindings; unrelated applications
keep separate data sets, and no Workspace's update alters another's data or
version. Merely running on a CVM does not persist container-local writes.
Purchase confirmation and Workspace details explain that customers must download and back up their data
before expiry. The platform does not promise data retention or restoration
after expiry.

An unpaid Workspace stops providing access and its Runtime is stopped. Details
show whether the original resources can be recovered, need verification, or
have been reclaimed. “续费并恢复” states the monthly charge and that it enables
subsequent auto-renewal; it requires explicit confirmation. Recharging alone
never submits a recovery. The entry opens only after entitlement and Runtime
readiness are both confirmed; uncertain results continue the same operation.

Permanent deletion confirms the customer's intent once and continues in the
background. Reopening details reads the original deletion progress. Removing a
Workspace from the list is insufficient to show success while its deletion
Receipt is pending. Deletion and any permitted original-charge refund remain
separate verified operations. There is no new customer
backup platform, resource replacement or free retention capability.

## Availability

This document defines the intended interaction. Current implemented and verified
capability is recorded in [status](../status.md), with open outcomes in
[roadmap](../roadmap.md).
