# Console Workspace Launch Controller Boundary

## Decision

The sixth bounded-context cut extracts the Workspace Launch browser workflow
from the broad `useConsoleController` into one capability owner:

```text
Control Plane Launch DTOs + pricing catalog + read-only wallet projection
  -> Workspace Launch presentation/application controller
  -> configure / confirm / submit / recover / poll / authoritative readback
  -> narrow Workspace Launch page contract
```

This is an internal Console ownership change. It preserves the current Control
Plane APIs, DTO JSON, request order, idempotency keys, polling bounds,
navigation, customer-visible text, and rendered behavior.

## Why This Slice

`useConsoleController` currently has 1,782 lines and owns session/router state,
21 remote sources, Workspace lifecycle commands, Gateway/Billing views,
Secrets, Operator commands, Announcements, Support, pagination, timers, and
multiple mutation intents.

Three cuts were considered:

1. Extract Session first. Rejected because Session currently invalidates every
   capability; moving it before capability-owned reset contracts would retain
   the same broad coupling behind a new file.
2. Extract a small Announcement or Support controller first. Rejected as the
   primary slice because it would prove file composition but not the high-risk
   idempotency, recovery, polling, and stale-request boundaries that justify
   separating the broad controller.
3. Extract Workspace Launch. Selected because it already has one business
   lifecycle, one mutation identity, one bounded polling model, one
   authoritative completion readback, and existing browser acceptance
   coverage.

The change moves one complete capability. It does not split the controller by
method count or create a generic frontend state framework.

## Owner

`useWorkspaceLaunchController` owns browser-side Workspace Launch presentation
and application coordination:

- Launch name, selected plan, auto-renew choice, configure/confirm step, and
  customer confirmation;
- pricing catalog source and per-plan price previews;
- the current Launch operation and polling issue;
- Launch-specific busy state;
- the current Launch intent and its idempotency key;
- catalog load, interrupted-operation recovery, review, submit, polling,
  authoritative Workspace confirmation, open, and reset.

It does not own:

- durable Launch Stage, status, attempts, authorization, or facts; those remain
  Control Plane `WorkspaceLaunchReconciler` facts;
- wallet balance, pricing policy, purchase eligibility, or final admission;
- Workspace existence or Runtime readiness;
- Session, Router, global toast, or other Console capabilities.

## Narrow Inputs

The composition root provides only current dependencies:

- current `AuthSession` or `null`;
- a read-only wallet `RemoteState` projection;
- a session/request-current guard for suppressing stale completion;
- `navigate` and `flash` commands;
- a callback that refreshes the authoritative Workspace list after confirmed
  completion.

The capability continues to call the existing typed API functions from
`console-read-api.ts` and `workspaces-api.ts`. No new transport abstraction is
added until another current caller needs one.

## Narrow Output

`WorkspaceLaunchController` exposes only the state and commands consumed by
Workspace Launch views:

- catalog, previews, selected plan, selected price, customer-owned mode, and
  balance sufficiency;
- form and confirmation state setters;
- operation, poll issue, and Launch-specific busy state;
- `load`, `recover`, `review`, `submit`, `open`, and `reset` commands.

`PlanOption`, `WorkspaceOrderSummary`, `WorkspaceLaunchPage`,
`WorkspaceLaunchConfirm`, and `LaunchOperation` consume this type instead of
the broad `ConsoleController`. `WorkspaceListPage` may receive both its current
list controller and the narrow Launch capability while it still renders an
active operation.

## Invariants

- One unchanged Launch input retains one idempotency key after an unknown
  response. A conflicting input cannot reuse that intent.
- A known failure may clear the intent; an unknown result must remain
  recoverable and must not permit a duplicate purchase.
- A Session generation or request generation change invalidates old API,
  polling, and readback completions.
- Recovery accepts zero or one non-terminal Launch. Multiple non-terminal
  operations fail closed.
- `manual_review` and terminal operations stop polling.
- Polling remains exactly 30 attempts at the current 10-second interval.
- `succeeded` does not navigate directly. The controller first confirms the
  Workspace through the existing authoritative paged Workspace read.
- `refunded` never navigates to a Workspace.
- `resourceBillingMode === "none"` forces the submitted `autoRenew` value to
  `false`.
- The UI presents authoritative `status`, `phase`, `checks`, and errors. It does
  not reconstruct the server state machine.

## State And Reset Lifecycle

The capability resets its form, catalog, previews, operation, poll issue,
busy state, and intent when the composition root changes Session identity or
explicitly resets protected Console state. Route changes may stop stale writes
without discarding a still-recoverable server operation.

The current broad `commandBusy` remains for unrelated commands until their
owners are extracted. Workspace Launch receives a separate busy value so an
Operator, Support, Delete, or Wallet command cannot become its state owner.

## Migration

1. Add focused pure lifecycle tests for intent retention, recovery
   classification, polling termination, and authoritative completion.
2. Add the narrow controller type and hook.
3. Move the complete Launch state and workflow from `useConsoleController`.
4. Compose the capability in `useConsoleController` using narrow inputs.
5. Switch real Workspace Launch page components to the narrow type.
6. Remove the retired Launch fields, refs, helpers, and `commandBusy` coupling
   from the broad controller.

Moving code without steps 4-6 is not completion because the broad controller
would remain the effective owner.

## Verification

- Focused lifecycle tests cover response loss with key reuse, conflicting
  input, zero/one/multiple recovery candidates, stale completion, terminal
  polling, and authoritative success readback.
- Existing Console model, pricing preview, API adapter, desktop/mobile browser
  acceptance, response-loss, and post-Launch Workspace/Runtime readback paths
  remain green.
- TypeScript typecheck proves Launch page components no longer require the
  broad controller for Launch state.
- `npm run verify:local` remains the repository gate.

## Non-goals

- No Control Plane, Fabric, Ledger, database, or HTTP contract change.
- No Redux, Zustand, XState, new Context hierarchy, event bus, or generic state
  repository.
- No simultaneous Auth, Operator, Billing, Secret, Delete, Renewal,
  Announcement, or Support extraction.
- No polling, request-order, UX copy, navigation, or pricing-policy change.
- No broad typing of every Console status string.
- No Acceptance B retirement or Instance deployment.
