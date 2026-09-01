# Console Workspace Task Experience Design

Date: 2026-09-01

State: approved design record

## Objective And Ownership

UX-02A optimizes one current customer outcome: purchase a Workspace and enter it
without needing to understand the Control Plane operation model, Fabric runtime
checks, secret implementation, or Sub2API budget units.

The task chain is:

```text
select plan -> confirm price -> launch Workspace -> determine availability
-> obtain credentials -> open Workspace
```

- Primary module owner: `apps/console-ui`.
- Product guidance owner: `docs/product/console-experience-guide.md`.
- Current business and authority owners remain the existing Control Plane APIs,
  Workspace DTOs, and their service owners.
- Real callers are the Workspace launch and detail routes selected by the typed
  Console route owner completed in UX-01.
- Contract impact: none. This package changes presentation and task hierarchy,
  not prices, lifecycle commands, polling, readback, or persisted state.

This document records an approved implementation decision. It is historical
evidence, not another current product, roadmap, deployment, or production-state
owner.

## Observed Problem

The current pages expose domain and implementation fields as primary customer
content:

- Launch renders raw `status`, `phase`, `operation ID`, `errorCode`,
  `blockReason`, and check names.
- The success command says `读取 Workspace`, which describes an internal
  readback rather than the customer's next task.
- Workspace detail places `打开 WebUI` at the bottom of a credential table.
- The first screen uses `Runtime ready`, `Workspace URL`, `Workspace Key`, and
  `Secret`, while the actual question is whether the Workspace can be opened.
- Model-budget fields require customers to understand raw `micros` and occupy
  more visual priority than plan, price, and entitlement period.
- `CPU / 内存规格` is permanently `-`, raw renewal status is displayed, and a
  delete-unavailable reason code is shown to customers.
- Refresh and delete are placed beside the Workspace name while the primary
  open action is visually subordinate.

The underlying operations are real and must remain available for diagnosis.
The experience defect is that implementation evidence and customer decisions
share the same default hierarchy.

## Decision Logic

The design follows the current product outcome rather than a visual-style
exercise:

```text
business outcome
-> user decision at each step
-> authoritative facts required for that decision
-> primary and secondary actions
-> diagnostic disclosure
-> responsive visual hierarchy
-> task-level verification
```

This order is required because color, spacing, and component polish cannot make
an interface understandable when the wrong facts receive the highest priority.
It also prevents the browser from inventing business truth: every user-facing
state remains a deterministic presentation of a current typed DTO or explicit
source-unavailable state.

Three approaches were considered:

1. **One vertical task slice with a local typed Presenter (selected).** This
   removes internal language from the purchase-to-entry path, keeps raw evidence
   available on demand, and can be verified end to end without changing domain
   ownership.
2. **Global localization and content framework.** This could eventually unify
   the entire Console, but it would mix unrelated customer, operator, API, and
   billing work before the highest-value task has a proven vocabulary.
3. **Direct JSX copy edits.** This is smaller initially, but it leaves status
   interpretation distributed across components and cannot prove that unknown
   or future owner values fail closed instead of being mislabeled.

## Presentation Boundary

Create one pure Workspace experience model under `apps/console-ui/src/app`.
It maps only the current Workspace launch, runtime, renewal, and budget values
needed by these pages into a discriminated display model.

The model uses exact value switches. It does not use substring matching,
regular-expression translation, inferred progress, or a generic fallback
dictionary. Values outside the current known set become an explicit
`unknown`/`unconfirmed` display state and retain the raw value only in technical
details.

The Presenter does not own:

- launch recovery, polling, idempotency, or authoritative Workspace readback;
- Workspace, Runtime, wallet, price, renewal, or budget truth;
- navigation or Session lifetime;
- secret retrieval, rotation, cleanup, or persistence.

Those behaviors continue to use their current controllers and APIs.

## Launch Experience

The default Launch result answers two questions: what is happening, and what
can the customer safely do next.

- `pending`: `正在准备工作空间`; show the exact customer-stage label and allow
  only status refresh.
- `manual_review`: `需要人工处理`; state that the order is retained and expose
  raw diagnostic evidence only inside `技术详情`.
- `succeeded`: `工作空间已可使用`; make `查看工作空间` the primary command.
- `failed`: `开通失败`; preserve the owner's failure fact without claiming a
  refund.
