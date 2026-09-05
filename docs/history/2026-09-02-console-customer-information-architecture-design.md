# Console Customer Information Architecture And Support Retirement Design

Date: 2026-09-02

State: approved design record

## Objective And Ownership

UX-02B makes the customer Console follow the customer's recurring tasks rather
than the internal service topology. It also retires the unused Support Mapping
capability because the product has no current ticket system.

The customer task chain is:

```text
understand account state -> use a Workspace or API -> understand cost
-> read messages or manage the account
```

- `apps/console-ui` owns customer presentation and calls to Control Plane
  product APIs.
- `services/control-plane` owns the retiring Support HTTP, application, Store,
  and persistence access paths.
- `docs/product/console-workspace-v1.md` owns the current Console product
  surface; `docs/implementation-architecture.md` owns current implementation
  facts; `docs/status.md` owns current evidence.
- Existing Workspace, API Key, usage, billing, announcement, account, and
  Session contracts remain authoritative for their behavior.

This record does not authorize publication, deployment, Instance
qualification, production mutation, or deletion of historical Support data.

## Observed Problem

The current Console mixes customer tasks with implementation language and
low-value navigation:

- the top-level navigation contains five items and uses inconsistent Chinese
  and English terms;
- API sub-navigation is duplicated between the shell and the page;
- announcements, account settings, and Support compete with frequent customer
  tasks;
- default customer content exposes internal service names, identifiers, raw
  enums, reason codes, and micro-dollar units;
- source evidence is repeated beside normal values instead of being disclosed
  when diagnosis is needed;
- Support presents a customer form even though no real ticket system exists.

Support is not only a visible slide. Runtime tracing found active read or write
paths through the customer API, management state, application state, Store and
Ent clients, and the retention worker. Removing only the button or HTTP route
would leave a partially live capability and could continue exposing or
deleting historical mappings.

## Decision Logic

The design follows this sequence:

```text
customer outcome
-> frequent task
-> authoritative fact required for the task
-> primary action
-> optional diagnostic evidence
-> responsive presentation
-> end-to-end verification
```

This prevents visual polish from preserving the wrong information hierarchy.
It also prevents the Console from inventing business truth: every visible
state remains an exact presentation of a typed owner DTO or an explicit
unavailable or unconfirmed state.

Three approaches were considered:

1. **Terminology replacement only.** This is small but retains duplicated
   navigation, internal fields, and a false Support affordance.
2. **Customer task information architecture plus complete Support capability
   retirement (selected).** This closes one coherent customer experience and
   the full unused Support access chain while preserving all current business
   behavior and historical data.
3. **Full Console rewrite with a localization framework and new design
   system.** This mixes customer, Admin, framework, and visual work without a
   current need and is therefore excluded.

## Customer Information Architecture

The customer top-level navigation has exactly four destinations in this order:

1. `概览`
2. `工作空间`
3. `API`
4. `费用`

Desktop and mobile use the same order. `消息` remains reachable from the
top bar at `/console/announcements`. Account information remains reachable
from the account menu. Support is absent.

The API section contains one page-level navigation with `服务信息`, `用量`, and
`密钥`. The shell does not duplicate these destinations. The fees section uses
`订阅与续费` and `账单记录` as its customer concepts without changing current
billing routes or APIs.

Existing typed route ownership remains the only navigation authority. This
package changes route presentation, not browser-history or Session ownership.

## Customer Presentation Boundary

Add one pure customer presentation model under `apps/console-ui/src/app`. It
maps only current customer-visible terms, statuses, amounts, and source states
needed by the affected shell, API, billing, account, and announcement views.

The model uses exact typed mappings. It does not use regular expressions,
substring replacement, a generic translation dictionary, or a catch-all that
labels unknown data as healthy. Known owner values receive explicit customer
labels. Unknown or unavailable values become `待确认` or `暂不可用`; raw values
may appear only in technical details.

The default customer vocabulary is:

| Owner or current term | Customer term |
| --- | --- |
| Workspace | 工作空间 |
| API Key / Key | API 密钥 |
| API Endpoint | API 地址 |
| Account Settings | 账号信息 |
| Billing | 费用 |
| Announcements | 消息 |
| Receipt | 账单记录 or 收据, according to the object |
| Runtime | 运行环境, technical details only |
| operation ID | 操作编号, technical details only |
| Session | 登录会话, technical details only |

