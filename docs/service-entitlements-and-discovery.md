# Unified Companion Service Entitlements And Endpoint Discovery

Owner: `one-person-lab-cloud`
Purpose: `service_entitlements_and_discovery_target_reference`
State: `active_target_reference`

This document defines the target architecture for Cloud-governed companion
service authorization, per-user entitlement management, dynamic service-endpoint
discovery, and short-lived cryptographic grant issuance across the OPL client
ecosystem (`opl-netfleet`, `opl-link`, `opl-app`, `@opl/pack`, and future
companion services). Durable decisions belong to [decisions.md](decisions.md),
open delivery work to [roadmap.md](roadmap.md), and restricted Software Center
package delivery to [software-package-entitlements.md](software-package-entitlements.md).

## Scope And Single-Authority Principle

- **One Cloud Authority, Zero Per-Service Allowlists**: `CloudIdentity`
  (`services/gateway-integration`, `opl_tenant` schema) is the sole canonical
  authority answering which companion services a user may use and which backend
  service endpoint that user should connect to. Individual companion services
  must not maintain parallel user databases or static email allowlist env vars.
- **Portable Code, Instance-Bound Topology**: Public product repositories hold
  portable protocols and schemas without environment-specific endpoints or user
  lists. Each Cloud instance configures its own Service Catalog and governs
  entitlement rows in its live `opl_tenant` database.
- **Identity Backend Decoupling**: Clients authenticate through
  `<cloud-origin>` rather than binding edge devices directly to upstream
  `Sub2API` endpoints. Replacing or federating the upstream identity backend
  later requires changes only inside OPL Cloud without updating edge devices.

## Canonical Identity Root

- Users sign in once using their existing OPL Gateway (`Sub2API`) credentials
  (or an active Gateway session token on already-signed-in clients).
- Cloud resolves the live Gateway subject to its immutable numeric `actor_id`
  via `gateway.identity_mappings` (keyed by verified external subject ID, never
  by mutable email address).
- Companion services (such as multi-tenant `opl-netfleet-cloud`) isolate all
  tenant state—sites, subscriptions, tokens, and encryption AAD—strictly by
  `actor_id` (carried in `sub`).

## Two-Layer Control Model

### 1. Instance Service Catalog

Each OPL Cloud instance binds a non-secret runtime service directory mapping
each logical `service_id` (such as `netfleet_cloud`, `link_relay`,
`software_center`, `llm_gateway`, `workspace`) to:

- `default_origin`: canonical HTTPS base origin serving that companion service
  for the instance (for example, switching from a staging hostname to a custom
  service domain updates one catalog entry without reconfiguring devices).
- `audience`: downstream JWT `aud` identifier expected by that companion
  service (for example, `opl-netfleet-cloud`).
- `admission_mode`:
  - `allowlist_only` (fail-closed; missing or expired entitlement rows deny
    access—mandatory for `workspace_purchase_eligibility` and restricted
    `package_entitlements`).
  - `active_gateway_default_with_overrides` (all active OPL Gateway subjects
    are admitted to shared multi-tenant companion services such as
    `netfleet_cloud` unless explicitly disabled, suspended, or expired in
    `tenant.service_entitlements`, while explicit rows can still attach
    `enabled: false`, `role`, `limits_json`, or `origin_override`).

### 2. Per-User Service Entitlements (`tenant.service_entitlements`)

- **Canonical Table**:
  `tenant.service_entitlements(actor_id, service_id, enabled, role, origin_override, limits_json, expires_at, reason, updated_by, updated_at)`
  in `opl_tenant`.
- **Single-Writer Operator Governance**: Platform administrators grant, update,
  set `limits_json` (such as `{ "max_sites": 10 }`), set `origin_override`
  (routing a user or team to a dedicated regional/staging instance), or revoke
  access atomically via Console `/operator`, recording audit events and
  idempotency keys alongside `tenant.workspace_purchase_eligibility` and
  `tenant.package_entitlements`.
- **Admission Evaluation**:
  1. Verify active Gateway / `CloudIdentity` subject (`actor_id`).
  2. Read `tenant.service_entitlements` for `(actor_id, service_id)`.
  3. Under `allowlist_only`, require an explicit row with `enabled = true` and
     unexpired `expires_at`.
  4. Under `active_gateway_default_with_overrides`, deny if a matching row has
     `enabled = false` or expired `expires_at`; otherwise admit using row
     overrides or instance defaults.

