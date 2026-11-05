# Product Release Operations

OPL Cloud owns reusable source, contracts, portable Candidate bytes and public
product releases. Instance owns installation configuration, Secrets, deployment,
rollback and deployment receipts. Production-private access runs only through
`opl-instance-medopl` protected workflows and authorized runners.

- Generic installation: [Install OPL Cloud](../installation.md)
- Product/Instance ownership: [Architecture](../architecture.md)
- Current evidence: [Status](../status.md)

## Cloud Agent WebUI Build Input

The Cloud-owned independent Agent WebUI has its own exact-source artifact build:
`.github/workflows/build-cloud-agent-webui.yml`. Dispatch `Build Cloud Agent WebUI`
with `product_sha` equal to the selected branch head. It archives only
`webui/cloud-agent`, builds Linux amd64/arm64 images and publishes to
`ghcr.io/<repository-owner>/opl-cloud-agent-webui` under a source/run/attempt tag.
It uses the job's GitHub token, without Instance credentials or deployment access.

The `cloud-agent-webui-<sha>-<attempt>` artifact retains push metadata, registry
index/config readbacks, Git commit/tree identifiers and `SHA256SUMS`. Its
single-platform child digests are the Build inputs; the multi-platform index is
not a substitute. Image source labels and the original GitHub run bind source
and workflow provenance, not Runtime compatibility, Capability admission or a Product Release. Instance
registry transfer and catalog/default-policy writes remain separately authorized
owner operations. Runtime recipes remain Runtime-publisher inputs.

## Product Release Admission

