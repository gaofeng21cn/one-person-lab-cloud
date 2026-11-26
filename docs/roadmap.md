# OPL Cloud Roadmap And Current Gaps

Owner: `opl-cloud`
Purpose: `open_outcomes_and_acceptance`
State: `active`

This page tracks only open gaps and acceptance outcomes. Current evidence belongs
to [status.md](status.md); durable decisions belong to [decisions.md](decisions.md);
target ownership belongs to [architecture.md](architecture.md). Completed work is
removed here and preserved in Git history.

## Priority Outcomes

### Protected Same-Candidate Lifecycle & Paid Customer Acceptance

Close end-to-end customer lifecycle verification for the latest Cloud Candidate on
`opl-instance-medopl` Tencent/TKE:

- Default OPL App launch (`create -> quote -> debit -> Fabric provision -> Serve delivery -> readiness -> access -> renewal -> deletion -> refund/reconcile -> Ledger receipt`), plus custom Agent Package build-to-serve E2E on TKE.
- The renewal step of that chain is not closed yet. The Resource Catalog prices
  the renewal offer and the Fabric owner can extend the provider's prepaid month
  for the same compute and storage the Workspace already owns, but no accepted
  grant can reach that extension yet: the identity owner has to admit
  `renewAcceptedResources` for a Workspace-owned renewal obligation, and the
  Workspace owner still has to charge the accepted offer and commit the renewed
  paid period. The Workspace owner also has to reconcile the two periods: the
  provider extends its own prepaid deadline, while the paid period of an expired
  Workspace starts at the renewal instant.
- Verify that one customer deletion reaches confirmed CBS final release, compute
  absence, and settlement through the installed Fabric path without an operator
  releasing the recycled disk.
- Invoke protected J0 READY producer against the same Candidate digest and capture Instance-side profile-digest-bound installation summary receipts.
- Retire the remaining 11 zero-replica split Deployments on Instance only after fresh readback confirms zero active traffic.

### PlanChange v2 (D17 Resource Plan Resize & Downgrade Execution)

Implement the [D17 settlement and lifecycle specification](invariants.md#plan-change-and-settlement-d17):

- Resource Catalog typed `PlanChangePolicy` & exact micro-USD ceil/floor proration (`unusedSupplement` / `upgradeCharge`).
- Workspace `PlanChange` state machine (`upgrade_immediate` and next-period `downgrade_next_period` scheduled at `paidThrough`), cancellation CAS and billing calendar boundary enforcement.
- Fabric compute/CBS provider adaptation (`ResetInstancesType` / pool replacement + `ResizeDisk` without shrinking prepaid disks).
- Console quote preview, active-plan display and explicit error/pending surfaces.

### Build Three-Input W01 End-to-End Parity

- Ensure `packageVersionId` + `runtimeVersionId` + independent approved `webuiVersionId` claims, `build.artifact_confirmed.v1` Outbox/Inbox handoff, and BFF `ownerOperation`/receipt projections are fully exercised by production builds and protected TKE delivery without synthetic fallbacks.

### Agent Read Carrier Adoption

- Roll out Cloud's read-only MCP endpoint (`/api/v2/mcp`), same-origin quote review and 1-hour parent-session-bound helper credentials (`runtime/cloud-connect.md`) to an installed OPL App / Studio Host under the same CloudIdentity account.

### Retained v0.1.8 Deployment Worker Upgrade Qualification

Qualify an installed upgrade on the Instance-owned managed TKE or Local
Workspace overlay. For one retained Workspace, read back an accepted pending
or running `workspace.application.deploy` child that completes under its
original operation, resource and digest identities, with activation and receipt
recovered. An unresolved rotation parent still fences Workspace deletion.
Cross-installation transfer requires separate evidence if used.

## Workspace Key And Customer Read Boundaries

Complete migration of remaining transitional customer reads off legacy Control
Plane / delegated process-local custody onto canonical domain owners:

1. **Personal Gateway Key & Balance Reads**: Move key CRUD, usage/balance
   history and Gateway endpoint reads out of transitional delegated custody onto
   a verified first-class Gateway Integration boundary, retiring legacy `/api/*`
   Gateway panels once live callers migrate.
   - The key CRUD half has moved for the published Console: the Gateway owner
     owns the personal Key create/update/reveal/revoke capability and the Console
     reaches it through the Console BFF. The legacy `/api/*` write panels are
     retired in the follow-up slice, together with the retained Control Plane
     Console build's call to them; the Control Plane's read and reveal routes
     stay, because a non-browser consumer still reads them.
2. **Billing & Announcements Extraction**: Retire retained Control Plane
   bridges (`/internal/cloud/legacy-billing/*`, `/internal/cloud/announcements`)
   once Cloud Ledger and Announcement owners serve authenticated CloudIdentity
   reads directly without losing historical user acknowledgements or receipts.
3. **Retained Control Plane Cleanup**: Progressively retire remaining legacy
   Launch/Policy/Admin surfaces once open persisted Workspaces converge and all
   non-browser Instance callers confirm zero dependency.
   - The purchase-eligibility policy writer
     (`POST /api/operator/accounts/{accountId}/workspace-purchase-eligibility`)
     stays until a two-sided owner/caller migration exists: an Account-policy
     writer in the target topology plus the native Workspace consumer's read
     path. Deleting the writer alone would freeze
     `Account.WorkspacePurchaseEnabled` and keep gating new purchases on a
     stale fact.
   - Preserve the retained deletion, refund, and renewal executors until their
     live clients have migrated and a current owner census can distinguish
     non-terminal operations from completed settlement. Include accepted
     refunds that still need their first execution and renewals whose expiry
     or Ledger settlement is incomplete.
4. **Module & DB-Role Coverage**: Expand `tests/contracts/module-physical-boundaries.test.ts`
   and PostgreSQL role-isolation checks to cover every extracted service and Console BFF.
