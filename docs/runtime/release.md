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
Workspace artifacts. This is only the `customer_reads` scope of the
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
still requires those owner-authoritative receipts. A
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

### Publication

After candidate qualification succeeds, an allowlisted Cloud Release publisher
supplies the exact canonical SHA and the Candidate, Local qualification,
Instance deployment-verification, qualification-decision, and `workspace_verified`
run IDs, plus the required `cloud_customer_readback_run_id`. The Cloud customer
receipt consumer validates the receipt, producer source/run provenance, exact
Candidate identity and original installation receipt hash. Installation
verification is admitted independently from product qualification: the exact producing Instance revision validates its native
`deployment_verified` receipt; Cloud binds its raw-byte digest, workflow run,
Candidate source/index/amd64 digest, Workspace image and provider scope to the
qualification decision. The sealed release manifest and attestation retain a
separate `installation` record and a customer scope chain. Release manifest and
provenance use `schemaVersion: 3`; the Candidate manifest remains version 2.
Release admission contract schema 5 adds required
`cloud_customer_reads_verified` and retains all five previous evidence
requirements. A customer-read pass does not replace any retained Instance
qualification-decision or Workspace gate; those gates remain until full parity
is actually verified. A successful rollout alone cannot satisfy them. Admission
validates their run/artifact provenance, native receipts, release assets,
checksums, Product tree, and index/platform digests. All evidence must bind the
same Cloud Candidate while retaining its owner-specific Workspace image and
Provider Profile facts.
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
