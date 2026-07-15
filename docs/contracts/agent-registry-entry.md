# Agent Registry Entry Tombstone

Owner: `one-person-lab-cloud`
Purpose: `retired_agent_registry_schema_pointer`
State: `history_tombstone`
Machine boundary: This file is not an active schema or registry source. It only
preserves the old path and routes readers to the current package boundary.

The former Cloud-owned `OPL Agent Registry` model is retired. It duplicated
package identity, version and approval state across Fabric and Console, which
could drift from the Framework package lock.

The current boundary is:

| Former field or action | Current owner |
| --- | --- |
| Package id, version, digest and dependencies | Validated OPL Package manifest |
| Installed state and current version | OPL Packages package lock |
| Install, update, rollback, uninstall and repair | OPL Packages lifecycle transaction |
| Lifecycle evidence | OPL Packages lifecycle receipt |
| Account/service availability | Console policy referencing an exact package ref |
| Compute, storage, environment and connector requirements | Fabric binding derived from package requirements |
| User-visible Agent Instance | OPL App / Workspace |
| Portable service entrypoint declaration | Package/domain owner contract |
| Agent Service, immutable Revision, Deployment, endpoint and traffic state | OPL Serve |
| External Invocation and Session lifecycle | OPL Runway; Serve projects status |
| Run and review evidence refs | OPL Ledger |

Active machine truth is obtained through the OPL Framework package contracts
and fresh `opl packages ... --json` readback. Cloud surfaces may project those
refs, but must not create another package registry, lock or lifecycle ledger.
