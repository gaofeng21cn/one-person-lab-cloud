# Resource Ownership And Billing

This matrix defines when Console manages or bills resources. `Service plan`
means a commercial offering; `OPL Package` means a Framework-managed Agent or
capability package. The two are not interchangeable.

| Resource source | Example | Console management | Billing default |
| --- | --- | --- | --- |
| OPL Cloud-hosted | Managed Workspace, compute or storage | Yes | Service plan plus usage |
| Organization-managed | Hospital, lab or enterprise resource under shared policy | Yes | Organization policy |
| User-provided local | User laptop or local runtime | Optional visibility | Not Cloud-billed by default |
| User-provided SSH/HPC | Lab server or HPC account | Optional visibility | Not Cloud-billed by default |
| External provider | Model, storage, compute or data source | Policy-managed when connected | Provider or pass-through usage |

## Metering Sources

- Gateway: provider, model, tokens, requests and cost signal.
- Workspace: service plan, uptime, storage allocation and users.
- Compute: adapter, duration, GPU and queue signals.
- Storage: allocation, retention and transfer signals.
- Connector: provider action, request count and data-boundary signal.
- Agent: exact OPL Package ref, instance, run, resource profile and review gate.

Console can attribute usage to an OPL Package ref but cannot change its
manifest, digest, installed version or lock. Those facts remain with OPL
Packages.
