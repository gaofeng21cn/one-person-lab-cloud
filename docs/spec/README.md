# Machine API And Publisher Contracts

Human-readable product rules, ownership boundaries and settlement invariants
live in [architecture](../architecture.md), [invariants](../invariants.md),
[opl-workspace](../opl-workspace.md) and [opl-serve](../opl-serve.md). Narrative
design/work-package snapshots are retired to Git history.

This directory retains only the active machine-consumed schema sources:

- OpenAPI HTTP contract: [target/03_api_contract_complete.yaml](./target/03_api_contract_complete.yaml)
- Publisher JSON Schema: [target/contracts/publisher-contract.schema.json](./target/contracts/publisher-contract.schema.json)
- Production proto and generated Go bindings: [`packages/contracts/proto/`](../../packages/contracts/proto/) and [`packages/contracts/go/`](../../packages/contracts/go/)

Use [contract ownership](../../packages/contracts/README.md) for generation
rules and consumer verification (`tools/verify-generated-contracts.ts`). Prose
spec updates alone do not modify wire APIs or database schemas.
