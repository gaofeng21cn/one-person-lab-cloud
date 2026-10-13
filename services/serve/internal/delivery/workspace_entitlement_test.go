package delivery

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
)

type accessOwnerProbe struct {
	api.WorkspaceAccessReadbackClient
	answer *api.WorkspaceAccessDecision
	err    error
}

func (p *accessOwnerProbe) ReadWorkspaceAccess(ctx context.Context, r *api.ReadWorkspaceAccessRequest, _ ...grpc.CallOption) (*api.WorkspaceAccessDecision, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("unbounded owner read")
	}
	return p.answer, p.err
}
func TestWorkspaceEntitlementReadsEachRequestAndRefusesForeignOrMissingVerdict(t *testing.T) {
	p := &accessOwnerProbe{answer: &api.WorkspaceAccessDecision{WorkspaceId: "w", Verdict: api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED}}
	gate := WorkspaceEntitlement{Client: p}
	if got, err := gate.Verdict(t.Context(), "w"); err != nil || got != EntitlementPermitted {
		t.Fatalf("%s %v", got, err)
	}
	p.answer.Verdict = api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED
	if got, err := gate.Verdict(t.Context(), "w"); err != nil || got != EntitlementClosed {
		t.Fatalf("%s %v", got, err)
	}
	p.answer.Verdict = api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_UNSPECIFIED
	if got, err := gate.Verdict(t.Context(), "w"); err != nil || got != EntitlementUnverified {
		t.Fatalf("%s %v", got, err)
	}
	p.answer.WorkspaceId = "foreign"
	if _, err := gate.Verdict(t.Context(), "w"); err == nil {
		t.Fatal("foreign answer accepted")
	}
	p.answer = nil
	if _, err := gate.Verdict(t.Context(), "w"); err == nil {
		t.Fatal("nil answer accepted")
	}
	p.err = errors.New("owner unavailable")
	if _, err := gate.Verdict(t.Context(), "w"); err == nil {
		t.Fatal("owner failure accepted")
	}
}
