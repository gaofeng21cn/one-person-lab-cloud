# Console Customer Read Inventory (#840)

Owner: `opl-cloud`
Question: which customer reads does the Console issue under a CloudIdentity
session, which route and owner answer each one, and which remain transitional?
State: current source inventory; installed acceptance is not claimed
Observed: 2026-10-18 at `592446e1` (`fix/console-cloudidentity-reads`, base
`origin/main`)

This is the issue-scoped inventory for
[#840](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/840). It
extends [Console Browser Lifecycle](./console.md) and the
[Console source truth](../implementation-architecture.md#console-source-truth);
it is not another architecture overview. It is also an input to the Cloud
business-evidence boundary tracked by
[#872](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/872), but it
records customer reads only and claims no qualification. The migrated rows stay
in this table so a later change cannot silently move a customer read back to the
retained Control Plane.

The identity is baked into the bundle by `VITE_CONSOLE_IDENTITY`. A cloud build
reads `/api/v2/*` through the Console BFF with the live CloudIdentity session; a
legacy build reads the retained Control Plane `/api/*` routes with its own
Account session. The two answers are validated by owner tag and accepted only
under the identity that requested them.
See [`console-identity.ts`](../../apps/console-ui/src/app/console-identity.ts)
and [`console-read-api.ts`](../../apps/console-ui/src/api/console-read-api.ts).

## Current Read Callers

| Read | Console caller | CloudIdentity route | Retained route | Owner answering today | Identity proof | Transitional? |
| --- | --- | --- | --- | --- | --- | --- |
| Signed-in session | `use-console-controller` | `GET /api/v2/auth/session` | `GET /api/auth/me` | Cloud BFF and Tenant identity | HttpOnly session cookie; CSRF token | No |
| Personal wallet | `use-gateway-account-read-controller` | `GET /api/v2/wallet` | `GET /api/gateway/wallet` | Gateway owner; Sub2API remains wallet truth | Live CloudIdentity User resolved to the Gateway subject | No |
| Account usage summary | `use-gateway-account-read-controller` | `GET /api/v2/usage/summary` | `GET /api/gateway/usage-summary` | Gateway owner over the Sub2API usage record | Live CloudIdentity User | No |
| Personal balance history | `use-gateway-account-read-controller` | `GET /api/v2/gateway/balance-history` | `GET /api/gateway/balance-history` | BFF calls the retained Sub2API provider adapter; source `sub2api` | Fresh CloudIdentity proof and Gateway subject mapping | Yes: provider adapter is retained behind the Cloud route |
| Gateway endpoint | `use-gateway-account-read-controller` | `GET /api/v2/gateway/endpoint` | `GET /api/gateway/endpoint` | BFF calls the retained Sub2API provider adapter; source `sub2api` | Fresh CloudIdentity proof and Gateway subject mapping | Yes: provider adapter is retained behind the Cloud route |
| Key list and detail | `use-gateway-usage-controller`, `KeysPanel` | `GET /api/v2/gateway/keys`, `GET /api/v2/gateway/keys/{keyId}` | `GET /api/gateway/keys`, `GET /api/gateway/keys/{keyId}` | Gateway owner personal key reader; process-local delegated credential | Live CloudIdentity User | Yes: the delegated credential custody is transitional |
| Per-Key usage and summary | `use-gateway-usage-controller` | `GET /api/v2/gateway/keys/{keyId}/usage`, `.../usage-summary` | `GET /api/gateway/keys/{keyId}/usage`, `.../usage-summary` | Gateway owner personal key reader | Live CloudIdentity User | Yes: same delegated credential custody |
| Available Key groups | `KeysPanel` | `GET /api/v2/gateway/groups` | `GET /api/gateway/groups` | Gateway owner personal key reader | Live CloudIdentity User | Yes: same delegated credential custody |
| Fee receipts (list and detail) | `use-billing-controller` | `GET /api/v2/legacy-billing/receipts`, `.../receipts/{receiptId}` | `GET /api/billing/receipts`, `.../receipts/{receiptId}` | BFF calls the retained original financial owner (Ledger); source `ledger` | Fresh CloudIdentity proof; reciprocal active User/Account mapping and original order ownership | Yes: original financial owner is retained behind the Cloud route |
| Workspace settlement trend | `use-billing-controller` | `GET /api/v2/legacy-billing/workspace-settlements` | `GET /api/billing/workspace-settlements` | BFF calls the retained Control Plane settlement projection; source `control_plane` | Fresh CloudIdentity proof and reciprocal mapping | Yes: original owner is retained behind the Cloud route |
| Announcement list | `use-customer-announcement-controller` | `GET /api/v2/announcements` | `GET /api/announcements` | BFF calls the retained original announcement owner; source `control-plane` | Live CloudIdentity proof; no Tenant membership required | Yes: original publication owner is retained behind the Cloud route |
| Announcement acknowledgement | `use-customer-announcement-controller` | `POST /api/v2/announcements/{announcementId}/read` | `POST /api/announcements/{announcementId}/read` | BFF calls the retained original ACK owner; source `control-plane` | Live CloudIdentity proof, same-origin CSRF and idempotency key | Yes: original ACK owner is retained behind the Cloud route |
| Package pricing catalog | `use-workspace-launch-controller.loadPricingCatalog` | None | `GET /api/pricing/catalog` | Retained Control Plane only | Retained Account session | Yes: legacy-only; no Cloud package-pricing catalog owner exists yet |
| Package price preview | `use-workspace-launch-controller.loadPricingCatalog` | None | `POST /api/pricing/preview` | Retained Control Plane only | Retained Account session | Yes: legacy-only; Cloud prices through the Resource Catalog quote |
| Workspace list and detail | `use-customer-workspace-read-controller` | `GET /api/v2/workspaces`, `GET /api/v2/workspaces/{workspaceId}` | `GET /api/workspaces`, page scan | Workspace owner | Live CloudIdentity proof and Workspace owner authorization | No |
| Workspace operation readback | `use-customer-workspace-read-controller`, launch controller | `GET /api/v2/operations/workspace/{operationId}` | None | Workspace owner | Live CloudIdentity proof | No |
| Workspace models | `use-workspace-models-controller` | `GET /api/v2/workspaces/{workspaceId}/models` | None | Workspace owner | Live CloudIdentity proof | No |
| Workspace deletion readback | `use-workspace-delete-controller` | `GET /api/v2/workspaces/{workspaceId}/deletion` | `GET /api/workspaces/{workspaceId}/deletion` | Workspace owner | Live CloudIdentity proof | No |
| Workspace transactions and trend | `WorkspaceTransactionsPanel` | `GET /api/v2/workspaces/{workspaceId}/transactions`, `.../transactions/trend` | None | Gateway and Ledger owners through the BFF | Live CloudIdentity Tenant owner/admin role | No |
| Workspace financial receipt | `WorkspaceTransactionsPanel` | `GET /api/v2/workspaces/{workspaceId}/financial-receipts/{receiptId}` | None | Ledger owner through the BFF | Live CloudIdentity Tenant role; private execution evidence keeps its own fence | No |
| Agent delivery view | `AgentDeliveryPanel` | `GET /api/v2/delivery/{workspaceId}` | None | BFF composition of Workspace, Serve, Capability and Build owner facts | Live CloudIdentity proof | No |
| Legacy Workspace renewal, runtime status, Gateway budget, launch list/detail | Renewal, Fabric Runtime Read, console detail, launch recovery | No cloud caller; cloud detail receives no Control Plane Workspace | Retained `/api/workspaces/*` and `/api/workspace-launches` | Retained Control Plane | Retained Account session | Yes: legacy-only; no cloud caller is allowed to address these routes |

The seven operator reads (`/api/v2/operator/overview`, `/accounts`, `/workspaces`,
`/workspaces/{workspaceId}`, `/workspace-runtime-image-policy`, `/announcements`,
`/workspaces/{workspaceId}/runtime-image-replacements/preview`) use the same
build-time route decision through `operatorReadRoute`: the BFF authorizes the
CloudIdentity platform operator and reads the retained Control Plane owner
through `/internal/cloud/operator/*`. The platform-operator fact is CloudIdentity's
existing platform-admin decision for `CREATE PUBLISHER NAMESPACE`; a tenant
administrator is refused. Other administrator operations (writes, reconciliation,
health, runtime observations, registry, launch recovery and image replacement
activation/replacement writes) remain on the retained administrator surface and
are outside this inventory. `/api/management/state`, runtime and production
readiness also remain retained.

## Identity Fences

- The identity is a build-time constant. A read does not switch identity at
  runtime, and a CloudIdentity cookie is never translated into a retained
  Account cookie.
- A customer read selects exactly one route for its baked identity. There is no
  catch-and-retry against the other route when the selected owner is unavailable.
  `walletReadRoute`, `gatewayUsageSummaryRoute`, `billingReadRoute`,
  `announcementReadRoute` and `operatorReadRoute` are the single route decisions.
- Cloud routes require a live BFF session. Historical billing, announcement and
  personal Gateway reads carry a fresh CloudIdentity proof to the retained
  original owner; that owner verifies the reciprocal mapping before returning
  data.
- The projection's `source` tag must match the caller's identity before the UI
  commits it: `gateway` for wallet and account usage, `sub2api` or
  `control_plane`/`ledger` for the transitional owner reads, and `workspace` for
  Workspace reads. A mismatched answer is refused, not rendered.
- An unavailable owner is an explicit source envelope
  (`status: "unavailable"`, `available: false`, `reasonCode`, no `data`). The
  controllers report that state and offer a retry; they never substitute a zero,
  an empty collection or cached data.
- The package pricing catalog has no Cloud owner yet. A cloud build returns the
  explicit `pricing_catalog_owner_unavailable` state before issuing a request;
  it does not fall back to the retained route that answers a CloudIdentity
  session with 401. The cloud launch surface prices the same purchase through
  `POST /api/v2/quotes` and the Resource Catalog plan reads.

## Transitional Reads

The following reads are still transitional and must not be used to claim #840
closed:

1. Personal balance history, Gateway endpoint, Key list/detail, per-Key usage and
   groups still depend on the retained Sub2API provider adapter or delegated
   credential custody behind the Cloud Gateway owner.
2. Personal fee receipts and Workspace settlement trend still call the retained
   original financial owner behind the BFF.
3. Announcement list and acknowledgement still call the retained original
   publication owner behind the BFF.
4. The legacy package pricing catalog and preview remain retained-Control-Plane
   only and are refused under a CloudIdentity build. No Cloud package-pricing
   catalog owner exists yet; the Cloud launch path uses Resource Catalog quotes.
5. Legacy Workspace renewal, runtime-status, Gateway-budget and launch
   list/detail routes have no cloud caller and remain retained. They are not
   maintenance fallbacks for a cloud Workspace.

Installed, same-Candidate acceptance and the protected customer-read receipt
remain open. The current protected `customer_reads` producer
(`tools/cloud-business-readback.ts`) asserts the four reads this Issue names in
one session: wallet (`GET /api/v2/wallet`), usage summary
(`GET /api/v2/usage/summary?period=month`), fee records
(`GET /api/v2/legacy-billing/receipts?limit=20`) and announcements
(`GET /api/v2/announcements?page=1&pageSize=20`), together with session,
external bindings, logout and revocation. The fee check requires the `ledger`
source envelope; a not-found blocks the receipt, because the BFF uses one generic
owner-not-found both for a customer without an original Account and for a missing
or older internal route, so it cannot show the fee route works. This source inventory and its focused
fixtures do not prove a deployment, an authenticated installed readback, or a
Release.

## Verification

The route and refusal decisions are covered without a live server or browser by
`tests/ui/cloud-customer-read-routes.test.ts`. The existing controller, billing,
announcement and launch model tests cover the identity-tagged projections and
explicit unavailable states. The acceptance producer's four-read session,
owner-tag, money-encoding and session-rejection decisions are covered by
`tests/tools/cloud-business-readback.test.ts`. Release evidence remains in
[status](../status.md#cloud-customer-readback-source-wiring-2026-10-16), the
protected procedure in
[release operations](../runtime/release.md#protected-cloud-customer-reads), and
the open outcome in
[the roadmap](../roadmap.md#workspace-key-and-customer-read-boundaries).
