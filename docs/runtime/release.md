# Product Release Operations

OPL Cloud owns reusable source, contracts, portable Candidate bundles, public
Releases and customer-business diagnostic validators/custody.
`opl-instance-medopl` owns concrete installation profiles, Secrets, TKE
deployment, rollback and installation receipts. See
[Installation](../installation.md), [Architecture](../architecture.md) and
[Status](../status.md).

## Candidate, Instance Profile And Receipt Boundary

1. **Candidate Bundle**: Built from an exact Cloud `main` commit with
   digest-addressed `linux/amd64` + `linux/arm64` images, `opl-cloud-candidate.json`,
   installation assets and `SHA256SUMS`. Building a Candidate never publishes a
   formal Release tag.
2. **Instance Profile**: Supplied solely by the target Instance repo (non-secret
   provider, resource, database, storage and Secret-reference declarations). Cloud
   never reads Instance Secret values, DSNs, kubeconfig or cluster config.
3. **Receipts**: Instance validates installation against its profile and emits
   redacted installation/profile-digest receipts bound to Candidate identity;
   Cloud validates receipt signatures/schemas without inspecting Secrets.

## Workspace Deletion Diagnostics And Evidence Custody

Cloud owns customer-deletion diagnostic tooling and final redacted custody while
Instance workflows provide bounded read-only transport in protected clusters:

| Tool (`tools/*.ts`) | Purpose | Safety Fence |
| --- | --- | --- |
| `workspace-fabric-dispatch-error-readback.ts` | Classify Fabric dispatcher `kubectl logs --timestamps` UTC excerpts against bound operation | Fails closed on symlinks; `unknown` / `blocked` never imply deletion or refund |
| `workspace-compute-live-readback.ts` | Inspect live `GET /fabric/compute-allocations/{id}/destroy-status` (Machine then CVM) | Never mutates resources or writes journals; `SHUTDOWN`/`MANMADE` stays present |
| `workspace-compute-destroy-journal.ts` | Query Fabric domain operation journal bound to pinned container `DATABASE_URL` | Diagnostic readback only; v1/v2 decoders stay strict |
| `workspace-compute-detach-reconciliation.ts` | Execute explicitly authorized `POST .../reconcile-prepaid-detach` for stuck prepaid detach | Permanent DB one-send fence per destroy; replays read only |

Protected Instance handoff artifacts (`cloud-workspace-compute-readback-handoff-*`)
are validated and retained by `.github/workflows/workspace-compute-readback-handoff.yml`.
Optional CloudAudit correlation (`include_provider_audit`) reads only bounded
`LookUpEvents` for the frozen cluster/Machine/window and never clears a send fence.

## Cloud Agent WebUI Build Input

`.github/workflows/build-cloud-agent-webui.yml` builds `webui/cloud-agent` at an
exact `product_sha` into multi-arch images under `ghcr.io/<owner>/opl-cloud-agent-webui`
and emits `cloud-agent-webui-<sha>-<attempt>` artifacts (per-platform child digests
are fixed as Build inputs).

## Product Release Admission

### Candidate Qualification

- **PR & Source CI**: Required PR CI checks (`validate`, `dependency-review` and
  source/unit suites) pass on the exact commit.
- **Clean-Host Local Qualification**: `local-v2` starts `compose.local-v2.yaml`,
  verifies native owner stores, HTTPS Console access, restart continuity and
  delete/replay (`LOCAL_NO_CHARGE`, `p0Ready: false`). Restart verifies that all
  previously observed Ledger receipts retain their contents, including the
  order's no-charge proof, while allowing pending owner events to arrive.
  If an uploaded package's
  Workspace stops at runtime, the `local-v2-native-evidence` artifact records
  the exact purchase's Workspace saga and Serve status and error codes before
  cleanup. The qualification result remains `NOT_READY`.
- **Instance Installation Verification**: The Instance owner verifies that the
  exact Candidate is installed and healthy. Formal Release requires the separate
  `deployment-verify.yml` run on Instance `main` with a `deployment_verified`
  receipt bound to the same Candidate receipt, source commit and tree, and image
  digest. Use the Instance [production runbook](https://github.com/gaofeng21cn/opl-instance-medopl/blob/main/docs/runtime/production-runbook.md)
  for installation and verification.
- **Source Paths (`source_verified`)**: The publication workflow binds the
  business entry paths to the exact source and records
  `paidBusinessExecuted: false` and `customerReadsExecuted: false`. Formal Release
  promotes the qualified Candidate digest without rebuilding.

## Optional Operational Qualification

- **Protected Cloud Customer Reads**: `tools/cloud-business-readback.ts` (driven via
  approved Instance transport) verifies authenticated CloudIdentity wallet, monthly
  usage, legacy billing receipt (`ledger` source) and announcement reads plus session
  revocation.
- **Protected Cloud Business Qualification**: Separately authorized end-to-end
  purchase/delivery/deletion verification against the Candidate digest on Instance TKE.

## Publication & Failure Classification

Daily continuation/release workflows promote verified Candidate artifacts with
GitHub build provenance attestations. Failures classify distinctly across
`source_gate_failed`, `clean_host_failed`, `instance_install_failed` and
`qualification_failed` without downgrading evidence layers.

The Framework daily release status reads the Cloud continuation run for the
frozen tag. The continuation receipt includes the exact tag and Product commit
once their version and canonical ancestry have been verified, including when a
later step blocks. Public publication is confirmed through the GitHub Release.

When the result is `qualification_pending`, retain the Candidate and Clean Host
run IDs. After the Instance owner produces the matching standalone installation
receipt, resume the Cloud `daily-release.yml` workflow on `main` with the same
`release_tag`. Check the current public Release before resuming a pending tag
after a newer version has been published.
