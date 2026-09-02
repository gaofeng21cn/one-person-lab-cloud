# Console Customer Visual System UX-03B Design

Date: 2026-09-02

State: approved design record

## Objective And Ownership

UX-03B defines the customer Console visual system and one implementable
reference slice for the already-operational business experience. It turns the
UX-03A audit findings into presentation decisions without redefining Workspace,
Gateway, billing, announcement, account, Session, Secret, Fabric, Ledger, or
Sub2API authority.

- `apps/console-ui` owns the affected presentation and calls only Control Plane
  product APIs.
- `docs/product/console-experience-guide.md` owns the durable customer outcome
  and experience principles.
- UX-03A owns the local audit baseline and prioritized findings.
- Source, focused tests, and runtime browser evidence will own implementation
  and verification claims after this design is executed.

This record approves local implementation planning. It does not claim that the
visual system is implemented, and it does not authorize publication,
deployment, Instance qualification, or production mutation.

## Business Baseline

The business path is already operational and remains fixed:

```text
public entry -> sign in -> understand account state
overview -> choose Workspace or OPL Gateway work
Workspace -> configure -> confirm -> provision -> obtain access -> open
OPL Gateway -> inspect service -> create/manage API Key -> inspect usage
fees -> understand subscription, renewal and billing evidence
```

The design follows the shortest customer-result sequence:

```text
current result
-> primary task
-> required decision or action
-> resulting state
-> secondary evidence
-> optional diagnostic detail
```

Visual hierarchy follows this sequence. It does not invent a second business
flow or make internal architecture visible to explain the interface.

## Approved Direction

The selected direction is a quiet, professional cloud workbench:

- restrained neutral surfaces with brand blue reserved for the current
  destination, focus, and primary command;
- teal, amber, and red reserved for success, warning, and error meaning;
- system fonts with no new font or visual-framework dependency;
- body text at `14px`, supporting text at least `12px`, and normal-text
  contrast of at least `4.5:1`;
- spacing derived from `4px` and `8px`, with card and control radii no greater
  than `8px`;
- information bands, rows, and unframed sections preferred over repeated
  equal-weight cards;
- icons use the current Console icon library and retain accessible names and
  tooltips where the symbol is not self-explanatory.

The implementation reuses current components and tokens. It does not add a
second design system, generic i18n framework, new theme runtime, or broad CSS
rewrite.

## Brand Boundary

`OPL Cloud` remains the platform brand. The current canonical brand asset is
`public/opl-app-icon.png`; UX-03B must reuse it without redraw, substitution,
or generated approximation. The brand control continues to navigate to the
customer Overview.

`OPL Gateway` is the customer API product identity. It may label the API
destination and page title, but it does not replace the `OPL Cloud` platform
name or icon. Internal `Sub2API` identifiers remain valid in their owning DTO,
controller, and backend boundaries and remain absent from the default customer
interface.

## Reference Slice

The first real reference slice is the shared customer Shell and Overview,
together with cross-cutting P1 repairs needed to keep current tasks usable.

### Customer Shell

- Desktop keeps one left navigation and one top action bar.
- Mobile keeps the same four task destinations in the same order and one
  mutually exclusive navigation drawer.
- Desktop navigation uses `概览`, `工作空间`, `OPL Gateway`, and `费用`.
- Mobile bottom navigation uses `概览`, `工作空间`, `Gateway`, and `费用`
  in the same task order.
- The Gateway page title is `OPL Gateway`; its local destinations remain
  `服务信息`, `用量`, and `API 密钥`.
- The top action bar contains only messages, refresh, and one account trigger.
- Refresh continues to invoke the route-owned refresh action rather than a
  browser reload.
- Logout moves into the account menu and continues to invoke the existing safe
  sign-out path.
- The sidebar and top bar do not expose two competing account surfaces.

### Overview

Overview keeps its current independent data reads and existing facts. It does
not calculate a new aggregate, trend, health status, ETA, or forecast.

The approved scan order is:

1. one summary information band for available Gateway balance, current-month
   API cost, current-month requests, and total Workspace count;
2. one primary Workspace region showing the one Workspace currently loaded by
   the Overview query plus the existing total count and route action;
3. secondary recent-fee and message regions with their current route actions.

The current dynamic primary action remains state-dependent. Loading, retry,
empty, unavailable, view, and create outcomes must not be flattened into a
single static call to action.

### Account Menu

Desktop uses a compact menu anchored to the single account trigger. Mobile
uses one modal or popover surface that cannot coexist with an open navigation
drawer.

The default customer layer contains only:

- email;
- customer identity;
- account status;
- safe logout.

`Account ID`, `Console User ID`, `Sub2API User ID`, `Session ID`, and Session
expiry are removed from the customer surface. The email must not break inside
the address when sufficient layout width is available.

### Workspace Confirmation Controls

The shared Checkbox must have a visible unchecked border, visible checked
state, keyboard focus indication, semantic checked state, and a touch target
that remains operable on mobile. The purchase command remains disabled until
the required confirmation is selected.

Workspace confirmation retains the desktop two-column review layout and a
linear mobile layout. Raw `priceVersion` is removed from the default customer
content. Price components, actual amount due, available balance, billing
period, and renewal choice continue to display owner-provided facts without
browser-side recalculation.

