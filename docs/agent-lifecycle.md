# OPL Agent Lifecycle

OPL Cloud can host agents built by OPL Meta Agent without becoming a separate
agent-building product.

Agent use should follow the same workbench split as the rest of OPL Cloud:
OPL App is the local user surface, and OPL Workspace is the cloud Docker/WebUI
surface. Both can expose approved Agent Instances.

The standard lifecycle is:

```text
Agent Blueprint
→ Agent Package
→ OPL Agent Registry
→ Console approval
→ Fabric resource binding
→ App / Workspace Agent Instance
→ Agent Run
→ Ledger receipt
```

## Lifecycle Objects

| Object | Meaning | Owner surface |
| --- | --- | --- |
| Agent Blueprint | The agent goal, boundary, stages, inputs, outputs, review rules, environment needs, connector needs, and acceptance evidence | OPL Meta Agent |
| Agent Package | A versioned package that can be reviewed, approved, and installed for use | OPL Meta Agent and OPL Framework package flow |
| OPL Agent Registry | The approved package index used by Console, Fabric, App, and Workspace | OPL Fabric |
| Agent Instance | A package version enabled inside OPL App or OPL Workspace with specific permissions, resources, and quotas | OPL App / OPL Workspace |
| Agent Run | One task execution by an Agent Instance | OPL Fabric and OPL Ledger |

## Product Flow

1. A user or team describes the agent they need.
2. OPL Meta Agent turns the request into an Agent Blueprint.
3. OPL Meta Agent produces an Agent Package candidate with tests, review
   evidence, environment requirements, connector requirements, and owner route.
4. OPL Console approves which package version can be used by which team.
5. OPL Fabric binds the package to approved compute, storage, environments, and
   connectors.
6. OPL App or OPL Workspace creates an Agent Instance for a project or user.
7. Users run the agent from App or Workspace.
8. OPL Ledger records the plan, approval, execution, inputs, outputs, review
   result, and continuation reference.

## Deployment Model

Agent deployment should make these decisions explicit before an Agent Instance
is available:

- package version;
- owner and support route;
- approved teams or workspaces;
- required compute profile;
- required storage profile;
- required connectors;
- required environment;
- review gates;
- quota policy;
- Ledger policy.

## Boundary

OPL Meta Agent builds and improves agent packages. OPL Console approves use.
OPL Fabric prepares and runs the required resources. OPL App and OPL Workspace
are where users use the agent. OPL Ledger records what happened.

This keeps agent creation, deployment, management, execution, and evidence in
one OPL Cloud path without creating a second platform.
