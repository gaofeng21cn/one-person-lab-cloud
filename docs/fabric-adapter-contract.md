# Fabric Adapter Contract

Fabric adapters connect approved App, Workspace, Serve and domain-Agent actions
to compute, storage, environments, connectors and managed execution.

```text
plan -> approve -> execute -> monitor -> collect -> receipt
```

## Adapter Fields

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

When an adapter is distributed through an OPL Package, planning also carries
the exact package manifest and lock refs. Fabric does not resolve or mutate
them.

## Adapter Methods

- `validate_config`
- `plan`
- `request_approval`
- `execute`
- `status`
- `cancel`
- `collect_outputs`
- `emit_receipt_refs`

## Adapter Categories

| Category | Examples |
| --- | --- |
| Compute | Docker, VM, GPU, SSH, HPC, managed workers |
| Storage | Workspace volume, bucket, institutional storage ref |
| Environment | Container image, runtime manifest, hardware profile |
| Connector | Literature provider, database, internal system, tool API |
| Agent resource binding | Package requirement refs, instance preparation, run dispatch |
| Serve execution binding | Exact Agent Revision, sandbox/worker, secrets, network, artifact collection and provider refs |

An Agent resource binding is not an Agent package registry. Package discovery,
validation, install, digest, lock, update, rollback and repair are owned by OPL
Packages. Fabric only consumes current refs and binds resources.

A Serve execution binding is also not a public endpoint or service control
plane. OPL Serve owns Service, Revision, Deployment and Agent Edge state; Runway
owns Invocation/Session lifecycle. Fabric owns only the resource facts behind an
approved execution.

## Boundary

Console applies account policy for hosted or managed usage. Ledger records
refs. The resource owner remains authority for resource state; OPL Packages
remains authority for package state; the domain owner remains authority for
professional truth and quality.
