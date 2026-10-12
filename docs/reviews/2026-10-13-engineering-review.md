# OPL Cloud Engineering Review

**Review Date**: 2026-10-13  
**Commit**: 05c7b168 (main branch, clean working tree)  
**Reviewer**: Claude Opus 5-5 via /plan-eng-review  
**Scope**: Full architecture audit (11 services, 870 Go files, 419 tests)

---

## Executive Summary

**Architecture Status**: ✅ **SOUND** with deployment gaps

The OPL Cloud architecture demonstrates strong separation of concerns, defense-in-depth security, and robust failure handling at the source level. Physical boundaries are enforced at build time, database isolation is strict, and idempotent coordination patterns handle distributed failures correctly.

**Critical Gap**: No Instance deployment or runtime qualification yet. Source tests pass (`verify:local` green), but the complete Browser→BFF→Service→Provider→Readback flow remains unverified in a real Tencent TKE environment.

### Key Findings

**✅ Architectural Strengths**:
1. Build-time boundary enforcement (module-physical-boundaries.test.ts)
2. Defense-in-depth security (CSRF + origin + idempotency on all writes)
3. 11 isolated PostgreSQL databases with unique 32+ char passwords
4. Robust failure handling (advisory locks, idempotency, provider convergence)
5. Exceptionally low technical debt (3 TODOs in 870 files)

**⚠️  Open Boundaries**:
1. **Instance Deployment** (HIGH): No Candidate/Instance/TKE qualification
2. **Sub2API Integration** (MEDIUM): HTTP fixture only, no live authority
3. **Serve Execution** (MEDIUM): TKE qualification pending
4. **Control Plane Migration** (LOW): Legacy callers not yet retired

### Service Topology

12 independent bounded contexts:
- **Console BFF**: Browser REST + gRPC aggregation
- **CloudIdentity/Tenant**: Session + authorization (1 process, 2 DBs)
- **Gateway Integration**: Managed keys + Sub2API coordination
- **Capability/Build/Runtime Control**: Package catalog + OCI + releases
- **Workspace/Resource Catalog/Serve**: Entitlement + pricing + deployment
- **Fabric**: Provider resources + Secret binding
- **Ledger**: Receipts + reconciliation
- **Control Plane**: Retained migration source (not target)

### Recommendations Summary

**Immediate** (unblock first launch):
1. Construct exact Candidate from SHA
2. Deploy to Instance and return readback
3. Qualify complete Browser→TKE→Readback flow

**Short-term** (next 30 days):
4. Complete Sub2API live integration
5. Finish Serve execution boundary
6. Retire Control Plane callers

**Long-term** (next quarter):
7. Runtime Secret delivery channel
8. Medical platform qualification

**Timeline**: 2-4 weeks for Candidate→Instance→Readback, assuming no major defects.

---

## Table of Contents

