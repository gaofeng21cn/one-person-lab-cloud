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

## Application Selection Target

Workspace identity is independent of its selected application; the
[architecture boundary](../architecture.md#workspace-application-boundary)
owns that decision. Resource provisioning and deployment have independent
progress and success states. A provisioned empty Workspace is usable for later
installation; an application failure does not make the resource purchase fail.
New customer delivery selects an approved Runtime Release for default OPL App,
or an already built immutable Agent version. Default App uses its native UI and
does not create a Package, BuildJob or CapabilityVersion; independent WebUI
selection belongs to the custom Agent build flow. One confirmation must reach
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
update. The customer surface presents assigned application status and access.

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
