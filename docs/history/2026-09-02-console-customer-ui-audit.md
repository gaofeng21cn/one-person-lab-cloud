# Console Customer UI Audit UX-03A

Date: 2026-09-02

State: completed local audit record

## Objective And Boundary

UX-03A audits how the existing customer Console presents the already-operational
business paths. It does not redefine the product, change a business contract,
or authorize implementation, publication, deployment, Instance qualification,
or production mutation.

The immutable business baseline for this audit is:

```text
public entry -> sign in -> understand account state
overview -> choose Workspace or OPL Gateway work
Workspace -> configure -> confirm -> provision -> obtain access -> open
OPL Gateway -> inspect service -> create/manage Key -> inspect usage
fees -> understand subscription, renewal and billing evidence
```

`apps/console-ui` owns the audited presentation. Control Plane remains the only
browser-facing product API. Workspace, Gateway, billing, Session, Secret,
Fabric, Ledger and Sub2API authority boundaries remain unchanged.

## Source Identity And Method

- Branch: `codex/workspace-task-experience`
- Audited HEAD: `d8c07e200a89108b88404013f4c8c3893fe4e90c`
- Merge base with `origin/main`: `f024fd34ab9944093672a07ee91e44ce5a8d62b8`
- Runtime: repository-owned fake-only in-memory Console demo at
  `http://127.0.0.1:5198`
- Viewports: `1280x900` and `390x844`

The browser audit covered the public home and login surfaces, Overview,
Workspace list, Workspace configuration and confirmation, Workspace detail,
Gateway service information, usage, Keys and Key creation, fees subscription
and receipts, announcements, navigation and account information. It inspected
normal, empty, unavailable and sensitive-information states exposed by the
fixture, plus source-owned wording for states not directly reproducible in the
fixture.

No horizontal document overflow was observed at `390x844`. Browser screenshots,
page text captures and DOM probes are retained outside the repository under
`/tmp/one-person-lab-cloud-ux03a-audit-2026-09-02/`; they are local audit
evidence, not a visual product contract.

## Findings

No P0 data-loss, security or business-authority defect was found. The following
P1 issues materially obstruct comprehension or a core task.

### P1-1 Required Checkbox Affordances Are Invisible

The unchecked auto-renew control and the required prepaid-confirmation control
have an `18x18` interactive `role="checkbox"`, but live computed style reports a
transparent background and `0px` border. The surrounding text remains
clickable, so automation can complete the task, but a customer cannot see the
control they must select.

This affects both desktop and mobile Workspace purchase. The required
confirmation directly gates the final purchase action, so this is the first
visual control defect to repair. The current wrapper is
`apps/console-ui/src/components/ui/Checkbox.tsx`; Workspace consumes it in
`apps/console-ui/src/pages/CustomerPages.tsx`.

### P1-2 Account Information Exposes Internal Identity And Breaks On Mobile

The customer account disclosure exposes `Account ID`, `Console User ID`,
`Sub2API User ID`, `Session ID` and `Session` expiry. `Sub2API User ID` violates
the agreed customer product boundary, while permanently unavailable Session
facts have no current customer task.

At `390x844`, account information opens over a still-open navigation drawer.
The two surfaces compete for focus and space, the email wraps inside the word,
and technical labels and values visually collide. Desktop also wraps the email
despite ample surrounding space. Account information has two separate desktop
entry buttons, in the sidebar and top bar, without a distinct task for each.

### P1-3 The API Surface Does Not Establish OPL Gateway Identity

The top-level route, page title and content use only `API` or `API service`.
Customers can see a balance, endpoint, Keys and usage but cannot identify these
as one `OPL Gateway` product. Internal code and DTO names must remain unchanged;
the missing layer is customer-facing product identity only.

### P1-4 Mobile Key Management Hides The Actual Keys Below Filters

At `390x844`, endpoint actions and five always-visible filter controls consume
the first viewport. The first Key and every Key operation appear only after a
substantial scroll. On the Key card, seven icon-only operations then require
individual discovery. Search and advanced filtering are secondary to viewing,
copying and managing the customer's current Keys.

### P1-5 A Failed Workspace Path Still Refers To Retired Support

The Workspace experience model can still instruct a customer to contact
support, although UX-02B retired the Support capability. This creates a dead
next step precisely when the customer cannot complete Workspace provisioning.
The wording must point to a current action or explicit unavailable state; UX-03
must not reintroduce Support.

The following P2 issues increase cognitive load or weaken consistency but do
not independently block a task.

