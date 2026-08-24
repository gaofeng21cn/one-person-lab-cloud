package contracts_test

import (
	"testing"

	contracts "github.com/gaofeng21cn/one-person-lab-cloud/packages/contracts/go"
)

func TestDeviceUploadAttachmentEvidenceRequiresConversationBinding(t *testing.T) {
	tests := []struct {
		name     string
		evidence contracts.DeviceUploadAttachmentEvidence
		wantErr  string
	}{{
		name:     "uploaded requires visible pending attachment",
		evidence: contracts.DeviceUploadAttachmentEvidence{FileChooserObserved: true, UploadResponseOK: true, UploadResponseStatus: 200},
		wantErr:  "pending attachment not attached to conversation",
	}, {
		name: "complete device upload passes",
		evidence: contracts.DeviceUploadAttachmentEvidence{
			FileChooserObserved: true, UploadResponseOK: true, UploadResponseStatus: 200,
			PendingAttachmentTagVisible: true,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.evidence.Uploaded()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Uploaded() = %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("Uploaded() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
