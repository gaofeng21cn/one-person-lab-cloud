package catalog

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// TestListRefundPolicyVersionsReadsBackTheStoredRefundAlgorithm proves the
// administrator refund-policy list reads the stored `workspace-delete-refund-v1`
// algorithm back as its wire member and can encode the page on the public
// contract. The stored column spells the algorithm with hyphens while the wire
// enum member is spelled with underscores, and `algorithm` is a required public
// property, so a scan that resolves the wrong member would answer an
// unspecified enum and the public encoding of the page would refuse it.
func TestListRefundPolicyVersionsReadsBackTheStoredRefundAlgorithm(t *testing.T) {
	service, _ := system(t)
	ctx := peerContext(t)
	want := api.RefundPolicyVersionAlgorithmEnum_REFUND_POLICY_VERSION_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1
	retention, err := service.CreateRetentionPolicyVersion(ctx, &api.CreateRetentionPolicyVersionRpcRequest{Context: platformCall("admin", "readback-r", "readback-r"), Body: &api.CreateRetentionPolicyRequest{VersionLabel: "retention-readback", CustomerTerms: "data destroyed after confirmed deletion"}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateRefundPolicyVersion(ctx, &api.CreateRefundPolicyVersionRpcRequest{Context: platformCall("admin", "readback-f", "readback-f"), Body: &api.CreateRefundPolicyRequest{VersionLabel: "refund-readback", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1, RetentionPolicyVersionId: retention.GetId(), CustomerTerms: "720-hour policy", ValidFrom: timestamppb.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListRefundPolicyVersions(ctx, &api.ListRefundPolicyVersionsRpcRequest{Context: platformCall("admin", "readback-list", "readback-list")})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != created.GetId() {
		t.Fatalf("listed refund policies = %v, want the created version", page.GetItems())
	}
	listed := page.GetItems()[0]
	if listed.GetAlgorithm() != want {
		t.Errorf("listed refund algorithm = %v, want %v", listed.GetAlgorithm(), want)
	}
	if listed.GetRetentionPolicyVersionId() != retention.GetId() {
		t.Errorf("listed retention policy version = %q, want %q", listed.GetRetentionPolicyVersionId(), retention.GetId())
	}
	// The administrator list is served through the public JSON contract, where
	// algorithm is required; an unspecified member cannot be encoded at all.
	encoded, err := publicjson.Marshal(page)
	if err != nil {
		t.Fatalf("encode refund policy page: %v", err)
	}
	var view struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 || view.Items[0]["algorithm"] != "workspace-delete-refund-v1" {
		t.Fatalf("encoded page = %s, want the listed version to carry the public algorithm text", encoded)
	}
}
