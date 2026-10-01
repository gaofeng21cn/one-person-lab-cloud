# OPL Connect

Owner: `one-person-lab-cloud`
Purpose: `connect_target_reference`
State: `active_target_reference`
Machine boundary: Human-readable logical capability reference; concrete
backends and readiness come from Framework, domain, Instance and owner
readback.

OPL Connect is the logical access capability inside OPL Fabric. It defines the
stable request, policy, result, evidence and recovery boundary for App,
Workspace and approved domain agents to use external systems. The capability
does not prescribe one transport or deployment. A configured backend may be a
native service, API/CLI adapter, MCP path, package, device-control service or
another qualified implementation.

`glkvm-native` is one possible OPL Connect backend. It supplies device/session
control and observation primitives; it does not become the Connect logical
owner, a hospital-system semantic adapter or a clinical decision authority.
Serve invocations may use Connect through Runway and Fabric when an exact
revision and consumer data policy permit it. Institution-specific semantics
remain with the relevant domain Agent and its deployment profile.

## Logical Capability Responsibility

The logical Connect contract owns:

- request/result envelopes and capability references;
- credential, authorization and data-egress boundaries;
- normalized source and observation references;
- error, retry, timeout and unknown-result semantics;
- backend execution receipts and opaque provenance.

The selected backend owns its transport protocol, device or provider calls,
provider-specific diagnostics and implementation details. Domain agents retain
the meaning of returned data and the decision to act on it.

## Standard Call Shape

```text
App / Workspace / domain Agent
-> OPL Connect logical contract
-> selected backend (for example `glkvm-native`, API, CLI or MCP)
-> normalized source/observation refs + backend receipt
-> domain semantic adapter and workflow
-> optional Ledger receipt refs
```

Console may approve account-managed credentials, backend availability, service
egress and quotas. Ledger may retain opaque source and execution refs. Neither
changes backend or domain truth.

## Package Boundary

An implementation may be carried by an OPL Package, exposed by a native
service, or supplied by the Instance. Its implementation owner controls
identity, capabilities and exact revision; the configured carrier or Instance
reads back the effective backend. Framework aggregates installed/callable
implementation state. Connect binds the selected backend and its
credentials/resources to a run without making that backend the logical owner.

## Transport And Domain Boundary

Connect is a logical module, not a single provider implementation. Framework,
Instance or another approved owner may select and expose a backend, while the
Connect contract remains stable across backend replacements. The backend owns
transport invocation, provider/device retries, cache behavior and diagnostics;
Connect owns the cross-backend envelope, policy, normalization and recovery
semantics.

MAS and other domain owners consume the exact provider/source refs and retain
query strategy, result selection, evidence interpretation, claim support and
quality decisions. MAP follows the same pattern: its semantic hospital-system
adapters call the Connect contract, while `glkvm-native` or an API/CLI/MCP
implementation supplies the selected backend. Current capability readiness
requires fresh logical-contract, backend and domain-owner readback.

## Governance And Evidence

| Concern | Owner |
| --- | --- |
| Logical Connect contract, envelopes, policy and recovery semantics | OPL Connect |
| Backend transport, device/provider calls and implementation diagnostics | Selected backend owner, such as `glkvm-native` or an API/CLI/MCP adapter |
| Backend identity, capabilities and publication revision | Backend/package owner |
| Backend physical install, update, remove and readback | Configured native carrier or Instance; Framework delegates and aggregates |
| Account/service availability, credential approval, quota and audit policy | OPL Console |
| Resource and environment binding | OPL Fabric |
| Retrieval strategy, semantic mapping, evidence use, writing and review | Domain Agent, including MAP |
| Receipt and opaque provenance refs | OPL Ledger; the calling owner retains continuation authority |

Logical-contract availability, backend health, policy approval and domain
readiness are separate states and must remain separately readable. A MAP site
adapter is qualified by MAP and the Instance; selecting `glkvm-native` or
another backend does not make the clinical capability callable by itself.
