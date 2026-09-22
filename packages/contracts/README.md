# OPL Cloud Contracts

Keep a machine contract only when two current owners or an external consumer
must agree on bytes or a stable public identity. Runtime behavior, internal
fields, retired alternatives, current progress, and implementation layout stay
in source, focused tests, or documentation.

The `go/` module is a build-time dependency of Control Plane, Fabric and Ledger,
and its shared runtime types are consumed by Control Plane and Fabric.
It owns their shared stage/status, resource, provider, operation and protocol
types. Keep service-local types in the service; a test fixture alone does not
justify adding a cross-owner type.

## v2.26 Contract Layout

The single Cloud GitHub repository retains one shared Go contracts module at
`packages/contracts/go/go.mod`. W01 places production proto source in
`packages/contracts/proto/` and generated Go bindings in
`packages/contracts/go/v226/`; that generated package is not another Go module.
The v2.26 schema under `docs/spec/v2.26/contracts/` remains the specification
owner. W01 must verify the production schema against that owner, lock generation
tools and record the schema hash; merely adding this layout does not complete
W01 or implement its consumers.

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

The Candidate and Distribution contracts bind portable artifact identity; they
do not describe an instance deployment. Domains, provider selection, production
Secrets, deployment, rollback, qualification, and receipts belong to the
instance owner.

The Fabric launch contract contains only the hash encoding, golden vectors, and
bounded Runtime image revision proof that both Go modules consume. The proof is
validated independently and preserves the original stage request hash. The
Workspace Runtime ABI contains only the fixed protocol and port projected by
both modules.

## Admission

Add or retain a contract field only when:

- a current cross-module or external consumer reads it;
- the value has one authoritative owner;
- source or an existing public schema is not already the stronger owner; and
- removing deterministic enforcement would break a current compatibility,
  integrity, security, or irreversible-side-effect boundary.

Tests should exercise the consumer or public behavior. A test that only reads a
JSON file and repeats its fields does not justify the contract.
