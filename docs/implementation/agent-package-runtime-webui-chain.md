# Agent Package + Runtime Release + WebUI → immutable OCI chain

Machine boundary: this document owns the Cloud integration contract for the
end-to-end Agent delivery chain. It is a current implementation/authority
mapping, not a deployment or runtime-readiness claim. Source, tests and receipts
prove their own layer.

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
| Package manifest digest | `candidate-index.json` `blueprint_digest` / `manifest_digest` | OMA/Foundry (upstream) | Capability W08 |
| Package content digest | `candidate_digest` (`opl-foundry-candidate-index.v2`) | OMA/Foundry (upstream) | Capability W08 |
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

## Current blocking state

As of 2026-09-26 two of the three inputs are missing upstream
([intake audit](../evidence/source-checks/2026-09-26-oma-package-runtime-webui-intake-audit.json)):

1. **OMA/Foundry**: no qualified AgentVersion can be produced locally because
   `OPL_FOUNDRY_EVALUATOR_BIN` / `OPL_FOUNDRY_REVIEWER_BIN` are unset, so
   `foundry/versions` is empty. The candidate directory/digest format itself is
   real and stable.
2. **OPL App/Framework**: no approved server/headless Runtime Release OCI exists;
   only a desktop DMG and a mutable-tag WebUI image were found.
3. **WebUI**: Cloud must build its own digest-pinned, port-3000, health-carrying
   artifact; that is a Cloud deliverable, not an upstream one.

Until (1) and (2) land, the chain stays fail-closed: the intake validator
(`tools/package-runtime-webui-intake.ts`) refuses to admit a chain that lacks any
of the three immutable inputs, and the IBD candidate may be used only as a
labelled development fixture, never as a Build input.
