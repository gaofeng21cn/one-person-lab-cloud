# Console Workspace Secret Controller Boundary

## Decision

The next Console cut extracts the Workspace detail access-secret workflow from
the broad `useConsoleController` into one capability owner:

```text
current Session + selected Workspace identity
  -> Workspace Secret presentation/application controller
  -> reveal Runtime password / reveal Workspace Key / rotate password
  -> one ephemeral secret projection with bounded lifetime
```

This is an internal Console ownership change. It preserves the current Control
Plane APIs, typed DTOs, request order, idempotency keys, 60-second lifetime,
navigation, customer-visible text, and rendered behavior.

## Why This Slice

Three cuts were considered:

1. Keep Secret state in the root and move only the API calls. Rejected because
   the root would still own the timer, request freshness, reset, busy state, and
   therefore the actual lifecycle.
2. Build a generic `useEphemeralSecret` utility shared with `KeysPanel`.
   Rejected because the two callers have different aggregate boundaries and
   failure models. The Workspace flow includes Runtime credential rotation and
   Workspace readback; `KeysPanel` owns Gateway Key CRUD, dialogs, and paging.
3. Extract one Workspace access-secret capability. Selected because its two
   displayed Secret kinds share one Workspace/session scope, one timer, mutual
   exclusion, reset triggers, and stale-completion policy.

This is a capability extraction, not a file-size refactor.

## Owner

`useWorkspaceSecretController` owns the browser-side Workspace access-secret
lifecycle:

- the currently revealed Runtime credential or Workspace Gateway Key;
- mutual exclusion between those two projections;
- Workspace-password and Gateway-Key busy state;
- the 60-second timer;
- request generation used to reject late reveal/rotation completion;
- Runtime credential rotation intent and idempotency key;
- reveal, rotate, clear, timeout, and reset behavior.

It does not own the durable Secret facts. Fabric remains authoritative for
provider-neutral Runtime and Secret binding facts, Sub2API remains authoritative
for Gateway Keys, and Control Plane remains the customer API and orchestration
boundary. The Console owner controls only the ephemeral browser projection and
its commands.

## Narrow Inputs

The composition root provides only current dependencies:

- current `AuthSession` or `null`;
- selected Workspace identity and its `workspaceApiKeyId`;
- current route/session mutation guard;
- a command to refresh the authoritative Workspace detail after rotation;
- the existing global `flash` notification command.

The capability calls the existing typed API functions directly. No repository,
Context, store, event bus, or generic Secret framework is added.

## Narrow Output

`WorkspaceSecretController` exposes only state and commands consumed by the
Workspace detail view:

- current Workspace credential and Workspace Key projections;
- separate password and Key busy facts;
- `revealWorkspacePassword`, `revealWorkspaceKey`,
  `rotateWorkspacePassword`, and `clear`.

Composition-only reset is not added to the public view contract. The owning
hook clears itself when its Session, route, or selected Workspace scope changes
and on unmount.

## Invariants

- At most one Secret kind is visible at a time.
- A reveal arms one `60,000ms` timer whose callback clears the Secret unless it
  was cleared earlier.
- Route, Session, selected Workspace, logout/reset, and unmount changes clear
  the Secret and invalidate pending completions.
- A completion from an invalidated request cannot restore cleared Secret data
  or alter busy state in the new scope.
- Reveal may display only the authoritative response for the requested
  Workspace or Gateway Key.
- Runtime rotation reuses one idempotency key after an unknown outcome and
  clears the intent only after an accepted response.
- Successful rotation displays the returned credential and then requests the
  existing authoritative Workspace-detail refresh.
- Clipboard is a transient UI side effect, not a second stored Secret fact.

## Migration

1. Add typed pure lifecycle tests for mutual exclusion, reset/invalidation,
   timer decisions, and rotation-intent reuse.
2. Add `WorkspaceSecretController` and `useWorkspaceSecretController`.
3. Compose the capability from `useConsoleController` with narrow inputs.
4. Switch the Workspace detail view to the narrow contract.
5. Remove the retired Secret state, refs, timer, intent, helpers, and imports
   from the broad controller.

Moving only functions or retaining compatibility fields is not completion.

## Verification

- Focused lifecycle tests use typed contract DTOs.
- Browser acceptance proves timeout, route/logout cleanup, and rejection of
  late completion.
- Existing Workspace API idempotency, Console browser acceptance, typecheck,
  lint, and build remain green.
- `npm run verify:local` remains the repository gate.

## Non-goals

- No backend, HTTP contract, DTO JSON, persistence, or provider change.
- No `KeysPanel` refactor or shared Secret abstraction.
- No Auth, Router, Workspace source, Billing, Operator, or Support extraction.
- No change to Secret lifetime, copy behavior, user-visible text, or request
  ordering.
- No deployment, qualification, or Instance receipt.
