# Shared Execution Contract

This contract defines the common resource execution path for OPL App,
OPL Workspace, OPL Fabric, OPL Console, and OPL Ledger.

The same contract should work for local App actions, cloud Workspace actions,
managed Docker or VM jobs, SSH or HPC jobs, GPU jobs, and later connector-backed
jobs. MAS and other domain agents can use the same contract as approved callers
without routing every request through Console.

## Standard Flow

```text
plan -> approve -> execute -> monitor -> collect -> receipt
```

## Request

An execution request describes what the user wants to run and which resources
may be used.

Required fields:

- `request_id`
- `surface`: `app`, `workspace`, `console`, or `domain_agent`
- `caller_ref`
- `actor`
- `workspace_ref`
- `goal`
- `resource_profile`
- `environment_ref`
- `input_refs`
- `expected_outputs`
- `approval_policy`

## Approval

Approval records what will be used before the job starts.

Required fields:

- `approval_id`
- `request_id`
- `approved_by`
- `approved_at`
- `allowed_compute`
- `allowed_storage`
- `allowed_connectors`
- `allowed_environment`
- `cost_policy`
- `expiry`

## Execution

Execution records the concrete job submitted through Fabric.

Required fields:

- `job_id`
- `request_id`
- `approval_id`
- `adapter`
- `command_or_action_ref`
- `environment_ref`
- `status`
- `started_at`
- `finished_at`
- `logs_ref`

## Collection

Collection records what was produced and where the outputs live.

Required fields:

- `job_id`
- `output_refs`
- `artifact_refs`
- `delivery_refs`
- `review_required`
- `cost_signal`

## Receipt

Every completed, failed, cancelled, or reviewer-blocked job should produce a
Ledger receipt. The receipt links the request, approval, execution, outputs,
review results, owner, and continuation entry.

## Surface Responsibilities

| Surface | Responsibility |
| --- | --- |
| OPL App | Starts local or user-provided resource work through the same plan and receipt path |
| OPL Workspace | Starts cloud workbench jobs and displays status, outputs, and receipts |
| OPL Console | Applies organization policy, quota, approval, and billing rules for managed resources and exact package refs |
| OPL Fabric | Selects adapters, prepares resources, runs jobs, and collects outputs |
| OPL Ledger | Records receipts, provenance, reviewer results, and continuation refs |
| OPL Packages | Owns package manifest, digest, install, lock, update, rollback, repair, and lifecycle receipts outside this resource-execution contract |
| Domain agent | Uses approved Fabric capabilities while keeping domain truth, quality judgment, and delivery authority |
