package contracts

import "testing"

func validDataMaterial() WorkspaceApplicationDataMaterial {
	return WorkspaceApplicationDataMaterial{
		SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0",
		Artifacts: []WorkspaceApplicationDataArtifact{
			{Name: "documents", SHA256: "a000000000000000000000000000000000000000000000000000000000000000", SizeBytes: 1024},
			{Name: "index", SHA256: "b100000000000000000000000000000000000000000000000000000000000000", SizeBytes: 2048},
		},
		RestoreTool:      WorkspaceApplicationRestoreTool{Image: "repo.example/restore@sha256:" + "c000000000000000000000000000000000000000000000000000000000000000"},
		RestoreArguments: []string{"--target", "/data"},
	}
}

func TestValidateWorkspaceApplicationDataMaterial(t *testing.T) {
	material := validDataMaterial()
	if err := ValidateWorkspaceApplicationDataMaterial(material); err != nil {
		t.Fatalf("valid material rejected: %v", err)
	}
	for name, mutate := range map[string]func(*WorkspaceApplicationDataMaterial){
		"schema version":   func(m *WorkspaceApplicationDataMaterial) { m.SchemaVersion = 2 },
		"application id":   func(m *WorkspaceApplicationDataMaterial) { m.ApplicationID = "Knowledge App" },
		"version":          func(m *WorkspaceApplicationDataMaterial) { m.Version = "" },
		"empty artifacts":  func(m *WorkspaceApplicationDataMaterial) { m.Artifacts = nil },
		"artifact name":    func(m *WorkspaceApplicationDataMaterial) { m.Artifacts[0].Name = "Data Set" },
		"artifact digest":  func(m *WorkspaceApplicationDataMaterial) { m.Artifacts[0].SHA256 = "nothex" },
		"artifact size":    func(m *WorkspaceApplicationDataMaterial) { m.Artifacts[0].SizeBytes = -1 },
		"restore tool":     func(m *WorkspaceApplicationDataMaterial) { m.RestoreTool.Image = "repo.example/restore:latest" },
		"restore argument": func(m *WorkspaceApplicationDataMaterial) { m.RestoreArguments = []string{" "} },
	} {
		broken := validDataMaterial()
		mutate(&broken)
		if err := ValidateWorkspaceApplicationDataMaterial(broken); err == nil {
			t.Fatalf("%s: invalid material accepted", name)
		}
	}
	duplicate := validDataMaterial()
	duplicate.Artifacts = append(duplicate.Artifacts, duplicate.Artifacts[0])
	if err := ValidateWorkspaceApplicationDataMaterial(duplicate); err == nil {
		t.Fatal("duplicate artifact names must be rejected")
	}
}
