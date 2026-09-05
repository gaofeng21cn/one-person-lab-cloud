# Console Customer Visual System UX-03C Verification

Date: 2026-09-04

State: locally verified

## Objective And Boundary

UX-03C implements the approved customer Console visual reference slice from
UX-03B around the already-running business path:

```text
登录 -> 概览 -> 工作空间 / OPL Gateway -> 费用
                       -> 消息 / 账号信息
```

The change is presentation-only. It improves customer task hierarchy,
terminology, account-surface composition, Gateway product identity, Workspace
confirmation affordances, failure wording, and responsive layout. It does not
redefine Workspace, Gateway, billing, announcement, Session, Secret, Fabric,
Ledger, Control Plane, or Sub2API authority.

This record is local source and fake-only browser evidence. It does not claim a
Candidate, publication, deployment, Instance qualification, production
availability, or a business redesign.

## Source Identity

- Branch: `codex/workspace-task-experience`
- Verified implementation HEAD: `0b860e70a4258894297368b3d36fe5818ca9a761`
- UX-03A audit: `3fcc95b3` (`docs(console): record customer UI audit`)
- UX-03B design: `975498dd` (`docs(console): approve customer visual system`)
- UX-03C implementation commits:
  - `38bd3530` — freeze customer visual acceptance contract
  - `00c27e80` — align Gateway presentation identity
  - `6c018797` — consolidate customer account actions
  - `c3761eba` — clarify Workspace launch confirmation
  - `0b860e70` — prioritize customer Overview tasks

## Delivered Surface

- Customer navigation remains four task destinations in the approved order:
  `概览`、`工作空间`、`OPL Gateway`/`Gateway`、`费用`.
- The platform identity remains `OPL Cloud` and reuses the canonical
  `public/opl-app-icon.png` asset.
- The API destination is presented as `OPL Gateway`; its local tabs remain
  `服务信息`、`用量`、`API 密钥`.
- Customer account information has one surface containing email, identity,
  account status, and safe logout. Internal IDs and Session facts remain out
  of the default customer layer. Mobile navigation and account overlays do
  not remain open together.
- Workspace confirmation retains the existing Apps SDK Checkbox, adds a
  visible unchecked border and checked action-blue state, preserves semantic
  keyboard operation, and keeps the final command disabled until confirmation.
- Failed Workspace launch wording points to technical details and retry rather
  than the retired Support capability.
- Overview now follows the business scan order: summary facts, the Workspace
  primary region/action, then recent fees and messages. Existing independent
  reads, source-state distinctions, limits, and route actions are unchanged.

## Browser Review

Reference viewports:

- desktop: `1280x900`
- mobile: `390x844`

The fake-only browser review covered:

- Overview hierarchy and dynamic Workspace action;
- mobile navigation drawer and account-surface mutual exclusion;
- account email/identity/status/logout presentation;
- Workspace confirmation unchecked and checked states;
- visible Checkbox border, semantic `aria-checked`, keyboard focus, and
  disabled/enabled submit transition;
- no horizontal document overflow;
- no page errors, Console errors, or external requests in the Workspace audit
  harness.

Captured evidence is retained outside the repository at:

```text
/tmp/one-person-lab-cloud-ux03c-task6-2026-09-04/
```

The retained manifest is
`/tmp/one-person-lab-cloud-ux03c-task6-2026-09-04/manifest.json`.

Selected PNG SHA-256 values:

| Evidence | SHA-256 |
| --- | --- |
| `desktop-overview.png` | `dbe4bf4625f01b39b13b6497e6070be8636ab001dd356f3483e4035fe096d3c5` |
| `desktop-account.png` | `b65e28f95a227c13c495d493e79b4af18acd77ac31ac4987bfbd51d5d71787a1` |
| `desktop-workspace-confirm-unchecked.png` | `5cceeb62275920190441ac33f189fac78d42b5721e4d8d2054d89a6345ffb462` |
| `desktop-workspace-confirm-checked.png` | `55a091821f19276bb7031b0d11065a0b26898ee4cf6353f77adb5b7dce6601ff` |
| `mobile-overview.png` | `03ee5098ab6b4275d7ff608d00a07f35ed1fb1f800adaecebb7b889e9c593d22` |
| `mobile-navigation-drawer.png` | `71ae506a63a1caa85b1319f127a86ae36666e3c812e3606613a1389b0b894d22` |
| `mobile-account.png` | `1028ea5aed450d4a15dba6f7d41f0a53fef1ff96fbaf92a6ed260b0c11230c1b` |
| `mobile-workspace-confirm-unchecked.png` | `63cda522e47a6fed02c4e812db992259740e958c62e123e53e37ed6507239b19` |
| `mobile-workspace-confirm-checked.png` | `663f35788e52cc7750e63b3748eea20c02792e327b1aadffaba7f0068fc0357b` |

The mobile account capture was regenerated after the drawer transition settled;
its recorded hash is the settled capture above.

## Verification Commands

All commands ran from this worktree and passed with exit code 0:

```text
node --test tests/ui/console-model.test.ts tests/ui/workspace-experience-model.test.ts
npm run test:browser:customer-experience
npm run test:browser:workspace-lifecycle
npm run typecheck
npm run lint
npm run build
npm run verify:local
```

The final `npm run verify:local` passed the product-boundary check, 193 Node
source tests, billing, Gateway usage, owner-read, announcement, customer
experience, operator, and Workspace browser suites, TypeScript checks, Vite
build, all Go module compile/database-free tests, and the Git whitespace gate.

## Preserved Business And Authority Boundaries

- No Control Plane route, API, DTO, controller, persistence, or schema change.
- No Workspace launch, readback, renewal, delete, budget, Runtime, or Secret
  state-owner change.
- No Gateway Key, wallet, usage, billing, receipt, or announcement read-owner
  change.
- No new browser aggregate, trend, forecast, health calculation, unread count,
  or Support request.
- Sub2API remains the internal wallet, Key, Gateway, and usage authority.
- Control Plane remains the only browser-facing product API.
- No push, PR, merge to `main`, deployment, Instance receipt, or production
  claim was made.

## Unresolved And Deferred Work

UX-03C is complete for the approved reference slice. The following remain
separate work and were not silently included:

- UX-03D: page-level Gateway Key mobile priority and operation grouping;
- separate visual slices for Workspace list/detail, Gateway service/usage,
  fees, messages, login, and public entry;
- any future product-wide terminology or information-architecture decision.

The visual review does not establish pixel equality with historical mockups;
runtime behavior, semantic access, source-state preservation, and viewport
reachability remain the acceptance criteria.