### P2-1 Public And Customer Vocabulary Is Not Yet Coherent

Public surfaces mix `Your lab, online`, `Workspace`, `AI API`, `Pilot` and
Chinese task language. Customer error strings also revert to `Workspace` and
`API Key` while normal pages use `工作空间` and `API 密钥`. `OPL Cloud`,
`OPL Gateway`, `API`, `Token`, `USD` and `GB` remain valid product, task or unit
terms; this finding does not call for indiscriminate translation.

### P2-2 Default Workspace Content Includes Implementation Detail

Workspace purchase confirmation displays raw `priceVersion`. Workspace lists
repeat `生命周期状态`, and customer technical disclosures contain extensive raw
status, reason, runtime and evidence fields without a current diagnostic
consumer. Request IDs, API addresses, Key facts and units with a direct task
remain useful; unrelated implementation evidence should not be promoted into
the customer layer.

### P2-3 The Fees Destination Does Not Answer The Whole Cost Question

The `费用` destination initially shows only Workspace subscriptions. Gateway
balance and actual API spend are presented under `API` and Overview, so the
place named for cost does not itself answer the customer's overall cost
question. UX-03 may reorganize existing read-only facts, but must not calculate
or invent a new total in the browser.

### P2-4 Supporting Text Falls Below Normal-Text Contrast

Live computed contrast found visible `10px` and `12px` supporting text between
`2.58:1` and `3.32:1` on its current surfaces. Examples include the customer
role and the explanations below Overview metrics. These labels communicate
meaning and do not qualify as decorative text; the visual system should meet
normal-text contrast rather than relying on font size and color together.

### P2-5 Equal-Weight Panels Weaken Scan Priority

Overview, Gateway service, fees and announcements rely on repeated bordered
panels with nearly identical emphasis. Sparse screens retain large empty areas,
while dense screens such as Keys place many controls at the same level. The
problem is not the existence of panels; it is the absence of a consistent
hierarchy between the current result, primary action, supporting content and
diagnostic detail.

## Implementation Cause Relevant To UX-03

The current UI loads Apps SDK UI styles, product tokens, shared component styles
and a `6477`-line `styles.css` in cascade order. `styles.css` contains repeated
historical definitions and final cascade guards for shared selectors such as
the sidebar, account band, panels and launch confirmation. The invisible
Checkbox demonstrates that setting only a later `border-color` does not supply
the missing border width and style expected from the upstream component.

UX-03B should establish the reference slice through the existing token and
component owners and remove only superseded rules in the affected surface. A
full CSS rewrite, a second design system or another UI framework is not
justified by this audit.

## Preserved Strengths

- Desktop and mobile expose the same four customer destinations in the same
  order: `概览`, `工作空间`, `API`, `费用`.
- The audited mobile routes do not create horizontal document overflow.
- Workspace access, Key management, usage and receipt facts remain reachable.
- Loading, empty and unavailable facts are represented explicitly rather than
  silently converted to healthy zero values.
- Sensitive Workspace credentials remain hidden by default with explicit
  reveal actions.
- Icon-only global actions have accessible names in the current DOM.

These strengths are constraints for the redesign, not reasons to freeze the
current visual treatment.

## Evidence Exclusions

The fake demo receipt uses status `succeeded`, while current local qualification
requires the Ledger purchase receipt status `completed`. The customer presenter
therefore correctly treats the fixture value as unknown and renders `待确认`.
This is a fixture-fidelity issue and is not evidence that the production billing
status mapping is wrong.

The repeated Vite HMR WebSocket error for port `24678` came from the local browse
session and did not prevent route or API use. It is excluded from the customer
UI findings.

Admin information architecture and admin-visible Control Plane, Fabric, Ledger
and Sub2API diagnostic language are outside UX-03A. Internal DTO, source and
controller identifiers such as `sub2apiUserId` and `source: "sub2api"` also
remain outside the customer presentation change.

## UX-03B Entry Gate

UX-03B may begin from one real reference slice: shared customer shell and
Overview, plus the cross-cutting Checkbox and customer-language boundary needed
to keep the core flow operable. Its result must be reviewed at both audited
viewports before extending the visual rules to Workspace, Gateway and fees.

The first implementation priorities are:

1. restore visible, accessible Checkbox affordances;
2. remove customer-facing internal identity and retired-Support language;
3. establish `OPL Gateway` as the API product identity;
4. resolve the mobile account-surface composition;
5. establish readable type, contrast, spacing and hierarchy in the reference
   shell and Overview without changing business authority.
