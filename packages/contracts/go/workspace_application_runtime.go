package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
)

// WorkspaceApplicationRuntimeComponentState is the authoritative per-component
// observation of one deployed application runtime. State values mirror the
// runtime observation vocabulary: absent, pending, ready, failed.
type WorkspaceApplicationRuntimeComponentState struct {
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Image      string   `json:"image"`
	State      string   `json:"state"`
	Ports      []int    `json:"ports,omitempty"`
	LastError  string   `json:"lastError,omitempty"`
	ReadyCheck string   `json:"readyCheck,omitempty"`
	CheckNames []string `json:"checkNames,omitempty"`
}

// WorkspaceApplicationRuntimeObservation is the whole-runtime readback: every
// declared component of the admitted revision, observed where it actually
// runs. A component missing from the observation never proves absence; the
// observer must report it with State "absent".
type WorkspaceApplicationRuntimeObservation struct {
	SchemaVersion int    `json:"schemaVersion"`
	WorkspaceID   string `json:"workspaceId"`
	RuntimeID     string `json:"runtimeId"`
	Status        string `json:"status"`
	// EntryURL is the user-facing web entry of the main component. It is set
	// when the exposure policy and explicit EntryPort allow a published entry and
	// stays empty for cloud-private or worker-only applications.
	EntryURL   string                                      `json:"entryUrl,omitempty"`
	Components []WorkspaceApplicationRuntimeComponentState `json:"components"`
}

const (
	// WorkspaceApplicationComponentMain is the revision's primary component.
	WorkspaceApplicationComponentMain = "main"
	// WorkspaceApplicationComponentDependency marks a supporting service
	// declared by the revision's Dependencies.
	WorkspaceApplicationComponentDependency = "dependency"
)

// WorkspaceApplicationRuntimeComponents derives the declared component list of
// one revision: the main component plus one component per dependency. Names
// and images are checked by revision admission before provider execution.
func WorkspaceApplicationRuntimeComponents(revision WorkspaceApplicationRevision) []WorkspaceApplicationRuntimeComponentState {
	components := []WorkspaceApplicationRuntimeComponentState{{
		Name: WorkspaceApplicationComponentMain, Role: WorkspaceApplicationComponentMain,
		Image: revision.Image, State: "absent",
	}}
	for _, dependency := range revision.Dependencies {
		components = append(components, WorkspaceApplicationRuntimeComponentState{
			Name: dependency.Name, Role: WorkspaceApplicationComponentDependency,
			Image: dependency.Image, State: "absent",
		})
	}
	return components
}

// WorkspaceApplicationRuntimeOverallStatus derives the runtime status from
// its components: any failed component fails the runtime, an all-absent
// runtime is absent, any pending component keeps it pending, and only an
// all-ready runtime is ready. A partially created runtime whose existing
// components are all suspended is suspended; missing components remain absent.
func WorkspaceApplicationRuntimeOverallStatus(components []WorkspaceApplicationRuntimeComponentState) string {
	absent, pending, ready, suspended := 0, 0, 0, 0
	for _, component := range components {
		switch component.State {
		case "failed":
			return "failed"
		case "absent":
			absent++
		case "pending":
			pending++
		case "ready":
			ready++
		case "suspended":
			suspended++
		}
	}
	switch {
	case suspended+absent == len(components) && suspended > 0:
		return "suspended"
	case absent == len(components):
		return "absent"
	case pending > 0 || ready < len(components):
		return "pending"
	case ready == len(components):
		return "ready"
	}
	return "pending"
}

// ValidateWorkspaceApplicationRuntimeObservation checks one observation
// against the revision it claims to observe: every declared component appears
// exactly once with the declared image, and states use the runtime vocabulary.
func ValidateWorkspaceApplicationRuntimeObservation(revision WorkspaceApplicationRevision, observation WorkspaceApplicationRuntimeObservation) error {
	if observation.SchemaVersion != 1 || observation.WorkspaceID == "" {
		return errors.New("workspace_application_runtime_observation_invalid")
	}
	switch observation.Status {
	case "absent", "pending", "ready", "failed", "suspended":
	default:
		return errors.New("workspace_application_runtime_status_invalid")
	}
	declared := WorkspaceApplicationRuntimeComponents(revision)
	seen := map[string]struct{}{}
	byName := map[string]WorkspaceApplicationRuntimeComponentState{}
	for _, component := range observation.Components {
		if _, duplicate := seen[component.Name]; duplicate {
			return errors.New("workspace_application_runtime_component_duplicate")
		}
		seen[component.Name] = struct{}{}
		byName[component.Name] = component
		switch component.State {
		case "absent", "pending", "ready", "failed", "suspended":
		default:
			return errors.New("workspace_application_runtime_component_state_invalid")
		}
	}
	for _, expected := range declared {
		observed, found := byName[expected.Name]
		if !found {
			return errors.New("workspace_application_runtime_component_missing")
		}
		if observed.Image != expected.Image || observed.Role != expected.Role {
			return errors.New("workspace_application_runtime_component_identity_mismatch")
		}
	}
	if len(observation.Components) != len(declared) {
		return errors.New("workspace_application_runtime_component_unexpected")
	}
	if observation.Status != WorkspaceApplicationRuntimeOverallStatus(observation.Components) {
		return errors.New("workspace_application_runtime_status_mismatch")
	}
	return nil
}

