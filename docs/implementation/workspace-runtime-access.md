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

Access uses the current paid entitlement. After renewal, its period is accepted
only from the original-period committed renewal and exact debit confirmation,
with the same account, owner and resources. The immutable initial Launch record
is not rewritten. An existing proxied connection rechecks entitlement at its
paid-through boundary: a confirmed renewal extends the connection's deadline;
unpaid, deleted or unconfirmed access closes it. Runtime suspension and recovery
remain Fabric operations described in the lifecycle implementation reference.

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

## Local-Docker Routing

`OPL_FABRIC_LOCAL_DOCKER_PUBLISH_HOST` chooses the Docker published-port bind
address; `OPL_FABRIC_LOCAL_DOCKER_HOST` chooses the host used for Runtime URLs
when Docker reports a loopback, unspecified or empty HostIP. The portable
environment example supplies both explicitly. The adapter otherwise defaults
the publish address to the configured Runtime host. Neither value changes the
fixed internal port or bypasses Runtime readiness.

Runtime readback verifies the persisted operation and live container identity.
For retained failed `local_docker_runtime_readback_mismatch` operations, the
current read engine additionally requires exact Service name, image, username,
credential status/version and Secret ref before recovering the binding. This
is a read-only persisted-data constraint, not a second creation or mutation
entrypoint.

## Gateway Network Recovery

An administrator may call
`POST /api/operator/workspaces/{workspaceId}/runtime-gateway-network/recover`
with an idempotency key and exactly `confirmationWorkspaceId` and `reason`.
The confirmation must match the URL Workspace. Control Plane requires a
succeeded Local-Docker Launch and its matching, running Workspace projection;
Runtime, Compute, original Runtime operation and Service identities are taken
from that Launch. A ready Runtime or the exact gateway-network mismatch is
admissible; other read failures stop recovery.

Control Plane persists the operation and audit, then sends a narrowly bound
`workspace_runtime_gateway_network` capability for
`recover_workspace_runtime_gateway_network` to Fabric. The Local-Docker adapter
verifies the healthy Runtime, exact Compute network and membership, and the
configured Control Plane gateway container's ownership labels. Its only
provider mutation connects that gateway to the existing Compute network. The
provider journal and final readback must match the original Runtime and network
before success. Recovery neither rebuilds the Runtime nor purchases resources
or changes the wallet; failed terminal operations are not replayed under the
same key. Source tests for this route do not prove Instance network recovery.
