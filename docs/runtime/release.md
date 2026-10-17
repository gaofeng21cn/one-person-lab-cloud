# Product Release Operations

OPL Cloud does not operate or automatically deploy a concrete instance. This
repository owns reusable source, contracts, installation assets, candidate
artifacts, and owner-admitted product releases.

- Generic Docker installation: [Install OPL Cloud](../installation.md)
- Product/instance ownership: [Architecture](../architecture.md)
- medopl production operations and `.com` domain bindings: private
  `gaofeng21cn/opl-instance-medopl` repository

Installation credentials, deployment approval gates, rollout, deployment rollback
and deployment receipts remain with the selected instance owner. Customer
operations and business qualification retain their product owners and require
separately bounded protected authorization.

Medopl-specific deployment, installation acceptance, deployment recovery,
service-health canary, rollback and deployment-evidence tools belong to
`opl-instance-medopl` and run from its scoped `main` checkout. Customer business
operations and business qualification belong to Cloud's existing product owners,
as defined by [the current decision](../decisions.md#2026-10-15-workspace-key-lifecycle-and-cloud-customer-reads).
Deployment and product qualification must bind the same immutable Candidate.

The publication source now adds a separately protected Cloud customer-read
receipt alongside the retained Instance launch/use, qualification-decision and
Workspace artifacts. It also requires a separate Cloud business qualification
receipt from the protected #926 producer; that producer is not present yet, so
formal publication remains blocked on it. This is only the `customer_reads`
scope of the
[open migration](../roadmap.md#workspace-key-and-customer-read-boundaries).
The legacy business, administrator and Fabric evidence paths remain required
until full parity is actually verified; their current execution location does
not transfer product ownership to Instance. This runbook does not authorize
workflow dispatch or customer business operations, and a local checkout does not
gain production-network access from the target decision. Current execution
availability and verification limits are recorded in
[status](../status.md#cloud-customer-readback-source-wiring-2026-10-16).
The current cross-repo classification of the Instance business paths and the
exact release inputs that must remain until #926/#928 replace them is in the
[Instance business-path retirement inventory](../implementation/instance-business-retirement-inventory.md).

## Product Release Admission

A formal OPL Cloud Release records a candidate that has already passed the
required adoption path. During the current pre-1.0 phase it is not a CI
checkpoint, a record of every merged change, or an input used to discover
whether `opl-instance-medopl` can deploy the product.

### Release Unit

Before building a release candidate, the Product Release owner records one
reviewable release unit with all of the following:

- **Objective:** the complete product problem or bounded product milestone the
  Release closes;
- **Owner:** the primary Product module responsible for that outcome;
- **Acceptance:** the scoped Product checks plus exact supported Local-Docker
  and `opl-instance-medopl` deployment/product readback for the same Candidate
  required before publication;
- **Artifact impact:** the runtime image, public contract, schema, dependency,
  installation asset, or security boundary that requires new released bytes;
- **Consumers:** during pre-1.0, the supported local installation and
  `opl-instance-medopl` Candidate deployment;
- **Predecessor gap:** why the current Release cannot satisfy that consumer;
- **Known blockers:** confirmation that no known Product-side blocker remains
  for the same objective.

The release unit is review evidence attached to the owning change or release
conversation. It does not become a second roadmap, status, architecture, or
machine-contract owner.

### Candidate Qualification

PR checks, CI artifacts, and exact-SHA candidate images are replaceable inputs.
They may fail, be superseded, or be deleted without creating a formal version.
The Candidate must identify one exact Cloud SHA on a workflow-admitted branch
and one digest-addressed multi-architecture image. It must be available to both qualification paths
without creating a Git tag, GitHub Release, or versioned GHCR tag.
Its portable bundle contains the installation assets, canonical
`opl-cloud-candidate.json`, and `SHA256SUMS`. The manifest binds the exact
Product repository/SHA/tree, Cloud index ref/digest/revision, both platform
child digests, each installation asset SHA-256, and workflow provenance.
`SHA256SUMS` covers every installation asset plus the manifest itself; it does
not cover itself. The canonical Candidate manifest schema does not bind a
selected Workspace image, Provider Profile, domain, or Instance fact. The
bundle includes the generic environment template from the same Product SHA.

The local installation owner qualifies that Candidate on a supported clean Linux
Docker host using an explicit Local-Docker Provider Profile and immutable
Workspace image, then returns a local owner-authoritative receipt.
The Clean Host workflow accepts an explicit immutable Workspace reference or
builds only the existing qualification Workspace fixture into a run-owned
loopback registry. It always qualifies the supplied Cloud Candidate digest;
fixture construction never rebuilds the Cloud image. Fixture acceptance does
not establish a production application or live Sub2API acceptance.
`opl-instance-medopl` independently binds the same Candidate SHA and digest,
deploys through Instance `main` with its explicit `.com` domains, Tencent/TKE
Provider Profile and immutable Workspace image. The existing qualification
mechanism combines rollout and product acceptance in Instance artifacts; it has
not yet adopted the separated ownership above. The current release consumer
still requires those owner-authoritative receipts, and separately requires the
Cloud business qualification receipt described under Publication. A new
provider-neutral qualification flow and its adapter tests do not by themselves
produce a protected receipt or satisfy release admission. A
failed or unknown result on either path returns the release unit to development;
it does not create a Product Release.

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
`OPL_QUALIFICATION_CUSTOMER_PASSWORD`. `OPL_INSTANCE_EVIDENCE_TOKEN` is restricted
to Instance `Actions: read` and `Contents: read` for original evidence and its
validator source. The exact producing Instance revision first validates the
native installation receipt; the producer then binds that receipt and run to
the same Candidate.

`tools/cloud-business-readback.ts` uses only public HTTPS CloudIdentity customer
login/session, external-bindings, wallet and announcements reads, followed by
logout and session-revocation checks. The independently approved origin is a
protected configuration input. Its receipt associates the original installation
bytes but sets `publicEndpointCandidateVerified: false`; the current public
customer APIs expose no Candidate identity proof. It cannot replace installation
or same-runtime business acceptance. Its receipt has scope `customer_reads`
and status `customer_reads_verified` or `blocked`; it explicitly does not prove
full business, Workspace lifecycle or Key lifecycle qualification. This entry
provides no production-private network access or resource/financial mutation
qualification.

### Protected Cloud Business Qualification

The same protected source entry now also runs
`tools/protected-business-qualification.ts` for the business qualification
decision after the original Candidate bundle and the native installation receipt
have been validated. The existing `customer_reads` producer remains a separate
step and receipt; the business producer does not call it, copy its reads or
replace its result.

The producer receives the declared Candidate receipt digest, the deployment
binding, the account/package declarations, the live authority class and the
external J0 READY producer receipt. The workflow materializes the deployment
binding and J0 receipt under `$RUNNER_TEMP` with mode `0600`; the producer derives
the Candidate digest from the exact bytes, checks it against the declaration and
the deployment binding, validates the original installation receipt/run against
the same Candidate, checks the protected owner origin and the fixed mutation
scope, and admits only the `tencent-tke` J0 receipt for the exact source. It then
builds the request with the shared `createQualificationRequest`, runs the shared
`runWorkspaceQualification` through the `tencent-tke` adapter, and writes the
shared `opl.workspace-qualification.purchase.v1` receipt.

The orchestrator's purchase step delegates to the merged deployed buyer,
`runDeployedWorkspacePurchase`. The producer hands it the authenticated owner
transport from the opened provider scope and the protected origin, and the
buyer performs the source-envelope account and wallet reads, the declared
account email digest check, the shared quote policy and the single purchase or
replay through the shared purchase core. The buyer's refusal codes are mapped
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

The protected runner supplies the Fabric/Instance boundary module named by
`vars.OPL_QUALIFICATION_PROVIDER_BOUNDARY`; the module owns TKE reachability and
the owner transport and receives the normalized deployment binding and protected
environment. It is not implemented in Cloud and its credentials do not enter the
receipt. The external J0 READY producer receipt is required as a dispatch input
for the exact Candidate; this source does not produce or fake it. The producer
requires the existing `cloud-business-qualification` environment,
`vars.OPL_QUALIFICATION_ORIGIN`, the independent customer Secrets
`OPL_QUALIFICATION_CUSTOMER_EMAIL`/`OPL_QUALIFICATION_CUSTOMER_PASSWORD`, and
`OPL_INSTANCE_EVIDENCE_TOKEN` limited to Instance `Actions: read` and `Contents:
read`. A missing environment, variable, Secret, J0 receipt or boundary module
fails closed with `external_prerequisite_absent` before a provider scope opens;
the workflow does not create any of them.

Only the shared orchestrator's actual qualification result becomes the
qualification receipt state. A settled receipt is preserved, and a blocked,
failed, unknown or recovery-required receipt is preserved before the workflow
fails. Deployment health is recorded but never moves the business result. The
artifact is `cloud-business-qualification-<run_id>-<run_attempt>` and contains
`receipt.json` plus `receipt.purchase.json` when the buyer reached a receipt;
the existing `cloud-customer-readback-<run_id>-<run_attempt>` artifact is
unchanged. Both receipts are Candidate-bound and contain no raw Secret, Key,
customer identifier, origin, cluster, namespace or entry URL. They cover the
declared Workspace purchase only: `uncovered.businessQualification` is `false`,
and they do not admit a release. #928 owns release admission and the remaining
legacy producer/consumer migration.

This section describes source wiring, not a protected run. The environment,
Secrets, J0 producer and TKE boundary are external and were absent while this
change was prepared; no protected business qualification ran, no settled receipt
is claimed, and #926 remains open.

### Publication

After candidate qualification succeeds, an allowlisted Cloud Release publisher
supplies the exact canonical SHA and the Candidate, Local qualification,
Instance deployment-verification, qualification-decision, and `workspace_verified`
run IDs, plus the required `cloud_customer_readback_run_id` and
`cloud_business_qualification_run_id`.

Release admission consumes two evidence classes that prove different things,
and neither may stand in for the other:

- **Cloud business qualification** is the protected Cloud producer's
  Candidate-bound receipt from the shared provider-neutral qualification
  contract (`tools/workspace-qualification-contract.ts`). The consumer requires
  the exact protected producer workflow run from Cloud `main` at the released
  source SHA, validates the native receipt through that contract, and requires
  terminal state `settled`. It refuses a receipt whose Candidate receipt
  digest, source SHA/tree or Cloud image digest does not equal the release
  Candidate, a receipt outside the bounded freshness window, and any receipt
  shape that carries an undeclared Secret, Key, customer identifier or private
  provider identifier. The producer, artifact, receipt kind, terminal state and
  freshness window are declared in
  `packages/contracts/opl-cloud-distribution-contract.json`.
- **Instance deployment** is the exact producing Instance revision's native
  `deployment_verified` receipt and workflow run. It proves installation
  identity and health, not customer business qualification. Cloud binds its
  raw-byte digest, workflow run, Candidate source/index/amd64 digest, Workspace
  image and provider scope to the qualification decision.

The Candidate, Local installation, Cloud customer-read, Instance
qualification-decision and Instance `workspace_verified` gates remain separate,
required evidence. The Cloud customer receipt consumer validates the receipt,
producer source/run provenance, exact Candidate identity and original
installation receipt hash. A `customer_reads_verified` pass and a
`deployment_verified` pass do not replace any retained gate; every retained
gate stays required until full parity is actually verified.

The sealed release manifest and attestation retain separate `installation`,
`qualifications.business` and customer scope records, and release provenance
records the two evidence classes explicitly. Release manifest and provenance
use `schemaVersion: 3`; the Candidate manifest remains version 2. Release
admission contract schema 6 replaces the undifferentiated `requiredEvidence`
list with typed evidence classes and retains all six previous evidence
requirements plus `cloud_business_qualification`. Admission validates every
class's run/artifact provenance, native receipts, release assets, checksums,
Product tree, and index/platform digests. All evidence must bind the same Cloud
Candidate while retaining its owner-specific Workspace image and Provider
Profile facts.

**Current external dependency.** The #926 producer exists in source
(`tools/protected-business-qualification.ts`, driven by
`cloud-business-readback.yml`; see [Protected Cloud Business
Qualification](#protected-cloud-business-qualification)), but the release
contract's dedicated `.github/workflows/cloud-business-qualification.yml` entry
and artifact binding do not exist yet. Until a protected run emits the settled
receipt for the exact Candidate and the #928 consumer binds that producer
workflow/artifact, a formal Release cannot satisfy the Cloud business
qualification class. This source change makes that dependency explicit; it does
not claim a real qualification, a Release run, or production evidence.
The repository owner or `RenDeHuang` then explicitly dispatches the Release
workflow from Cloud `main`; the original actor and current triggering actor must
match. Publication must promote the exact qualified image bytes and must fail
if it would rebuild a different digest.

The publication workflow now implements the core sequence as admission followed
by protected publication. Only publication holds the global lock and write
permissions. The separate `release-opl-cloud-public-readback.yml` follower is
triggered by a successful publication run and performs anonymous GHCR/Release
readback plus attestation checks from that run's sealed checkpoint. It is
read-only and non-blocking; a failed follower can be retried with the original
publication run ID. The Instance evidence reader Secret
`OPL_INSTANCE_EVIDENCE_TOKEN` must be limited to `Actions: read` and
`Contents: read` on `opl-instance-medopl`; it is not an Instance deployment
credential. The required evidence must exist for the exact Candidate before publication.
Retained publication evidence belongs only in [status](../status.md).

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
current pre-1.0 phase it still requires both exact Candidate qualification paths
before formal publication.
