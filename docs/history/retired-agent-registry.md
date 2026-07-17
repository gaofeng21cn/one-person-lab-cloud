# Retired Cloud Agent Registry

Owner: `one-person-lab-cloud`
Purpose: `retired_agent_registry_model`
State: `history_tombstone`
Machine boundary: Retired-model provenance only. This is not an active schema,
registry, compatibility path, package source, or implementation instruction.

The former Cloud-owned `OPL Agent Registry` model is retired. It duplicated
package identity, version, and approval state across Fabric and Console and
could drift from the Framework package lock.

| Retired responsibility | Current owner |
| --- | --- |
| Package id, version, digest, and dependencies | Validated OPL Package manifest |
| Installed state and current version | OPL Packages package lock |
| Install, update, rollback, uninstall, and repair | OPL Packages lifecycle transaction |
| Lifecycle evidence | OPL Packages lifecycle receipt |
| Account/service availability | Console policy referencing an exact package ref |
| Compute, storage, environment, and connector requirements | Fabric binding derived from package requirements |
| User-visible Agent Instance | OPL App / Workspace |
| Portable service entrypoint | Package/domain owner contract |
| Service, Revision, Deployment, endpoint, and traffic state | OPL Serve |
| Invocation and Session lifecycle | OPL Runway; Serve projects status |
| Run and review evidence refs | OPL Ledger |

Current machine truth comes from OPL Framework package contracts and fresh
`opl packages ... --json` readback. Cloud surfaces may project those refs but
must not create another package registry, lock, or lifecycle ledger. The old
contract path is intentionally absent and must not be recreated as an alias or
facade.