// WorkspaceApplicationRuntimeConfiguration contains only application-owned,
// non-secret process configuration. Credentials use immutable Secret bindings.
type WorkspaceApplicationRuntimeConfiguration struct {
	Environment map[string]string `json:"environment,omitempty"`
	Files       map[string]string `json:"files,omitempty"`
	// OPL credentials are stable across image attempts. Only an explicit
	// credential rotation advances Version; migration retains its proven source.
	CredentialVersion                  string `json:"credentialVersion,omitempty"`
	CredentialSourceRuntimeOperationID string `json:"credentialSourceRuntimeOperationId,omitempty"`
}

type WorkspaceApplicationRuntimeSecretBinding struct {
	Name      string `json:"name"`
	SecretRef string `json:"secretRef"`
	Version   string `json:"version"`
	Key       string `json:"key"`
}

// SchemaVersion 2 binds a runtime to a deployment generation, actual
// configuration, and isolated persistent data. Version 0 is historical only.
type WorkspaceApplicationRuntimeInput struct {
	SchemaVersion                int                                        `json:"schemaVersion"`
	AccountID                    string                                     `json:"accountId"`
	WorkspaceID                  string                                     `json:"workspaceId"`
	ComputeID                    string                                     `json:"computeId"`
	VolumeID                     string                                     `json:"volumeId"`
	AttachmentID                 string                                     `json:"attachmentId"`
	AttachmentOperationID        string                                     `json:"attachmentOperationId"`
	RuntimeOperationID           string                                     `json:"runtimeOperationId"`
	Revision                     WorkspaceApplicationRevision               `json:"revision"`
	Configuration                WorkspaceApplicationRuntimeConfiguration   `json:"configuration"`
	SecretBindings               []WorkspaceApplicationRuntimeSecretBinding `json:"secretBindings,omitempty"`
	DataSourceRuntimeOperationID string                                     `json:"dataSourceRuntimeOperationId,omitempty"`
	DataLayout                   string                                     `json:"dataLayout,omitempty"`
	DataBindingID                string                                     `json:"dataBindingId"`
	ConfigurationDigest          string                                     `json:"configurationDigest"`
	IdempotencyKey               string                                     `json:"-"`
	OperationID                  string                                     `json:"-"`
}

type WorkspaceApplicationRuntimeLifecycleInput struct {
	HistoricalApplicationRuntime bool   `json:"historicalApplicationRuntime,omitempty"`
	LegacyRuntime                bool   `json:"legacyRuntime,omitempty"`
	AccountID                    string `json:"accountId"`
	WorkspaceID                  string `json:"workspaceId"`
	RuntimeID                    string `json:"runtimeId"`
	RuntimeOperationID           string `json:"runtimeOperationId"`
	DesiredState                 string `json:"desiredState"`
	IdempotencyKey               string `json:"-"`
}

type WorkspaceApplicationRuntimeImageRetirement struct {
	Image string `json:"image"`
	State string `json:"state"` // removed, retained_reference, absent, instance_required
}

type WorkspaceApplicationRuntimeLifecycleResult struct {
	RuntimeID       string                                       `json:"runtimeId"`
	WorkspaceID     string                                       `json:"workspaceId"`
	State           string                                       `json:"state"` // pending, running, suspended, absent
	Observation     WorkspaceApplicationRuntimeObservation       `json:"observation"`
	ImageRetirement []WorkspaceApplicationRuntimeImageRetirement `json:"imageRetirement,omitempty"`
}

type WorkspaceApplicationRuntimeCredentials struct {
	RuntimeID     string `json:"runtimeId"`
	WorkspaceID   string `json:"workspaceId"`
	WebUIUsername string `json:"webuiUsername"`
	WebUIPassword string `json:"webuiPassword"`
}

func WorkspaceApplicationRuntimeID(runtimeOperationID string) string {
	digest := sha256.Sum256([]byte("workspace_application_runtime_generation:" + runtimeOperationID))
	return "rt_app_" + hex.EncodeToString(digest[:16])
}

// WorkspaceApplicationDataDirectory is a deterministic opaque subdirectory;
// user-controlled identifiers never become filesystem or Kubernetes paths.
func WorkspaceApplicationDataDirectory(dataBindingID string) string {
	digest := sha256.Sum256([]byte("workspace_application_data:" + dataBindingID))
	return "app-data-" + hex.EncodeToString(digest[:16])
}

var workspaceApplicationEnvironmentName = regexp.MustCompile(`^[-._A-Za-z][-._A-Za-z0-9]*$`)

// Container environment keys follow the common Docker/Kubernetes name grammar,
// not shell assignment identifiers (for example Elasticsearch uses dotted keys).
func ValidWorkspaceApplicationEnvironmentName(name string) bool {
	return workspaceApplicationEnvironmentName.MatchString(name)
}

