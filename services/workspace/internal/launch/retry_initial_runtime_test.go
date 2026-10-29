package launch

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

type retryOperatorIdentity struct {
	api.CloudIdentityAuthorizationClient
	denied, mismatch, expired, revokeSecond bool
	calls                                   int
}

func (i *retryOperatorIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.calls++
	if i.denied || (i.revokeSecond && i.calls > 1) {
		return nil, status.Error(codes.PermissionDenied, "operator denied")
	}
	if r.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || r.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RECONCILEOPERATION || r.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION || r.GetResource().GetId() != "operation-original" || r.GetActorId() != "operator-admin" || r.GetSessionId() != "session_private_operator_reference" || r.GetScope().GetPlatform() == nil || r.GetAcceptedOperationGrantId() != "" {
		return nil, status.Error(codes.PermissionDenied, "different operator authorization")
	}
	d := &api.AuthorizationDecision{Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, AuthorizationContextId: proto.String("operator-authorization"), Scope: r.Scope, ActorId: r.ActorId, SessionId: r.SessionId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	if i.mismatch {
		d.ActorId = "other"
	}
	if i.expired {
		d.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
	}
	return d, nil
}

type retryOriginalServe struct {
	api.ServeAgentCoordinationClient
	commands                               []*api.RuntimeDeployCommand
	loseResponse, drift, rejected, unknown bool
	beforeDeploy                           func()
}

func (s *retryOriginalServe) Deploy(_ context.Context, r *api.RuntimeDeployCommand, _ ...grpc.CallOption) (*api.RuntimeReadback, error) {
	s.commands = append(s.commands, proto.Clone(r).(*api.RuntimeDeployCommand))
	if s.beforeDeploy != nil {
		s.beforeDeploy()
	}
	if s.loseResponse {
		s.loseResponse = false
		return nil, status.Error(codes.Unavailable, "original request committed, response lost")
	}
	observed := readyRuntime(r)
	observed.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
	observed.ApplicationAvailable, observed.ProcessReady = false, false
	observed.ReadinessReceiptId, observed.AccessUrl = "", ""
	if s.drift {
		observed.RuntimeInstanceId = "other-runtime"
	}
	if s.rejected {
		observed.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
		observed.Outcome = api.Observation_OBSERVATION_REJECTED
	}
	if s.unknown {
		observed.Outcome = api.Observation_OBSERVATION_UNKNOWN
	}
	// A direct client may enrich the passed object. Workspace must retain its own
	// frozen command, even with a mutable in-process client during testing.
	r.Context.ActorId = "serve-enriched-input"
	return observed, nil
}

func retryOperatorRequest() *api.ReconcileOperationRpcRequest {
	return &api.ReconcileOperationRpcRequest{Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, OperationId: "operation-original", Body: &api.ReconcileOperationRequest{Reason: "image command repair acknowledged"}, Context: &api.CallContext{ActorId: "operator-admin", SessionId: proto.String("session_private_operator_reference"), RequestId: "operator-request", IdempotencyKey: "operator-replay", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}}
}

func retryOriginalFixture(t *testing.T) (*Service, *retryOperatorIdentity, *retryOriginalServe, *api.RuntimeDeployCommand) {
	t.Helper()
	service, op, accepted, _ := seedRuntimeOrder(t, runtimeDatabase(t))
	service.Capability = &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
	service.Serve = &runtimeServeClient{dispatchLost: true}
	if err := service.Resume(t.Context(), op.ID); status.Code(err) != codes.Unavailable {
		t.Fatalf("freeze original: %v", err)
	}
	stored, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := decodeOrder(stored)
	if err != nil {
		t.Fatal(err)
	}
	original := &api.RuntimeDeployCommand{}
	if err = protojson.Unmarshal(result.RuntimeCommand, original); err != nil {
		t.Fatal(err)
	}
	// All procurement clients are absent. Only original Deploy may be called.
	service.Catalog, service.Fabric, service.Capability, service.Ledger, service.Gateway = nil, nil, nil, nil, nil
	identity, serve := &retryOperatorIdentity{}, &retryOriginalServe{}
	service.Identity, service.Serve = identity, serve
	return service, identity, serve, original
}

func retryStoredResult(t *testing.T, s *Service) (ownerstore.Operation, orderResult) {
	t.Helper()
	op, err := s.Store.ReadOperation(t.Context(), "operation-original")
	if err != nil {
		t.Fatal(err)
	}
	_, r, err := decodeOrder(op)
	if err != nil {
		t.Fatal(err)
	}
	return op, r
}
func retryChangeResult(t *testing.T, s *Service, change func(*orderResult)) {
	t.Helper()
	op, r := retryStoredResult(t, s)
	change(&r)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB().ExecContext(t.Context(), `UPDATE workspace.operations SET result=$2 WHERE id=$1`, op.ID, raw); err != nil {
		t.Fatal(err)
	}
}

func TestRetryInitialRuntimeReplaysOriginalWithoutProcurement(t *testing.T) {
	service, identity, serve, original := retryOriginalFixture(t)
	request := retryOperatorRequest()
	answer, err := service.RetryInitialRuntime(t.Context(), request)
	if err != nil || answer.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || answer.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
	if identity.calls != 2 || len(serve.commands) != 1 || !proto.Equal(original, serve.commands[0]) {
		t.Fatalf("identity calls=%d deploy calls=%d command changed=%v", identity.calls, len(serve.commands), len(serve.commands) > 0 && !proto.Equal(original, serve.commands[0]))
	}
	op, r := retryStoredResult(t, service)
	frozen := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(r.RuntimeCommand, frozen) != nil || !proto.Equal(original, frozen) || !r.RuntimeDeployAccepted || op.WorkerLeaseToken != "" {
		t.Fatal("original command or acceptance or lease changed")
	}
	var raw []byte
	var stepKey string
	var count int
	if err = service.Store.DB().QueryRowContext(t.Context(), `SELECT input_snapshot,step_key FROM workspace.saga_steps WHERE operation_id=$1 AND step_key LIKE 'retry_initial_runtime_%'`, op.ID).Scan(&raw, &stepKey); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(stepKey) {
		t.Fatalf("step key violates Instance attribution contract: %q", stepKey)
	}
	var ack map[string]any
	if json.Unmarshal(raw, &ack) != nil || len(ack) != 4 || ack["actorId"] != request.Context.ActorId || ack["requestId"] != request.Context.RequestId || ack["reason"] != request.Body.Reason || !strings.HasPrefix(ack["commandDigest"].(string), "sha256:") || strings.Contains(string(raw), request.Context.GetSessionId()) {
		t.Fatalf("unsafe acknowledgment: %s", raw)
	}
	if err = service.Store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.operations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("orders=%d err=%v", count, err)
	}
	// The same acknowledged replay is idempotent even while readiness is pending.
	request.Context.RequestId = "operator-request-again"
	if _, err = service.RetryInitialRuntime(t.Context(), request); err != nil || len(serve.commands) != 1 {
		t.Fatalf("repeated replay: calls=%d err=%v", len(serve.commands), err)
	}
	request.Body.Reason = "different acknowledgment"
	if _, err = service.RetryInitialRuntime(t.Context(), request); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting acknowledgment=%v", err)
	}
}

