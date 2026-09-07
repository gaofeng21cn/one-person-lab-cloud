# Workspace Runtime Access

The Tencent/TKE adapter access path is:

```text
Browser
  -> configured Instance Workspace domain
  -> shared CLB / TKE Ingress
  -> Control Plane reverse proxy
  -> Fabric-created per-Workspace ClusterIP Service :3000
  -> Workspace runtime
```

The current Workspace Runtime compatibility boundary fixes the internal WebUI
port at `3000`. `opl-cloud-workspace-runtime-abi-contract.json` is its versioned
cross-module owner; Control Plane proxy routing and both Fabric adapters project
the fixed value through named constants. The ABI is not an environment override,
an Instance-selectable option, an installation default, or a separate feature
lane.

Cloud requires the Instance to supply `OPL_WORKSPACE_DOMAIN`; Control Plane and
Tencent/TKE startup fail closed when it is absent. There is no access-domain
fallback in current source. The `.cn` Kubernetes label and annotation keys are
persisted metadata identifiers rather than access domains; they require owner
inventory and a bounded metadata namespace migration instead of a mechanical
`.com` replacement.

`/w/<workspaceId>/` selects a Workspace from the URL. Root `/api/`, `/ws`, and
other Workspace-host requests select it from the `opl_ws_active` cookie or a
Workspace referrer. The proxy writes `opl_ws_active` as routing context when a
clean Workspace URL is opened; the cookie is not an authentication credential.
It forwards traffic only after Fabric reports the Runtime ready and the
persisted Workspace state becomes `running`.

Fabric runs the Workspace image in `cloud` deployment mode with `password`
authentication. Fabric derives the runtime password and session secret from a
stable per-Workspace credential seed. Tencent/TKE stores them in a Kubernetes
Secret; `local-docker` stores immutable versions under a protected host-owned
root and mounts only the selected version read-only into the Runtime.
Control Plane resolves the target Workspace's persisted `workspaceApiKeyId` and
hands the Key transiently to Fabric. Fabric writes or rotates a deterministic
Workspace-scoped secret bound to account, Workspace, Key ID, and fingerprint,
and records only its ref, version, and fingerprint. Existing
account-scoped Secrets remain read-compatible until that Workspace's first Key
rotation; ordinary reads never infer scope from Workspace count or Key name.
Ordinary runtime status is non-secret. Dedicated owner-only POST commands reveal
or rotate the password transiently; Control Plane never persists it, and Console
retains it only in Workspace detail component memory. A Workspace image candidate
combines exact `one-person-lab-app`, active-shell, and Framework revisions. The
Workspace owner publishes that image independently; an instance pins its
immutable `repository@sha256` alongside the OPL Cloud product release. The Cloud
product release does not build, publish, or promote an instance Workspace image.
Deployment acceptance requires the exact Ready-Pod `imageID`. A configured
digest, placeholder or local timestamp does not substitute for that readback.
Retained Instance evidence is reported only in [status](../status.md).

Control Plane carries Workspace HTML, API, and WebSocket traffic, so its
availability is coupled to every Workspace connection. It selects the Runtime
Service; the Runtime owns password validation, its authenticated session, and
WebSocket access. A 2xx/non-empty-page check proves routing only; acceptance
requires an authenticated Workspace session and the exact Ready-Pod `imageID`
readback described above.

The operator-provisioned Pilot retains this single shared entry. Revisit a
dedicated Workspace Router when measured connection load or CLB rule limits
require a separate scaling owner.

