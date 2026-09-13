package contracts

import (
	"errors"
	"regexp"
	"strings"
)

var (
	workspaceDataArtifactNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	workspaceDataSHA256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// WorkspaceApplicationDataArtifact names one declared data artifact and its
// expected content identity. Checksums are declared by the publisher and
// verified by the restore flow before any application reads the data.
type WorkspaceApplicationDataArtifact struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}

// WorkspaceApplicationRestoreTool is the publisher-owned tool image that
// writes the data artifacts onto the application's storage. Fabric executes
// it; it never interprets the data semantics.
type WorkspaceApplicationRestoreTool struct {
	Image string `json:"image"`
}

// WorkspaceApplicationDataMaterial is one versioned, immutable declaration of
// the data an application deployment consumes: the artifact list with content
// checksums and the restore tool that writes them onto the workspace storage.
type WorkspaceApplicationDataMaterial struct {
	SchemaVersion    int                                `json:"schemaVersion"`
	ApplicationID    string                             `json:"applicationId"`
	Version          string                             `json:"version"`
	Artifacts        []WorkspaceApplicationDataArtifact `json:"artifacts"`
	RestoreTool      WorkspaceApplicationRestoreTool    `json:"restoreTool"`
	RestoreArguments []string                           `json:"restoreArguments,omitempty"`
}

func ValidateWorkspaceApplicationDataMaterial(material WorkspaceApplicationDataMaterial) error {
	if material.SchemaVersion != 1 ||
		!workspaceApplicationIDPattern.MatchString(strings.TrimSpace(material.ApplicationID)) ||
		!workspaceApplicationVersionPattern.MatchString(strings.TrimSpace(material.Version)) {
		return errors.New("workspace_application_data_material_invalid")
	}
	if len(material.Artifacts) == 0 {
		return errors.New("workspace_application_data_artifacts_required")
	}
	seenArtifacts := map[string]struct{}{}
	for _, artifact := range material.Artifacts {
		if !workspaceDataArtifactNamePattern.MatchString(artifact.Name) {
			return errors.New("workspace_application_data_artifact_name_invalid")
		}
		if _, duplicate := seenArtifacts[artifact.Name]; duplicate {
			return errors.New("workspace_application_data_artifact_duplicate")
		}
		seenArtifacts[artifact.Name] = struct{}{}
		if !workspaceDataSHA256Pattern.MatchString(artifact.SHA256) {
			return errors.New("workspace_application_data_artifact_digest_invalid")
		}
		if artifact.SizeBytes < 0 {
			return errors.New("workspace_application_data_artifact_size_invalid")
		}
	}
	if !ValidWorkspaceImageReference(material.RestoreTool.Image) {
		return errors.New("workspace_application_restore_tool_invalid")
	}
	for _, argument := range material.RestoreArguments {
		if strings.TrimSpace(argument) == "" {
			return errors.New("workspace_application_restore_argument_invalid")
		}
	}
	return nil
}
