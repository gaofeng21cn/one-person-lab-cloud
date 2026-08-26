# Fabric Runtime Resource Read Engine

## Decision

The fourth bounded-context cut isolates the non-secret Workspace Runtime status
read model inside Fabric. It does not move Runtime creation, repair, destroy,
Gateway Secret reads, or delete-residue observation.

The read engine owns the complete path from the provider's authoritative Runtime
status to the Fabric Runtime observation:

```text
provider Runtime status
  -> persisted Workspace Runtime identity candidates
  -> exact identity validation
  -> redacted Runtime status
  -> ready/pending/absent/conflict/error observation
```

## Owner

`workspaceRuntimeReadEngine` owns:

- provider Runtime status readback;
- the persisted Runtime identity candidate lookup;
- matching provider identity to the one authoritative Fabric operation;
- password redaction for ordinary Runtime status;
- `ready`, `pending`, `absent`, `conflict`, and `error` classification.

Fabric `Service` retains:

- Runtime mutation workflows and their operation claims/outcomes;
- owner authorization for credential reveal;
- Gateway Secret and delete-residue capabilities, which have different owners
  and lifecycle/failure models;
- the stable public `WorkspaceRuntimeStatus` and `ObserveWorkspaceRuntime`
  facades.

The Control Plane and HTTP contracts remain unchanged.

## Narrow dependencies

The engine receives only:

```go
type workspaceRuntimeReadEngine struct {
    provider   runtimeResourceReader
    operations WorkspaceRuntimeReadStore
}
```

`runtimeResourceReader` contains only `WorkspaceRuntimeStatus`. The read store
contains only `WorkspaceRuntimeIdentityCandidates`. Runtime mutation code keeps
its separate mutation/query ports.

## Invariants

- Empty Workspace IDs fail closed through the existing observation result.
- Provider status errors are returned unchanged to the status facade and mapped
  to `error` by observation.
- A running or unready Runtime must have exactly one persisted identity
  candidate.
- The candidate must decode to the same Workspace and a non-empty Runtime ID
  and Runtime operation ID.
- Existing provider IDs and operation IDs, when present, must match the
  persisted candidate exactly.
- Ordinary status and observations never expose the provider password.
- No read path can claim, save, converge, or mutate a provider operation.

## Deliberate non-goals

- No new HTTP endpoint, JSON field, schema, contract, or workflow framework.
- No migration of Runtime create/repair/destroy state machines.
- No migration of Gateway Secret or delete-residue read models.
- No change to Provider adapter behavior or persisted Runtime operation payloads.

## Acceptance slice

The existing `WorkspaceRuntimeStatus` and `ObserveWorkspaceRuntime` callers,
including Control Plane runtime-status and Workspace Delete observation, continue
to pass unchanged. Response-loss readback in Runtime create and repair uses the
same engine read path. Focused tests cover identity backfill, ambiguous or
missing candidates, provider errors, redaction, and Local-Docker restart
readback.
