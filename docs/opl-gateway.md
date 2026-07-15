# OPL Gateway

OPL Gateway is the frontier AI capability gateway for One Person Lab.

It provides a unified access point for AI APIs, token management, provider
configuration, usage metering, and downstream OPL workflows.

## Role In OPL Cloud

OPL Gateway is the first available OPL Cloud component and remains a top-level
product surface. It supports:

- OPL App AI capability access.
- Codex and automation workflow integration.
- OPL Serve invocation and session model access.
- Usage visibility and quota management.
- A stable foundation for Console billing and account policy.

## Usage Model

Gateway usage should be groupable by:

- account;
- user, publisher service or consumer identity;
- Workspace;
- Agent Service, Revision, Deployment and Invocation/Session;
- task or job;
- exact OPL Package ref or Agent Instance;
- provider and model.

This gives Console enough information for quotas, budgets, service/package
attribution and downstream reporting without making Gateway responsible for
package lifecycle, service deployment or industry-specific policy.

Gateway is technically a resource access capability, but it should not be
hidden inside OPL Fabric. Users can directly configure it, use it, meter it, and
pay for it.

## Public Surface

The current public surface is integration-oriented rather than a standalone
public implementation repository. Public scripts and setup instructions can be
linked from this repository until a dedicated OPL Gateway repo exists.

## Positioning

OPL Gateway should be described as OPL Cloud's AI capability foundation, not as
a generic token platform.

## Boundary With Fabric

OPL Fabric owns general connectors, compute, storage, environments, and
execution adapters. OPL Gateway owns frontier AI access, provider policy, model
routing, keys, and usage metering. OPL Serve owns the external Agent endpoint;
it does not turn Gateway into the Agent Service control plane.
