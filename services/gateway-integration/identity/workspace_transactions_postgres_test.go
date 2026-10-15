package identity_test

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

func TestWorkspaceTransactionsPreserveHistoryAndTenantIsolation(t *testing.T) {
	client, tenant, service := gatewayProductSystem(t)
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='owner' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	raw, _ := login(t, tenant)
	call := &api.CallContext{RequestId: "transaction-read", ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(raw)), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}}
	// No live Workspace exists in this fixture. Its historical binding is revoked;
	// neither a current Workspace nor a current payer may erase recorded charges.
	_, err := service.GatewayStore.DB().ExecContext(t.Context(), `
		INSERT INTO gateway.tenant_wallet_bindings(id,tenant_id,billing_sub2api_user_id,delegation_ref,verification_evidence_ref,bound_at,revoked_at)
		VALUES('old-binding','tenant-test','101','delegation','proof',now(),now()),('other-binding','tenant-other','202','delegation','proof',now(),NULL);
		INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at,created_at,updated_at,purpose,receipt_id)
		VALUES('charge-a','tenant-test','old-binding','deleted-workspace','charge','confirmed',9007199254740993,'a','a','external-a','2026-10-01T12:00:00Z','2026-10-01T12:00:00Z','2026-10-01T12:01:00Z','base_period','receipt-a'),
		('charge-b','tenant-test','old-binding','deleted-workspace','charge','unknown',7,'b','b',NULL,NULL,'2026-10-02T12:00:00Z','2026-10-02T12:01:00Z','base_period',NULL),
		('charge-c','tenant-test','old-binding','deleted-workspace','charge','rejected',9,'c','c',NULL,NULL,'2026-10-02T12:00:00Z','2026-10-02T12:01:00Z','base_period',NULL),
		('other-tenant','tenant-other','other-binding','deleted-workspace','charge','unknown',99,'other','other',NULL,NULL,now(),now(),'base_period',NULL),
		('other-workspace','tenant-test','old-binding','other-workspace','charge','unknown',88,'other-workspace','other-workspace',NULL,NULL,now(),now(),'base_period',NULL);
		INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,original_wallet_operation_id,refund_entitlement_ref,refund_entitlement_snapshot,created_at,updated_at,purpose)
		VALUES('refund-d','tenant-test','old-binding','deleted-workspace','refund','unknown',3,'d','d','charge-a','entitlement-a','{}','2026-10-03T12:00:00Z','2026-10-03T12:01:00Z','base_period_delete');
		UPDATE gateway.wallet_operations SET plan_change_id='plan-change-a',coverage_start='2026-10-01T00:00:00.123Z',coverage_end='2026-11-01T00:00:00.456Z' WHERE id='refund-d';`)
	if err != nil {
		t.Fatal(err)
	}
	request := &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "deleted-workspace", QueryLimit: proto.Int32(2)}
	first, err := client.ListWorkspaceTransactions(t.Context(), request)
	if err != nil || len(first.GetItems()) != 2 || first.GetNextCursor() == "" {
		t.Fatalf("first page=%v err=%v", first, err)
	}
	if first.Items[0].GetId() != "refund-d" || first.Items[0].GetOriginalChargeOperationId() != "charge-a" || first.Items[0].GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN || first.Items[0].GetPurpose() != api.WalletOperationPurposeEnum_WALLET_OPERATION_PURPOSE_ENUM_BASE_PERIOD_DELETE || first.Items[1].GetId() != "charge-c" || first.Items[1].GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED || first.Items[0].GetPlanChangeId() != "plan-change-a" || first.Items[0].GetCoverageStartMilliseconds() != 1790812800123 || first.Items[0].GetCoverageEndMilliseconds() != 1793491200456 {
		t.Fatalf("refund/status facts changed: %v", first)
	}
	request.QueryCursor = first.NextCursor
	second, err := client.ListWorkspaceTransactions(t.Context(), request)
	if err != nil || len(second.GetItems()) != 2 || second.GetNextCursor() != "" {
		t.Fatalf("second page=%v err=%v", second, err)
	}
	if second.Items[0].GetId() != "charge-b" || second.Items[0].GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN || second.Items[1].GetAmountUsdMicros() != 9007199254740993 || second.Items[1].GetReceiptId() != "receipt-a" || second.Items[1].GetExternalReference() != "external-a" || second.Items[1].GetCreatedAt().AsTime().Format(time.RFC3339) != "2026-10-01T12:00:00Z" || second.Items[1].GetUpdatedAt().AsTime().Format(time.RFC3339) != "2026-10-01T12:01:00Z" {
		t.Fatalf("original financial facts changed: %v", second)
	}
	wire, err := publicjson.Marshal(second)
	if err != nil || !strings.Contains(string(wire), `"amountUSDMicros":"9007199254740993"`) {
		t.Fatalf("amount lost precision: %s, %v", wire, err)
	}
	var unknown int
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.wallet_operations WHERE id IN ('charge-b','refund-d') AND status='unknown'`).Scan(&unknown); err != nil || unknown != 2 {
		t.Fatalf("read reconciled unknown operations: count=%d err=%v", unknown, err)
	}
	for _, role := range []string{"admin", "owner"} {
		if _, err = service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role=$1 WHERE actor_id='101'`, role); err != nil {
			t.Fatal(err)
		}
		page, err := client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "deleted-workspace"})
		if err != nil || len(page.GetItems()) != 4 {
			t.Fatalf("role %s page=%v err=%v", role, page, err)
		}
	}
	empty, err := client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "absent-workspace"})
	if err != nil || len(empty.GetItems()) != 0 {
		t.Fatalf("absent workspace=%v err=%v", empty, err)
	}
	for _, limit := range []int32{0, -1, 101} {
		_, err := client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "deleted-workspace", QueryLimit: &limit})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("limit=%d err=%v", limit, err)
		}
	}
	_, err = client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "deleted-workspace", QueryCursor: proto.String("invalid")})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid cursor: %v", err)
	}
	// Use enough rows to verify the contract's default page size of 25.
	_, err = service.GatewayStore.DB().ExecContext(t.Context(), `INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint) SELECT 'default-'||n,'tenant-test','old-binding','default-workspace','charge','unknown',1,'default-'||n,'default' FROM generate_series(1,26) n`)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "default-workspace"})
	if err != nil || len(page.GetItems()) != 25 || page.GetNextCursor() == "" {
		t.Fatalf("default page size: page=%v err=%v", page, err)
	}
}

