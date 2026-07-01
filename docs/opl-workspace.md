# OPL Workspace

OPL Workspace is a managed online OPL App environment.

Each workspace should provide an isolated access URL, account, password,
compute configuration, storage configuration, and billing package.

## User Model

The product should feel closer to a managed cloud workspace than a container
hosting panel. Users choose the OPL workspace they need; the underlying Docker
runtime, compute node, and storage location remain implementation details.

## Initial Capability

- Create a managed OPL App workspace.
- Select compute and storage configuration.
- View package-based billing with compute, storage, and AI usage breakdown.
- Open the workspace through an isolated URL.
- Reset or rotate workspace credentials.
- Start approved jobs through OPL Fabric.
- Display OPL Ledger receipts and reviewer results.

## Boundary

OPL Workspace runs OPL App online. OPL Console manages workspace lifecycle.
OPL Gateway provides AI capability access. OPL Fabric runs approved resources.
OPL Ledger records receipts and provenance. These boundaries should stay visible
in product docs and implementation contracts.