## Non-Browser Client Bootstrap & Grant Protocol

Non-browser clients and edge devices (such as OpenWrt routers, desktop apps,
and CLI tools) use a single fixed Cloud discovery endpoint:

```text
POST <cloud-origin>/api/v2/client/service-grant
```

### Request & Transport Constraints

- **No Browser CSRF Cookie**: Because non-browser clients have no browser origin
  cookie jar, this route omits browser double-submit CSRF headers while
  enforcing strict TLS (`https://` in production), `Cache-Control: no-store`,
  bounded request size, `DisallowUnknownFields` JSON decoding, and per-IP /
  per-account rate limiting.
- **Supported Credential Inputs**:
  1. Direct Gateway credentials `{ email, password, totp_code?, service }`:
     `CloudIdentity` verifies credentials against the configured Gateway /
     Sub2API boundary, surfaces `{ requires_2fa: true }` challenges when
     needed, resolves `actor_id`, and immediately discards the plaintext
     password and upstream refresh token (never persisted or forwarded).
  2. Active Gateway Bearer session (`Authorization: Bearer <gateway_access_token>`)
     with `{ service }` for desktop or mobile apps that already hold a live
     Gateway session.

### Response Envelope

- **Denied / Suspended / Expired (`403 service_not_entitled`)**: Cloud refuses
  admission and returns **no** `origin` and **no** `grant_token`.
- **Authorized (`200 OK`)**: Cloud computes `origin = origin_override ??
  catalog[service].default_origin` and returns:

```json
{
  "actor_id": "1042",
  "email": "user@example.com",
  "service": "netfleet_cloud",
  "enabled": true,
  "origin": "https://netfleet.example.com",
  "grant_token": "<Ed25519-JWT>",
  "expires_at": "2026-11-28T03:02:00Z",
  "limits": { "max_sites": 10 }
}
```

### Short-Lived Ed25519 Grant Token (`grant_token`)

- **Purpose & Lifetime**: Single-purpose short-lived JWT (default 120s TTL)
  signed with OPL Cloud's Ed25519 private key.
- **Required Claims**: `iss` (`<cloud-origin>`), `sub` (`actor_id`), `email`,
  `aud` (`catalog[service].audience`), `service`, `scope` (for example,
  `"device:enroll"` or `"console:login"`), `role`, `limits`, `iat`, `exp`,
  and `jti`.
- **Public Key Distribution**: Cloud publishes active and overlapping verification
  keys at `GET /.well-known/opl-grant-jwks.json` and supports static public-key
  injection (`OPL_CLOUD_GRANT_PUBLIC_KEY`) for standalone or scale-to-zero
  deployments.

## Downstream Verification, Failure Isolation & Revocation

1. **Stateless Offline Verification**: Companion services verify `grant_token`
   cryptographically against OPL Cloud's Ed25519 public key (enforcing `iss`,
   `aud`, `exp`, `scope`, and numeric `sub`) without Cloud database access or
   static email allowlists, preserving scale-to-zero performance.
2. **Edge SSRF & Redirect Hygiene**: Edge clients validate the dispatched
   `origin` strictly (must match canonical `https://` origin, reject loopback /
   private / link-local addresses unless explicitly configured via local
   self-hosted override, and set `redirect: error`). Passwords are wiped
   immediately after bootstrap and never sent to companion origins.
3. **Scoped Enrollment Exchange**: For device onboarding, the router submits
   `grant_token` to `POST <origin>/api/v1/agent/enroll` to receive a permanent
   single-site `device_token`.
4. **Fail-Open Runtime Isolation**: Ongoing device heartbeats and subscription
   syncs connect directly to `<origin>` using `device_token`. OPL Cloud
   outages, restarts, or password changes never interrupt already-enrolled
   routers or local LAN routing.
5. **Revocation Boundary**: Disabling a user's `service_id` row in OPL Cloud
   immediately blocks new device enrollments and Web UI logins. Existing routers
   remain Fail-Open locally unless explicitly disconnected or revoked inside the
   companion control plane.
6. **Self-Hosted Compatibility**: Clients built against portable defaults keep a
   collapsible advanced override permitting either a self-hosted OPL Cloud
   origin or direct connection to a standalone companion instance.