func TestWorkspaceTransactionsRevalidateExactLiveAuthority(t *testing.T) {
	client, tenant, service := gatewayProductSystem(t)
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='owner' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	raw, _ := login(t, tenant)
	call := &api.CallContext{RequestId: "transaction-auth", ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(raw)), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}}
	auth := &api.AuthorizationRequest{RequestId: call.RequestId, ActorId: call.ActorId, SessionId: call.SessionId, Scope: call.Scope, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACETRANSACTIONS, AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("workspace-a")}}
	decision, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), auth)
	if err != nil {
		t.Fatal(err)
	}
	call.AuthorizationContextId = decision.GetAuthorizationContextId()
	read := func(c *api.CallContext, workspace string) error {
		_, err := client.ListWorkspaceTransactions(t.Context(), &api.ListWorkspaceTransactionsRpcRequest{Context: c, WorkspaceId: workspace})
		return err
	}
	if err := read(call, "workspace-a"); err != nil {
		t.Fatalf("valid exact decision: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*api.CallContext)
	}{
		{"forged actor", func(c *api.CallContext) { c.ActorId = "202" }},
		{"cross Tenant", func(c *api.CallContext) { c.Scope.GetTenant().TenantId = "tenant-other" }},
		{"platform scope", func(c *api.CallContext) {
			c.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
		}},
		{"grant substitution", func(c *api.CallContext) { c.AcceptedOperationGrantId = proto.String("grant") }},
		{"absent session", func(c *api.CallContext) { c.SessionId = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := proto.Clone(call).(*api.CallContext)
			tc.change(changed)
			if err := read(changed, "workspace-a"); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("allowed mismatched authority: %v", err)
			}
		})
	}
	if err := read(call, "workspace-b"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("decision replayed for another Workspace: %v", err)
	}
	auth.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS
	auth.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	other, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), auth)
	if err != nil {
		t.Fatal(err)
	}
	wrongAction := proto.Clone(call).(*api.CallContext)
	wrongAction.AuthorizationContextId = other.GetAuthorizationContextId()
	if err := read(wrongAction, "workspace-a"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("decision replayed for another action: %v", err)
	}
	if _, err := service.ListWorkspaceTransactions(ownerservice.WithPeerOwner(t.Context(), owneridentity.ControlPlane), &api.ListWorkspaceTransactionsRpcRequest{Context: call, WorkspaceId: "workspace-a"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong peer allowed: %v", err)
	}
	if _, err = service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='member' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	if err := read(call, "workspace-a"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("role downgrade kept old access: %v", err)
	}
	if _, err = service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	if err := read(call, "workspace-a"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked membership kept old access: %v", err)
	}
	// A newly logged-in User without a Tenant still cannot read team finances.
	raw, _ = login(t, tenant)
	call.SessionId = proto.String(owneridentity.SessionReference(raw))
	call.AuthorizationContextId = ""
	call.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if err := read(call, "workspace-a"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("no-Tenant User acquired team access: %v", err)
	}
}

