package catalog

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// TestDeployQuoteSelectionUnion proves the deploy decode admits exactly the two
// application sources, rejects a missing selection and a mixed one, and never lets
// an absent CapabilityVersion become an implicit default.
func TestDeployQuoteSelectionUnion(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		request *api.QuoteRequest
		wantErr bool
	}{
		{
			name:    "no selection",
			raw:     `{"purpose":"deploy","computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
		{
			name:    "explicit empty capability",
			raw:     `{"purpose":"deploy","capabilityVersionId":"","computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
		{
			name:    "agent selection",
			raw:     `{"purpose":"deploy","applicationSelection":{"kind":"agent","capabilityVersionId":"cv-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: false,
		},
		{
			name:    "default app selection",
			raw:     `{"purpose":"deploy","applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: false,
		},
		{
			name:    "mixed selection",
			raw:     `{"purpose":"deploy","applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-1","capabilityVersionId":"cv-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
		{
			name:    "selection and legacy capability together",
			raw:     `{"purpose":"deploy","capabilityVersionId":"cv-1","applicationSelection":{"kind":"agent","capabilityVersionId":"cv-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
		{
			name:    "kind disagrees with id",
			raw:     `{"purpose":"deploy","applicationSelection":{"kind":"agent","runtimeVersionId":"runtime-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
		{
			name:    "missing kind",
			raw:     `{"purpose":"deploy","applicationSelection":{"runtimeVersionId":"runtime-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := &api.QuoteRequest{}
			if err := publicjson.Unmarshal([]byte(tc.raw), request); err != nil {
				t.Fatal(err)
			}
			if _, err := validateDeployQuoteRequest(request); (err != nil) != tc.wantErr {
				t.Fatalf("admission error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestDeployQuoteRejectsLegacyAndSelection proves the retained capabilityVersionId
// field remains accepted only as the agent branch, and that supplying it beside an
// explicit selection is refused rather than silently preferred.
func TestDeployQuoteRejectsLegacyAndSelection(t *testing.T) {
	request := &api.QuoteRequest{
		Purpose:             api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY,
		CapabilityVersionId: proto.String("cv-1"),
		ApplicationSelection: &api.WorkspaceApplicationSelection{
			Kind:                api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT,
			CapabilityVersionId: proto.String("cv-1"),
		},
		ComputePlanId: "compute-1", StoragePlanId: "storage-1", PeriodMonths: 1,
	}
	if _, err := validateDeployQuoteRequest(request); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("both selection forms accepted: %v", err)
	}
}
