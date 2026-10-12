-- Provider action receipts table for recording deletion proofs and other provider-confirmed actions
CREATE TABLE IF NOT EXISTS ledger.provider_action_receipts (
    receipt_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    evidence JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for tenant-scoped queries
CREATE INDEX IF NOT EXISTS idx_provider_action_receipts_tenant ON ledger.provider_action_receipts(tenant_id);

-- Index for workspace-scoped queries
CREATE INDEX IF NOT EXISTS idx_provider_action_receipts_workspace ON ledger.provider_action_receipts(workspace_id);

-- Index for actor-scoped queries
CREATE INDEX IF NOT EXISTS idx_provider_action_receipts_actor ON ledger.provider_action_receipts(actor_id);
