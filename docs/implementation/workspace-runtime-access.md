# Workspace Runtime Access

## Selected Application Access

New installations resolve the selected application deployment independently of
the original resource Launch. `currentApplication` reports that generation's
live readiness and declared entry; `applicationInstallation` separately reports
pending or failed default installation. A ready application without a declared
web entry does not show an Open action. Fabric read failure cannot replay a
previous ready observation.

A declared public entry uses the application's own origin and root path.
Control Plane does not route a generic application through OPL App's `/w` cookie
proxy or transfer management credentials. The application owns its sessions,
assets, APIs and streaming behavior; DNS/TLS and real browser qualification are
Instance obligations. `cloud_private` does not expose an anonymous public URL.

The origin is one hostname per (Workspace, application) pair, derived from the
binding identity rather than allocated:

```text
<workspaceId>-<12 hex of stableID("workspace-application-origin", workspaceId, applicationId)>.<OPL_WORKSPACE_APPLICATION_DOMAIN>
```

`OPL_WORKSPACE_APPLICATION_DOMAIN` is its own installation value, not an
inference from `OPL_WORKSPACE_DOMAIN`. Which domain the origins sit under decides
which DNS record and which certificate the installation must hold, so making it
explicit keeps an installation requirement from hiding behind an inference. An
installation that publishes no such domain has no application origins, and every
binding keeps the retained path-based entry.

The origins deliberately sit **one label under the zone** rather than under the
Workspace host. A public DNS proxy's free edge certificate covers a root domain
and its first-level subdomains, so a first-level layout needs no origin-side
certificate at all; a second-level layout would require both a paid edge
certificate and a purchased certificate on the load balancer. Because the proxy
terminates TLS for these names, the origins carry no Ingress TLS entry.

`workspaceApplicationOriginHost` and `parseWorkspaceApplicationOriginHost` in
`services/control-plane/internal/server/workspace_application_origin.go` are the
pair that owns this shape. Routing reads the Workspace identity out of the name,
so it needs no allocation table and no second writer. The application part is
then confirmed against the Workspace's current binding: a name that no longer
matches belonged to a superseded application and answers `410`
`workspace_application_origin_retired`, so the previous application's service
worker or browser storage cannot act on its replacement. A compatible update
keeps the same application identity and therefore the same origin, which is what
lets a visitor's session survive an update.

A binding origin belongs entirely to its application. The request is dispatched
before the management route table, so this server's own routes never answer on
an application host. Only platform credentials are removed from the forwarded
request (`opl_session`, `opl_ws_active`, `opl_ws_session_*`, and the `X-OPL-CSRF`
headers); the application keeps its own `Authorization` header and cookies. On
the response side the application may set whatever cookies it needs, but a
`Domain` attribute is removed so a cookie cannot widen onto a sibling binding or
onto the Console's origin.

The proxy states the external origin itself: `X-Forwarded-Host` from the request
and `X-Forwarded-Proto` from the installation's TLS fact. A client-supplied value
is overwritten, because an application that builds absolute redirects or cookie
domains from these headers must not be steerable by its caller.

The external scheme is read from the installation's own `OPL_PUBLIC_URL`, which
Control Plane already requires and validates at startup. The in-cluster hop is
plain HTTP behind the instance's TLS terminator, so a request cannot report the
external scheme; stating it once means the published entry URLs and the forwarded
header cannot disagree. An installation without a valid `OPL_PUBLIC_URL` fails
startup rather than publishing an address that cannot resolve.

An installation whose `OPL_WORKSPACE_DOMAIN` is absent, or a binding whose
Workspace identity cannot form a DNS label, has no derivable origin. Such a
binding keeps the retained path-based entry below instead of an address that
cannot resolve.

Only a declared OPL runtime profile enables OPL password and Gateway controls.
It uses username `opl`, `/run/secrets/opl_webui_password`,
`/run/secrets/webui_session_secret` and its declared Gateway Key file. Ordinary
image updates and reinstalling the same application retain the frozen credential
identity; migration from a full Launch preserves its proven credential source.
Explicit password and Gateway rotation advance the credential version through a
successor deployment with the same data binding;
HTTP 202 means the operation is pending, not that a new password is already in
use. Console drops revealed credentials when the selection changes and keeps
passwords only in component memory.

## Retained Full Launch Access

The following compatibility path applies only to retained full Launch runtimes.
The Tencent/TKE adapter access path is:

```text
Browser
  -> configured Instance Workspace domain
  -> shared CLB / TKE Ingress
  -> Control Plane reverse proxy
  -> Fabric-created per-Workspace ClusterIP Service :3000
  -> Workspace runtime
```

For retained Runtime status and authorized repair readback, a provider may
report only the observed `ServiceName` when this installation publishes the
entry. Control Plane composes the customer URL with `workspaceGatewayEntryURL`;
a provider-published `URL` remains unchanged. A missing provider URL is not a
Runtime failure. Runtime identity, health, entitlement and credential checks
still apply; `unready` stays unready, and an absent Runtime gains no entry.

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