// WorkspaceApplicationConfigurationDigest binds actual non-secret inputs and
// Secret identities. Binding order is not semantically significant.
func WorkspaceApplicationConfigurationDigest(configuration WorkspaceApplicationRuntimeConfiguration, bindings []WorkspaceApplicationRuntimeSecretBinding, dataBindingID string) (string, error) {
	if strings.TrimSpace(dataBindingID) == "" {
		return "", errors.New("workspace_application_data_binding_required")
	}
	for name, value := range configuration.Environment {
		if !workspaceApplicationEnvironmentName.MatchString(name) || strings.ContainsRune(value, 0) {
			return "", errors.New("workspace_application_configuration_invalid")
		}
	}
	totalFileBytes := 0
	for name, content := range configuration.Files {
		if !workspaceApplicationInputNamePattern.MatchString(name) || len(content) > 256*1024 || strings.ContainsRune(content, 0) {
			return "", errors.New("workspace_application_config_file_invalid")
		}
		totalFileBytes += len(content)
	}
	if totalFileBytes > 1024*1024 {
		return "", errors.New("workspace_application_config_files_too_large")
	}
	ordered := append([]WorkspaceApplicationRuntimeSecretBinding(nil), bindings...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for i, binding := range ordered {
		if binding.Name == "" || binding.SecretRef == "" || binding.Version == "" || binding.Key == "" || (i > 0 && ordered[i-1].Name == binding.Name) {
			return "", errors.New("workspace_application_secret_binding_invalid")
		}
	}
	payload, err := json.Marshal(struct {
		Configuration  WorkspaceApplicationRuntimeConfiguration   `json:"configuration"`
		SecretBindings []WorkspaceApplicationRuntimeSecretBinding `json:"secretBindings,omitempty"`
		DataBindingID  string                                     `json:"dataBindingId"`
	}{configuration, ordered, dataBindingID})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidateWorkspaceApplicationRuntimeConfiguration(input WorkspaceApplicationRuntimeInput) error {
	if input.SchemaVersion != 2 {
		return errors.New("workspace_application_runtime_schema_unsupported")
	}
	if input.Revision.RuntimeProfile == "opl_app" {
		if strings.TrimSpace(input.Configuration.CredentialVersion) == "" {
			return errors.New("workspace_application_credential_version_required")
		}
	} else if input.Configuration.CredentialVersion != "" || input.Configuration.CredentialSourceRuntimeOperationID != "" {
		return errors.New("workspace_application_credentials_undeclared")
	}
	switch input.DataLayout {
	case "":
		if input.DataSourceRuntimeOperationID != "" {
			return errors.New("workspace_application_data_layout_invalid")
		}
	case "legacy_opl", "legacy_application":
		if input.DataSourceRuntimeOperationID == "" {
			return errors.New("workspace_application_data_source_required")
		}
	default:
		return errors.New("workspace_application_data_layout_invalid")
	}
	digest, err := WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		return err
	}
	if input.ConfigurationDigest != digest {
		return errors.New("workspace_application_configuration_digest_mismatch")
	}
	declared := map[string]bool{}
	for _, secret := range WorkspaceApplicationSecretInputs(input.Revision) {
		declared[secret.Name] = true
	}
	for _, binding := range input.SecretBindings {
		if !declared[binding.Name] {
			return errors.New("workspace_application_secret_binding_undeclared")
		}
		delete(declared, binding.Name)
	}
	if len(declared) != 0 {
		return errors.New("workspace_application_secret_binding_missing")
	}
	if err := validateWorkspaceApplicationInputs(input.Revision.SecretInputs, input.Revision.ConfigInputs, input.Configuration.Environment, append(append([]WorkspaceApplicationMount{}, input.Revision.PersistentMounts...), input.Revision.ScratchMounts...)); err != nil {
		return err
	}
	files := map[string]bool{}
	for _, file := range WorkspaceApplicationConfigInputs(input.Revision) {
		files[file.Name] = true
	}
	for name := range input.Configuration.Files {
		if !files[name] {
			return errors.New("workspace_application_config_file_undeclared")
		}
		delete(files, name)
	}
	if len(files) != 0 {
		return errors.New("workspace_application_config_file_missing")
	}
	return nil
}

// WorkspaceApplicationHistoricalRuntimeID identifies only the retained
// pre-generation application contract. New deployments never use this identity.
func WorkspaceApplicationHistoricalRuntimeID(workspaceID string) string {
	digest := sha256.Sum256([]byte("workspace_application_runtime:" + workspaceID))
	return "rt_app_" + hex.EncodeToString(digest[:])
}

type WorkspaceApplicationGatewaySecretCleanupInput struct {
	AccountID      string `json:"accountId"`
	WorkspaceID    string `json:"workspaceId"`
	SecretRef      string `json:"secretRef"`
	IdempotencyKey string `json:"-"`
}

func WorkspaceGatewaySecretRef(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return "opl-gateway-" + hex.EncodeToString(digest[:8])
}
