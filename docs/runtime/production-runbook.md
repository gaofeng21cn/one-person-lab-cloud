# Instance Operations Boundary

OPL Cloud does not operate or automatically deploy a concrete instance. This
repository owns reusable source, contracts, installation assets, candidate
artifacts, and owner-admitted product releases.

- Generic Docker installation: [Install OPL Cloud](../installation.md)
- Product/instance ownership: [Architecture](../architecture.md)
- medopl production operations and `.com` domain bindings: private
  `gaofeng21cn/opl-instance-medopl` repository

Production credentials, provider resources, approval gates, rollout, rollback,
and receipts must remain with the selected instance owner.

Medopl-specific production, acceptance, recovery, canary, rollback, and
approval/evidence tools are owned by `opl-instance-medopl` and are executed from
its run-scoped `main` checkout. Cloud retains only the product runtime,
provider-neutral contracts, reusable adapters, and portable candidate/release
assets. The Cloud checkout supplied to an Instance workflow is the immutable
product source identified by `product_sha`; it is not the source of an instance
operation.

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
The candidate must identify one canonical Cloud SHA and one digest-addressed
multi-architecture image. It must be available to both qualification paths
without creating a Git tag, GitHub Release, or versioned GHCR tag.
Its portable bundle contains the installation assets, canonical
`opl-cloud-candidate.json`, and `SHA256SUMS`. The manifest binds the exact
Product repository/SHA/tree, Cloud index ref/digest/revision, both platform
child digests, each installation asset SHA-256, and workflow provenance.
`SHA256SUMS` covers every installation asset plus the manifest itself; it does
not cover itself. The canonical Candidate manifest schema does not bind a
selected Workspace image, Provider Profile, domain, or Instance fact. The
bundle still contains the current environment template; removing its
installation-specific defaults remains the
`PB-N-DISTRIBUTION-RELEASE-01` roadmap gap.

The local installation owner qualifies that Candidate on a supported clean Linux
Docker host using an explicit Local-Docker Provider Profile and immutable
Workspace image, then returns a local owner-authoritative receipt.
`opl-instance-medopl` independently binds the same Candidate SHA and digest,
deploys through Instance `main` with its explicit `.com` domains, Tencent/TKE
Provider Profile and immutable Workspace image, performs the required rollout
and product acceptance, and returns an Instance owner-authoritative receipt. A
failed or unknown result on either path returns the release unit to development;
it does not create a Product Release.

### Publication

After candidate qualification succeeds, an allowlisted Cloud Release publisher
supplies the exact canonical SHA and the Candidate, Local qualification,
Instance qualification-decision, and `workspace_verified` run IDs. Admission
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
credential. No hosted run has yet proved the new path for one current qualified
Candidate, so do not claim or publish a successor to `v0.1.7` without that
evidence.

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
