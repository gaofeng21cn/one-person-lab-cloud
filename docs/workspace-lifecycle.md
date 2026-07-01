# Workspace Lifecycle

OPL Workspace is the cloud Docker/WebUI surface for OPL App.

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
| Create | Select package, compute, storage, and initial access policy | Console |
| Provision | Prepare runtime, storage, credentials, URL, and base OPL App payload | Fabric |
| Open | User enters the Workspace through an isolated URL | Workspace |
| Rotate credentials | Reset username or password for the WebUI surface | Console |
| Attach storage | Bind workspace volume, bucket, or organization storage ref | Fabric |
| Suspend | Stop user access and managed compute while retaining policy-defined data | Console |
| Resume | Restore access and runtime according to policy | Console |
| Delete | Remove runtime and apply retention policy to data and receipts | Console |

## Workspace Record

Required planning fields:

- `workspace_id`
- `workspace_name`
- `owner`
- `team_ref`
- `url`
- `status`
- `package_ref`
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
