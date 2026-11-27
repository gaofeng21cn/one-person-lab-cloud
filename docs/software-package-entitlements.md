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

- **Canonical Owner**: Follows the `tenant.workspace_purchase_eligibility`
  single-writer pattern in `services/gateway-integration` (`opl_tenant` schema):
  `tenant.package_entitlements(actor_id, package_id, enabled, reason, expires_at, updated_by, updated_at)`.
- **Fail-Closed Admission**: Missing or expired rows for non-admin subjects deny
  access (`enabled: false`).
- **Administrator Governance**: Platform administrators
  (`OPL_PLATFORM_ADMIN_SUBJECTS`) grant or revoke per-subject package
  entitlements through Console BFF and the Console Operator UI, recording atomic
  policy updates, audit events, and idempotency keys. Later subscription or
  order workflows may write to the same owner boundary without altering client
  APIs.
- **Deployment Separation**: Runtime entitlements live exclusively in the live
  Cloud instance database (e.g., `cloud.medopl.com`), never in git deployment
  repositories such as `opl-instance-medopl`. Self-hosted OPL Cloud instances
  bind their own `OPL_SUB2API_URL` and govern their own entitlements locally.

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
- **Reusable Admission Pattern For Companion Services**: The same per-subject
  `CloudIdentity` entitlement model may later gate optional service admission
  (such as OPL Link relay enrollment) strictly at control-plane admission
  boundaries while keeping service data planes independent.
