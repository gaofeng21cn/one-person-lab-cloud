# Original detach audit access blocker

Cloud #1161 and Instance #711 are merged and passed current-revision CI. The source-only protected audit reused Candidate `42859752971`, without rebuilding or deploying it.

Instance run `42879470010` and Cloud custody `42879631172` retain the original parent and child bytes in this directory. The parent readback observes a present `RUNNING` Machine. The auxiliary CloudAudit request is `blocked/audit_unavailable`; its safe error family is `AuthFailure`. SHA-256 of the public error code `AuthFailure.UnauthorizedOperation` exactly matches the child receipt's `query.failure.providerErrorCodeDigest`. No original provider event was obtained.

## Original access blocker (superseded by the later recheck)

The later [audit recheck](../2026-11-11-provider-audit-recheck/README.md) no longer received the original permission refusal and reached a response-decoder failure. The following describes the earlier attempt, not the current blocker.

The credential owner is the Tencent CAM identity referenced by the Instance repository's protected `production` environment Secrets `TENCENT_MUTATION_SECRET_ID` and `TENCENT_MUTATION_SECRET_KEY`. Those values must remain in their approved secret store.

A Tencent account/CAM administrator must authorize that identity to read the operation audit through `LookUpEvents`, or provide an explicitly approved audit-read identity through the Instance secret owner. Request only audit lookup access; no IAM administration, provider write action, or broad administrator policy is needed for this query. This repository exposes no approved IAM-policy mutation entry, and its protected environment has no separate audit credential reference. The observed refusal does not identify which exact attached policy or account-level restriction denied access; the administrator must check the effective policy rather than assume a missing allow statement is the only cause.

Once access is corrected, repeat the same protected read with the same Candidate, original Workspace operation, frozen provider target and failed-detach window. Cloud owns event interpretation and custody; Instance owns installation and credential transport. Do not resume or reset the uncertain detach fence based on the audit-query refusal. Original request attribution, provider absence, destroy finalization, Workspace deletion, refund and Key closeout remain unresolved.