The [2026-11-05 owner decision](../decisions.md#2026-11-05-stable-publication-uses-non-spending-acceptance)
sets the current acceptance: actual installation verification and business
source-path availability, without cost-incurring business execution. A release
unit identifies its product outcome, owner, supported installation, changed
artifact bytes and predecessor gap in the owning PR or release conversation.

### Candidate Qualification

Build one exact Cloud SHA on a workflow-admitted branch, with a digest-addressed
Linux amd64/arm64 image. The portable bundle contains its canonical
`opl-cloud-candidate.json`, installation assets and `SHA256SUMS`. The Candidate
manifest binds Product repository/SHA/tree, index and platform digests, asset
hashes and workflow provenance. Candidate construction creates no formal tag or
Release. Formal publication requires that exact source reachable from canonical
main and promotes the same Candidate image without rebuilding.

The main Clean Host workflow qualifies the supplied Candidate on a clean Linux
Docker host. Its default `local-v2` profile uses the declared native topology,
CA-backed Console HTTPS, independent owner stores, project quota and the
existing anonymous application fixture. The version-two READY receipt binds
source/images, owner processes/stores, actual application access, restart
continuity, owner deletion and replay, and zero remaining Workspace resources.
It is explicitly `LOCAL_NO_CHARGE` with `p0Ready: false`. The explicit
`legacy-fixture` mode retains its own version-one validator and evidence scope.
A failed or mismatched installation remains NOT_READY.

Instance independently deploys the same Candidate through its protected main
workflow, then its separate `deployment-verify.yml` records native installation
identity and technical health. Cloud validates the original bytes through the
producing Instance revision and binds the receipt/run to that Candidate.
Successful installation does not imply paid business acceptance.

The exact Candidate's successful Pull Request CI run must include all six
completed successful jobs. Release admission inspects the existing business
entry files from its clean exact checkout, records their hashes and binds the
source CI run. It emits `source_verified` with `paidBusinessExecuted: false` and
`customerReadsExecuted: false`; it never relabels an unexecuted purchase as
settled or creates a Workspace acceptance receipt.

## Optional Operational Qualification

The following existing producers remain available for separately authorized
operational acceptance. Their native contracts and protected boundaries remain
in force. They are not required or invoked by the current non-spending Release
admission, and their absence does not create a substitute success receipt.

### Protected Cloud Customer Reads

The source entry `.github/workflows/cloud-business-readback.yml` has two
`ubuntu-latest` jobs: `admission`, then `readback`. Dispatch is limited to Cloud
`main`, the repository owner or `RenDeHuang`, with the original actor matching
the triggering actor. Before the readback job references
`cloud-business-qualification`, admission uses GET readback to require that the
environment already exists, has nonempty required User reviewers restricted to
the allowed owners, `prevent_self_review: true`, and exactly one custom branch
policy for `main`. The workflow does not configure or create that environment.

The protected environment supplies `vars.OPL_QUALIFICATION_ORIGIN` and the
independent customer Secrets `OPL_QUALIFICATION_CUSTOMER_EMAIL` and
`OPL_QUALIFICATION_CUSTOMER_PASSWORD`. The protected environment reuses
`OPL_GITHUB_RELEASE_ADMIN_TOKEN` for Instance evidence and validator source
reads. These calls require only Instance `Actions: read` and `Contents: read`.
The owner explicitly approved reuse of the existing publisher credential,
whose all-repository read/write permissions exceed those required reads.
Its value remains in the protected environments; it is not available to PR or
Candidate jobs. The exact producing Instance revision first validates the
native installation receipt; the producer then binds that receipt and run to
the same Candidate.

`tools/cloud-business-readback.ts` uses only public HTTPS CloudIdentity customer
login/session, external-bindings, wallet, usage-summary, fee-record and
announcements reads, followed by logout and session-revocation checks. The
wallet and usage reads must carry the Gateway owner's public encoding; the
fee-record read must answer the `ledger` source envelope without a session
rejection, and a not-found blocks the receipt because the BFF cannot distinguish
a customer without an original Account from a missing or older internal route. The independently
approved origin is a protected configuration input. Its receipt associates the original installation
bytes but sets `publicEndpointCandidateVerified: false`; the current public
customer APIs expose no Candidate identity proof. It cannot replace installation
or same-runtime business acceptance. Its receipt has scope `customer_reads`
and status `customer_reads_verified` or `blocked`; it explicitly does not prove
full business, Workspace lifecycle or Key lifecycle qualification. This entry
provides no production-private network access or resource/financial mutation
qualification.

The customer workflow emits only `cloud-customer-readback-<run>-<attempt>`,
retained for its own operational acceptance. It does not invoke the business purchase producer.
The protected Instance lane below remains the sole workflow producer of business
qualification evidence; its trust and exact-Candidate gates are unchanged.

### Protected Cloud Business Qualification

The protected Instance entry
`.github/workflows/cloud-business-qualification.yml` runs the Cloud-owned
`tools/protected-business-qualification.ts` after the original Candidate bundle,
the exact product checkout and the native installation receipt have been
validated. Cloud owns the producer source, receipt schema and release consumer;
Instance owns the protected `production` environment and provider boundary.
The existing `customer_reads` producer remains a separate receipt; the business
producer does not call it, copy its reads or replace its result.

The producer receives the declared Candidate receipt digest, the deployment
binding, the account, RuntimeVersion, compute/storage plan declarations, the
live authority class and the J0 READY receipt. The workflow materializes the deployment binding and J0 receipt
under `$RUNNER_TEMP` with mode `0600`; the producer derives the Candidate digest
from the exact bytes, checks it against the declaration and the deployment
binding, validates the original installation receipt/run against the same
Candidate, checks the protected owner origin and the fixed mutation scope, and
admits only a `tencent-tke` J0 receipt bound to the exact Candidate. It then
builds the request with the shared `createQualificationRequest`, runs the shared
`runWorkspaceQualification` through the `tencent-tke` adapter, and writes the
shared `opl.workspace-qualification.purchase.v1` receipt.

The orchestrator's purchase step delegates to the merged deployed buyer,
`runDeployedWorkspacePurchase`. The producer hands it the authenticated owner
transport from the opened provider scope and the protected origin, and the
buyer checks the retained source-envelope customer identity, the live
CloudIdentity session and the declared Tenant's active billing subject, then
issues the single native quote/purchase or replays its original command through
the shared purchase core. Current catalog availability cannot block an already
accepted order. Personal-wallet deltas do not prove the Tenant payer's charge. The buyer's refusal codes are mapped
onto the shared terminal reasons. The orchestrator consumes the buyer's bounded
result and continues with the owner charge receipt, Workspace and Runtime
readbacks, so the deployed buyer is actually exercised rather than copied or
merely guarded.

The buyer returns its own `opl.deployed-workspace-purchase.v1` receipt. It binds
the exact Candidate, the declared account identity digest and the observed
purchase count, and is preserved as `receipt.purchase.json` beside the
qualification receipt. The orchestrator's operation identity in
`receipt.json` is the buyer's platform order identity; the qualification request
remains the admission, and the buyer's receipt remains a purchase record rather
than a qualification claim.

Cloud fabric produces the J0 READY receipt
([status](../status.md#fabric-j0-ready-sandbox-qualification-producer-2026-10-18))
from a provider sandbox qualification run. `tools/workspace-qualification-j0-ready.ts`
drives `runTencentTkeSandboxQualification` in the `tencent-tke` adapter through the
same boundary module the protected lane loads: the declared sandbox deployment is
opened, its owner transport is checked against that deployment, the provider's
own health readback must answer `healthy`, and the scope is released. The
resulting `opl.workspace-qualification.j0-ready.v2` receipt is bound to the exact
Candidate in `source.cloudImageDigest`; the protected loader refuses another
Candidate's receipt as an absent prerequisite and any non-confirmed,
non-`sandbox`, non-`live`, non-dedicated or non-zero `failed`/`skipped` receipt as
invalid. The receipt crosses the job boundary as a file under `$RUNNER_TEMP`,
created mode `0600` with `O_EXCL | O_NOFOLLOW` and re-read under the same file
standard and a 1 MiB byte cap. The workflow's jq gate is unchanged and remains
weaker than the loader, which is the authority.

The protected runner supplies the Fabric/Instance boundary module named by
`vars.OPL_QUALIFICATION_PROVIDER_BOUNDARY`; the module owns TKE reachability and
the owner transport and receives the normalized deployment binding and protected
environment. It is not implemented in Cloud and its credentials do not enter the
receipt. The producer requires the `cloud-business-qualification` protected-context
declaration (a marker that the step came from that protected context, not a
GitHub environment lookup), `vars.OPL_QUALIFICATION_ORIGIN` and the independent
customer Secrets `OPL_QUALIFICATION_CUSTOMER_EMAIL`/`OPL_QUALIFICATION_CUSTOMER_PASSWORD`.
It also requires `OPL_QUALIFICATION_TENANT_ID` and
`OPL_QUALIFICATION_BILLING_SUBJECT_ID`, compared with live owner readback before
purchase. Its product arguments are `--runtime-version-id`, `--compute-plan-id`
and `--storage-plan-id`; `--package-id` no longer selects a native Workspace.
Instance #593 adopted this native consumer source migration; Cloud #1072
records its adoption evidence.
Source adoption does not supply the installation's declarations, genuine
same-Candidate J0 or complete protected execution receipt; see the
[remaining qualification work](../roadmap.md#native-v2-protected-qualification-and-cp-retirement-post-1042).
It deliberately requires no cross-repository evidence token: it reads only the
files this workflow materialized and its own J0 receipt, so it has no
cross-repository read to authorize. The Instance workflow reads its own
installation artifacts with its repository-scoped `github.token`. Cloud's
separate cross-repository consumers use the protected publisher credential
as described above. A missing declaration, variable, Secret, J0 receipt or
boundary module fails closed with `external_prerequisite_absent` before a
provider scope opens; the workflow does not create any of them.

Only the shared orchestrator's actual qualification result becomes the
qualification receipt state. A settled receipt is preserved, and a blocked,
failed, unknown or recovery-required receipt is preserved before the workflow
fails. Deployment health is recorded but never moves the business result.
The Instance run uploads the exact artifact `cloud-business-qualification`. It
contains `business-qualification.json` and `product-source.json`, plus
`business-qualification.purchase.json` when the buyer reached a receipt; the
existing `cloud-customer-readback-<run_id>-<run_attempt>` artifact is unchanged.
The product-source file binds the exact Cloud product SHA and tree independently
of the Instance run's own `head_sha`. The retained full-business evidence consumer resolves this run and
artifact from the Instance repository with `OPL_GITHUB_RELEASE_ADMIN_TOKEN`, rejects
another repository or workflow, and validates both receipt files through the
Cloud-owned typed consumer. Receipts are Candidate-bound and contain no raw
Secret, Key, customer identifier, origin, cluster, namespace or entry URL. They
cover the declared Workspace purchase only: `uncovered.businessQualification`
is `false`, and they do not admit a release by themselves.

This section describes source wiring, not a protected run. No fabric sandbox
qualification and no protected business qualification run have produced a real
J0 READY or settled receipt (the tests inject only the transport). The producer
source from #926 is delivered; its historical issue state does not establish
protected acceptance. #1042 tracks the current consumer migration and exact-
Candidate qualification: the protected environment must declare the account,
Tenant, payer, RuntimeVersion, plans, origin, credentials and provider boundary,
a fabric sandbox must emit the exact-Candidate J0 receipt, and the Instance lane
must emit the settled business receipt for that same Candidate.

## Publication

The owner or `RenDeHuang` dispatches `release-opl-cloud-image.yml` from main;
original and triggering actors must match. Its required inputs are `version`,
exact Product `ref`, `candidate_run_id`, `local_qualification_run_id`,
`instance_deployment_verification_run_id` and `source_verification_run_id`.
Admission validates their original run/artifact authority and exact identity.

Both admission and publication use the existing protected `cloud-release`
environment. Admission reuses the owner-approved
`OPL_GITHUB_RELEASE_ADMIN_TOKEN` solely for Instance evidence and validator
reads. Its actual publisher permissions are broader than the Actions/Contents
reads require; Candidate and PR jobs do not receive it. Instance installation
validation and source-path verification produce distinct evidence.

The sealed schema-four manifest and provenance bind source, Candidate digest,
actual local/Instance installation and `business_source_paths` evidence. They
explicitly declare `installation_and_source_paths` acceptance and no paid or
customer-read business execution. The publication job alone holds the global
publication lock and write permissions. It verifies the sealed cohort, attests
all assets, promotes the admitted image digest and reconciles the same tag.
Release notes disclose the unexecuted paid paths. The separate public follower
checks anonymous GHCR/Release bytes, checksums and attestation against the
original sealed provenance; a failure is recovered for the original tag/run.

Publication retries reconcile the same tag: they promote the admitted digest,
replace the complete Release asset cohort, remove non-contract assets, and then
read back the public bytes. A publication-tool failure never requires a version
bump. Reuse is rejected only when the existing tag resolves to a different
Product SHA; changed Product bytes require a new qualified Candidate and a
product version decision.

Do not publish a new Product Release solely for:

- documentation, status, plan, or test changes;
- CI, cache, runner, or build-performance changes;
- Instance workflow, environment, Secret, provider, or cluster configuration;
- deployment retries, failed qualification, account state, or approvals;
- an unclassified failure or an attempt to obtain more debugging evidence.

### Failure Classification

When candidate adoption or qualification fails, classify the owner before the
next attempt:

| Proven owner | Required action | Formal Product Release |
| --- | --- | --- |
| Local installation asset, host contract, Local-Docker adapter, or local qualification tool | Fix the owning Cloud product/install surface, create a new Candidate when bytes change, and repeat local qualification | Not admitted until both paths pass |
| Instance configuration, workflow, provider, cluster, Secret, account, approval, or runtime data | Correct or retry in the Instance owner with the same candidate when its bytes remain valid | Not admitted |
| Unknown or conflicting evidence | Stop mutation and obtain authoritative readback for the exact candidate | Not admitted |
| Product runtime, public contract, schema, installation asset, dependency, or security boundary | Fix Cloud, create a new Candidate SHA/digest, and repeat both qualification paths | Allowed only after the new Candidate succeeds on both paths |

An urgent security correction may define a narrow release unit, but during the
current pre-1.0 phase it still requires both exact Candidate installation paths
before formal publication.
