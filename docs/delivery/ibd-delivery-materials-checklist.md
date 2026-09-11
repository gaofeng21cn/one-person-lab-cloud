# IBD Delivery Materials Checklist

Owner: `IBD publisher`, with Fabric verifying executable capability
Purpose: `handoff_checklist`
State: `current_checklist`

This checklist turns the [roadmap handoff inputs](../roadmap.md#handoff-inputs-and-parallel-work)
into a per-item list the IBD publisher can prepare and the Cloud owners can
verify. Preparation runs in parallel with Cloud implementation; the latest
consumption point for each group is binding. A material is delivered when its
content is machine-readable, immutable, and checksum-bound — a pullable image,
a local demo or an oral description is not delivery.

## 1. Images And Runtime Description

Latest consumption: business loop 3, full IBD deployment.

- [ ] Main image: complete `registry/repository@sha256:<digest>` reference plus
      the platforms it is built for (linux/amd64 at minimum; arm64 declared or
      explicitly excluded).
- [ ] Every dependent service image (six knowledge services or an explicit
      statement that a dependency is an external binding instead) with the same
      reference quality as the main image.
- [ ] Versioned machine-readable runtime description declaring, per component:
      service ports, health probes, process identity, startup command, required
      runtime capabilities, CPU/memory/storage requirements, persistent or
      scratch mounts, private dependency connections, and configuration inputs.
- [ ] The description states which environment variables or files are
      configuration (non-secret) and which are Secret inputs, without embedding
      any secret values.

Verification by Cloud owners: every declared image reference resolves on the
approved registry for every declared platform, and Fabric's declared
capabilities cover every runtime capability requirement.

## 2. Connection Graph And Access

Latest consumption: business loop 3, dependency wiring and application access.

- [ ] The service connection graph: which component calls which component, on
      which ports and paths, and which connections must stay private to the
      Workspace.
- [ ] Explicit statements for every external service the IBD needs (or the
      declaration that there are none).
- [ ] The application's web entry: root path, root-URL behavior, session
      cookie mechanics, streaming (SSE) usage, and whether the application
      performs its own login.

Verification by Cloud owners: the access chain (entry, cookies, streaming)
passes on an isolated environment with a non-OPL application before the IBD
claims production use.

## 3. Data Materials And Restore

Latest consumption: business loop 3, data import; loop 5, restore and rollback.

- [ ] The knowledge-base data set: archives, models, PDFs and indexes with a
      consistent checksum (SHA-256) for every artifact and a manifest binding
      them together.
- [ ] The explicit choice between initial import and schema-migration restore,
      with the compatibility statement for each supported source version.
- [ ] A versioned restore tool (or image) and a verification tool that prove
      retrieval correctness after restore, plus the compatible versions of both.
- [ ] The statement of which Docker volumes or paths the data came from, so the
      Docker-volume to TKE/PVC import path can be exercised.

Verification by Cloud owners: a restore into a clean target passes the
publisher's own verification tool, and a real retrieval question returns
answers with reference evidence.

## 4. Capacity And Platform Requirements

Latest consumption: isolated testing in loop 3; production preflight in loop 5.

- [ ] Per-component and recovery-peak CPU, memory, storage and platform
      requirements, stated as the maximum the deployment must reserve.
- [ ] The target Workspace's available capacity, existing bindings and
      acceptable interruption windows, confirmed by the Instance owner against
      real provider readback.

Verification by Cloud owners: the sum of components plus recovery peak fits the
target Workspace; a shortfall is returned as an explicit missing list, and
capacity purchase stays a separate action.

## 5. Registry, Secret Interfaces And Instance Bindings

Latest consumption: generic interfaces with non-sensitive fixtures in loop 3;
production values only at the Instance owner boundary in loop 5.

- [ ] Registry connection facts (which registry, repositories, and how access is
      granted) for the images and the deployment-description artifact.
- [ ] Every Secret input's consumer, format and rotation statement — interface
      only, never values.
- [ ] DNS, TLS, origin and provider-policy facts the Instance owner must bind.

Verification by Cloud owners: non-sensitive fixtures pass through the same
interfaces in isolated tests; production Secret values are consumed only by the
Instance owner's protected workflow and never transit Cloud code or logs.

## Consumption Order

Loop 1 and loop 2 do not wait for any item above. Loop 3 keeps the IBD-specific
acceptance open until groups 1–4 pass their verification; a non-OPL application
proves the generic path meanwhile. Group 5 separates fixture development from
production values so nothing here becomes a total gate on Cloud work.
