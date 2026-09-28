# Agent Package + Runtime Release + WebUI → immutable OCI chain

This document maps the Cloud delivery chain to its source owners and typed
contracts. It does not introduce another runtime or admission authority.
Implementation evidence is recorded separately from deployment and readiness.

## Goal

Fix one immutable OCI from three exact, immutable, upstream inputs and let Cloud
deploy it through Workspace → Fabric → Serve, then read it back through the BFF:

```
OMA/Foundry qualified Agent Package (manifest/content digest)
  + OPL App/Framework approved Runtime Release (OCI digest, ABI)
  + Cloud WebUI artifact (digest, port 3000, health, compatibility)
  → W09 Build fixes all three refs → immutable OCI digest + descriptor + receipt
  → W15 Workspace order/entitlement/model config
  → W11 Fabric resource/attachment/Secret refs + readback
  → W17 Serve deploy → container → readiness → access → current
  → W26 BFF/Console readback
```

Cloud does **not** implement OMA semantics, does **not** implement a Codex/DSH
internal runtime, and does **not** create a second Package/Runtime/WebUI
authority. Cloud only consumes approved upstream facts.

## Field-level mapping

| Chain element | Canonical field | Owner (writer) | Consumed by |
| --- | --- | --- | --- |
| Agent manifest digest | transport `manifest_digest` binds `candidate/agent/agent-pack.json` | OMA/Foundry (upstream) | Capability W08 |
| Candidate identity | `candidate_digest` binds the canonical v2 index; `content_digest` separately binds indexed file bytes | OMA/Foundry (upstream) | Capability W08 |
| Package version identity | `capability.package_versions.sha256` (uploaded content) + version label | Capability W08 | Build W09 |
| Package claim | `capability.reference_claims` `package_claim_id` | Capability W08 | Build W09 |
| Runtime release | `runtime_control.runtime_releases` (`artifact_digest`, `runtime_abi_version`, `publisher_contract`) | Runtime Control W10 | Build W09 |
| Runtime image digest | `RuntimePublisherContract.image.digest` | OPL App/Framework (upstream) → Runtime Control W10 | Build W09 |
| Runtime ABI | `runtimeAbiVersion` + `packageFormatVersions` | OPL App/Framework (upstream) | Build W09 compatibility |
| Runtime entrypoint / readiness / model / Secret | `RuntimePublisherContract` `buildRecipe`, `applicationAccess`, `modelConfiguration` | OPL App/Framework (upstream) | Serve W17 via descriptor |
| WebUI artifact | `capability.webui_versions` (`artifact_digest`, `publisher_contract`) | Capability W07/W08 | Build W09 |
| WebUI port / health | `webuiContract.integrationMode` + Workspace Runtime ABI (`port: 3000`) | Cloud WebUI owner + contracts | Serve W17 readiness |
| Build input snapshot | `capability.capability_versions.provenance_evidence.input` (`BuildInputSnapshot`) | Build W09 | Serve W17 |
| OCI artifact digest | `capability.capability_versions.artifact_digest` / `build.build_artifacts` | Build W09 | Serve W17 |
| Deployment descriptor | `DeploymentDescriptor` + `deployment_descriptor_digest` | Build W09 | Serve W17 |
| Resource refs | `fabric.resource_sets/resources/attachments/secret_bindings` | Fabric W11 | Serve W17 |
| Model config | `workspace.model_configurations` (`modelConfigRef`) | Workspace W15 | Serve W17 |
| Secret binding refs | `fabric.secret_bindings` (`secretBindingRefs`) | Fabric W11 | Serve W17 |
| Runtime readback | `serve.agent_runtime_instances` (`readiness_evidence_ref`, `access_url`) | Serve W17 | BFF W26 |
| Entry URL | `serve.access_bindings` | Serve W17 | BFF W26 |

## Ownership / write-set boundaries

- **Capability (W07/W08)**: publisher namespaces, packages, package versions,
  reference claims, WebUI versions. It is the only writer of Package and WebUI
  catalog facts.
- **Runtime Control (W10)**: the runtime release catalog only. It never deploys
  an Agent, never owns a runtime instance or readiness.
- **Build (W09)**: fixes the three exact refs and emits one immutable OCI digest,
  descriptor, and build receipt. No `latest`/tag drift.
- **Workspace (W15)**: order, entitlement, model config and resource intent only.
  It never writes deployment, current selection or access.
- **Fabric (W11)**: resources, attachments, Secret bindings, actions and readback
  only. It never starts the Agent OCI.
- **Serve (W17)**: generic OCI deployment, runtime, readiness and access only. It
  never recognizes an OMA/DSH business object.
- **BFF/Console (W13/W14/W16/W26)**: composition and readback only.

## Safety gates

- API keys and model secrets travel only as Secret references / a dedicated
  secure input. They never enter the OCI, ordinary Workspace fields, or logs.
- No cross-owner database join; every cross-owner read is a typed owner readback.
- Build refuses a mutable tag or a desktop package in place of a Runtime OCI.
- Serve refuses a revision whose declared contract needs an input the deployment
  command does not carry.

## Current source acceptance

The [September 28 source repair](../evidence/source-checks/2026-09-28-runtime-capability-readback-repair.json)
checks native candidate identity and byte integrity without upgrading its
`not_qualified` / `not_evaluated` state. Capability owns the intake and approved
catalogs; Build validates the immutable owner snapshot and fixed recipe.

The [exact Runtime local build](../evidence/source-checks/2026-09-28-exact-runtime-local-build.json)
uses the approved GHCR Runtime digest, Cloud WebUI and a synthetic Package. It
proves composition, Registry digest readback, publisher browser behavior and
restart recovery. The original OMA candidate and TCR publication remain
unverified on this machine. These unknown facts do not imply Agent loading,
model use or deployment success, and do not impose a recurring source-merge
obligation. The historical September 26 missing-Runtime diagnosis is superseded.

`tools/package-runtime-webui-intake.ts` is a development diagnostic exercised by
its focused tests. It is not called by the Build admission path and must not be
used as another authority over the typed owner contracts.
