# OPL Cloud Architecture

OPL Cloud is organized around four platform surfaces:

1. Gateway: AI model access, token management, provider routing, and usage data.
2. Console: user, organization, billing, workspace, permission, and operations
   management.
3. Workspace: managed OPL App runtime instances with isolated URLs and
   credentials.
4. Evidence: job receipts, artifact provenance, reviewer checks, and audit
   records.

```mermaid
flowchart TB
  subgraph Users
    Human[Researcher / Builder / Team]
  end

  subgraph Local
    App[OPL App]
  end

  subgraph Cloud[OPL Cloud]
    Console[OPL Console]
    Gateway[OPL Gateway]
    Workspace[OPL Workspace]
    Broker[Job Adapter]
    Evidence[Provenance Store]
    Reviewer[Reviewer Gate]
  end

  subgraph Infrastructure
    Docker[Docker Runtime]
    VM[Remote VM / GPU]
    SSH[SSH / HPC]
    Models[Frontier AI Providers]
    Storage[User Storage / Private Bucket]
  end

  Human --> App
  Human --> Console
  App --> Gateway
  Console --> Gateway
  Console --> Workspace
  Workspace --> Docker
  Console --> Broker
  Broker --> Docker
  Broker --> VM
  Broker --> SSH
  Gateway --> Models
  Broker --> Evidence
  Workspace --> Evidence
  Evidence --> Reviewer
  Evidence -. refs, metadata, receipts .-> Storage
```

## Boundary

Cloud may store refs, metadata, lineage, receipts, usage, policy, and billing
records. Sensitive source data should remain in user-controlled workspace,
institutional storage, or private buckets unless explicitly configured
otherwise.

