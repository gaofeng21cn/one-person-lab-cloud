# Workspace Lifecycle

OPL Workspace is the cloud OPL App workbench. Each user account has exactly one
primary Workspace Instance with its own URL, runtime, storage, and lifecycle.
Projects and collaboration do not create additional instances.

Agent Services published through OPL Serve are separate deployment resources.
An account may own multiple Services without creating another Workspace, and a
Workspace URL cannot become a Service endpoint.

The lifecycle should be understandable as a workbench lifecycle, not as a
container-hosting workflow.

## Lifecycle States

```text
requested -> provisioning -> active -> suspended -> deleted
                         \-> failed
```

## Lifecycle Actions

| Action | Meaning | Main owner |
| --- | --- | --- |
| Create | Provision the account's initial Workspace with permitted OPL Package refs, compute, storage, and access policy | Console policy; package state remains OPL Packages |
| Provision | Prepare runtime, storage, credentials, URL, and base OPL App payload | Fabric |
| Open | User enters the Workspace through an isolated URL | Workspace |
| Rotate credentials | Reset username or password for the WebUI surface | Console |
| Attach storage | Bind workspace volume, bucket, or institutional/managed storage ref | Fabric |
| Suspend | Stop user access and managed compute while retaining policy-defined data | Console |
| Resume | Restore access and runtime according to policy | Console |
| Delete | Remove runtime and apply retention policy to data and receipts | Console |

## Workspace Record

Required planning fields:

- `workspace_id`
- `workspace_name`
- `owner_account_ref`
- `collaboration_policy_ref`
- `url`
- `status`
- `package_manifest_ref`
- `package_lock_ref`
- `compute_profile`
- `storage_profile`
- `gateway_policy_ref`
- `connector_policy_refs`
- `environment_ref`
- `ledger_policy_ref`
- `created_at`
- `updated_at`

## Receipts

Create, suspend, resume, delete, credential rotation, storage binding, and
managed job actions should leave Ledger receipts when they affect user work,
cost, security, or reproducibility.

Workspace records only reference package truth. Provisioning cannot install,
update, roll back, repair, or rewrite an OPL Package lock; those actions route to
OPL Packages.