func TestRetryInitialRuntimeLostResponsePreservesOriginalAndAcceptedFlag(t *testing.T) {
	service, _, serve, original := retryOriginalFixture(t)
	retryChangeResult(t, service, func(r *orderResult) { r.RuntimeDeployAccepted = true })
	serve.loseResponse = true
	request := retryOperatorRequest()
	if _, err := service.RetryInitialRuntime(t.Context(), request); status.Code(err) != codes.Unavailable {
		t.Fatalf("lost response=%v", err)
	}
	op, r := retryStoredResult(t, service)
	if !r.RuntimeDeployAccepted || op.Observation != "unknown" || op.WorkerLeaseToken != "" {
		t.Fatal("response loss fabricated success or cleared previous accepted fact")
	}
	if _, err := service.RetryInitialRuntime(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if len(serve.commands) != 2 || !proto.Equal(original, serve.commands[0]) || !proto.Equal(original, serve.commands[1]) {
		t.Fatal("response retry changed the original deployment identity")
	}
	var attempts int
	if err := service.Store.DB().QueryRowContext(t.Context(), `SELECT attempt_count FROM workspace.saga_steps WHERE operation_id=$1 AND step_key LIKE 'retry_initial_runtime_%'`, op.ID).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRetryInitialRuntimeRefusesOperatorAuthorization(t *testing.T) {
	for _, mode := range []string{"denied", "mismatch", "expired", "revoked after lock"} {
		t.Run(mode, func(t *testing.T) {
			service, identity, serve, _ := retryOriginalFixture(t)
			identity.denied = mode == "denied"
			identity.mismatch = mode == "mismatch"
			identity.expired = mode == "expired"
			identity.revokeSecond = mode == "revoked after lock"
			if _, err := service.RetryInitialRuntime(t.Context(), retryOperatorRequest()); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("authorization=%v", err)
			}
			if len(serve.commands) != 0 {
				t.Fatal("denied operator deployed")
			}
			op, _ := retryStoredResult(t, service)
			if op.WorkerLeaseToken != "" {
				t.Fatal("denied operator retained lease")
			}
		})
	}
}

func TestRetryInitialRuntimeRefusesOrderAndWorkspaceState(t *testing.T) {
	cases := map[string]string{
		"terminal":        `UPDATE workspace.operations SET status='succeeded',completed_at=now()`,
		"wrong kind":      `UPDATE workspace.operations SET kind='renew_workspace'`,
		"wrong stage":     `UPDATE workspace.operations SET stage='activation'`,
		"active":          `UPDATE workspace.workspaces SET status='active'`,
		"closed":          `UPDATE workspace.workspaces SET status='deleted',deleted_at=now()`,
		"closure":         `UPDATE workspace.workspaces SET status='deleting'`,
		"tenant drift":    `UPDATE workspace.workspaces SET tenant_id='other'`,
		"operation drift": `UPDATE workspace.workspaces SET active_operation_id=NULL`,
		"subscription":    `INSERT INTO workspace.subscriptions(id,workspace_id,accepted_quote_id,accepted_quote_snapshot,billing_subject_ref,current_period_start,current_period_end,last_charge_wallet_operation_id,period_months,provenance,renewal_mode,current_price_policy_version_id,current_monthly_usd_micros,billing_anchor_day) VALUES('subscription-original','workspace-original','quote-original','{}','tenant:tenant-original',now(),now()+interval '1 month','wallet-original',1,'quoted','manual','price-original',100,1)`,
		"busy":            `UPDATE workspace.operations SET worker_lease_token='background-worker',worker_lease_until=now()+interval '60 seconds'`,
		"delete intent":   `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result) VALUES('delete-original','tenant-original','actor-original','delete_workspace','workspace-original','accepted','runtime','delete-request','{}','{}')`,
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			service, _, serve, _ := retryOriginalFixture(t)
			if _, err := service.Store.DB().ExecContext(t.Context(), sql); err != nil {
				t.Fatal(err)
			}
			want := codes.FailedPrecondition
			if name == "busy" {
				want = codes.Aborted
			}
			if _, err := service.RetryInitialRuntime(t.Context(), retryOperatorRequest()); status.Code(err) != want {
				t.Fatalf("refusal=%v want=%v", err, want)
			}
			if len(serve.commands) != 0 {
				t.Fatal("ineligible order deployed")
			}
		})
	}
}