### Later Page Slice: OPL Gateway Key Management

After the Shell and Overview reference slice passes desktop and mobile review,
a later page-level slice may apply the approved Gateway direction. Desktop
retains an efficient Key table. Mobile shows the first current Key and primary
Key command before advanced filters. Search, sorting, status, and page-size
controls move behind one explicit filter control when space is limited.

Every current Key operation remains reachable. Low-frequency operations may be
grouped under a named `更多操作` menu, but the design does not delete the
current show or hide, usage instructions, edit, enable or disable, reset quota
usage, reset spend-limit usage, or delete actions. System Workspace Key
restrictions on editing, enable or disable, reset, and deletion remain intact.
Sensitive values remain hidden by default and preserve their current explicit
reveal lifecycle.

## State And Failure Behavior

- Independent reads remain independently useful; one failed source does not
  hide valid results from another source.
- Loading, empty, unavailable, partial-failure, permission, unknown, and
  unconfirmed states remain explicit.
- Unavailable values are not rendered as healthy zero, empty data, or success.
- Workspace failure text must not direct a customer to the retired Support
  capability. It uses an existing retry, return, or explicit unavailable action.
- Customer pages do not expose raw reason codes or internal authorities by
  default. A technical disclosure is retained only when a current customer
  diagnostic task requires it.

## Responsive And Accessibility Rules

- Reference viewports are `1280x900` and `390x844`.
- Desktop and mobile preserve the same task order and all current actions.
- No document-level horizontal overflow is introduced at the mobile viewport.
- Buttons, Checkbox controls, menus, disclosures, and navigation retain
  semantic keyboard interaction and visible focus.
- Status does not rely on color alone.
- Text cannot collide with adjacent controls or break inside a customer email.
- Icon-only commands keep an accessible name; unfamiliar commands have a
  tooltip.
- Account and navigation overlays are mutually exclusive and own focus while
  open.

## CSS And Component Boundary

The implementation starts at the existing owners:

- `apps/console-ui/src/layout/ConsoleShell.tsx` for global composition;
- `apps/console-ui/src/pages/CustomerPages.tsx` for Overview and customer page
  composition;
- `apps/console-ui/src/components/ui/Checkbox.tsx` for Checkbox affordance;
- `apps/console-ui/src/components/keys/KeysPanel.tsx` for Key priority and
  responsive controls;
- the existing product tokens and only the affected selectors in
  `apps/console-ui/src/styles.css`.

The reference slice may remove superseded selectors that it actually replaces.
It must not opportunistically decompose the entire stylesheet or introduce a
new component abstraction without at least two current callers.

## Scope

Included in the next UX-03C reference-slice implementation:

- canonical OPL brand reuse and OPL Gateway customer identity;
- visible accessible Checkbox behavior;
- retired-Support failure wording removal;
- one customer account menu without internal IDs;
- customer Shell and Overview visual hierarchy;
- customer-visible terminology and supporting-text contrast required by the
  affected reference slice;
- focused desktop and mobile behavior and visual verification.

Deferred until the UX-03C reference slice passes desktop and mobile review:

- Gateway Key filter priority and operation grouping;
- page-level rollout to Workspace list and detail, Gateway service and usage,
  fees and receipts, messages, public entry, and login.

Excluded:

- business API, DTO, billing, Workspace, Gateway, Session, or authority changes;
- Admin visual redesign;
- a second wallet, Gateway, route, or state owner;
- full-site CSS cleanup, generic localization, theme switching, animation, or
  new visual framework;
- release, deployment, Instance work, or production changes.

These later pages inherit the approved primitives only when their page-level
slice begins. UX-03B does not claim final pixel-level redesign of every
customer page.

## Visual Approval Evidence

The user reviewed current-versus-proposed PNGs for desktop Overview, desktop
account, desktop OPL Gateway Keys, desktop Workspace confirmation, mobile
Overview, mobile account, mobile Gateway Keys, and the required Checkbox. The
user approved the direction after requiring that the proposed Shell reuse the
existing OPL icon exactly.

The PNGs are design-review evidence, not immutable product contracts. Source,
behavior tests, browser readback, and post-implementation screenshots will
determine whether the implementation satisfies this design.

## UX-03C Entry And Completion Evidence

UX-03B is the approved design input. The next phase, UX-03C, implements the
reference slice and is complete only when:

1. the canonical OPL icon, platform name, brand navigation, current routes, and
   business actions remain intact;
2. required unchecked and checked Checkbox states are visible at both reference
   viewports and operable by keyboard;
3. the customer account surface contains no internal IDs and cannot overlap the
   mobile navigation drawer;
4. `OPL Gateway` identity is visible while internal Sub2API terms remain absent
   from the default customer DOM;
5. Overview preserves independent source and dynamic-action behavior while
   following the approved scan hierarchy;
6. affected normal text meets contrast and minimum-size rules without desktop
   or mobile overflow;
7. focused browser tests, typecheck, lint, build, and repository local
   verification pass;
8. post-implementation desktop and mobile screenshots are reviewed against the
   business outcomes rather than exact mockup pixels;
9. a local verification record is committed without claiming deployment or
    production completion.
