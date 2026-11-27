BEGIN;
DO $$ BEGIN
 IF current_database() <> 'opl_gateway' THEN RAISE EXCEPTION 'Wrong database: expected opl_gateway, got %', current_database(); END IF;
END $$;
SET LOCAL TIME ZONE 'UTC';
SET LOCAL ROLE opl_gateway_owner;

-- An operator-initiated change to a Tenant's spendable balance is a wallet
-- operation of its own kinds: a recharge credits the customer, a debit collects
-- from the customer, and a business refund credits back a confirmed original
-- charge the command names. These kinds are distinct from the Workspace
-- obligation kinds, so an operator action can never be read as a purchase or as
-- a deletion refund, and the original charge of a business refund is always
-- named.
ALTER TABLE gateway.wallet_operations
  DROP CONSTRAINT wallet_operations_kind_check,
  ADD CONSTRAINT wallet_operations_kind_check CHECK (kind IN ('charge','refund','recharge','debit','business_refund')),
  ADD CONSTRAINT wallet_operations_business_refund_original CHECK (kind <> 'business_refund' OR original_wallet_operation_id IS NOT NULL);

-- Who ordered a balance change and why belongs to the money row itself: the
-- adjustment and its operator are one fact, so a read can never attribute a
-- balance change to nobody. A Workspace obligation carries no operator, so the
-- two attributions are exclusive rather than both optional.
ALTER TABLE gateway.wallet_operations
  ADD COLUMN actor_id text,
  ADD COLUMN reason text,
  ADD CONSTRAINT wallet_operations_operator_attribution CHECK (
    (kind IN ('recharge','debit','business_refund')) = (actor_id IS NOT NULL AND reason IS NOT NULL)
  );
COMMIT;