1. [Architecture Review](#1-architecture-review)
2. [Code Quality Review](#2-code-quality-review)
3. [Test Coverage Review](#3-test-coverage-review)
4. [Performance Review](#4-performance-review)
5. [Appendix: Detailed Analysis](#appendix-detailed-analysis)

---


## 1. Architecture Review

**Overall Assessment**: ✅ **SOUND**

The OPL Cloud architecture demonstrates strong separation of concerns with 12 independent bounded contexts, each owning its database and domain logic. Physical boundaries are enforced at build time via `module-physical-boundaries.test.ts`, preventing cross-service implementation coupling.

### Key Architectural Patterns

**Database Isolation**: 11 separate PostgreSQL databases with unique 32+ character passwords per service. No cross-owner SQL queries allowed.

**Security Boundaries**:
- CSRF validation with constant-time comparison
- Same-origin enforcement (Sec-Fetch-Site checks)
- Idempotency key requirement on all writes
- Defense-in-depth: BFF + service authorization

**Coordination Patterns**:
1. **Idempotent Commands**: Advisory locks (`pg_advisory_xact_lock`) + fingerprint validation
2. **Staged Launch**: Preflight admission → provider mutation → authoritative readback
3. **Owner Readback**: Provider is source of truth, idempotency survives lost responses

### Findings

✅ **Strengths**:
- Build-time boundary enforcement prevents architectural drift
- Robust failure handling with advisory locks and idempotency
- No raw secret storage (only opaque references)
- Clear module structure with domain-neutral shared helpers

⚠️ **Concerns**:
- **Instance Deployment Gap** (HIGH): No Candidate/Instance/TKE qualification
- Control Plane retained as migration source (adds complexity)
- No shared idempotency library (acceptable for domain isolation)

See [Appendix: Section 1 - Architecture](#appendix-section-1-architecture) for detailed analysis including data flow traces, failure modes, and architecture diagrams.

---

## 2. Code Quality Review

**Overall Assessment**: ✅ **HIGH**

Exceptionally low technical debt (3 TODOs in 870 Go files = 0.3% rate). Strong typing via gRPC contracts and TypeScript strict mode. Consistent error handling across all layers.

### Code Organization

Clear domain boundaries with shared helpers isolated in `services/internal/`. Naming conventions consistently follow Go/TypeScript idioms:
- Go packages: lowercase, no underscores
- Database tables: snake_case with owner prefix
- gRPC services: PascalCase with ProductService suffix
- Errors: ErrPrefix convention

### Security Code Patterns

**Input Validation**:
- Constant-time comparison for secrets (`subtle.ConstantTimeCompare`)
- Explicit length checks on all inputs
- Fingerprint validation (64-char hash requirement)

**Authorization**:
- Defense in depth: BFF validates, services re-validate
- Never trust browser-asserted tenant/price/provider/wallet identity

**Secret Handling**:
- Only opaque references stored (ExternalKeyID, KeyBindingID, SecretRef)
- Raw key material never appears in state

### Findings

✅ **Strengths**:
- Security-first patterns throughout
- Strong typing prevents entire classes of bugs
- Clear error handling with typed errors, gRPC status codes, HTTP codes

⚠️ **Concerns**:
- No shared idempotency library (pattern repeated across services)
- Migration path complexity (Control Plane retained)

---

## 3. Test Coverage Review

**Overall Assessment**: ⚠️ **ADEQUATE FOR SOURCE**

419 test files with comprehensive source coverage. `verify:local` qualification gate passes. However, critical gaps exist at the Instance deployment level.

### Covered Areas

✅ **Service Boundaries**: `module-physical-boundaries.test.ts` enforces physical isolation
✅ **Authorization**: CSRF, same-origin, idempotency, per-owner/action/resource authorization
✅ **Database Isolation**: PostgreSQL owner tests for Workspace, Serve, Gateway, Fabric
✅ **Browser Flows**: 8 concurrent Playwright suites for workspace lifecycle, billing, gateway
✅ **Idempotency**: Advisory lock behavior, fingerprint validation, terminal answer immutability

### Coverage Gaps

❌ **Instance Deployment**: No Candidate construction, Instance deployment, TKE runtime, DNS/TLS, or real browser access tests
❌ **Sub2API Integration**: Uses HTTP fixture only, no live Sub2API tests
❌ **Serve Execution**: No TKE route activation or real application execution tests
❌ **Ledger Reconciliation**: No real charge reconciliation or wallet mutation tests

### Test Quality

**Strengths**:
- Clear test names describing behavior
- Isolated fixtures (each test owns setup)
- Fast feedback loop (verify:local runs in minutes)

**Concerns**:
- Environment dependencies (Docker daemon, Playwright, PostgreSQL)
- No Instance-level smoke tests

---

## 4. Performance Review

**Overall Assessment**: ⚠️ **UNKNOWN AT SCALE**

No performance benchmarks, load tests, or production evidence. Architecture is adequate for current scale but has identifiable bottlenecks.

### Performance Characteristics

**Request Latency** (estimated):
- Session validation: ~10ms
- Authorization check: ~5ms
- gRPC owner call: ~50-500ms
- Total: ~65-515ms per request

**Provider Operations**:
- TKE resource creation: ~10-60s
- Docker container creation: ~5-30s
- Model configuration apply: 60s timeout

### Scalability

**Horizontal Scaling**:
- Console BFF and services are stateless (can scale behind load balancer)
- Advisory locks coordinate across instances

**Bottlenecks**:
1. PostgreSQL write throughput (11 services, 1 database instance)
2. Provider API rate limits
3. Sequential BFF authorization (no batching/caching)
4. Synchronous model configuration (60s blocking)

### Optimization Opportunities

**Low Risk**:
- Session caching (5min TTL)
- Connection pooling

**Medium Risk**:
- Authorization caching (1min TTL)

**High Risk**:
- Provider readback caching (stale state risk)

### Findings

✅ **Adequate for Development**: verify:local runs in minutes, 419 tests execute without timeout

⚠️ **Unknown for Production**: No load testing, capacity planning, real traffic patterns, or Instance performance baselines

🔴 **Risks**:
- No resource limits configured (services can exhaust host)
- No caching on hot paths
- No performance SLOs defined

### Recommendations

**Before First Launch**:
- Add resource limits (CPU/memory per service)
- Configure connection pools
- Define performance SLOs (p50/p95/p99 targets)
- Add basic metrics (request rate, error rate, latency)

**After First Launch**:
- Load testing and capacity planning
- Session/authorization caching
- PostgreSQL read replicas
- Async operation patterns for long-running mutations

---

## Final Assessment

**Source-Level Readiness**: ✅ **PRODUCTION-READY**

The OPL Cloud architecture is production-ready at the source level with:
- Strong boundaries enforced at build time
- Defense-in-depth security (CSRF + origin + idempotency)
- Robust failure handling (advisory locks, idempotency, convergence)
- High code quality (3 TODOs in 870 files)
- Comprehensive source test coverage (419 tests)

**Deployment Readiness**: 🔴 **BLOCKED**

The critical blocker is **Instance deployment**:
- Source tests pass, but complete flow unverified in real TKE environment
- No Candidate construction or Instance qualification
- No real browser access verification
- No live Sub2API or Ledger reconciliation tests

**Timeline to Production**: 2-4 weeks for Candidate→Instance→Readback cycle, assuming no major integration defects.

---

## Appendix: Detailed Analysis

### Appendix Section 1: Architecture

# Section 1: Architecture Review

## 1.1 System Boundaries

### Service Topology (12 Independent Domains)
```
Browser Console (apps/console-ui)
    ↓ /api/v2/* (same-origin)
Console BFF (apps/console-bff)
    ↓ gRPC typed owner APIs
┌─────────────────────────────────────────────────────────────┐
│ Independent Services (each: own process, DB, authorization) │
├─────────────────────────────────────────────────────────────┤
│ CloudIdentity/Tenant    │ Gateway Integration (1 process)  │
│ Capability              │ Build                             │
│ Runtime Control         │ Workspace                         │
│ Resource Catalog        │ Serve                             │
│ Fabric                  │ Ledger                            │
├─────────────────────────────────────────────────────────────┤
│ Control Plane (retained migration source, not target)       │
└─────────────────────────────────────────────────────────────┘
    ↓ provider SDKs (Fabric only)
Tencent TKE / Docker (provider boundary)
```

### Database Isolation
Each service has dedicated PostgreSQL database + role:
- `cloud_identity_user` → `cloud_identity`
- `gateway_user` → `gateway`
- `capability_user` → `capability`
- `build_user` → `build`
- `runtime_control_user` → `runtime_control`
- `workspace_user` → `workspace`
- `resource_catalog_user` → `resource_catalog`
- `serve_user` → `serve`
- `fabric_user` → `fabric`
- `ledger_user` → `ledger`
- `control_plane_user` → `control_plane` (retained)

**Boundary enforcement**: compose.yaml:166-180 requires 32+ char unique passwords per role.

### Module Physical Boundaries (Enforced)
Source: tests/contracts/module-physical-boundaries.test.ts
- No Go service may import another service implementation
- Only Fabric imports Tencent/Kubernetes SDKs
- Console network calls stay inside API adapter, resolve to /api/*
- Runs through `npm test` lane

## 1.2 Data Flow Traces

### Flow 1: Browser → BFF → gRPC → DB (Publisher Route)

**1. Browser initiates request**
```typescript
// apps/console-ui → /api/v2/workspaces (same-origin)
fetch('/api/v2/workspaces', {
  headers: { 
    'X-CSRF-Token': session.csrfToken,
    'Idempotency-Key': uuid(),
    'Origin': window.location.origin
  }
})
```

**2. Console BFF validates security boundaries**
```go
// apps/console-bff/internal/httpapi/publisher.go:52-66
- Validates CSRF token (constant-time comparison)
- Verifies same-origin (Sec-Fetch-Site + Origin header)
- Requires Idempotency-Key (1-256 chars)
- Extracts session via CloudIdentity
```

**3. BFF routes to typed gRPC owner**
```go
// publisher.go:26-27
NewPublisherHandler(
  capability api.CapabilityProductServiceClient,
  runtimeControl api.RuntimeControlProductServiceClient,
  build api.BuildProductServiceClient,
  identity IdentityReader
)
```

**4. Service authorizes and executes**
```go
// packages/contracts/proto/internal.proto:11-20
message CallContext {
  string request_id = 1;
  string idempotency_key = 2;
  string authorization_context_id = 3;
  string actor_id = 4;
  AuthorizationScope scope = 6;
}
```

**5. PostgreSQL isolation**
- Each service writes to own database
- No cross-owner SQL (proto comment line 10)
- Migration helper is domain-neutral (tests/contracts/module-physical-boundaries.test.ts:65-68)

**Boundary enforcement evidence**:
- ✅ Physical isolation: module-physical-boundaries.test.ts passes (npm test)
- ✅ Network isolation: Console fetch calls restricted to /api/* (line 99)
- ✅ SDK isolation: Only Fabric imports cloud SDKs (line 58)
- ✅ Session security: CSRF + same-origin + idempotency (publisher.go:52-66)

### Flow 2: Gateway Managed Key → Fabric Secret → Serve Handover

**1. Gateway Integration issues managed key**
```go
// services/gateway-integration/identity/managed_key_command.go:82-100
ReserveManagedKeyCommand(ctx, ManagedKeyCommandReservation{
  TenantID, ActorID, WorkspaceID, IdempotencyKey, CommandFingerprint
})
// Returns: KeyBindingID, SecretRef, ResolvedModelIDs, ExpiresAt
// Raw key NEVER stored (line 61)
```

**2. Fabric coordinates Secret binding**
```
Status evidence: status.md:65
- Serve is sole approved caller
- Launch freezes Gateway key handover
- Serve confirms initial bind through Fabric
- One active binding preserved
```

**3. Serve applies model configuration**
```go
// services/serve/internal/tkeapply/modelconfiguration.go:36-57
Protocol: "opl-model-config/v1"
ApplyPath: "/model-config/apply"
ReadbackPath: "/model-config/readback"
Authorization: via declared secret input
Timeout: 60s
```

**Coordination boundary evidence**:
- ✅ Key issuance idempotency: pg_advisory_xact_lock (managed_key_command.go:93)
- ✅ No raw key storage: managedKeyCommandState carries only IDs (line 59-73)
- ✅ Serve-only binding: explicit in status.md:65
- ✅ Model readback verification: Serve records application's readback (status.md:66)

### Flow 3: Workspace Lifecycle (Entitlement → Deployment → Access)

**1. Workspace owns entitlement**
- Accepts order, Saga state, configuration intent
- No deployment or provider authority

**2. Resource Catalog owns pricing**
- Approved plans and policies
- Quote acceptance

**3. Serve owns deployment/access**
```go
// services/serve/internal/tkeapply/runtime.go
- Resolves frozen publisher contract
- Applies configuration through declared interface
- Records readiness and access route
- Returns owner-authoritative status
```

**4. Fabric owns provider resources**
```go
// services/fabric/internal/fabric/workspace_launch_stage_engine.go:53-100
- Validates launch stage binding (line 68-100)
- Coordinates provider mutations
- Returns authoritative readback
- Never owns deployment continuation
```

**Ownership boundary evidence**:
- ✅ Workspace entitlement: status.md:50-53
- ✅ Serve deployment: implementation-architecture.md:176
- ✅ Fabric provider only: implementation-architecture.md:178
- ✅ No overlapping authority: decisions.md:1-50

## 1.3 Security Boundaries

### Authentication & Authorization
```
CloudIdentity/Gateway Integration (services/gateway-integration)
├─ Tenant scope & session policy
├─ Member authorization
├─ Accepted-operation grants
└─ Gateway managed keys (Sub2API coordination)

Console BFF (apps/console-bff/internal/httpapi/publisher.go)
├─ CSRF token validation (line 53-55)
├─ Same-origin enforcement (line 57-60)
├─ Idempotency key requirement (line 62-66)
└─ Session extraction & authorization check (line 92-94)
```

**Security enforcement evidence**:
- ✅ CSRF: Constant-time comparison (subtle.ConstantTimeCompare)
- ✅ Origin: Sec-Fetch-Site + Origin header validation
- ✅ Idempotency: Required on all writes, 1-256 chars
- ✅ Authorization: Per-owner, per-action, per-resource (line 82-94)

### Database Isolation
```
PostgreSQL (compose.yaml:166-180)
├─ 11 independent databases
├─ 11 dedicated user roles
├─ Password requirements: 32+ chars, unique per role
└─ No shared credentials
```

**Isolation enforcement evidence**:
- ✅ Schema isolation: Each service owns its tables
- ✅ Credential isolation: Unique passwords per role
- ✅ Migration isolation: postgresmigrate is domain-neutral
- ✅ No cross-owner SQL: proto contract comment (internal.proto:10)

### Secret Management
```
Gateway Integration
├─ Issues managed keys via Sub2API service route
├─ Records binding ID + resolved model scope
└─ Never stores raw key material

Fabric
├─ Coordinates Secret delivery
├─ Accepts only from Serve (sole approved caller)
└─ Binds via PutManagedSecret

Serve
├─ Freezes Gateway key handover on launch
├─ Confirms binding through Fabric
└─ Applies Secret to application via declared interface
```

**Secret boundary evidence**:
- ✅ No raw key storage: managedKeyCommandState JSON (managed_key_command.go:59-73)
- ✅ Serve-only binding: status.md:65
- ✅ Predecessor-checked rebind: RebindSecret is Serve-owned
- ✅ Authorization Secret input: modelconfiguration.go:80

## 1.4 Cross-Service Coordination Patterns

### Pattern 1: Idempotent Command Reservation
```go
// Gateway managed key example
ReserveManagedKeyCommand:
1. Advisory lock on tenant+idempotency_key
2. Check existing command by fingerprint
3. Return existing OR reserve pending
4. External effect (Sub2API)
5. Confirm OR reject terminal answer
```

**Idempotency evidence**:
- ✅ Advisory locks: pg_advisory_xact_lock
- ✅ Fingerprint validation: 64-char SHA
- ✅ Terminal answer immutability: never re-derived
- ✅ Conflict detection: ErrManagedKeyCommandConflict

### Pattern 2: Staged Launch Coordination
```go
// Fabric launch stage engine
workspace_launch_stage_engine.go:68-100:
1. Validate input against preflight admission
2. Check provider profile + binding ref
3. Verify spec digest + request hash
4. Coordinate provider mutations
5. Return authoritative readback
```

**Launch coordination evidence**:
- ✅ Preflight validation: admission stored before effects
- ✅ Binding conflict detection: ErrLaunchStageBindingConflict
- ✅ Provider isolation: Fabric only (no Build/Serve authority)
- ✅ Immutable admission: frozen before provider mutations

### Pattern 3: Owner Readback Authority
```
Serve model configuration:
1. Freeze publisher contract from Runtime Release
2. Apply via declared HTTP interface
3. Read application's own readback
4. Record ONLY application's answer
5. Refuse if readback doesn't match requested version
```

**Readback authority evidence**:
- ✅ Application is source of truth: modelconfiguration.go:48-56
- ✅ No synthetic versions: ErrModelConfigurationUnconfirmed
- ✅ Conflict detection: ErrModelConfigurationConflict
- ✅ 60s timeout: lost response = unconfirmed


## 1.5 Failure Modes & Recovery

### Failure Mode 1: Lost Response After External Effect

**Scenario**: Gateway issues managed key to Sub2API, response lost, retry arrives
```go
// managed_key_command.go handles this:
1. Advisory lock prevents concurrent issue
2. Original command reserved with fingerprint
3. Pending answer (202) stored before external call
4. Retry reads existing pending answer
5. Never re-issues to Sub2API
```

**Recovery**: Idempotency key lookup returns original pending command

**Gap**: Sub2API has no idempotent lookup by original issue identity (status.md:64)
- Current: Pending answer never re-dispatched
- Open: Need Sub2API idempotent readback to confirm/reject

### Failure Mode 2: Database Unavailable During Write

**Scenario**: PostgreSQL connection fails mid-operation
```
Service behavior:
- Advisory locks released on connection loss
- Transaction rolled back
- Client retries with same idempotency key
- Owner reads existing terminal answer OR re-executes
```

**Recovery**: Idempotency table survives connection loss

**Evidence**: Each service has health check (compose.yaml:28-37)

### Failure Mode 3: Service Crash After Provider Mutation

**Scenario**: Fabric crashes after creating TKE resource, before recording
```go
// workspace_launch_stage_engine.go:53-100
1. Preflight admission stored BEFORE provider mutation
2. Crash before recording mutation
3. Restart: preflight exists, mutation may/may not exist
4. Provider readback determines actual state
5. Convergence based on authoritative provider answer
```

**Recovery**: Provider is source of truth, Fabric converges to it

**Evidence**: 
- Preflight stored first (line 85-86)
- Provider mutations store (line 45)
- Machine ownership store (line 46)

### Failure Mode 4: Cross-Service Coordination Failure

**Scenario**: Serve calls Fabric Secret bind, Fabric succeeds, Serve crashes before recording
```
Serve perspective: No binding confirmed
Fabric perspective: Binding exists

Recovery:
1. Serve retries with predecessor check
2. Fabric RebindSecret requires predecessor identity
3. Mismatch detected → conflict
4. Serve must resolve: keep existing OR explicit revoke
```

**Evidence**: Predecessor-checked rebind (status.md:65)

### Failure Mode 5: Concurrent Operation Conflict

**Scenario**: Two actors try to update Workspace model configuration simultaneously
```go
BFF guards:
1. Each request has unique idempotency key (publisher.go:62)
2. Workspace operation uses optimistic locking
3. Second request sees changed version → conflict
4. Client decides: retry with new version OR abort
```

**Recovery**: Optimistic locking + idempotency prevents lost update

### Failure Mode 6: Cascading Service Unavailability

**Scenario**: Gateway Integration down, Console cannot create Workspace
```
Impact:
- Console BFF: 503 owner_unconfigured (publisher.go:96-98)
- Browser: Clear error, no state mutation
- Database: No partial writes (transaction semantics)

Recovery:
- User retries after Gateway restoration
- Idempotency prevents duplicate on retry
```

**Evidence**: Explicit unavailable check before routing (line 96-98)

## 1.6 Architecture Diagrams

### System Context Diagram
```
┌─────────────────────────────────────────────────────────────┐
│                         OPL Cloud                            │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  Browser ──┐                                                 │
│            ├─ Console UI (apps/console-ui)                   │
│            │  ├─ React 19.3.0                                │
│            │  ├─ Vite 8.3.1 build                            │
│            │  └─ /api/v2/* same-origin calls                 │
│            │                                                  │
│            └─ Console BFF (apps/console-bff)                 │
│               ├─ HTTP REST publisher handler                 │
│               ├─ CSRF + origin + idempotency guards          │
│               └─ gRPC typed owner clients                    │
│                                                               │
│  ┌────────────────── Independent Services ──────────────┐   │
│  │                                                        │   │
│  │  CloudIdentity/Tenant  Gateway Integration            │   │
│  │  ├─ Session/member     ├─ Managed keys               │   │
│  │  └─ Authorization      └─ Sub2API coordination       │   │
│  │                                                        │   │
│  │  Capability            Build            Runtime Ctrl  │   │
│  │  ├─ Package catalog    ├─ BuildKit     ├─ Releases   │   │
│  │  └─ Admission          └─ OCI registry └─ ABI facts  │   │
│  │                                                        │   │
│  │  Workspace             Resource Catalog    Serve      │   │
│  │  ├─ Entitlement        ├─ Plans          ├─ Deploy   │   │
│  │  ├─ Saga state         └─ Pricing        ├─ Access   │   │
│  │  └─ Config intent                        └─ Readback │   │
│  │                                                        │   │
│  │  Fabric                Ledger                         │   │
│  │  ├─ Provider resources ├─ Receipts                    │   │
│  │  ├─ Secret binding     └─ Reconciliation              │   │
│  │  └─ Mutations                                          │   │
│  │                                                        │   │
│  │  Control Plane (retained migration source)            │   │
│  │  └─ Legacy callers not yet migrated                   │   │
│  │                                                        │   │
│  └────────────────────────────────────────────────────────┘   │
│                          ↓                                    │
│                    PostgreSQL 16.8                            │
│                    11 isolated databases                      │
│                                                               │
└─────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────────────────┐
              │   Provider Boundary   │
              ├───────────────────────┤
              │  Tencent TKE          │
              │  Docker (local dev)   │
              │  Kubernetes           │
              └───────────────────────┘
                          ↓
              ┌───────────────────────┐
              │   External Services   │
              ├───────────────────────┤
              │  Sub2API (wallet/key) │
              │  OCI Registry         │
              └───────────────────────┘
```

### Data Flow Sequence: Workspace Launch
```
User Browser
    │
    │ POST /api/v2/workspaces (CSRF + Origin + Idempotency-Key)
    ↓
Console BFF
    │ RequireSession(CloudIdentity)
    │ RequireAuthorizedAction(Workspace, CREATE)
    ↓
Workspace Service
    │ Accept order
    │ Record Saga state
    │ Call Resource Catalog for quote
    ↓
Resource Catalog
    │ Validate plan
    │ Return quote
    ↓
Workspace Service
    │ Confirm order
    │ Call Gateway for managed key
    ↓
Gateway Integration
    │ Reserve command (advisory lock)
    │ Call Sub2API service route
    │ Record binding ID + resolved models
    ↓
Workspace Service
    │ Call Serve for deployment
    ↓
Serve Service
    │ Freeze Gateway key handover
    │ Call Fabric for provider resources
    ↓
Fabric Service
    │ Validate launch stage binding
    │ Mutate provider (TKE/Docker)
    │ Confirm Secret bind (from Serve only)
    │ Return authoritative readback
    ↓
Serve Service
    │ Apply model configuration (if declared)
    │ Wait for application readiness
    │ Record access route
    │ Return deployment status
    ↓
Workspace Service
    │ Record Ledger receipt
    │ Return Operation to BFF
    ↓
Console BFF
    │ Return JSON to browser
    ↓
User Browser
    │ Poll operation until complete
    │ Navigate to Workspace detail
```

### Authorization Flow
```
Browser Request
    │
    │ Headers: X-CSRF-Token, Idempotency-Key, Origin
    ↓
Console BFF (publisher.go:36-94)
    │
    ├─ RequireSession(CloudIdentity)
    │  └─ Validates session token
    │     Extracts actor_id, tenant_id, permissions
    │
    ├─ CSRF validation (constant-time)
    │
    ├─ Same-origin check (Sec-Fetch-Site + Origin)
    │
    ├─ Idempotency key required (1-256 chars)
    │
    └─ RequireAuthorizedAction(owner, action, resource)
       │
       └─ CloudIdentity validates:
          - Actor has permission for action
          - Action allowed on resource kind
          - Tenant scope matches (if tenant-scoped)
          - Accepted-operation grant (if required)
              ↓
       gRPC CallContext to owner service
       │
       └─ Service re-validates tenant/actor scope
          Never trusts browser-asserted identity
```

## 1.7 Architecture Findings

### ✅ Strengths

1. **Physical isolation enforced at build time**
   - module-physical-boundaries.test.ts prevents cross-service imports
   - Only Fabric allowed to import cloud SDKs
   - Console restricted to /api/* calls
   - Runs in `npm test` lane — breaks build if violated

2. **Defense in depth for write operations**
   - CSRF token (constant-time comparison)
   - Same-origin validation (Sec-Fetch-Site + Origin)
   - Idempotency key required (prevents replay)
   - Per-owner authorization (BFF + service validate)

3. **Database isolation with strong credentials**
   - 11 independent databases
   - Unique 32+ char passwords per role
   - No shared credentials or cross-owner SQL
   - Migration helper is domain-neutral

4. **Idempotent coordination patterns**
   - Advisory locks prevent concurrent effects
   - Fingerprint validation prevents command substitution
   - Terminal answers immutable
   - Provider is source of truth for convergence

5. **Secret hygiene**
   - Raw keys never stored (only binding IDs)
   - Serve-only Secret binding (sole approved caller)
   - Predecessor-checked rebind
   - Authorization via declared secret input

### ⚠️  Concerns

1. **Instance deployment gap** (status.md:129-146)
   - Source qualification passes
   - No Candidate construction yet
   - No Instance deployment evidence
   - No runtime/route/billing readback from real environment
   - **Impact**: Cannot verify TKE execution, DNS/TLS, real browser access

2. **Sub2API integration incomplete** (status.md:64, 106-112)
   - Gateway uses HTTP contract fixture, not live authority
   - Service route qualification open
   - Instance secret wiring unverified
   - No idempotent readback for lost responses
   - **Impact**: Cannot recover from lost managed-key responses

3. **Control Plane caller migration** (status.md:60, 124)
   - Console model-configuration still uses legacy path
   - Source ownership settled, caller retirement not
   - Two parallel paths during migration
   - **Impact**: Testing both paths, eventual legacy removal needed

4. **Serve execution boundary** (status.md:66-67, 113-116)
   - Old Fabric adapter path in retained caller set
   - Typed application-execution boundary incomplete
   - Route activation performer open
   - TKE qualification pending
   - **Impact**: Cannot qualify access owner on TKE yet

5. **Runtime Secret delivery** (status.md:120)
   - Gateway-managed input works
   - Non-Gateway declared Secret inputs refused
   - Runtime Secret delivery channel incomplete
   - **Impact**: Applications with custom auth Secrets blocked

### 🔴 Risks

**High**: No production evidence for complete flow
- Source tests pass, but no Instance/Candidate/Release yet
- Critical path Browser→BFF→Service→Provider→Readback unverified in real environment
- Medical platform target (decisions.md:1-50) has no clinical workflow qualification
- **Mitigation needed**: Candidate construction + Instance deployment + runtime readback

**Medium**: External dependency fragility
- Sub2API fixture-only testing
- No idempotent recovery for lost external responses
- **Mitigation needed**: Live Sub2API qualification + idempotent readback

**Low**: Migration path complexity
- Two parallel paths (Control Plane legacy + new owners)
- Increases test surface, eventual removal needed
- **Mitigation**: Documented as retained source, clear non-target status


### Appendix Section 2-4: Code Quality, Tests, Performance

# Section 2: Code Quality Review

## 2.1 Code Organization

### Module Structure
```
apps/
├── console-ui/          TypeScript React UI (Vite 8.3.1)
├── console-bff/         Go HTTP + gRPC aggregation
services/
├── gateway-integration/ Go (CloudIdentity + Gateway)
├── capability/          Go (Package catalog)
├── build/               Go (BuildKit + OCI)
├── runtime-control/     Go (Runtime releases)
├── workspace/           Go (Entitlement + Saga)
├── resource-catalog/    Go (Plans + pricing)
├── serve/               Go (Deployment + access)
├── fabric/              Go (Provider resources)
├── ledger/              Go (Receipts + reconciliation)
├── control-plane/       Go (retained migration source)
└── internal/
    ├── postgresmigrate/ Domain-neutral migration helper
    ├── ownerservice/    Shared owner service patterns
    ├── ownerstore/      Shared owner store patterns
    └── protectedresource/ Shared resource protection
packages/
└── contracts/
    ├── proto/           gRPC service definitions
    ├── go/              Generated Go bindings
    └── *.json           Cross-owner byte contracts
```

**Evidence**: Clear domain boundaries, shared helpers isolated in services/internal/

### Naming Conventions
- **Go packages**: lowercase, no underscores (workspace, fabric, ledger)
- **TypeScript**: camelCase for functions, PascalCase for components
- **Database tables**: snake_case with owner prefix (workspace.workspaces, fabric.machines)
- **gRPC services**: PascalCase with ProductService suffix (CapabilityProductService)
- **Errors**: ErrPrefix convention (ErrLaunchStageBindingNotFound)

**Consistency**: Strong adherence to Go/TypeScript idioms

### Technical Debt Indicators
- **TODO markers**: Only 3 in 870 Go files (0.3% rate)
- **FIXME markers**: 0
- **HACK markers**: 0
- **XXX markers**: 0

**Assessment**: Exceptionally low technical debt

## 2.2 Code Quality Metrics

### Complexity
- **Services**: 11 independent + 1 retained = moderate coupling
- **Cross-service calls**: Explicit via gRPC contracts (no implicit coupling)
- **Database queries**: Isolated per service (no cross-owner joins)
- **External dependencies**: Sub2API (wallet/key), OCI Registry, TKE/Docker

**Complexity sources**:
1. Multi-service coordination (Gateway→Fabric→Serve)
2. Idempotency + advisory locks (distributed state)
3. Provider convergence (Fabric authoritative readback)
4. Legacy migration path (Control Plane retained)

### Error Handling
```go
// Pattern 1: Typed errors with context
var ErrLaunchStageBindingNotFound = errors.New("launch_stage_binding_not_found")
var ErrManagedKeyCommandConflict = errors.New("managed key command differs from its original execution")

// Pattern 2: gRPC status codes
if err != nil {
    return nil, status.Error(codes.NotFound, "workspace not found")
}

// Pattern 3: HTTP error responses
writePublisherError(w, r, 403, "csrf_required", "session CSRF token required")
```

**Strengths**:
- Typed errors for domain failures
- gRPC status codes for RPC failures
- HTTP error codes for REST failures
- Clear error messages with context

### Code Duplication
**Shared patterns**:
- Idempotency command reservation (Gateway, Workspace, Serve)
- Advisory lock + fingerprint validation
- Preflight admission + convergence
- Owner readback authority

**Legitimate duplication**: Each service owns its idempotency semantics
**Concern**: No shared idempotency library (acceptable for domain isolation)

## 2.3 Security Code Patterns

### Input Validation
```go
// Constant-time comparison for secrets
subtle.ConstantTimeCompare([]byte(caller.Session.CsrfToken), []byte(r.Header.Get("X-CSRF-Token")))

// Length validation
if call.IdempotencyKey == "" || len(call.IdempotencyKey) > 256 {
    writePublisherError(w, r, 400, "idempotency_key_required", "Idempotency-Key is required")
}

// Fingerprint validation
if len(reservation.CommandFingerprint) != 64 {
    return ManagedKeyCommandRecord{}, false, errors.New("managed key command requires... 64-char hash")
}
```

**Strengths**: Timing-safe comparisons, explicit length checks, fingerprint validation

### Authorization Checks
```go
// BFF authorization
if err := RequireAuthorizedAction(ctx, s.identity, caller, owner, action, resource, call.RequestId); err != nil {
    writePublisherIdentityError(w, r, err)
    return
}

// Service re-validation
// proto comment line 8: "Internal services... revalidate tenant/actor authorization"
// Never accept browser self-asserted tenant, price, provider, wallet identity
```

**Strengths**: Defense in depth (BFF + service validate), never trust browser assertions

### Secret Handling
```go
// Never store raw keys
type managedKeyCommandState struct {
    ExternalKeyID           string `json:"externalKeyId,omitempty"`
    KeyBindingID            string `json:"keyBindingId,omitempty"`
    SecretRef               string `json:"secretRef,omitempty"`
    // Raw key material NEVER appears here
}
```

**Strengths**: Only opaque references stored, no credential material in state

## 2.4 Code Quality Findings

### ✅ Strengths
1. **Exceptionally low technical debt** (3 TODOs in 870 files)
2. **Strong typing** (gRPC contracts, TypeScript strict mode)
3. **Consistent error handling** (typed errors, gRPC status, HTTP codes)
4. **Security-first patterns** (timing-safe comparisons, defense in depth)
5. **Clear module boundaries** (enforced by build-time tests)

### ⚠️  Concerns
1. **No shared idempotency library** (pattern repeated across services)
   - Acceptable for domain isolation
   - Each service owns its semantics
2. **Migration path complexity** (Control Plane retained)
   - Two parallel paths increase maintenance
   - Documented as non-target

### 🟢 Recommendations
- Continue current quality bar
- Document idempotency pattern (if not already)
- Plan Control Plane caller retirement timeline

---

# Section 3: Test Coverage Review

## 3.1 Test Inventory

### Test File Count
- **Total test files**: 419
- **Go tests**: `*_test.go` pattern
- **TypeScript tests**: `*.test.ts` pattern
- **Browser tests**: Playwright-based E2E tests

### Test Categories

**1. Contract Tests** (tests/contracts/)
- `module-physical-boundaries.test.ts`: Enforces service isolation
- Owner API contract tests

**2. Source Tests** (tests/tools/, tests/ui/)
- Console model tests (workspace, billing, gateway)
- Development tooling tests (dev-session, PR governance)

**3. Browser Tests** (tests/ui/*-browser.test.ts)
- 8 concurrent browser suites
- Billing, gateway usage, workspace lifecycle
- Customer/operator experiences

**4. PostgreSQL Owner Tests**
- Workspace focused tests (deletion, model config)
- Serve focused tests (deployment, readback)
- Gateway focused tests (managed keys)
- Fabric focused tests (Secret binding, provider mutations)

**5. Integration Tests**
- Fabric Local-Docker integration (requires Docker daemon)
- BuildKit OCI integration (live registry)

## 3.2 Test Execution

### Qualification Gate
```json
// package.json:33
"verify:local": "node tools/verify-local.ts"
```

**Runs**:
- Repository contract validators
- Source tests (`npm run test:source`)
- Console browser suite (`test:browser:suite`)
- TypeScript typecheck
- ESLint
- Vite build
- Go compilation
- Database-free owner tests

**Evidence**: verify:local passes (status.md:77-81)

### Test Scripts
```json
"test": "npm run test:source",
"test:source": "node --test \"tests/contracts/*.test.ts\" \"tests/tools/*.test.ts\" tests/ui/*.test.ts",
"test:browser:suite": "npm run test:ownership && node --test --test-concurrency=3 tests/ui/*-browser.test.ts",
"test:ownership": "node tools/validate-test-ownership.mjs",
"test:qualification": "node --test \"tests/**/*.test.ts\"",
"verify:local": "node tools/verify-local.ts",
"verify:local:focused": "node tools/verify-local.ts --focused",
"verify:local:full": "node tools/verify-local.ts --with-postgres"
```

**Test orchestration**: Clear separation of quick vs. full qualification

## 3.3 Coverage Analysis

### Covered Areas

**✅ Service Boundaries** (module-physical-boundaries.test.ts)
- Go service imports (no cross-service implementation)
- Cloud SDK imports (Fabric only)
- Console network calls (/api/* only)

**✅ Authorization** (publisher.go + CloudIdentity tests)
- CSRF validation
- Same-origin enforcement
- Idempotency key requirement
- Per-owner, per-action, per-resource authorization

**✅ Database Isolation** (PostgreSQL tests)
- Workspace owner tests (entitlement, Saga, config)
- Serve owner tests (deployment, readiness, access)
- Gateway owner tests (managed keys, idempotency)
- Fabric owner tests (provider mutations, Secret binding)

**✅ Browser Flows** (8 concurrent browser suites)
- Workspace lifecycle (create, renewal, deletion)
- Billing controller (charges, refunds)
- Gateway usage (wallet, keys)
- Operator/customer experiences

**✅ Idempotency** (Gateway managed_key_command_test.go)
- Advisory lock behavior
- Fingerprint validation
- Terminal answer immutability
- Conflict detection

### Coverage Gaps

**❌ Instance Deployment** (status.md:129-146)
- No Candidate construction tests
- No Instance deployment tests
- No TKE runtime tests
- No DNS/TLS tests
- No real browser access tests

**❌ Sub2API Integration** (status.md:106-112)
- Uses HTTP contract fixture
- No live Sub2API tests
- No idempotent readback tests
- No Instance secret wiring tests

**❌ Serve Execution** (status.md:113-116)
- No TKE route activation tests
- No real application execution tests
- Fabric adapter path not fully retired

**❌ Ledger Reconciliation** (status.md:67-69)
- No real charge reconciliation tests
- No real wallet mutation tests
- No production receipt binding tests

## 3.4 Test Quality

### Test Patterns

**Good: Isolated unit tests**
```go
func TestManagedKeyCommandReservation(t *testing.T) {
    // Advisory lock prevents concurrent issue
    // Fingerprint validation
    // Terminal answer immutability
}
```

**Good: Contract enforcement**
```typescript
test("Go services remain physically isolated behind typed owner contracts", async () => {
    // Enforces module boundaries at build time
});
```

**Good: Browser automation**
```typescript
test("workspace lifecycle freshness", async () => {
    // Real browser, real DOM, real interactions
});
```

### Test Maintainability

**Strengths**:
- Clear test names (describes behavior, not implementation)
- Isolated fixtures (each test owns its setup)
- Fast feedback (verify:local runs in minutes)

**Concerns**:
- Fabric Local-Docker test requires Docker daemon (environment dependency)
- Browser tests require Playwright setup (CI complexity)
- PostgreSQL tests require database (local vs. CI differences)

## 3.5 Test Findings

### ✅ Strengths
1. **Comprehensive source coverage** (419 test files)
2. **Build-time boundary enforcement** (module-physical-boundaries)
3. **Browser automation** (8 concurrent Playwright suites)
4. **PostgreSQL owner tests** (Workspace, Serve, Gateway, Fabric)
5. **Clear qualification gate** (verify:local)

### ⚠️  Gaps
1. **No Instance-level tests** (TKE, DNS/TLS, real access)
2. **No live Sub2API tests** (fixture only)
3. **No real Ledger reconciliation** (charges, wallet mutations)
4. **No medical platform tests** (clinical workflows)

### 🟢 Recommendations
1. **Add Instance smoke tests** (deploy Candidate, verify route, check readiness)
2. **Add Sub2API integration tests** (live service route qualification)
3. **Add Ledger reconciliation tests** (real charge/refund flows)
4. **Plan medical platform acceptance** (clinical workflow qualification)

---

# Section 4: Performance Review

## 4.1 Performance Characteristics

### Request Latency

**Console BFF → gRPC owners**
- Synchronous calls (no async aggregation yet)
- Sequential authorization checks
- Per-request session validation

**Estimated latency budget**:
- Session validation: ~10ms (CloudIdentity lookup)
- Authorization check: ~5ms (policy evaluation)
- gRPC owner call: ~50-500ms (depends on owner)
- Total: ~65-515ms per request

**Evidence**: No performance benchmarks in source

### Database Performance

**PostgreSQL 16.8** (compose.yaml:13)
- 11 isolated databases
- Connection pooling per service
- No cross-owner joins (by design)

**Query patterns**:
- Advisory locks for idempotency (pg_advisory_xact_lock)
- Optimistic locking for updates (version checks)
- Indexed lookups (tenant_id, workspace_id, operation_id)

**Evidence**: No query performance tests in source

### Provider Operations

**Fabric → TKE/Docker** (provider mutations)
- Blocking provider API calls
- Authoritative readback (convergence)
- Retry logic for transient failures

**Estimated latency**:
- TKE resource creation: ~10-60s
- Docker container creation: ~5-30s
- Provider readback: ~1-5s

**Evidence**: No provider performance benchmarks

### Model Configuration

**Serve → Application** (modelconfiguration.go:41)
- 60s timeout per apply/readback
- Synchronous HTTP calls
- No retry at Serve level (client decides)

**Latency budget**: 60s max per configuration update

## 4.2 Scalability

### Horizontal Scaling

**Console BFF**
- Stateless (sessions in CloudIdentity)
- Can scale horizontally behind load balancer
- No inter-instance coordination

**Services**
- Stateless (state in PostgreSQL)
- Can scale horizontally per service
- Advisory locks coordinate across instances

**PostgreSQL**
- Single instance in compose.yaml
- Horizontal scaling not configured
- Bottleneck for write-heavy workloads

### Vertical Scaling

**No resource limits configured** (compose.yaml)
- No CPU/memory limits
- No connection pool limits
- No max concurrent requests

**Evidence**: compose.yaml has no resource constraints

### Bottleneck Analysis

**Likely bottlenecks**:
1. **PostgreSQL write throughput** (11 services, 1 database instance)
2. **Provider API rate limits** (TKE, Docker, Sub2API)
3. **Sequential BFF authorization** (no batching or caching)
4. **Synchronous model configuration** (60s blocking apply)

## 4.3 Caching & Optimization

### Current Caching

**None observed in source**:
- No Redis or Memcached
- No HTTP cache headers (Cache-Control: no-store)
- No session cache (CloudIdentity lookup per request)
- No authorization cache (policy evaluated per request)

**Trade-off**: Consistency over performance (correct choice for financial/security operations)

### Optimization Opportunities

**1. Session caching** (low risk)
- Cache CloudIdentity sessions (5min TTL)
- Reduces session lookup per request
- Invalidate on logout/expiry

**2. Authorization caching** (medium risk)
- Cache policy evaluation results (1min TTL)
- Reduces authorization overhead
- Invalidate on permission changes

**3. Provider readback caching** (high risk)
- Cache Fabric authoritative readback (30s TTL)
- Reduces provider API calls
- Risk: stale state during convergence

**4. Connection pooling** (low risk)
- Configure PostgreSQL connection pools
- Reduces connection overhead
- No correctness risk

## 4.4 Performance Findings

### ✅ Adequate for Current Scale
- verify:local runs in minutes (acceptable for dev)
- 419 tests execute without timeout
- Source qualification passes

### ⚠️  Unknown at Production Scale
- No load testing results
- No capacity planning data
- No real user traffic patterns
- No Instance performance baselines

### 🔴 Risks
1. **PostgreSQL bottleneck** (single instance, 11 services)
2. **No resource limits** (services can exhaust host)
3. **No caching** (repeated lookups on hot paths)
4. **No performance SLOs** (no latency/throughput targets)

### 🟢 Recommendations

**Before First Launch**:
1. **Add resource limits** (CPU/memory per service)
2. **Configure connection pools** (PostgreSQL max connections)
3. **Define performance SLOs** (p50/p95/p99 latency targets)
4. **Add basic metrics** (request rate, error rate, latency)

**After First Launch**:
5. **Load testing** (simulate production traffic)
6. **Capacity planning** (identify scaling thresholds)
7. **Session caching** (reduce CloudIdentity lookups)
8. **Authorization caching** (reduce policy evaluation overhead)

**Long-Term**:
9. **PostgreSQL read replicas** (scale read-heavy workloads)
10. **Async operation patterns** (long-running provider mutations)
11. **Rate limiting** (protect from abuse)
12. **Circuit breakers** (isolate failing dependencies)

---

# Section 5: Review Completion

## 5.1 Overall Assessment

**Architecture**: ✅ **SOUND** (strong boundaries, robust security, correct failure handling)

**Code Quality**: ✅ **HIGH** (3 TODOs in 870 files, consistent patterns, security-first)

**Test Coverage**: ⚠️  **ADEQUATE FOR SOURCE** (419 tests, but no Instance/Sub2API/Ledger live tests)

**Performance**: ⚠️  **UNKNOWN AT SCALE** (no benchmarks, no load tests, no production evidence)

## 5.2 Critical Blockers

**None** for source development, but **Instance deployment gap blocks production**:
1. No Candidate construction
2. No Instance deployment
3. No TKE runtime qualification
4. No real browser access verification
5. No Ledger reconciliation with real charges

## 5.3 High-Priority Recommendations

**Immediate** (unblock first launch):
1. Construct exact Candidate from SHA
2. Deploy to Instance (opl-instance-medopl)
3. Qualify complete Browser→TKE→Readback flow
4. Verify real Sub2API integration
5. Confirm Ledger reconciliation

**Short-term** (next 30 days):
6. Add resource limits and connection pools
7. Define performance SLOs
8. Complete Serve execution boundary
9. Retire Control Plane callers
10. Add Instance smoke tests

**Long-term** (next quarter):
11. Implement session/authorization caching
12. Load testing and capacity planning
13. Runtime Secret delivery channel
14. Medical platform qualification
15. PostgreSQL read replicas

## 5.4 Confidence Levels

**HIGH confidence** (verified in source):
- Architecture boundaries and isolation
- Security patterns (CSRF, origin, idempotency, authorization)
- Database isolation and credentials
- Idempotent coordination patterns
- Secret hygiene (no raw key storage)

**MEDIUM confidence** (unverified, but design is sound):
- Instance deployment and routing
- Sub2API integration (fixture only)
- Serve execution on TKE
- Performance at production scale

**LOW confidence** (no evidence):
- Medical platform clinical workflows
- Patient-data production use
- Real wallet/charge reconciliation
- Capacity limits and scaling behavior

## 5.5 Timeline Estimate

**Candidate → Instance → Readback**: 2-4 weeks
- Assumes no major integration defects
- Requires opl-instance-medopl coordination
- Joint qualification Cloud + Instance teams

**First Tencent/TKE Launch**: 4-6 weeks from today
- Includes Candidate construction
- Instance deployment and qualification
- Real browser access verification
- Sub2API live integration

**Medical Platform Qualification**: 3-6 months
- Clinical workflow acceptance
- Patient-data production evidence
- Medical quality certifications
- Regulatory compliance
