# Console Workspace Experience

This document owns the Workspace product capability boundary. The
[Console experience guide](console-experience-guide.md) describes durable user
outcomes without freezing navigation or visual implementation; APIs and domain
contracts own field-level facts and permissions.

## User Job

```text
register -> sign in -> observe zero balance -> receive administrator top-up
         -> list Workspaces -> select Basic or Pro -> confirm one Workspace total
         -> provision -> reveal/copy that Workspace access -> open Workspace
```

The target public beta allows one customer to register one Account and create multiple
independent Workspaces after an administrator funds its Sub2API wallet. A new
Account starts at zero balance, and registration performs no purchase or Fabric
mutation. Each Workspace has its own launch identity, resources, Key, Secret,
entitlement, Runtime, and Receipt.

## Owner Surface

Console shows:

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

A Workspace is a stable URL backed by one independently owned StorageVolume and
the current runtime pointer. Purchase confirmation and Workspace details explain
that customers must download and back up their data before expiry. The platform
does not promise data retention or restoration after expiry.

An unpaid Workspace stops providing access and its Runtime is stopped. Details
show whether the original resources can be recovered, need verification, or
have been reclaimed. “续费并恢复” states the monthly charge and that it enables
subsequent auto-renewal; it requires explicit confirmation. Recharging alone
never submits a recovery. The entry opens only after entitlement and Runtime
readiness are both confirmed; uncertain results continue the same operation.

Permanent deletion confirms the customer's intent once and continues in the
background. Reopening details reads the original deletion progress. Removing a
Workspace from the list is insufficient to show success while its deletion
Receipt is pending. Normal deletion has no refund. There is no new customer
backup platform, resource replacement or free retention capability.

## Availability

This document defines the intended interaction. Current implemented and verified
capability is recorded in [status](../status.md), with open outcomes in
[roadmap](../roadmap.md).