func TestWorkspaceTransactionTrendUsesOriginalConfirmationTimesAndWholeWindow(t *testing.T) {
	client, tenant, service := gatewayProductSystem(t)
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='admin' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	raw, _ := login(t, tenant)
	call := &api.CallContext{RequestId: "trend-read", ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(raw)), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}}
	_, err := service.GatewayStore.DB().ExecContext(t.Context(), `
 INSERT INTO gateway.tenant_wallet_bindings(id,tenant_id,billing_sub2api_user_id,delegation_ref,verification_evidence_ref,bound_at,revoked_at) VALUES('old','tenant-test','101','d','v',now(),now()),('other','tenant-other','202','d','v',now(),NULL);
 INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at,created_at)
 VALUES('confirmed-old-created','tenant-test','old','deleted-workspace','charge','confirmed',9007199254740993,'a','a','a',now()-interval '1 day',now()-interval '40 days'),
 ('confirmed-outside','tenant-test','old','deleted-workspace','charge','confirmed',123,'b','b','b',now()-interval '20 days',now()),
 ('unknown','tenant-test','old','deleted-workspace','charge','unknown',7,'c','c',NULL,NULL,now()),
 ('requested','tenant-test','old','deleted-workspace','charge','requested',11,'d','d',NULL,NULL,now()),
 ('rejected','tenant-test','old','deleted-workspace','charge','rejected',13,'e','e',NULL,NULL,now()),
 ('other-tenant','tenant-other','other','deleted-workspace','charge','confirmed',999,'f','f','f',now(),now()),
 ('other-workspace','tenant-test','old','other-workspace','charge','confirmed',999,'g','g','g',now(),now()),
 ('old-unknown','tenant-test','old','deleted-workspace','charge','unknown',17,'h','h',NULL,NULL,now()-interval '20 days');
 INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at,created_at,original_wallet_operation_id,refund_entitlement_ref)
 VALUES('refund-confirmed','tenant-test','old','deleted-workspace','refund','confirmed',3,'i','i','i',now()-interval '1 day',now(),'confirmed-old-created','refund-i'),
 ('refund-unknown','tenant-test','old','deleted-workspace','refund','unknown',5,'j','j',NULL,NULL,now(),'confirmed-old-created','refund-j'),
 ('refund-requested','tenant-test','old','deleted-workspace','refund','requested',2,'k','k',NULL,NULL,now(),'confirmed-old-created','refund-k');
 INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at,created_at)
 VALUES('charge-over','tenant-test','old','deleted-workspace','charge','confirmed',10,'over-charge','over-charge','over-charge',now()-interval '1 day',now()-interval '1 day');
 INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,original_wallet_operation_id,refund_entitlement_ref,external_reference,confirmed_at,created_at)
 VALUES('refund-over','tenant-test','old','deleted-workspace','refund','confirmed',20,'over-refund','over-refund','charge-over','refund-over','over-refund',now()-interval '1 day',now());
 INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at)
 SELECT 'batch-'||n,'tenant-test','old','deleted-workspace','charge','confirmed',10,'batch-'||n,'batch','batch-'||n,now() FROM generate_series(1,30) n;`)
	if err != nil {
		t.Fatal(err)
	}
	request := &api.GetWorkspaceTransactionTrendRpcRequest{Context: call, WorkspaceId: "deleted-workspace"}
	trend, err := client.GetWorkspaceTransactionTrend(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -13)
	if trend.GetWorkspaceId() != "deleted-workspace" || len(trend.GetDays()) != 14 || !trend.GetPeriodStart().AsTime().Equal(start) || trend.Days[12].GetConfirmedChargeUsdMicros() != 9007199254741003 || trend.Days[12].GetConfirmedRefundUsdMicros() != 3 || trend.Days[13].GetConfirmedChargeUsdMicros() != 300 || trend.GetUnknownChargeUsdMicros() != 7 || trend.GetUnknownRefundUsdMicros() != 25 || trend.GetRequestedChargeUsdMicros() != 11 || trend.GetRequestedRefundUsdMicros() != 2 {
		t.Fatalf("wrong aggregate: %v", trend)
	}
	for i, day := range trend.Days {
		if day.GetDate() != start.AddDate(0, 0, i).Format("2006-01-02") {
			t.Fatalf("missing/incorrect UTC day: %v", day)
		}
	}
	wire, err := publicjson.Marshal(trend)
	if err != nil || !strings.Contains(string(wire), `"confirmedChargeUSDMicros":"9007199254741003"`) {
		t.Fatalf("lossy trend: %s err=%v", wire, err)
	}
	forged := proto.Clone(request).(*api.GetWorkspaceTransactionTrendRpcRequest)
	forged.Context.Scope.GetTenant().TenantId = "tenant-other"
	if _, err = client.GetWorkspaceTransactionTrend(t.Context(), forged); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross Tenant trend: %v", err)
	}
	if _, err = service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='member' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetWorkspaceTransactionTrend(t.Context(), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("member trend: %v", err)
	}
	if _, err = service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='owner' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	_, err = service.GatewayStore.DB().ExecContext(t.Context(), `INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,workspace_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at) VALUES('overflow','tenant-test','old','deleted-workspace','charge','confirmed',9223372036854775807,'overflow','overflow','overflow',now())`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetWorkspaceTransactionTrend(t.Context(), request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("overflow not refused: %v", err)
	}
}
