# Fabric Adapter Contract

Fabric adapters connect OPL workbench actions to compute, storage, environments,
connectors, and managed execution.

Every adapter should fit the shared execution flow:

```text
plan -> approve -> execute -> monitor -> collect -> receipt
```

## Adapter Fields

Required planning fields:

- `adapter_id`
- `adapter_type`
- `display_name`
- `owner`
- `configuration_schema_ref`
- `approval_requirements`
- `supported_environment_refs`
- `supported_storage_refs`
- `cost_signal`
- `ledger_receipt_policy_ref`

## Adapter Methods

Every implementation should provide these conceptual methods:

- `validate_config`
- `plan`
- `request_approval`
- `execute`
- `status`
- `cancel`
- `collect_outputs`
- `emit_receipt_refs`

## Initial Adapter Order

1. Docker or VM adapter with one storage path.
2. SSH or HPC adapter with explicit user approval.
3. GPU or managed worker adapter.
4. Literature, database, and institutional storage connectors.

## Adapter Categories

| Category | Examples |
| --- | --- |
| Compute | Docker, VM, GPU, SSH, HPC, managed workers |
| Storage | Workspace volume, bucket, institutional storage ref |
| Environment | Container image, package lock, runtime manifest |
| Connector | Literature source, database, internal system, tool API |
| Agent | Agent package binding, instance preparation, run dispatch |

## Boundary

Adapters prepare and run work. They report status, outputs, and cost signals.
OPL Console applies management policy. OPL Ledger records receipts. The owning
system remains the authority for the underlying resource state.