func TestRetryInitialRuntimeRefusesFrozenEvidenceDrift(t *testing.T) {
	cases := map[string]func(*orderResult){
		"actor": func(r *orderResult) {
			c := &api.RuntimeDeployCommand{}
			_ = protojson.Unmarshal(r.RuntimeCommand, c)
			c.Context.ActorId = "other"
			r.RuntimeCommand = wire(c)
		},
		"tenant": func(r *orderResult) {
			c := &api.RuntimeDeployCommand{}
			_ = protojson.Unmarshal(r.RuntimeCommand, c)
			c.Context.Scope.GetTenant().TenantId = "other"
			r.RuntimeCommand = wire(c)
		},
		"idempotency": func(r *orderResult) {
			c := &api.RuntimeDeployCommand{}
			_ = protojson.Unmarshal(r.RuntimeCommand, c)
			c.Context.IdempotencyKey = "other"
			r.RuntimeCommand = wire(c)
		},
		"grant": func(r *orderResult) { r.GrantID = "other" },
		"reservation": func(r *orderResult) {
			c := &api.RuntimeReservation{}
			_ = protojson.Unmarshal(r.RuntimeReservation, c)
			c.DeploymentId = "other"
			r.RuntimeReservation = wire(c)
		},
		"epoch": func(r *orderResult) {
			c := &api.RuntimeReservation{}
			_ = protojson.Unmarshal(r.RuntimeReservation, c)
			c.ExecutionEpoch = 2
			r.RuntimeReservation = wire(c)
		},
		"source": func(r *orderResult) {
			var v map[string]any
			_ = json.Unmarshal(r.ApplicationSource, &v)
			v["deploymentDescriptorDigest"] = "sha256:other"
			r.ApplicationSource, _ = json.Marshal(v)
		},
		"binding": func(r *orderResult) { r.RuntimeBinding = []byte(`{}`) },
		"funding": func(r *orderResult) { r.ZeroChargeReceipt = nil },
		"fulfilled": func(r *orderResult) {
			c := &api.RuntimeDeployCommand{}
			_ = protojson.Unmarshal(r.RuntimeCommand, c)
			o := readyRuntime(c)
			o.FirstFulfilledAt = timestamppb.Now()
			r.RuntimeReadback = wire(o)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			service, _, serve, _ := retryOriginalFixture(t)
			retryChangeResult(t, service, change)
			if _, err := service.RetryInitialRuntime(t.Context(), retryOperatorRequest()); err == nil {
				t.Fatal("evidence drift accepted")
			}
			if len(serve.commands) != 0 {
				t.Fatal("inconsistent evidence deployed")
			}
		})
	}
}

