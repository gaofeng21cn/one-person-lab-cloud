# Audit response recheck

Protected readback `42946920364` at 10:34 CST on November 11, 2026 reused the same installed Candidate and original Workspace deletion. Cloud custody `42947505427` retained these exact parent and child bytes.

The earlier `AuthFailure.UnauthorizedOperation` was not observed again. The new child is `blocked/audit_schema_invalid`, with `query.failure=null`, one request and zero accepted response pages. The remaining uncertainty is the response-envelope or pagination decoder, before event matching. This is not evidence that every IAM permission is granted, and it is not a reason to request broader IAM policies.

The parent remains `observed`: Machine present and TKE `RUNNING`; CVM was not queried. The original detach result, physical absence, Workspace deletion, refund and Key closeout remain unresolved. Continue with a narrowly corrected Cloud-owned response decoder and fixed safe schema-stage codes, preserving the original Candidate, operation and dispatch fence.