- `refunded`: `开通失败，费用已退回`; distinguish it from an unrefunded failure.
- unknown status or poll/readback issue: `结果待确认`; explicitly prohibit a
  duplicate purchase and keep refresh as the only safe action.

Current exact stage values receive customer labels. The raw operation ID,
status, stage, error code, block reason, and checks move into a closed
`技术详情` disclosure. No diagnostic fact is discarded.

## Workspace Detail Experience

The first Workspace panel answers the current customer decision:

- Workspace name and customer-facing availability;
- plan, monthly price, and entitlement end date;
- one primary `打开工作空间` command, enabled only when the authoritative
  Runtime is running, ready, and has a URL;
- a secondary refresh command.

The default content order is:

1. availability and open action;
2. access credentials;
3. plan and renewal terms;
4. advanced settings;
5. technical details.

Credential labels become `登录账号`, `登录密码`, and `API 密钥`. The panel says
that sensitive information is hidden after 60 seconds. Reveal, copy, rotate,
timer cleanup, logout cleanup, and no-store behavior remain unchanged.

Plan and entitlement facts remain visible because they determine whether the
Workspace should be usable. The permanent `CPU / 内存规格: -` row is removed.
Renewal values receive exact customer labels rather than raw enums.

Model budget and destructive deletion move into a closed `高级设置`
disclosure. Budget money is formatted as USD for reading, while existing
inputs and write payloads keep their current exact micro-dollar contract. The
disclosure is a hierarchy change, not a budget-policy redesign.

Runtime status, URL, checks, Workspace ID, price version, raw lifecycle values,
and other support evidence remain in a closed `技术详情` disclosure. Source
unavailable, empty, zero, and error states remain distinct through the current
`SourceState` behavior.

## Responsive And Interaction Rules

- Desktop and mobile use the same semantic order and primary action.
- The open button and key facts must fit without horizontal page overflow.
- Disclosures use native `details`/`summary` keyboard semantics.
- Icon buttons retain accessible names; command buttons use icon plus action
  text where the command is not unambiguous from an icon alone.
- No new animation, decorative surface, nested card hierarchy, or broad visual
  theme is introduced.

## Failure And Safety Behavior

- An unavailable Runtime never becomes `未运行`, `0`, or `正常`; it remains
  explicitly unavailable and the open command is disabled.
- Runtime not found, destroyed, unready, ready-without-URL, and ready-with-URL
  are distinct presentation outcomes.
- A Launch result that is unknown or not authoritatively read back never
  enables another purchase from the operation view.
- Raw error and diagnostic fields are not shown by default but remain
  inspectable for support.
- Delete and renewal commands keep their existing confirmation, idempotency,
  stale-response, and readback rules.
- Secret values remain transient controller state and are never copied into the
  Presenter, browser storage, logs, screenshots, or verification records.

## Scope

Included:

- a local typed Workspace presentation model and focused tests;
- Launch result hierarchy and customer language;
- Workspace detail primary action, credential labels, fact ordering, advanced
  settings, and technical disclosures;
- focused desktop/mobile task acceptance;
- updates to existing browser assertions whose visible hierarchy changes.

Excluded:

- global i18n or a Console-wide terminology migration;
- customer navigation, API Key, Billing, Support, or Admin redesign;
- controller extraction or backend API/DTO/schema changes;
- budget-policy redesign or conversion of write units;
- CSS decomposition beyond selectors required by this task;
- publication, deployment, Instance qualification, or production actions.

## Completion Evidence

UX-02A is complete when:

1. pure tests cover every current Launch status and stage plus explicit unknown
   handling, Runtime availability outcomes, renewal labels, and budget labels;
2. the default Launch view contains no visible raw operation, status, stage,
   error, or check names, while technical details preserve them;
3. a customer can select a plan, confirm the price, launch, view the resulting
   Workspace, reveal/copy credentials, and reach the open action at desktop and
   mobile widths;
4. model budget and delete are reachable only after opening advanced settings;
5. existing secret cleanup, stale-read, idempotency, renewal, delete, budget,
   and authoritative-readback tests remain green;
6. `npm run verify:local` passes and a verification record is stored under
   `docs/history/**` before the final local commit.