`API` and `Token` remain because the target customer needs those established
technical concepts to use the product. `Control Plane`, `Fabric`, `Ledger`,
`Sub2API`, raw `reasonCode`, and `micros` do not appear in the default customer
interface.

Internal identifiers, raw lifecycle values, source timestamps, and diagnostic
facts are not discarded. Where currently useful, they move into a closed
`技术详情` disclosure following the UX-02A Workspace pattern. A disclosure is
not added to a page that has no current diagnostic consumer.

The Presenter does not own API decoding, polling, key lifetime, secret state,
billing calculations, Session lifetime, persistence, or backend policy.

## Support Capability Retirement

The product decision is capability retirement with historical data
preservation:

- remove the Support slide, controller, model, DTOs, API client, customer
  requests, and Support-specific styles;
- remove Control Plane `GET /api/support/tickets` and
  `POST /api/support/tickets` handlers and application service;
- register `/api/support/tickets` as an exact retired Console path so every
  method returns `404` instead of falling through to the static application;
- remove Support projections from management state and application state;
- remove Support methods from Store interfaces and implementations;
- remove the Support Ent schema and generated client access so a fresh database
  does not create the table;
- remove Support retention configuration and deletion so startup or scheduled
  retention cannot mutate historical rows;
- remove stale Support product claims from customer, Admin, current product,
  status, and implementation documentation.

The existing migrations that created
`control_plane_support_ticket_mappings`, existing deployed tables and rows, and
existing audit events remain unchanged. Schema initialization must not drop a
legacy table. Generic audit evidence may still record that a historical Support
action occurred; it does not expose or revive a Support-specific product API.

No migration drops, rewrites, exports, archives, or anonymizes historical
Support rows in UX-02B. Any future disposition of those rows is a separate data
governance and migration decision.

## Failure And Safety Behavior

- Missing or failed customer data remains explicitly unavailable; it is never
  converted to `正常`, zero usage, or zero balance.
- Unknown owner enums remain unconfirmed and preserve raw evidence only in
  technical details.
- API Key creation, one-time reveal, copy, 60-second hiding, enable, disable,
  and delete behavior remain unchanged.
- Workspace launch recovery, authoritative readback, secret cleanup, renewal,
  budget, and delete behavior completed in UX-02A remain unchanged.
- Requests to the retired Support endpoint never mutate state and always
  return `404`.
- A fresh database does not create the retired Support table. A database with a
  legacy Support table retains both the table and its rows through schema
  initialization and retention execution.

## Responsive And Accessibility Rules

- Desktop and mobile use the same four-item task hierarchy.
- The mobile bottom navigation fits without horizontal overflow or truncated
  labels.
- Current-route semantics are available through `aria-current` in both desktop
  and mobile navigation.
- Existing focus, modal, reduced-motion, 44-pixel touch-target, and semantic
  disclosure behavior remain intact.
- Only styles required by the changed shell and customer content are edited.
  CSS decomposition, new animation, new visual tokens, and a new theme are
  excluded.

## Scope

Included:

- customer navigation and terminology convergence;
- one exact typed customer presentation model and focused tests;
- default/technical information hierarchy for affected customer pages;
- complete retirement of live Support application access and retention paths;
- preservation tests for a legacy Support table and its rows;
- desktop and mobile customer acceptance plus current documentation updates.

Excluded:

- full Admin information-architecture or table redesign;
- a generic i18n framework or design-system rewrite;
- changes to Workspace, API Key, usage, billing, announcement, account, or
  Session business contracts;
- destructive Support data migration or audit-history rewriting;
- broad CSS cleanup;
- release, deployment, Instance work, or production actions.

## Completion Evidence

UX-02B is complete when:

1. desktop and mobile customer navigation contain exactly the same four
   destinations and announcements and account remain reachable;
2. the default customer DOM contains none of the prohibited internal terms,
   while required diagnostic evidence remains available in technical details;
3. every current customer-visible enum changed by this package has an exact
   typed presentation and unavailable data is never presented as healthy;
4. all Support UI, controller, DTO, HTTP, application, Store, Ent, state
   projection, and retention access paths are absent;
5. retired Support GET, POST, and other methods return `404`;
6. a fresh schema omits the Support table while a legacy table and marker row
   survive schema initialization and retention unchanged;
7. current API Key and UX-02A Workspace behavior remain green at desktop and
   mobile widths;
8. `npm run verify:local:full` passes because this package changes a retained
   persistence boundary;
9. current product, implementation, status, and roadmap owners are reconciled,
   and a local verification record is committed without claiming deployment or
   production completion.
