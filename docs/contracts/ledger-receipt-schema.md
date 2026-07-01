# Ledger Receipt Schema

This document defines the minimal receipt shape for OPL Ledger planning and
implementation.

Receipts are evidence records. They point to source systems, artifacts, logs,
and review results instead of copying all underlying data into Ledger.

## Minimal JSON Shape

```json
{
  "receipt_id": "ledger.receipt.example",
  "schema_version": "0.1",
  "surface": "workspace",
  "actor": "user-or-service-ref",
  "workspace_ref": "workspace-ref",
  "job_ref": "fabric-job-ref",
  "request": {
    "goal": "human-readable goal",
    "input_refs": [],
    "expected_outputs": []
  },
  "approval": {
    "approved_by": "owner-ref",
    "approved_at": "2026-07-01T00:00:00Z",
    "policy_ref": "policy-ref"
  },
  "execution": {
    "adapter": "docker",
    "environment_ref": "environment-ref",
    "command_or_action_ref": "action-ref",
    "status": "completed",
    "started_at": "2026-07-01T00:00:00Z",
    "finished_at": "2026-07-01T00:00:00Z",
    "logs_ref": "logs-ref"
  },
  "outputs": {
    "artifact_refs": [],
    "delivery_refs": []
  },
  "review": {
    "required": false,
    "reviewer_gate_ref": null,
    "result": "not_required"
  },
  "cost": {
    "meter_refs": [],
    "estimated_cost": null
  },
  "owner": {
    "owner_ref": "owner-ref",
    "responsibility": "review-and-continuation"
  },
  "continuation_ref": "continuation-ref"
}
```

## Status Values

- `planned`
- `approved`
- `running`
- `completed`
- `failed`
- `cancelled`
- `review_required`
- `review_blocked`

## Reviewer Result Values

- `not_required`
- `passed`
- `needs_human_review`
- `blocked`
- `waived_by_owner`

## Boundary

Ledger receipts support review, audit, handoff, continuation, and later
reproduction. Runtime truth still belongs to the owning runtime, storage truth
belongs to the owning storage system, and domain-quality truth belongs to the
responsible domain owner or reviewer.
