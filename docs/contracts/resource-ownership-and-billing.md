# Resource Ownership And Billing

This matrix defines when OPL Console manages and bills resources.

## Ownership Matrix

| Resource source | Example | Console management | Billing default |
| --- | --- | --- | --- |
| OPL Cloud-hosted | Managed Workspace, managed Docker or VM, managed storage | Yes | Billed by package and usage |
| Organization-managed | Hospital, lab, or enterprise resource under shared policy | Yes | Billed or reported according to organization policy |
| User-provided local | User laptop or local Docker | Optional visibility | Not Cloud-billed by default |
| User-provided SSH or HPC | User credential to lab server or HPC login node | Optional visibility | Not Cloud-billed by default |
| External provider | Model provider, storage provider, compute provider | Policy-managed when connected | Billed through provider usage, package, or pass-through policy |

## Metering Sources

- Gateway usage: provider, model, tokens, requests, cost signal.
- Workspace usage: package, uptime, storage allocation, user count.
- Compute usage: adapter, runtime duration, GPU flag, queue signal.
- Storage usage: volume, bucket, retention class, transfer signal.
- Connector usage: connector, action, request count, data boundary signal.
- Agent usage: package, instance, run, resource profile, reviewer gate.

## Product Rule

Console should show user-facing packages first, then expose usage breakdowns for
compute, storage, Gateway, connectors, and agent runs. Raw infrastructure fields
belong in operator views and receipts.