func TestRetryInitialRuntimePreservesUnknownAndRejectsBadReadback(t *testing.T) {
	for _, mode := range []string{"unknown", "drift", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			service, _, serve, _ := retryOriginalFixture(t)
			serve.unknown = mode == "unknown"
			serve.drift = mode == "drift"
			serve.rejected = mode == "rejected"
			_, err := service.RetryInitialRuntime(t.Context(), retryOperatorRequest())
			want := codes.OK
			if mode == "drift" {
				want = codes.DataLoss
			}
			if mode == "rejected" {
				want = codes.FailedPrecondition
			}
			if status.Code(err) != want {
				t.Fatalf("readback err=%v want=%v", err, want)
			}
			op, _ := retryStoredResult(t, service)
			if mode != "rejected" && op.Observation != "unknown" {
				t.Fatalf("unknown observation fabricated confirmation: %s", op.Observation)
			}
		})
	}
}

func TestRetryInitialRuntimeReplaysOriginalPositiveEpoch(t *testing.T) {
	service, _, serve, original := retryOriginalFixture(t)
	original.ExecutionEpoch = 3
	retryChangeResult(t, service, func(result *orderResult) {
		reservation := &api.RuntimeReservation{}
		if err := protojson.Unmarshal(result.RuntimeReservation, reservation); err != nil {
			t.Fatal(err)
		}
		reservation.ExecutionEpoch = original.ExecutionEpoch
		result.RuntimeReservation, result.RuntimeCommand = wire(reservation), wire(original)
		if len(result.RuntimeReadback) > 0 {
			readback := &api.RuntimeReadback{}
			if err := protojson.Unmarshal(result.RuntimeReadback, readback); err != nil {
				t.Fatal(err)
			}
			readback.ExecutionEpoch = original.ExecutionEpoch
			result.RuntimeReadback = wire(readback)
		}
	})
	if _, err := service.RetryInitialRuntime(t.Context(), retryOperatorRequest()); err != nil {
		t.Fatal(err)
	}
	if len(serve.commands) != 1 || !proto.Equal(serve.commands[0], original) {
		t.Fatal("original positive execution epoch was not preserved")
	}
}
