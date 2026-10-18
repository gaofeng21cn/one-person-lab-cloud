# Provider Boundary Placement (#872)

Owner: `opl-cloud` (producer, qualification contract and release consumer) with
`opl-instance-medopl` (provider access, runner and artifact custody)
Question: which repository hosts the protected provider boundary and the
workflow that executes it, who owns the receipt and the customer credential once
it runs there, and what happens to the Cloud-hosted producer entry?
State: decision record; no protected run, qualification receipt or Release is
claimed
Observed: 2026-10-18 at `fb4d885f` (`docs/provider-boundary-placement`, base
`origin/main`); Instance citations read at `6deabc5` (`origin/main`)

This is the placement decision for the producer that
[#872](https://github.com/gaofeng21cn/one-person-lab-cloud/issues/872) requires.
It extends [Product release operations](../runtime/release.md#protected-cloud-business-qualification)
and the [Instance business-path retirement inventory](./instance-business-retirement-inventory.md);
it is not another architecture overview and it does not restate the receipt
contract. It decides one thing: where the protected provider boundary executes.
The product and the business assertions are unchanged by this document, and the
business-qualification receipt keeps its schema and meaning. The
`customer_reads` receipt keeps its field set but two schema-validated producer
fields change meaning: `producer.repository` and `producer.workflowPath` name the
Instance lane instead of the Cloud workflow
(`tools/cloud-business-readback.ts:26-34`, `:96`), so its `schemaVersion` moves
from 2 to 3 (`:26`, `:88`, `:168`) and the tool, its tests and the Release
consumer are scheduled in steps 6-7. The receipt is already at version 2 because
#954 added the usage and fee-record checks to the schema-validated check set, so
this migration is the second bump rather than the first. Line numbers here and
below predate #954 and shift with it. What this placement changes on the Cloud
side is therefore the producer identity recorded in the distribution contract,
the boundary validator that asserts it, the customer-readback tool, and the
Release consumer that reads all three. §4 names the single owner of each and §7
schedules the edits; this document does not become a second writer for any of
them.

## The Gap In Current Source

The producer exists and is wired. The one module it loads at runtime does not
exist in either repository.

- The producer takes `--boundary` as a required option (listed in `RUN_OPTIONS`
  at `tools/protected-business-qualification.ts:123`; its requiredness is
  enforced at `:677-680`, `:683-685`) and requires it to be an **absolute**
  filesystem path (`:411`). A missing module is an external
  prerequisite (`:410`, `:414-418`); a present module that cannot be loaded, does
  not export the factory or returns a malformed boundary is invalid
  (`:419-437`). The factory name is `createTencentTkeProviderBoundary`
  (`PROTECTED_BOUNDARY_FACTORY`, `:80-83`), and the producer falls back to a
  default export only after that name (`:425`).
- The boundary shape is three reachability operations bound to a provider
  identity — `establishReachability`, `releaseReachability`, `readProviderHealth`
  (`tools/workspace-qualification-tencent-tke.ts:46-48`, `:126-141`).
  `establishReachability` returns the owner-API transport (`:85-92`, `:96-99`)
  and `readProviderHealth` "must use the provider's own readback channel rather
  than the business scope" (`:118-125`).
- Repository-wide search at the observed revision finds the factory name only in
  the producer's own declaration (`tools/protected-business-qualification.ts:83`)
  and in test fixtures that write a throwaway module at runtime
  (`tests/tools/protected-business-qualification.test.ts:93-102`,
  `tests/tools/cloud-business-workflow.test.ts:211`). Neither
  `gaofeng21cn/one-person-lab-cloud` nor `gaofeng21cn/opl-instance-medopl`
  contains an implementation. The fixture boundary
  (`tests/helpers/protected-qualification-fixture.ts:263-284`) is a test double
  and is not a production path.
- The Cloud workflow that drives the producer runs on `ubuntu-latest`
  (`.github/workflows/cloud-business-readback.yml:78`) inside the
  `cloud-business-qualification` environment (`:80`), and only *checks* that a
  boundary file exists (`:233`, `:239`) before passing it (`:282`). The workflow
  never creates that file, so the declared prerequisite cannot be satisfied on a
  GitHub-hosted runner by anything already in the repository.
- The provider channel is not obtainable from Cloud either. The Instance holds
  the production kubeconfig (`opl-instance-medopl .github/workflows/deployment-verify.yml:227-246`),
  Cloud's own rules name the permitted access path as Instance protected
  workflows and authorized runners and forbid this repository from dispatching
  Instance deployment (`AGENTS.md:302-309`), and #872 states the constraint
  directly: "Do not solve this by copying Instance Secrets, kubeconfig,
  private-network access or customer authority into Cloud."

Two facts make the producer's placement a decision rather than a preference.
First, the producer performs a real, bounded customer purchase
(`PROTECTED_QUALIFICATION_SCOPE`, `tools/protected-business-qualification.ts:94-100`;
buyer invocation `:619-641`), so the runtime is a money-spending path, not a
read-only probe. Second, the Cloud producer's owner-API fetcher refuses any
non-public destination (`tools/cloud-business-readback.ts:122-128`, `:138-147`),
so the Cloud customer-read lane is deliberately a public-internet vantage, not a
VPC vantage.

## 1. Decision And Rejected Alternatives

Decision: **Option B.** The provider boundary module and the protected producer
workflow live in `gaofeng21cn/opl-instance-medopl`, execute on the existing
`[self-hosted, tencent-cloud, opl-cloud, tke-vpc]` runner under the Instance
`production` environment, and run the Cloud producer from an exact-SHA Cloud
product checkout. Cloud keeps the producer source, the qualification contract,
the assertion logic, the distribution contract that records the producer
identity, and the release consumer.

| Option | Where the three operations execute | Concrete failure mode or custody problem | Verdict |
| --- | --- | --- | --- |
| A: public-HTTP-only boundary | GitHub-hosted runner in the Cloud repository | The owner-API transport and the customer login are reachable over public HTTPS, but `readProviderHealth` must read the provider's own channel (`tools/workspace-qualification-tencent-tke.ts:118-125`). A GitHub-hosted runner has no cluster access, so the operation either reads the business scope instead (a contract violation that lets provider readiness be inferred from the very purchase it is meant to check) or returns `unknown` forever (`:290-300`). Making it real means putting Instance cluster credentials or a private-network path into Cloud, which #872 forbids. It also keeps the customer credential inside a Cloud environment, i.e. the second copy that §5 rejects. | Rejected |
| C: register the Instance runner into the Cloud repository | The same self-hosted host, but selected by a Cloud workflow | The host carries the Instance production kubeconfig and production Secrets (`opl-instance-medopl .github/workflows/deployment-verify.yml:227-246`). Registering it to Cloud lets Cloud-side workflow definitions — whose change, review and dispatch rules are Cloud's — execute with those credentials, so the Instance secret boundary becomes a function of Cloud repository permissions. It also bypasses every Instance-side control: Instance requires production workflows to run from its own `main` under its own `production` environment (`opl-instance-medopl AGENTS.md:19-22`), and its workflow validator enforces Instance checkout, product-SHA and environment bindings for workflows in that repository only (`tools/validate-workflow-boundary.mjs:215-217`, `:371-480`). | Rejected |
| B: Instance-authorized self-hosted runner | `[self-hosted, tencent-cloud, opl-cloud, tke-vpc]`, inside the Instance repository's own workflow | Provider access, credentials and the runner stay with their current owner; the Cloud producer runs unmodified in its flow — its checks, its assertions and its receipt — from an exact product SHA, with only a prerequisite leaving step 3 (§6). Cost: two repositories are involved in one evidence chain, so the receipt must carry a product binding that no longer follows from the run's `head_sha` (§4). | Chosen |

Why Option B is not a duplicated implementation:

| Concern | Owner | Why it is not duplicated |
| --- | --- | --- |
| Qualification request, scope, authority, terminal reasons | Cloud | `tools/workspace-qualification-contract.ts`, `tools/workspace-qualification-orchestrator.ts`; one flow, one receipt schema |
| The purchase | Cloud | The producer delegates to `runDeployedWorkspacePurchase` (`tools/protected-business-qualification.ts:619-641`); the Instance module never buys anything |
| Provider reachability, owner transport, provider health | Instance | Exactly three operations (`tools/workspace-qualification-tencent-tke.ts:46-48`); the adapter's own comment states that an adapter able to answer business questions could invent a qualified result (`:14-18`) |
| Receipt | Cloud contract, written by the Cloud producer | The boundary receives no receipt, result slot or output path (`tools/workspace-qualification-tencent-tke.ts:118-125`; construction input is only `{deployment, environment}` at `tools/protected-business-qualification.ts:431`) |

The Instance module is a provider adapter, not a second business flow: it holds
no qualification state, no receipt field and no business assertion. The Cloud
producer still derives the Candidate digest, validates the installation
receipt/run, checks the owner origin, fixes the mutation scope and writes the
receipt (`tools/protected-business-qualification.ts:552-669`).

This placement does not move qualification ownership. `docs/decisions.md:62-78`
(the 2026-10-15 decision) owns the durable split: Instance owns deployment,
installation and its receipts; customer business operations and business
qualification stay with Cloud's product owners, with separately authorized
protected execution and exact-Candidate evidence. One sentence there —
"Cloud's existing product owners and protected product execution" (`:65-66`) —
can be read as naming the execution host. Option B changes only that host: the
same Cloud-authored producer runs on the Instance-authorized runner, which is
the only permitted path to the cluster (`AGENTS.md:302-309`). `docs/decisions.md`
and `docs/architecture.md:186-193` remain the owners of that split; step 1
amends the decisions entry and step 8 the architecture clause, and this
document does not restate the decision.

## 2. Who Runs What

| Thing | Host | Exact source and pattern |
| --- | --- | --- |
| Protected producer workflow | `gaofeng21cn/opl-instance-medopl`, new `.github/workflows/cloud-business-qualification.yml` | Same name the Cloud release consumer already pins (`tools/cloud-release-evidence.ts:42-47`); modeled on `bootstrap.yml` and `cloud-workspace-launch-readback.yml` |
| Runner | `[self-hosted, tencent-cloud, opl-cloud, tke-vpc]` | `opl-instance-medopl .github/workflows/bootstrap.yml:21`, `cloud-workspace-launch-readback.yml:66` |
| Protected environment | Instance `production` | `bootstrap.yml:23`, `qualification-decision.yml:56` |
| Boundary module | Instance `tools/tencent-tke-provider-boundary.mjs` | New; the only caller is the workflow above |
| Instance-owned tooling | Instance `.opl-instance-checkouts/<run-id>-<attempt>-<job>/` | `bootstrap.yml:123-129`, `cloud-workspace-launch-readback.yml:89-95`; the validator requires this exact run-scoped path (`tools/validate-workflow-boundary.mjs:276-298`) |
| Cloud producer, contract, adapter, buyer | Cloud at the exact product SHA in `.opl-checkouts/<run-id>-<attempt>-<job>/` | `bootstrap.yml:146-153`, `cloud-workspace-launch-readback.yml:112-119` |
| Release consumer, receipt validator, docs | Cloud | `docs/runtime/release.md`, `tools/cloud-release-evidence.ts`, `docs/roadmap.md:130` |

Product pinning and re-verification, reusing what exists (no new mechanism):

1. The dispatch carries the neutral Candidate receipt as base64. The Instance
   tool `tools/candidate-receipt-envelope.mjs admit --receipt-base64-env
   OPL_CANDIDATE_RECEIPT_B64 --output ... --github-env "$GITHUB_ENV"
   --github-output "$GITHUB_OUTPUT"` (`cloud-workspace-launch-readback.yml:102-110`)
   yields `product_sha` and `product_tree`.
2. The product is checked out by that SHA only: `repository:
   gaofeng21cn/one-person-lab-cloud`, `ref: ${{ steps.candidate.outputs.product_sha }}`,
   `fetch-depth: 1`, `persist-credentials: false`
   (`cloud-workspace-launch-readback.yml:112-119`). The Instance validator
   requires the exact product repository (`tools/validate-workflow-boundary.mjs:433-435`)
   and the exact Candidate ref with `fetch-depth: 1` (`:444-446`), and requires a
   following "Verify product revision" step (`:331-369`). It enforces
   `persist-credentials: false` only for `workspace-image-rollout.yml`'s
   qualification job (`:436-443`); for every other lane that setting is a
   convention copied from the existing workflows
   (`cloud-workspace-launch-readback.yml:118`, `bootstrap.yml:152`), not a
   validator rule.
3. The revision is proven, not assumed: `tools/exact-product-source.mjs
   --product-sha "$OPL_PRODUCT_SHA" --expected-product-tree "$OPL_PRODUCT_TREE"
   --evidence ...` pins `git rev-parse HEAD` and the tree
   (`opl-instance-medopl tools/exact-product-source.mjs:15-26`) and writes
   `{schemaVersion: 1, productSha, productTree}` mode 0600 (`:43-46`).
4. The Candidate contract is the Cloud-owned one, executed from the product
   checkout: `node tools/cloud-candidate-receipt.ts validate --receipt
   "$OPL_CANDIDATE_RECEIPT"` then `export-env`
   (`bootstrap.yml:165-171`), which emits `OPL_PRODUCT_SHA`, `OPL_PRODUCT_TREE`,
   `OPL_CLOUD_IMAGE`, `OPL_CLOUD_IMAGE_DIGEST` and the per-platform digests
   (`tools/cloud-candidate-receipt.ts:184-200`). The image digest is therefore
   read from the receipt, never from a dispatch string.
5. The producer is invoked from the product checkout, exactly as today, with one
   changed argument: `--boundary` names
   `${{ github.workspace }}/.opl-instance-checkouts/<run-id>-<attempt>-<job>/tools/tencent-tke-provider-boundary.mjs`.
   The producer re-derives the Candidate digest from the bytes and refuses a
   mismatched declaration (`tools/protected-business-qualification.ts:263-277`).
6. The installation evidence is the Instance's own receipt: the run uploads the
   `opl-cloud-deployment-verification-<installation_run_id>` artifact
   (`opl-instance-medopl .github/workflows/deployment-verify.yml:464-472`), and
   the protected lane downloads it plus the same run's JSON from
   `gh api repos/$GITHUB_REPOSITORY/actions/runs/<id>`.
   The producer binds both to the Candidate through the existing validator
   (`tools/protected-business-qualification.ts:311-330`;
   `tools/cloud-business-readback.ts:65-75`).

The protected inputs move with the lane and are renamed at the step boundary,
not copied: `vars.OPL_CONSOLE_DOMAIN` (already an Instance variable,
`cloud-workspace-launch-readback.yml:71`) supplies the owner origin, and the
designated customer credential is aliased from the Instance's single Secret
owner (§5). The J0 READY receipt stays a declared dispatch input materialized to
a mode-0600 file under `$RUNNER_TEMP`
(`.github/workflows/cloud-business-readback.yml:250-259`). Cloud fabric now owns
the producing code (`tools/workspace-qualification-j0-ready.ts`, #952); a real
sandbox run that emits the receipt is still outstanding.

Artifacts, uploaded by the Instance run:

| Artifact | Contents | Retention | Consumer |
| --- | --- | --- | --- |
| `cloud-business-qualification` | `business-qualification.json` (the contract's receipt name), `product-source.json`, and `business-qualification.purchase.json` when the buyer reached a receipt | 30 days, matching today's Cloud upload (`.github/workflows/cloud-business-readback.yml:295`, `:304`) and the consumer's 720-hour window (`tools/cloud-release-evidence.ts:54`) | Cloud Release admission (§4) |
| `cloud-customer-readback-<run_id>-<run_attempt>` (after §6) | `receipt.json` | 30 days, matching today's Cloud upload (`.github/workflows/cloud-business-readback.yml:295`) | Cloud Release admission (§4) |

The business artifact name and receipt file are not chosen here: they are the
`cloudBusinessQualification` values in
`packages/contracts/opl-cloud-distribution-contract.json:83-99`, and §4 says why
the contract wins over the two conflicting writers. The customer-readback
artifact name stays run-attempt-suffixed because the release workflow's resolver
owns it (`.github/workflows/release-opl-cloud-image.yml:146`), not the contract.
The artifact's file set is exact and conditional: `business-qualification.json`
and `product-source.json` in both cases, plus
`business-qualification.purchase.json` when the buyer reached a receipt. The
purchase sibling is derived from `--output` by `purchaseReceiptPath`
(`tools/protected-business-qualification.ts:539-543`, called at `:651`), so
naming the output `business-qualification.json` yields that sibling. §4's `:533`
assertion therefore accepts exactly
`{business-qualification.json, product-source.json}` or
`{business-qualification.json, product-source.json,
business-qualification.purchase.json}` and nothing else. The producer's
`--output` names `business-qualification.json` in the artifact root, so the
downloaded path is `evidence/business/business-qualification.json` for the
consumer at `.github/workflows/release-opl-cloud-image.yml:524`.

Instance receipt policy still applies: produced in the same run as the action,
redacted, never overwritten (`opl-instance-medopl receipts/README.md:10-16`,
`:28-31`). Neither repository stores the transient evidence directories; the run
deletes them at the end, as both current lanes do
(`.github/workflows/cloud-business-readback.yml:306-317`,
`cloud-workspace-launch-readback.yml:179-203`).

## 3. The Boundary Module Contract

The module exports exactly one factory, by the name the producer requires
(`tools/protected-business-qualification.ts:80-83`). It may also be the module's
default export, but the named export is the contract.

```ts
export function createTencentTkeProviderBoundary(input: {
  deployment: TencentTkeDeployment;                                  // frozen, normalized
  environment: Readonly<Record<string, string | undefined>>;         // the protected step environment
}): TencentTkeProviderBoundary | Promise<TencentTkeProviderBoundary>;
```

`TencentTkeDeployment` is `{ deploymentMode: "managed_tke" | "platform_owned";
fabricProvider: "tencent-tke"; candidateReceiptDigest; clusterId; region;
namespace; ownerOrigin }`, already validated and frozen by
`tencentTkeDeployment` before the factory is called
(`tools/workspace-qualification-tencent-tke.ts:64-72`, `:178-205`). The factory
receives no request, no operation key, no receipt and no output path.

| Operation | Receives | May read | Must return | Must not do |
| --- | --- | --- | --- | --- |
| `establishReachability` | `{deployment, request, operationKey}` | The deployment's provider identity; the protected environment (customer credential, any runner-local kubeconfig path the step exported); the provider's reachability state | `{state: "open", provider, clusterId, namespace, transport}` where `transport = {origin, http: {json, request}, auth}` and `origin` equals `deployment.ownerOrigin` exactly, or `{state: "absent" \| "unknown", reason, ...identity}` | Read or write a purchase, charge, wallet, Workspace, Runtime or Key fact; it has no owner-API call of its own beyond establishing the session |
| `releaseReachability` | `{deployment, request, operationKey}` | Only what the operation key opened | `{state: "released" \| "absent" \| "unknown", ...identity}` | Delete, scale or repair provider resources; the close is a reachability release, not a resource mutation |
| `readProviderHealth` | `{deployment, request}` | The provider's own readiness channel for that cluster/namespace (for example namespace and workload readback through the prepared kubeconfig) | `{state: "ready" \| "degraded" \| "absent" \| "unknown", ready, serviceReady, failedChecks, ...identity}` | Use the business scope, the owner API or the returned transport as its health source (`tools/workspace-qualification-tencent-tke.ts:118-125`) |

Rules the adapter enforces on the module's answers, so the module cannot widen
its own authority:

- Every readback must repeat `provider: "tencent-tke"`, the exact `clusterId`
  and the exact `namespace`; another deployment's answer is refused, not adopted
  (`tools/workspace-qualification-tencent-tke.ts:257-265`).
- The transport must be bound to `deployment.ownerOrigin`, must expose both
  `http.json` and `http.request`, and must carry a non-empty `auth`
  (`:270-279`). Returning the boundary's own transport is what keeps the adapter
  from synthesizing an owner payload or a session.
- `establishReachability` is recorded as attempted before it is called, so a
  lost or malformed answer still produces a release (`:331-350`, `:363-383`).
- Provider health never moves the qualification result; every unreadable answer
  is `unknown` (`:281-300`, `:385-395`).

What the module must not be able to do:

- Write a receipt, a result or a summary. The producer writes both artifacts
  (`tools/protected-business-qualification.ts:545-550`, `:643-657`) after the
  orchestrator returns, and the boundary is constructed with `{deployment,
  environment}` only (`:431`).
- Touch the business Ledger, the purchase path, the wallet, the Workspace or the
  managed Key. Those reads and the one purchase belong to the Cloud buyer and
  orchestrator; the boundary is not given their state and cannot produce it.
- Persist or log the customer session, the password or the kubeconfig. The
  transport lives inside the process, and the receipt has no field that could
  carry a credential: the contract closes every receipt object to its declared
  keys (`tools/workspace-qualification-contract.ts`), and the release consumer
  re-validates the whole body on read (`tools/cloud-release-evidence.ts:146-156`).
- Read `request` as a business fact. `request` is the sealed qualification
  request, passed for identity only; the operation key is the contract's own
  derivation (`tools/workspace-qualification-tencent-tke.ts:235-253`).
- Import Cloud code. The module lives in the Instance repository, which has no
  dependency on the Cloud checkout; if it needs the transport or readback
  shapes it declares them locally. This is what keeps the two write sets
  reviewable by their own owners.

The module reads only the environment it is given, and the workflow exports only
the protected inputs into that step (`tools/protected-business-qualification.ts:74-78`):
`OPL_QUALIFICATION_ORIGIN`, `OPL_QUALIFICATION_CUSTOMER_EMAIL`,
`OPL_QUALIFICATION_CUSTOMER_PASSWORD`, the boundary path and, for the health
read, the prepared kubeconfig path. `OPL_INSTANCE_EVIDENCE_TOKEN` is not part of
this contract: the producer requires it today only as an external-prerequisite
declaration and never reads it (`:210-212`, `:222`), and inside the Instance run
there is no cross-repository evidence read to authorize.

## 4. Receipt Ownership And Custody

The receipt is Cloud-owned and Instance-executed. That sentence is the whole
split, and both halves are load-bearing:

- Cloud owns the schema and the meaning: `opl.workspace-qualification.purchase.v1`
  and its uncovered-scope declaration live in
  `tools/workspace-qualification-contract.ts:87`; the terminal `settled` state is
  decided by the orchestrator (`tools/workspace-qualification-orchestrator.ts:631-634`);
  the release-side meaning
  (`settled`, Candidate-bound, 30-day freshness) lives in
  `tools/cloud-release-evidence.ts:54-57`, `:117-134`.
- Instance owns the execution and the custody of the artifact: the run happens on
  the Instance runner, under the Instance `production` environment, and the
  artifact is uploaded to the Instance run. The provider evidence the run touches
  (kubeconfig, cluster identity) never leaves that boundary.

What proves the receipt came from that runner and that product SHA:

| Proof | Where it comes from |
| --- | --- |
| Run identity: repository `gaofeng21cn/opl-instance-medopl`, path `.github/workflows/cloud-business-qualification.yml`, `workflow_dispatch`, `head_branch: main`, completed and successful, id/attempt matching the declaration | The same run-readback shape already used at `.github/workflows/cloud-business-readback.yml:119-129` and validated at `tools/cloud-release-evidence.ts:175-184` |
| Runner identity: the producing job reports the `self-hosted`, `tencent-cloud`, `opl-cloud` and `tke-vpc` labels, so a GitHub-hosted run cannot produce this artifact | The Actions jobs API exposes each job's runner labels ([workflow jobs](https://docs.github.com/en/rest/actions/workflow-jobs#list-jobs-for-a-workflow-run)); the label set is the one the Instance workflows declare (`bootstrap.yml:21`) |
| Product binding: the receipt's `candidate.productSha`/`productTree` and `source.sha`/`source.tree` equal the released Candidate, and the run's `product-source.json` (`{schemaVersion: 1, productSha, productTree}`) equals the same values | Receipt fields checked at `tools/cloud-release-evidence.ts:163-170`; source evidence written by `opl-instance-medopl tools/exact-product-source.mjs:43-46` |
| Receipt integrity: the producer's `receiptDigest` over the exact emitted bytes, and the consumer's digest over the exact downloaded bytes | `tools/protected-business-qualification.ts:662`; `tools/cloud-release-evidence.ts:194-200` |
| Currency: `settled` with `reason: "none"`, within the 720-hour window, no future timestamp | `tools/cloud-release-evidence.ts:159-161`, `:186-192` |

The producer identity already has one machine-readable owner, and this placement
does not get a second one. `packages/contracts/opl-cloud-distribution-contract.json`
`distribution.publication.cloudBusinessQualification` (`:83-99`) owns the
producer's `owner`, `workflow`, `environment`, `artifact` and `receipt`;
`tools/validate-product-boundary.mjs:265-275` asserts that block and
`tests/contracts/product-distribution.test.ts:107-117` repeats the same values.
`tools/cloud-release-evidence.ts:42-47` mirrors the workflow, artifact and
receipt names in the consumer. Under Option B only two fields move: `owner`
becomes `gaofeng21cn/opl-instance-medopl` and `environment` becomes `production`.
The workflow path, the artifact `cloud-business-qualification` and the receipt
`business-qualification.json` stay, because they describe the evidence, not the
host. That edit is a contract revision, so `schemaVersion`
(`packages/contracts/opl-cloud-distribution-contract.json:2`, asserted at
`tools/validate-product-boundary.mjs:236` and
`tests/contracts/product-distribution.test.ts:96`) is bumped with it.

Which name wins: the contract wins. The artifact is exactly
`cloud-business-qualification` and the receipt file is exactly
`business-qualification.json`. Today the Cloud producer uploads
`cloud-business-qualification-<run_id>-<run_attempt>` containing `receipt.json`
(`.github/workflows/cloud-business-readback.yml:301-302`), and
`docs/runtime/release.md:197-199` documents that name, while the Release
consumer already requires the contract names
(`.github/workflows/release-opl-cloud-image.yml:159`, `:274`, `:524`). The
producer and the release page are the stale writers: the Instance lane uploads
the contract's exact name and receipt file, and step 7 rewrites
`docs/runtime/release.md`. The release workflow's "exactly one file" assertion
(`.github/workflows/release-opl-cloud-image.yml:533`) becomes an exact file-set
assertion: `business-qualification.json` and `product-source.json` always, plus
`business-qualification.purchase.json` when the buyer reached a receipt, and
nothing else (§2).

The consumer change is not one binding. Option B does not make Cloud the
deployment authority, but it changes at least five things:

- The producer identity in the distribution contract, its validator assertion
  and its contract test, described above.
- The producer run and artifact resolution in the release workflow: today it
  reads `business-run.json` and `business-artifacts.json` from
  `$PRODUCT_REPOSITORY` with `$CLOUD_TOKEN`
  (`.github/workflows/release-opl-cloud-image.yml:110`, `:166`) and validates the
  run against that repository (`:150`); both must read from
  `$INSTANCE_REPOSITORY` with `$INSTANCE_TOKEN`.
- The business download step: today it has no `repository` and uses
  `${{ github.token }}` (`:271-277`); it must use the Instance repository and
  `${{ secrets.OPL_INSTANCE_EVIDENCE_TOKEN }}`, exactly as the three Instance
  downloads already do (`:279-304`).
- The artifact and receipt names: the Instance lane must upload the contract's
  names, and the current producer upload and the release page are corrected or
  retired.
- The product binding in `tools/cloud-release-evidence.ts`:
  `validateReleaseBusinessQualification` requires
  `run.head_sha === identity.productSha` (`:182`), which can never hold for an
  Instance-hosted run because that `head_sha` is the Instance `main` commit. It
  is replaced by the `product-source.json` binding above, checked from the same
  artifact and same run, while `run.head_sha` is required only to be a 40-hex
  Instance `main` commit. Keeping the equality "for safety" would make every
  future production run fail; dropping the product binding entirely would let a
  run that checked out a different product SHA emit an admissible receipt.

`tools/validate-product-boundary.mjs` is the gate that makes the second, third
and fourth items executable. It requires the release `evidence` step to contain
`actions/runs/$CLOUD_BUSINESS_QUALIFICATION_RUN_ID`, `business-run.json`,
`".github/workflows/cloud-business-qualification.yml"`,
`business_artifact="cloud-business-qualification"` and the matching
`require_artifact` line (`:72-80`), and requires the business download step to
have `name: cloud-business-qualification`, `path: evidence/business`,
`run-id: ${{ inputs.cloud_business_qualification_run_id }}`,
`repository === undefined` and `github-token === ${{ github.token }}`
(`:81-89`). The name and receipt assertions stay; the repository and token
assertions invert. Step 4 schedules all of this as one reviewable change.

How Cloud consumes it without becoming the deployment authority: the Release
admission downloads the artifact by run ID from the Instance repository with
`OPL_INSTANCE_EVIDENCE_TOKEN` and validates it
(`.github/workflows/release-opl-cloud-image.yml:271-277` is the business download
step that gains that repository and token; `:279-304` already downloads the
three Instance evidence classes this way). Cloud never
dispatches `deployment-verify.yml`, never holds the kubeconfig, and never calls a
provider operation. The release promotes bytes that were qualified; it does not
qualify them (`docs/runtime/release.md:139-210`).

The split, stated once:

| Cloud owns | Instance owns |
| --- | --- |
| Product source, qualification contract, assertion logic, buyer, terminal reasons, receipt schema | Provider reachability, owner transport, provider health readback, cluster and namespace identity |
| The Candidate contract and its digest derivation (`tools/cloud-candidate-receipt.ts:55-88`) | The exact product checkout and its source evidence (`tools/exact-product-source.mjs`) |
| Release admission and the evidence classes (`tools/cloud-release-evidence.ts:30-33`) | The protected run, the `production` environment approval and the artifact |
| The decision whether a receipt satisfies a release | Deployment, rollback, health and read-only installed-state observation |

## 5. Credential Custody And SSOT

Option B fixes the custody model: **the customer credential stays solely in the
Instance `production` environment, under the single credential-owner pair
`OPL_TENCENT_MVP_CUSTOMER_EMAIL` / `OPL_TENCENT_MVP_CUSTOMER_PASSWORD`. Cloud
holds no copy for this lane.**

Three facts make that the only consistent answer.

1. One credential owner is already a decided Instance invariant, not a
   preference. `opl-instance-medopl docs/runtime/operator-workflows.md:124`
   states that the designated acceptance customer has exactly one credential
   owner in the protected environment and that a second Secret copy was **not**
   retained, because a copy cannot observe rotation of the other and will keep
   submitting a stale password, surfacing as a stable read-stage failure code
   instead of an identity error. That invariant is enforced by
   `tests/designated-customer-credential-owner.test.mjs:7-22`, `:33-65`, and the
   consuming lanes alias the same Secrets rather than storing their own
   (`workspace-special-chain-acceptance.yml:92-97`,
   `package-build-delivery-readback.yml:51-54`,
   `workspace-billing-settlement-probe.yml:34-37`).
2. Under Option B the credential must be present **in the Instance run**. Cloud
   cannot inject a Secret into an Instance workflow run: a Secret held by a Cloud
   environment is not visible to a run in another repository, and passing it
   through dispatch inputs would put the credential into a recorded run payload,
   which the Instance rule already forbids for these lanes
   (`tests/designated-customer-credential-owner.test.mjs:51-65`: every
   account-named input must be a scoped digest).
3. Cloud's current shape is a second copy in the making. The Cloud workflow
   expects `OPL_QUALIFICATION_CUSTOMER_EMAIL` / `OPL_QUALIFICATION_CUSTOMER_PASSWORD`
   in the `cloud-business-qualification` environment
   (`.github/workflows/cloud-business-readback.yml:196-197`, `:215-217`,
   `:267-269`; `docs/runtime/release.md:119-123`), and the producer hands that
   environment to the boundary, which is what actually authenticates
   (`tools/protected-business-qualification.ts:208-213`, `:609-612`). Those
   Secrets do not exist yet (`docs/status.md:56`). This decision exists so the
   copy is never created.

One decision, not an open question: the qualification purchase spends the
designated MVP customer account's real money. The single-owner custody model
above is what makes the charge attributable to the account the acceptance lanes
already read; it does not make the spend unbounded. The purchase stays inside
`PROTECTED_QUALIFICATION_SCOPE`
(`tools/protected-business-qualification.ts:94-100`), and §7 step 5 still
requires the Instance `production` approval, the Cloud product owner's
declaration and a J0 READY receipt for the exact Candidate, produced by the
fabric sandbox qualification. The
owner's approval of the spend is recorded in `docs/decisions.md:62-78` by step
1, which lands before every execution step; that entry, not this document, is
the authorization the later steps cite.

Consequently:

- The Instance lane aliases the single owner into the producer's expected names
  at the step: `OPL_QUALIFICATION_CUSTOMER_EMAIL:
  ${{ secrets.OPL_TENCENT_MVP_CUSTOMER_EMAIL }}` and the password pair, exactly
  as the three existing lanes do. The stored Secret count stays one pair, and
  rotation stays one action (`docs/runtime/operator-workflows.md:124`).
- The Cloud `cloud-business-qualification` environment and its
  `OPL_QUALIFICATION_*` variables are never created for this lane. If the
  environment already exists it is deleted after §6, in an owner-authorized
  change, and `docs/runtime/release.md:119-123` is rewritten in the same PR.
- A "dedicated Cloud qualification account that is not the MVP customer account"
  is rejected as the custody model. It would not remove the need for an
  Instance-side credential: the account would still have to be stored in the
  Instance `production` environment (a second designated customer with its own
  wallet and its own rotation), and it would make the qualified purchase belong
  to an account other than the designated acceptance customer whose Workspace
  the Instance lanes already read
  (`opl-instance-medopl .github/workflows/package-build-delivery-readback.yml:51-54`,
  `docs/runtime/operator-workflows.md:123-124`). It buys
  no custody separation that §5's chosen model lacks.

What Cloud still needs under Option B, and what it no longer needs:

| Cloud keeps | Cloud no longer needs for this evidence |
| --- | --- |
| The producer source, the qualification contract, the orchestrator, the adapter, the buyer | Any customer credential for the qualification run |
| The release consumer and its evidence classes (`tools/cloud-release-evidence.ts`) | A protected environment (`cloud-business-qualification`), reviewer configuration of its own, or the `OPL_QUALIFICATION_PROVIDER_BOUNDARY` variable |
| `OPL_INSTANCE_EVIDENCE_TOKEN` with `Actions: read` (and `Contents: read` only where a validator checkout still needs it, `.github/workflows/cloud-business-readback.yml:145-152`) | Kubeconfig, cluster identity, private-network access, or any provider client |

What verifies the receipt instead of a Cloud environment is the Release admission
consumer described in §4: Instance run identity, runner labels, the
`product-source.json` product binding, the receipt digest and the freshness
window. Cloud's environment protection is replaced by the Instance `production`
environment's protection plus those bindings; this document does not weaken the
Release admission's own protection, which it does not change.

## 6. What Happens To `cloud-business-readback.yml`

Recommendation: **retire it as a producer and then retire the file.** The
Instance-hosted lane becomes the single protected producer for both
`customer_reads` and `business_qualification`, and the Cloud Release admission
becomes the single consumer.

Justification against the existing boundary rules:

- The file's two jobs and its protected environment are what force a second
  credential copy (§5). Retiring it is the only way to reach one credential
  owner without keeping two.
- Both producers share one account, one Candidate and the same installation
  evidence. Splitting them across two repositories makes one evidence chain with
  two custody models; keeping them together keeps one.
- `customer_reads` needs no provider access — its tool is public-HTTPS-only by
  construction (`tools/cloud-business-readback.ts:129-136`, `:122-128`) — so it
  can run on the same runner without gaining any provider capability, and its
  public-address refusal still governs the read regardless of host. The Instance
  already runs customer-session readbacks against the public Console origin —
  `OPL_CONSOLE_ORIGIN: https://${{ vars.OPL_CONSOLE_DOMAIN || 'cloud.medopl.com' }}`
  (`opl-instance-medopl .github/workflows/console-customer-workspace-readback.yml:34`,
  `package-build-delivery-readback.yml:47`) — authenticated as that same
  designated account.
- A verifier-only Cloud workflow is rejected. It would need the same
  `OPL_INSTANCE_EVIDENCE_TOKEN`, would duplicate the Release admission check
  without adding authority, and would be a second consumer of one artifact with
  no distinct caller. `docs/status.md:131-144` records that the #928 release
  consumer has not yet bound this producer and that no protected receipt exists,
  which is the gap this section closes.

What replaces each part:

| Current in `cloud-business-readback.yml` | Replacement |
| --- | --- |
| `admission` job and the `cloud-business-qualification` environment checks (`:47-73`) | The Instance workflow's own guard (`github.repository`, `github.ref == 'refs/heads/main'`, actor equals triggering actor) plus a readback of the Instance `production` environment protection before the job runs |
| Cloud checkout at `github.sha` (`:95-99`) | Product checkout at the admitted Candidate SHA, `.opl-checkouts/<run-id>-<attempt>-<job>` (`bootstrap.yml:146-172`) |
| Candidate/installation provenance resolution (`:106-143`) | The same values from the Instance run itself: admitted Candidate envelope, `exact-product-source.mjs`, and the `deployment-verify.yml` artifact downloaded from this repository |
| Customer product reads (`:193-206`) | Instance workflow `cloud-customer-readback.yml` (step 6), running the Cloud-owned `tools/cloud-business-readback.ts` from the product checkout with the admitted producer passed in; the tool and the Release consumer change in steps 6-7 (§6.1) |
| Declaration materialization and producer run (`:208-286`) | Same steps in the Instance workflow, with `--boundary` naming the Instance module |
| Artifact uploads (`:288-304`) | Instance artifact uploads (§2) |
| Private-input cleanup (`:306-317`) | The Instance cleanup pattern (`cloud-workspace-launch-readback.yml:179-203`) |

### The customer-reads harness must move with the lane

`tools/cloud-business-readback.ts` is Cloud-owned, but it hard-codes the producer
it accepts. `CLOUD` (`:10`) and `WORKFLOW` (`:12`) are compared to the receipt at
`:96` (`p.repository !== CLOUD || p.workflowPath !== WORKFLOW`), passed as the
expected producer authority at `:111`, and `:166` builds the producer from
`env.GITHUB_REPOSITORY` plus `WORKFLOW` before `:169` validates the blocked
receipt and before any network request. The CLI accepts exactly four options and
a fixed argument count (`:242-245`). Inside an Instance workflow
`GITHUB_REPOSITORY` is the Instance repository, so the receipt validation at
`:169` fails with `cloud_business_readback_invalid` before any network request;
"the same tool from the product checkout" is not executable until the
Cloud-owned change below lands.

Decision: keep the lane on the Instance self-hosted runner and schedule the
Cloud-owned change. The single-credential-owner invariant
(`docs/runtime/operator-workflows.md:123-124`, enforced by the Instance
`tests/designated-customer-credential-owner.test.mjs`) is the stronger property
and stays intact; Cloud keeps ownership of the receipt schema and of the
qualification judgment; only the execution host moves. Keeping the lane in Cloud
is rejected because it forces the customer credential into Cloud and breaks that
single owner.

The concrete Cloud change, in step 6:

- `producer.repository` is the running lane's repository
  (`env.GITHUB_REPOSITORY`, already read at `:166`), and `producer.workflowPath`
  comes from the lane's declared workflow path instead of the `WORKFLOW`
  constant at `:12`. `:96` and `:111` compare both fields against the passed
  expected producer instead of the `CLOUD`/`WORKFLOW` constants at `:10`/`:12`.
- `validateCloudBusinessReadbackReceipt` (`:82`) and
  `validateCloudBusinessReadbackBinding` (`:105-118`) take the expected producer
  as a parameter, so a Cloud receipt is still accepted where it is the producer
  and an Instance receipt is accepted by the same code.
- The receipt `schemaVersion` becomes 3 (`:26`, `:88`, `:168`) because two
  schema-validated fields change meaning; it is version 2 today after #954, and
  the shape and every other field stay.
- The CLI at `:242-245` gains the producer-workflow option and replaces the
  fixed `args.length!==9` check with the option-pair count.
- `tests/tools/cloud-business-readback.test.ts` moves its fixtures (`:18-20`) to
  the Instance producer and adds the wrong-repository and wrong-workflow-path
  refusals beside the existing negative matrix (`:86-88`).

The Release consumer changes in step 7: the `customer-authority` checkout
(`.github/workflows/release-opl-cloud-image.yml:205-210`) becomes the **Instance**
repository at `steps.evidence.outputs.customer_authority_sha` with
`secrets.OPL_INSTANCE_EVIDENCE_TOKEN` instead of the Cloud repository, because
that output is now an Instance run's `head_sha`; `CUSTOMER_VALIDATOR` at `:427`
moves to the product-source checkout at `inputs.ref`
(`.github/workflows/release-opl-cloud-image.yml:197-203`), since the Instance
checkout does not contain the Cloud tool; the assertion at `:432` still compares
`customer-authority`'s `HEAD` with `EXPECTED_CUSTOMER_SHA` and therefore still
binds the exact Instance revision; the one-file check at `:433` is unchanged.
The binding call at `:439-445` already receives the producer's
`customer-run.json` (`:444`), so the expected repository and workflow path come
from that admitted run rather than from new inputs.

Cloud still validates and still decides. The Release admission runs
`validateCloudBusinessReadbackBinding` on the downloaded receipt before using
it, accepts only `customer_reads_verified`, and still owns the qualification
judgment; the receipt itself asserts `fullBusinessQualification: false`,
`workspaceLifecycleVerified: false` and `keyLifecycleVerified: false` (`:26-34`).
Relocating the harness gives the Instance lane no qualification authority: it
performs the reads and writes the receipt; Cloud validates the receipt and
admits or refuses the Release. `readProviderHealth` and every provider or
cluster concern stay on the Instance self-hosted runner in the qualification
lane (steps 2 and 5), not in this public-HTTPS-only lane (`:122-136`).

The file is not deleted before both replacements have run for a real Candidate:
today nothing else produces the `customer_reads` receipt, and the Release
consumer requires a successful `cloud-business-readback.yml` run on `main`
(`.github/workflows/release-opl-cloud-image.yml:142-147`, `:177`).

## 7. Ordered Implementation Plan

Each step is one PR unless two are explicitly bundled. Step 5, the dispatch in
step 6 and step 7 touch the cluster or production and are marked
owner-authorized.

**Step 1 — Record the decision (Cloud, docs only).**
Files: `docs/decisions.md:62-78` (the 2026-10-15 decision paragraph) gains the
durable record of two things: the execution-host reading (its `:65-66` clause
names the Instance-authorized runner while Cloud keeps qualification ownership)
and the owner-approved spend on the designated MVP customer account, bounded by
`PROTECTED_QUALIFICATION_SCOPE`
(`tools/protected-business-qualification.ts:94-100`); and this document, the
reviewable rationale for that entry. The entry lands before step 2, so every
execution step below runs under a recorded decision.
Verification: the diff selects no behavior targets, so no test evidence is
claimed; every `path:line` citation in this document was opened and checked
against the file and line it names at the observed revisions (Cloud
`fb4d885f`, Instance `6deabc5`). Authorization: none. The `docs/README.md` index
line lands in step 8, with the state change, so the index never points at a
decision that is not yet current.

**Step 2 — Instance: boundary module and producer lane (no dispatch).**
Repository: `gaofeng21cn/opl-instance-medopl`.
Files: new `.github/workflows/cloud-business-qualification.yml`; new
`tools/tencent-tke-provider-boundary.mjs`; `tools/validate-workflow-boundary.mjs`
(register the file in `scenarioWorkflowFiles:6-13`, its exact inputs in
`dispatchInputsByFile:58-99`, and the module in `instanceToolNames:100-153`);
`tests/designated-customer-credential-owner.test.mjs` (add the new lane to
`CUSTOMER_SESSION_LANES`, `:18-21`; the duplicate-name guard at `:17` only
forbids `OPL_SPECIAL_CHAIN_ACCOUNT_*`, so a new `OPL_QUALIFICATION_*` alias would
otherwise be unpoliced); new `tests/cloud-business-qualification-workflow.test.mjs`;
new `tests/tencent-tke-provider-boundary.test.mjs`.
The workflow: Instance `main` guard, `[self-hosted, tencent-cloud, opl-cloud,
tke-vpc]`, `environment: production`, `confirmation` input, run-scoped Instance
and product checkouts, Candidate admission, `exact-product-source.mjs`,
`cloud-candidate-receipt.ts validate`/`export-env`, `deployment-verify.yml`
artifact download, declaration materialization, producer run that writes
`business-qualification.json`, artifact upload under the contract's exact
`cloud-business-qualification` name, cleanup. The module: the three operations
over the prepared kubeconfig and the owner origin, no imports from the Cloud
checkout.
Verification: `npm run validate` (`opl-instance-medopl package.json:7`: the
profile, workflow-boundary and receipt-redaction validators) and `node --test
tests/cloud-business-qualification-workflow.test.mjs
tests/tencent-tke-provider-boundary.test.mjs`; the workflow test asserts the
runner labels, environment, exact checkout refs, artifact name and cleanup, and
the module test drives the three operations against a local fixture with no
network.
Authorization: none. Nothing is dispatched and the module opens no connection in
its tests.

**Step 3 — Cloud: producer prerequisites for the Instance-hosted context.**
Repository: `gaofeng21cn/one-person-lab-cloud`.
Files: `tools/protected-business-qualification.ts` (drop the
`OPL_INSTANCE_EVIDENCE_TOKEN` prerequisite and its comment, `:210-212`, `:222`;
keep origin, email and password; document the environment-name declaration at
`:71`, `:215-216` as a protected-context marker rather than a GitHub environment
lookup), `tests/helpers/protected-qualification-fixture.ts`,
`tests/tools/protected-business-qualification.test.ts`,
`tests/tools/cloud-business-workflow.test.ts`.
Verification: `npm run verify:local:focused -- --base <verified-base> --test
tests/tools/protected-business-qualification.test.ts --test
tests/tools/cloud-business-workflow.test.ts`, expecting the absent-prerequisite
matrix to lose the evidence-token case and keep environment, origin, credential,
J0 and boundary cases (`tests/tools/protected-business-qualification.test.ts:403-438`).
Authorization: none; Cloud product-owner review because a prerequisite contract
changes.

**Step 4 — Cloud: contract, gate and release consumer accept the Instance producer.**
Repository: `gaofeng21cn/one-person-lab-cloud`.
Files: `packages/contracts/opl-cloud-distribution-contract.json`
(`cloudBusinessQualification` at `:83-99`: `owner` becomes the Instance
repository and `environment` becomes `production`; `schemaVersion` at `:2`;
`artifact` and `receipt` stay), `tools/validate-product-boundary.mjs` (the
`cloudBusinessQualification` assertion at `:265-275`, the `schemaVersion`
assertion at `:236`, and the release evidence and download assertions at
`:72-89`, which must resolve the run and artifact from the Instance repository
and token), `tests/contracts/product-distribution.test.ts` (`:96`, `:107-117`),
`tools/cloud-release-evidence.ts` (producer `repository` becomes the Instance
repository at `:42-47`; replace `run.head_sha === identity.productSha` at `:182`
with the `product-source.json` product binding plus a 40-hex Instance `main`
`head_sha`; keep the workflow path, terminal state and freshness rules),
`.github/workflows/release-opl-cloud-image.yml` (`:110` and `:166` resolve the
business run and artifacts from `$INSTANCE_REPOSITORY` with `$INSTANCE_TOKEN`,
`:150` validates the run against that repository, `:271-277` gains the Instance
`repository` and `github-token`, `:524` keeps reading
`business-qualification.json`, and `:533` becomes the exact file-set assertion
for the two- or three-file set in §2), `tests/tools/cloud-release-evidence.test.ts`,
`tests/tools/cloud-release-installation.test.ts` if it shares fixtures.
Verification: `npm run verify:local:focused -- --base <verified-base> --test
tests/contracts/product-distribution.test.ts --test
tests/tools/cloud-release-evidence.test.ts --test
tests/tools/cloud-release-installation.test.ts`, including a negative case that
a run in the Cloud repository with the same workflow path is refused, and a
negative case that a receipt without `product-source.json` is refused.
Authorization: none for source; release-owner review because this is a
production gate.

**Step 5 — Owner-authorized: first protected qualification run.**
Repository: `gaofeng21cn/opl-instance-medopl` (dispatch only, no new files).
Authorization: **required** — this performs one real Workspace purchase
(`tools/protected-business-qualification.ts:94-100`) against the cluster, so it
needs the Instance `production` environment approval, the Cloud product owner's
declaration of account/package/authority class, and a fabric-produced J0 READY
receipt for the exact Candidate.
Verification: the run's job labels, the artifact contents,
`business-qualification.json` with `state: settled` and `reason: none`,
`business-qualification.purchase.json`, and a Cloud release-evidence validation
of the downloaded bytes. A blocked or failed receipt
is preserved and reported as such; it does not become a qualification claim.

**Step 6 — Cloud and Instance: customer-reads harness and lane, then one authorized run.**
Repositories: both; bundled because the lane cannot produce a valid receipt
until the tool accepts the Instance producer.
Files (Cloud): `tools/cloud-business-readback.ts` — the receipt and binding
validators take the expected producer instead of comparing to `CLOUD` (`:10`)
and `WORKFLOW` (`:12`) at `:96` and `:111`; `:166` takes the lane's workflow
path instead of `WORKFLOW`; the receipt `schemaVersion` becomes 3 at `:26`,
`:88` and `:168`; the CLI at `:242-245` accepts the producer-workflow option and
replaces the fixed `args.length!==9` check. `tests/tools/cloud-business-readback.test.ts`
— fixtures at `:18-20` move to the Instance producer, while the existing
wrong-repository and wrong-workflow-path cases at `:86` still refuse it.
Files (Instance): new `.github/workflows/cloud-customer-readback.yml` and its
validator registrations, plus a workflow test.
The workflow: Instance `main` guard, runner labels, `environment: production`,
run-scoped Instance and product checkouts, `exact-product-source.mjs`, the same
Candidate and installation inputs the Cloud lane uses
(`cloud-business-readback.yml:95-143`), then the
product checkout's `tools/cloud-business-readback.ts run` with
`--producer-workflow .github/workflows/cloud-customer-readback.yml`, artifact
upload `cloud-customer-readback-<run_id>-<run_attempt>` containing
`receipt.json`, cleanup. Ordering: the lane runs the product checkout's tool, so
a Candidate built from a pre-step-6 product SHA fails at `:169`; the first
Instance-produced `customer_reads` receipt requires a Candidate built after
step 6 lands.
Verification: `npm run verify:local:focused -- --base <verified-base> --test
tests/tools/cloud-business-readback.test.ts`; then `npm run validate`
(`opl-instance-medopl package.json:7`) and the new Instance workflow test, then
one owner-authorized run producing `customer_reads_verified` or an explicit
`blocked` receipt.
Authorization: none for the source; **required** for that one run, because it
authenticates as the designated customer in the Instance `production`
environment and writes a `customer_reads` receipt.

**Step 7 — Cloud: retire `cloud-business-readback.yml` (owner-authorized).**
Repository: `gaofeng21cn/one-person-lab-cloud`.
Files: delete `.github/workflows/cloud-business-readback.yml`;
`.github/workflows/release-opl-cloud-image.yml:26`, `:78-79`, `:142-147`,
`:263-269` re-pointed to the Instance run and repository (the customer run's
repository and workflow path at `:142-143` become the Instance lane
`.github/workflows/cloud-customer-readback.yml`); the `customer-authority`
checkout at `:205-210` becomes the Instance repository at
`steps.evidence.outputs.customer_authority_sha` with
`secrets.OPL_INSTANCE_EVIDENCE_TOKEN`, and `CUSTOMER_VALIDATOR` at `:427` moves
to `product-source/tools/cloud-business-readback.ts`, and the binding call at
`:439-445` takes the expected producer from the admitted `customer-run.json`
(`:444`), so the assertion at `:432` still compares that Instance revision with
`EXPECTED_CUSTOMER_SHA`;
`packages/contracts/opl-cloud-distribution-contract.json` `customerReadback`
(`:131-139`) re-pointed to the Instance lane (`repository`, `workflow` and
`environment` values), with the contract `schemaVersion` bumped and
`tools/validate-product-boundary.mjs` (`allowedWorkflows` at `:6-17`, which
still lists the retired file at `:10`; the `customerReadback` assertions at
`:282-288`) and `tests/contracts/product-distribution.test.ts` updated with it;
`tests/tools/cloud-business-workflow.test.ts` (delete the `workflow` parse at
`:11` and the five tests that exercise `cloud-business-readback.yml` at `:28`,
`:67`, `:90`, `:182`, `:366`; the release-sealing test at `:268` stays);
`docs/runtime/release.md:108-210` rewritten so the credential paragraph no
longer names Cloud-held customer Secrets and the artifact and receipt names are
the contract's; `tools/cloud-business-readback.ts` stays in Cloud, its producer
and CLI change lands in step 6, and the release runs it from the product-source
checkout.
Authorization: **required** — it removes a protected surface and changes release
inputs; both repository owners must approve.
Verification: focused consumer tests; after a successful step 5 and step 6 run,
one release admission that consumes both Instance artifacts, or an explicit
record that no release was dispatched.
Note: deleting the Cloud environment and its `OPL_QUALIFICATION_*` values is part
of this step, not a separate one, because leaving them behind keeps a latent
second copy.

**Step 8 — Closeout documentation.**
Repositories: both.
Files: `docs/architecture.md:186-193` (its ownership sentence stays and its
execution clause names the host; the durable decision itself landed in step 1),
`docs/status.md`, `docs/roadmap.md:130`, `docs/runtime/release.md`,
`docs/README.md:101-106` (index line),
`opl-instance-medopl docs/runtime/workspace-qualification.md`,
`opl-instance-medopl receipts/` if an admitted receipt is committed.
Verification: documentation checks; the #872 outcome is updated only for the
parts that actually executed, and the remaining Instance gates are named as
retained.

## 8. Open Questions

1. **Instance `production` environment protection.** Does it carry required
   reviewers, `prevent_self_review` and a `main` branch policy for a
   money-spending lane? The Cloud entry checked this explicitly before use
   (`.github/workflows/cloud-business-readback.yml:47-73`) and its equivalent
   cannot be read from either repository. This design ports the readback as an
   admission step, but the values themselves need the Instance owner's
   confirmation before step 5.
2. **Who may dispatch the lane.** The Cloud gate admits the Cloud repository
   owner or `RenDeHuang` with actor equal to triggering actor
   (`.github/workflows/cloud-business-readback.yml:48`); some Instance lanes are
   narrower (`cloud-workspace-launch-readback.yml:65` admits only
   `RenDeHuang`). One allowlist must be named for a lane that spends customer
   money, and the design needs to know whether the Cloud product owner's
   declaration is sufficient or a second Instance-side approval is required.
3. **The J0 READY producer.** #952 decided that Cloud fabric produces it, and
   `tools/workspace-qualification-j0-ready.ts` now derives it from a provider
   sandbox qualification run
   ([status](../status.md#fabric-j0-ready-sandbox-qualification-producer-2026-10-18)).
   No real sandbox run has produced one, and who dispatches that run, where its
   mode-0600 file is produced, and how its dispatch value is authorized are still
   open (`.github/workflows/cloud-business-readback.yml:38-41`, `:250-259`;
   `docs/runtime/release.md:183-197`).
4. **Least privilege between the Cloud producer process and the kubeconfig.**
   In Option B one job both runs the Cloud producer and holds a prepared
   kubeconfig. The current lanes already reduce the exposure (a temp directory,
   mode 0600, exported only into the steps that need it,
   `deployment-verify.yml:227-246`), but the owner should decide whether the
   boundary must run in a separate step or job whose environment alone carries
   the kubeconfig, and whether the producer step's environment must exclude it.
5. **Whether a settled receipt is also committed to
   `opl-instance-medopl/receipts/`.** Instance policy allows admitted redacted
   repository evidence (`receipts/README.md:10-16`) and the qualification receipt
   contains no credential, Key, customer identifier, origin, cluster or namespace
   (`docs/runtime/release.md:200-203`), but the Release consumer reads the
   artifact either way. Artifact-only is this document's default; committing is
   the Instance owner's call.
