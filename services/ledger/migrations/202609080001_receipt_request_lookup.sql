CREATE INDEX IF NOT EXISTS evidence_receipts_account_request_created
ON evidence_receipts (account_id, (payload_json::jsonb ->> 'requestId'), created_at DESC, id DESC);
