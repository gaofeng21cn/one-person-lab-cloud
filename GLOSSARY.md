# OPL Cloud Glossary

- **User**: A Gateway-authenticated human identity, such as `testcloud4@medopl.com`. A User is not a Tenant and may hold a role in a Tenant.
- **Tenant**: The Cloud customer boundary for authorization, repository ownership, Package/Build scope, and Workspace membership. A Tenant may have multiple Users with roles such as `owner`, `admin`, or `member`.
- **Tenant owner**: The User admitted as the owner of a Tenant. Ownership is a business role, not a separate login identity or wallet replacement.
- **Tenant repository**: The immutable OCI repository binding reserved for one Tenant, currently `oplcloud/testcloud4` for the agreed test Tenant.
- **Legacy Workspace**: The existing Workspace/order retained under the Control Plane path. It is not silently converted into a new Cloud Workspace.
- **Cloud Workspace**: A Workspace created through the Tenant-scoped Cloud product flow and owned by the Workspace service.
- **Cloud chain**: The new Tenant-scoped path from Tenant admission through Cloud Workspace creation, default OPL App delivery, application use, and Ledger readback.
- **Legacy cleanup gate**: The rule that the Legacy Workspace may be deleted and refunded only after the Cloud chain has a complete PASS receipt.
- **Lane**: An independently owned vertical delivery path with its own seam, tests, receipt, and next consumer. Lanes may develop in parallel and converge at serialized production or acceptance boundaries.
