# Issue draft — Cloud WebUI artifact and Console/BFF integration (#356 follow-up)

> Draft only. Do not create, merge, publish, or deploy from this file.

## Proposed title

`Cloud WebUI: publish an immutable port-3000 artifact and integrate W13/W14/W16/W26 read paths`

## Context

The Cloud Package + Runtime + WebUI chain now has a field-level mapping and a fail-closed intake gate. The Cloud WebUI contract requires a digest-pinned multi-arch OCI artifact with:

- `linux/amd64` and `linux/arm64`;
- container port `3000`;
- `/healthz` health contract;
- declared Runtime ABI compatibility;
- declared Package format compatibility;
- no mutable `latest`/`stable` tag as the Build identity.

The current evidence does **not** contain an admitted Cloud WebUI artifact. The observed upstream `ghcr.io/gaofeng21cn/one-person-lab-webui:stable` is mutable and lacks the required admitted compatibility/health manifest. Upstream OMA/Foundry also has no fresh qualified AgentVersion in the audited environment, and no approved server/headless Runtime Release OCI is available.

## Goal

Deliver the Cloud-owned WebUI carrier and the narrow Console/BFF integration needed to expose the real owner facts without creating a second Package, Runtime, Build, Workspace, Fabric, Serve, or Secret authority.

## Scope

### Cloud WebUI artifact

1. Build a standalone, digest-pinned multi-arch OCI artifact from an exact Cloud source SHA/tree.
2. Serve the Cloud WebUI on port `3000` and expose `/healthz` plus readiness evidence.
3. Emit an artifact manifest containing source SHA/tree, build-context digest, index/platform digests, port, health/readiness, Runtime ABI and Package format compatibility, and the manifest SHA256.
4. Record a receipt that binds the exact artifact bytes, build logs, UTC, and non-leakage evidence.

### Console/BFF

1. **W13:** keep BFF composition-only; same-origin session/CSRF/Origin and typed owner reads; no business-table writes.
2. **W14:** read Package, Runtime, and WebUI catalogs; require exact immutable refs; show owner-reported Build progress/log/digest; UI must not compute a digest or infer success.
3. **W16:** read Workspace/Quote/Deploy/Access/Model/Secret references; do not write deployment/current selection from the UI; API keys only through the secure form/Secret ref boundary.
4. **W26:** add browser coverage for the real BFF → owner → local provider/runtime → readback path, including refusal and retry vectors.

## Acceptance criteria

- [ ] A real Cloud WebUI OCI index digest and both platform digests are present; no placeholder or mutable tag is admitted.
- [ ] Artifact smoke/readiness proves `3000` and `/healthz` from the built bytes.
- [ ] Runtime ABI and Package format compatibility are declared and match the exact owner refs used by Build.
- [ ] Build records all three exact inputs and returns an owner-generated immutable OCI digest.
- [ ] Console covers loading, empty, error, permission, expired, retry, refresh and success states; narrow viewport, keyboard and focus behavior are verified.
- [ ] API keys are absent from localStorage, caches, OCI layers, logs, errors and receipts; only Secret refs cross the owner boundary.
- [ ] Workspace/Quote/Deploy values are read back from their owners; Console/BFF does not write business tables.
- [ ] `npm run verify:local:full` and the focused browser suite pass on the same source/artifact provenance.
- [ ] A new receipt records tested source SHA/tree, artifact digest, dependency SHAs, browser scenarios, log SHA256s, exit codes, and all remaining gaps.

## Explicit non-goals

- Do not treat the desktop DMG as a Runtime Release.
- Do not adapt OMA/DSH internals inside Cloud.
- Do not accept `:latest`, `:stable`, or any mutable tag as an artifact identity.
- Do not add a fallback/compatibility shim when an upstream input is missing.
- Do not add a second owner writer, cross-owner database join, wallet, package registry, or Secret store.
- Do not deploy to Instance, publish a Product Release, purchase/delete provider resources, or charge real money as part of this issue.

## Dependencies and blockers

- OMA/Foundry owner: a qualified AgentVersion with immutable Package content identity.
- OPL App/Framework owner: an approved server/headless Runtime Release OCI with ABI, entrypoint, readiness/access, model and Secret contract.
- Cloud artifact implementation: the actual independent WebUI carrier/build context; the current repository has no admitted Cloud WebUI OCI digest.
- W09 Build, W15 Workspace, W11 Fabric, W17 Serve and W26 owner readback must be available before claiming an end-to-end result.

## Evidence to attach before closing

- `docs/evidence/source-checks/2026-09-26-cloud-webui-artifact-blocked-receipt.json` for the current blocked state.
- A replacement admitted artifact manifest and owner receipt with real digests.
- Focused browser receipt and `npm run verify:local:full` receipt.
- No credentials, API keys, customer data or private addresses in the issue, receipt or logs.

## Related evidence

- `packages/contracts/opl-cloud-webui-artifact-contract.json`
- `docs/implementation/agent-package-runtime-webui-chain.md`
- `docs/evidence/source-checks/2026-09-26-agent-package-runtime-webui-chain-integration.json`
- `docs/evidence/source-checks/2026-09-26-oma-package-runtime-webui-intake-audit.json`
- `docs/evidence/source-checks/fixtures/webui-artifact-development-fixture.json`
