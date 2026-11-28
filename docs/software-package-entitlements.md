# Private Software Package Entitlements And Controlled Delivery

Owner: `one-person-lab-cloud`
Purpose: `software_package_entitlements_target_reference`
State: `active_target_reference`

This document defines the target architecture for Cloud-governed authorization
and controlled delivery of restricted OPL Software Center packages
(`visibility: "authorized_only"`). Implementation status and open milestones live
in [roadmap.md](roadmap.md); durable boundaries live in
[decisions.md](decisions.md) and [opl-console.md](opl-console.md).

## Scope And Separation From Public Open-Source Packages

- **Public Packages Stay Anonymous And Zero-Cost**: Open-source packages marked
  `visibility: "public"` remain hosted on public GitHub (`opl-catalog` +
  public GHCR). End users browse, install, and update public packages directly
  through OPL Framework without signing in to OPL Cloud/Gateway or configuring
  a GitHub account.
- **Restricted Packages Only**: Cloud entitlement governance applies solely to
  restricted packages marked `visibility: "authorized_only"` (such as `occ` or
  `glkvm-native`). Restricted packages may optionally opt into public summary
  visibility via `opl-catalog/public-listings.json`, or remain completely
  discoverable only through authenticated entitlement reads.

## Identity And Zero Client-Side Registry Secrets

- **Single User Account (OPL Gateway)**: Desktop users sign in once using their
  existing OPL Gateway (`Sub2API`) account rather than configuring GitHub CLI,
  Docker logins, or manual source handles in Software Center.
- **Subject Mapping**: Cloud resolves the caller's live Gateway identity to its
  canonical `actor_id` via `gateway.identity_mappings` (keyed by verified numeric
  external subject ID, never mutable email).
- **No Shared Registry Credentials On Client Devices**: Cloud never distributes
  organization GitHub PATs or shared GHCR admin tokens to end-user devices.
  Developer-oriented `github_packages` handles remain CLI/advanced fallbacks
  only.

## Entitlement Policy Ownership (`CloudIdentity` / `opl_tenant`)

- **Canonical Owner & Uniform Fail-Closed Allowlist**: Follows the
  `tenant.workspace_purchase_eligibility` single-writer pattern in
  `services/gateway-integration` (`opl_tenant` schema). Every Cloud-governed
  Gateway-user capability uses one explicit fail-closed allowlist model:
  - `tenant.workspace_purchase_eligibility(actor_id, workspace_purchase_enabled, reason, updated_by, updated_at)` (existing)
  - `tenant.package_entitlements(actor_id, package_id, enabled, reason, expires_at, updated_by, updated_at)` (restricted packages such as `occ`)
  - `tenant.link_relay_entitlements(actor_id, relay_enabled, reason, updated_by, updated_at)` (OPL Link Relay forwarding admission)
- **Fail-Closed Admission**: Missing or expired rows for non-admin subjects deny
  access (`enabled: false`). Zero end-user configuration is required beyond
  signing in once with OPL Gateway.
- **Initial Active-User Seed For OPL Link Relay**: Because OPL Link already
  authenticates via mandatory OPL Gateway tokens, instances enabling Relay
  enforcement (such as `medopl`) explicitly backfill/enable all existing active
  Gateway users in `tenant.link_relay_entitlements` at rollout time so active
  users experience no disruption, while keeping ongoing grants and revocations
  under the same fail-closed allowlist control in Console `/operator`.
- **Platform Administrator & Entitlement Governance**: Deployment-configured
  `OPL_PLATFORM_ADMIN_SUBJECTS` remains the immutable bootstrap/break-glass root
  administrator list (protected against accidental lockout), while planned
  `tenant.platform_admins` DB delegation allows platform administrators to
  grant or revoke delegated administrator access, package entitlements,
  purchase eligibility, and Link Relay admission from Console `/operator` — all
  recording atomic policy updates, audit events, and idempotency keys. Later
  subscription or order workflows may write to the same owner boundary without
  altering client APIs.
- **Deployment Separation**: Runtime entitlements live exclusively in the live
  Cloud instance database (e.g., `cloud.medopl.com`), never in git deployment
  repositories such as `opl-instance-medopl`. Self-hosted OPL Cloud instances
  bind their own `OPL_SUB2API_URL` and govern their own entitlements locally.
- **Unified Companion Service Catalog & Grant Discovery**: General companion
  service admission (`tenant.service_entitlements`, covering multi-tenant
  services such as `netfleet_cloud` and `link_relay`), dynamic endpoint
  dispatch, and short-lived Ed25519 `service-grant` token issuance live in
  [service-entitlements-and-discovery.md](service-entitlements-and-discovery.md),
  while this document retains restricted Software Center package delivery.

## Client Contract And Two-Stage Physical Delivery

- **Stable Console BFF Contract**:
  - `GET /api/v2/software/entitlements`: authenticates the caller's live
    Gateway session and returns authorized restricted `package_id` entries plus
    current version metadata.
  - `POST /api/v2/software/packages/{packageId}/download`: verifies live
    identity and active entitlement, returning either a streamed immutable
    archive or a short-lived single-object download URL (e.g., 60s Presigned
    URL) bound to the expected immutable `sha256` digest.
- **Stage 1 — Zero Extra Cloud Storage Cost**:
  - Restricted packages continue to publish immutable artifacts to private GHCR.
  - Cloud fetches authorized immutable `sha256`-addressed blobs using a
    server-only read-only GHCR credential, caches them on the instance's
    existing capability object-store PVC (`opl-cloud-capability-objects`), and
    streams authorized downloads from the Cloud instance without exposing GHCR
    credentials or forcing Mainland China clients onto GHCR directly.
- **Stage 2 — Scale-Out Private Object Storage**:
  - When download volume grows, the Cloud download adapter switches to a
    strictly private object-storage bucket (e.g., Tencent Cloud COS for
    Mainland China reachability) issuing 60-second single-object Presigned URLs,
    keeping the client protocol completely unchanged. Google Cloud Storage is
    excluded for Mainland China package delivery due to reachability and egress
    cost.
- **Client Verification Invariant**: OPL Framework (`@opl/pack`) remains the sole
  local installer and verifies the immutable `sha256` digest
  (`opl-release.lock.json`) and publisher signature before unpacking any archive;
  Cloud authorization never bypasses cryptographic artifact verification.

## Cross-Repo Convergence Alignment

- **Post-Wave 1 Alignment**: Client-side Gateway session integration in
  `one-person-lab` (`@opl/pack` + `opld`) and Studio Software Center UX
  consolidation build on top of Framework v2's `opl.db` and `opld`
  `pack.sources.*` single-writer foundation without reintroducing v1 shims or
  local flat-file writers.
- **Uniform Companion Service Admission (OPL Link Relay)**: OPL Link Relay
  enforces `tenant.link_relay_entitlements` strictly at its 3 control-plane
  routes (`/api/v1/devices/register`, `/api/v1/link/account/hosts`,
  `/api/v1/link/account/enroll`) via HTTP `403` (`companion_access_denied`),
  keeping the encrypted WebSocket data plane unblocked and end-user setup
  zero-config (Gateway sign-in only).
