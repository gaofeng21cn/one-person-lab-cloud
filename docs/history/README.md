# OPL Cloud Documentation History

Owner: `one-person-lab-cloud`
Purpose: `history_index`
State: `history_index`
Machine boundary: Historical provenance and retired-model navigation only.
Files here do not define current product architecture, planning contracts,
runtime state, availability, readiness, or owner acceptance.

Current truth starts from [the docs index](../README.md),
[architecture](../architecture.md), and [roadmap](../roadmap.md).

## Records

- [Cloud repository unification, 2026-08-11](./cloud-repository-unification-2026-08-11.md): retained repository identity, absorbed product truth, archive boundary, and recovery point.
- [Research pattern provenance](./research-provenance.md): the external
  scientific-workbench pattern that informed the current Cloud split.
- [Retired Cloud Agent Registry](./retired-agent-registry.md): tombstone for the
  duplicate package-registry model replaced by package-owner identity,
  native-carrier lifecycle, and Framework aggregation.
- [Console UI display freeze, 2026-07-31](./console-display-contract-v1-2026-07-31.md):
  PR #75 page, slide and visual freeze retained as provenance only; it is not a
  current UI authority.
- [Fabric Launch Stage Engine design, 2026-08-26](./2026-08-26-fabric-launch-stage-engine-design.md)
  and [implementation plan](./2026-08-26-fabric-launch-stage-engine.md): the
  completed Owner boundary and migration sequence retained as provenance.
- [Fabric Runtime Resource Read Engine design, 2026-08-26](./2026-08-26-fabric-runtime-read-engine-design.md)
  and [implementation plan](./2026-08-26-fabric-runtime-read-engine.md): the
  completed non-secret Runtime status read Owner boundary and migration
  sequence retained as provenance.
- [Control Plane Workspace Launch Reconciler typed boundary design, 2026-08-26](./2026-08-26-control-plane-reconciler-typed-boundary-design.md)
  and [implementation plan](./2026-08-26-control-plane-reconciler-typed-boundary.md):
  the completed Stage, Launch status, and Stage observation state migration and
  strict schema-v3 codec boundary retained as provenance.
- [Console Workspace Launch Controller design, 2026-08-26](./2026-08-26-console-workspace-launch-controller-design.md)
  and [implementation plan](./2026-08-26-console-workspace-launch-controller.md):
  the completed browser Launch lifecycle Owner and narrow view-consumer
  migration retained as provenance.
- [Console Workspace Secret Controller design, 2026-08-26](./2026-08-26-console-workspace-secret-controller-design.md)
  and [implementation plan](./2026-08-26-console-workspace-secret-controller.md):
  the completed ephemeral Workspace access-secret lifecycle Owner, reset
  boundary, and narrow view-consumer migration retained as provenance.

Do not revive a historical term or owner path from this directory. Reintroduce
useful lessons only through an explicit update to the current canonical owner.
