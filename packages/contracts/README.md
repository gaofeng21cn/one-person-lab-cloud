# OPL Cloud Contracts

Keep a machine contract only when two current owners or an external consumer
must agree on bytes or a stable public identity. Runtime behavior, internal
fields, retired alternatives, current progress, and implementation layout stay
in source, focused tests, or documentation.

The `go/` module contains types required by actual cross-owner consumers.
Current service/module dependencies, protocol packages and consumer paths are
owned by [implementation architecture](../../docs/implementation-architecture.md),
not a fixed list of legacy services in this README. Keep service-local types in
the service; a test fixture alone does not justify adding a cross-owner type.

## Contract Layout And W01

The single Cloud GitHub repository retains one shared Go contracts module at
`packages/contracts/go/go.mod`. Current production proto/binding paths and
consumer dependencies belong to
[implementation architecture](../../docs/implementation-architecture.md).
A historical planned package name is not an instruction to rename an active
protocol or create another Go module.

[W01](../../docs/spec/target/14_implementation_work_packages.md#w01-生产契约与同仓版本一致性)
owns reconciliation of the production contracts with their canonical target
specification and migration of real consumers. It must bind the schema hash,
lock generation tools and verify both sides at the same revision. Merely adding
a layout or regenerating outputs does not complete W01 or implement consumers.

Cloud services consume the contract revision from the same source commit, not
independently floating repository tags. Necessary consumer `go.mod`/`go.sum`
changes are included and verified with the contract revision. Do not create a
second contracts module only to avoid dependency-file changes, or add legacy
compatibility fields merely to satisfy old tests. Cross-service contracts do
not authorize service implementation imports or shared domain persistence.
External authorities and artifact consumers keep their explicit version pins.

## Current Contracts

| Contract | Current consumers |
| --- | --- |
| `opl-cloud-candidate-receipt-contract.json` | Candidate bundle tooling and the Candidate workflow |
| `opl-cloud-distribution-contract.json` | Candidate/Release validation and the instance handoff |
| `opl-cloud-fabric-launch-binding-contract.json` | Control Plane and Fabric stage request hashing and Runtime image revision proof |
| `opl-cloud-workspace-runtime-abi-contract.json` | Control Plane and Fabric Workspace WebUI routing |
| `opl-cloud-webui-artifact-contract.json` | Cloud WebUI artifact identity and Build/Serve compatibility (W13/W14/W16) |

The Candidate and Distribution contracts bind portable artifact identity; they
do not describe an instance deployment. Domains, provider selection, production
Secrets, deployment, rollback, qualification, and receipts belong to the
instance owner.

The Fabric launch contract contains only the hash encoding, golden vectors, and
bounded Runtime image revision proof that both Go modules consume. The proof is
validated independently and preserves the original stage request hash. The
Workspace Runtime ABI contains only the fixed protocol and port projected by
both modules.

## Generated-Field Freshness

`npm run verify:generated-contracts` runs the covered generators against their
declared source inputs in an isolated temporary tree and compares exact output
bytes. Missing pinned tools, missing or obsolete covered outputs, and changed
bytes fail rather than silently passing. The actual verifier and generators
own the covered output set; this README does not create another field catalog.

Freshness is separate from W01 production/spec parity and consumer migration.
`events.json` is a source contract, not a regenerated output; checking derived
event identities does not prove that every source copy agrees. Canonical
proto/events/schema sources retain field ownership. Handwritten business fields
and semantic invariants require their owning consumer tests and are not frozen
by a checksum. Fixture or source-check evidence is not product runtime or
Instance qualification.

Contract development follows
[Scoped Development Contract](../../AGENTS.md#scoped-development-contract) and
[the developer entry](../../DEV_GUIDE.md#scoped-host-and-worker-entry). Existing
phase plans supply work, not a permanent authority registry; host run admission
and signed per-run receipts do not define another business schema or automatically
publish product status. Use `npm run dev:tools` and `npm run dev:run` as the host
integration entries, with CLI details finalized by the tool implementation.

## Admission

Add or retain a contract field only when:

- a current cross-module or external consumer reads it;
- the value has one authoritative owner;
- source or an existing public schema is not already the stronger owner; and
- removing deterministic enforcement would break a current compatibility,
  integrity, security, or irreversible-side-effect boundary.

Tests should exercise the consumer or public behavior. A test that only reads a
JSON file and repeats its fields does not justify the contract.
