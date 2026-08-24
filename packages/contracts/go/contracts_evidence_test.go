package contracts_test

import (
	"strings"
	"testing"

	contracts "github.com/gaofeng21cn/one-person-lab-cloud/packages/contracts/go"
)

func TestDeviceUploadCompleteBusinessChain(t *testing.T) {
	tests := []struct {
		name     string
		evidence contracts.DeviceUploadAttachmentEvidence
		wantErr  string
	}{{
		name: "upload without conversation binding is incomplete",
		evidence: contracts.DeviceUploadAttachmentEvidence{
			FileChooserObserved: true,
			UploadResponseOK:    true,
			UploadResponseStatus: 200,
		},
		wantErr: "uploaded attachment not visible in conversation",
	}, {
		name:     "complete business chain passes",
		evidence: contracts.DeviceUploadAttachmentEvidence{
			FileChooserObserved: true,
			UploadResponseOK:    true,
			UploadResponseStatus: 200,
			PendingAttachmentVisible: true,
			MessageAccepted: true,
			AssistantReplyObserved: true,
			UploadedContentReadbackValid: true,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.evidence.Complete()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Complete() = %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("Complete() = %v, want %q", err, tt.wantErr)
			}
		})
	}

	err := contracts.DeviceUploadAttachmentEvidence{FileChooserObserved: true, UploadResponseOK: true, UploadResponseStatus: 200, PendingAttachmentVisible: true, MessageAccepted: true, AssistantReplyObserved: true, UploadedContentReadbackValid: true, SettingsStrayComposerVisible: true}.Complete()
	if err == nil || !strings.Contains(err.Error(), "stray settings composer") {
		t.Fatalf("Complete() stray composer = %v", err)
	}
}
