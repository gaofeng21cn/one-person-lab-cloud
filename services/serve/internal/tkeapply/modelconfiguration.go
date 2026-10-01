package tkeapply

// This file owns the publisher-declared model configuration interface: the one
// HTTP interface a frozen Runtime Release declares for applying a model
// configuration to a running Workspace application and reading the applied
// version back.
//
// The interface belongs to the process that owns the workload, because only that
// process knows how a declared port becomes a reachable destination and because
// the control credential the interface authorizes itself with is materialised by
// the same boundary that materialises the application's own declared secret
// inputs. A boundary that cannot execute the declared interface refuses instead
// of reporting a version it never read.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// ModelConfigurationProtocol is the one protocol the publisher contract declares
// for this interface. Any other protocol is a capability this boundary does not
// have, so it refuses rather than guessing a wire.
const ModelConfigurationProtocol = "opl-model-config/v1"

// modelConfigurationTimeout bounds one apply or readback request. A runtime that
// does not answer within it leaves the configuration unconfirmed; the caller
// retries the same immutable version and payload.
const modelConfigurationTimeout = 60 * time.Second

var (
	// ErrModelConfigurationUnsupported refuses an interface this boundary cannot
	// execute: an unknown protocol, a path that is not a declared application
	// path, a field list the contract does not define, or an authorization that
	// names no declared secret input.
	ErrModelConfigurationUnsupported = errors.New("workspace_application_model_configuration_unsupported")
	// ErrModelConfigurationUnconfirmed reports that the running application did
	// not report the requested configuration version. The requested version is
	// never the applied one.
	ErrModelConfigurationUnconfirmed = errors.New("workspace_application_model_configuration_unconfirmed")
	// ErrModelConfigurationConflict reports that the application answered the
	// requested version with a different selection payload. One version names one
	// payload, so this is a conflict rather than an applied configuration.
	ErrModelConfigurationConflict = errors.New("workspace_application_model_configuration_conflict")
)

// ModelConfigurationContract is the frozen publisher declaration of the model
// configuration interface, projected from the approved Runtime Release the
// deployment was admitted against. It is a value the executor validates against
// the application revision before it addresses anything.
type ModelConfigurationContract struct {
	// Protocol is the declared wire. Only ModelConfigurationProtocol is
	// executable.
	Protocol string
	// PortName names one port declared by the application revision. The executor
	// resolves it to the destination it created for that port; it never uses a
	// port the revision does not declare.
	PortName string
	// ApplyPath and ReadbackPath are absolute application paths.
	ApplyPath    string
	ReadbackPath string
	// RequestFields and ReadbackFields are the declared payload fields. The
	// contract defines exactly version/selections and appliedVersion/selections.
	RequestFields  []string
	ReadbackFields []string
	// AuthorizationSecretInputName names the application revision's own secret
	// input carrying the control credential this interface authorizes with.
	AuthorizationSecretInputName string
}

// Declared reports whether the frozen interface exists at all. A release that
// declares none leaves the capability absent rather than supplying a default.
func (c ModelConfigurationContract) Declared() bool {
	return strings.TrimSpace(c.Protocol) != ""
}

// ModelConfiguration is the frozen interface a delivery carries: whether the
// release declares one, and its exact declaration.
type ModelConfiguration struct {
	Declared bool
	Contract ModelConfigurationContract
}

// ModelSelection is one declared model slot's chosen model. It is the only
// content this interface may carry: the interface is not a way to write arbitrary
// JSON into the application.
type ModelSelection struct {
	Slot    string
	ModelID string
}

// ModelConfigurationRequest is one apply request: the exact target version and the
// selections that version names.
type ModelConfigurationRequest struct {
	Version    int64
	Selections []ModelSelection
}

// ModelConfigurationReadback is what the running application reported: the version
// it actually holds and, when the contract declares the field, the selections that
// version names.
type ModelConfigurationReadback struct {
	AppliedVersion int64
	Selections     []ModelSelection
}

// modelConfigurationPayload is the declared request and readback body shape. The
// version travels as a decimal string so a 64-bit version is exact.
type modelConfigurationPayload struct {
	Version    string               `json:"version,omitempty"`
	Applied    string               `json:"appliedVersion,omitempty"`
	Selections []modelSelectionWire `json:"selections,omitempty"`
}

type modelSelectionWire struct {
	Slot    string `json:"slot"`
	ModelID string `json:"modelId"`
}

// ApplyModelConfiguration applies one model configuration to the running
// application through the frozen publisher interface and reads the applied version
// back. It reports only the version the application itself answered with: the
// requested version is never returned as the applied one, and an application that
// answers another version, or the same version with another payload, is refused.
func (p *Executor) ApplyModelConfiguration(ctx context.Context, input WorkspaceApplicationRuntimeInput, contract ModelConfigurationContract, request ModelConfigurationRequest) (ModelConfigurationReadback, error) {
	if err := p.clusterAccessError(); err != nil {
		return ModelConfigurationReadback{}, err
	}
	if err := validateModelConfigurationContract(contract, input.Revision); err != nil {
		return ModelConfigurationReadback{}, err
	}
	if request.Version <= 0 {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: a positive target version is required", ErrModelConfigurationUnsupported)
	}
	destination, err := modelConfigurationDestination(input, contract)
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	credential, err := p.modelConfigurationCredential(ctx, input, contract)
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	body, err := json.Marshal(modelConfigurationPayload{Version: strconv.FormatInt(request.Version, 10), Selections: modelSelectionWires(request.Selections)})
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	apply, err := p.modelConfigurationRequest(ctx, http.MethodPut, destination+contract.ApplyPath, credential, body)
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	apply.Body.Close()
	if apply.StatusCode < 200 || apply.StatusCode >= 300 {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the application refused the apply with HTTP %d", ErrModelConfigurationUnconfirmed, apply.StatusCode)
	}
	readback, err := p.modelConfigurationRequest(ctx, http.MethodGet, destination+contract.ReadbackPath, credential, nil)
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	defer readback.Body.Close()
	if readback.StatusCode < 200 || readback.StatusCode >= 300 {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the application refused the readback with HTTP %d", ErrModelConfigurationUnconfirmed, readback.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(readback.Body, 1<<20))
	if err != nil {
		return ModelConfigurationReadback{}, err
	}
	return modelConfigurationReadback(raw, contract, request)
}

// modelConfigurationReadback decodes the declared readback body and applies the
// contract's own rules: the applied version must be the requested one, and a
// declared selections field must be present and name exactly the requested
// payload.
func modelConfigurationReadback(raw []byte, contract ModelConfigurationContract, request ModelConfigurationRequest) (ModelConfigurationReadback, error) {
	var payload modelConfigurationPayload
	if json.Unmarshal(raw, &payload) != nil {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the application readback is not decodable", ErrModelConfigurationUnconfirmed)
	}
	applied, err := strconv.ParseInt(strings.TrimSpace(payload.Applied), 10, 64)
	if err != nil || applied <= 0 {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the application reported no applied version", ErrModelConfigurationUnconfirmed)
	}
	if applied != request.Version {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the application reported applied version %d, not the requested %d", ErrModelConfigurationUnconfirmed, applied, request.Version)
	}
	out := ModelConfigurationReadback{AppliedVersion: applied}
	if !declaresField(contract.ReadbackFields, "selections") {
		return out, nil
	}
	if payload.Selections == nil {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: the declared readback field selections is absent", ErrModelConfigurationUnconfirmed)
	}
	out.Selections = modelSelections(payload.Selections)
	if !sameModelSelections(out.Selections, request.Selections) {
		return ModelConfigurationReadback{}, fmt.Errorf("%w: version %d reported selections that differ from the applied payload", ErrModelConfigurationConflict, applied)
	}
	return out, nil
}

// modelConfigurationRequest issues one request to the application on the
// destination the executor created for the contract's declared port, presenting
// the declared control credential as its bearer.
func (p *Executor) modelConfigurationRequest(ctx context.Context, method, url, credential string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return p.modelConfigurationClient().Do(request)
}

// modelConfigurationClient is the boundary's own HTTP client for in-cluster
// application requests.
func (p *Executor) modelConfigurationClient() *http.Client {
	if p != nil && p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: modelConfigurationTimeout}
}

// modelConfigurationDestination resolves the declared port name to the exact
// destination the executor created for the application's main component: the
// Service it applied, on the port the revision declared under that name. A port
// the revision does not declare has no destination, so it is refused instead of
// being invented.
func modelConfigurationDestination(input WorkspaceApplicationRuntimeInput, contract ModelConfigurationContract) (string, error) {
	port, declared := declaredRevisionPort(input.Revision, contract.PortName)
	if !declared {
		return "", fmt.Errorf("%w: the application revision declares no port named %q", ErrModelConfigurationUnsupported, contract.PortName)
	}
	service := workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	if strings.TrimSpace(service) == "" {
		return "", fmt.Errorf("%w: the application destination is unresolvable", ErrModelConfigurationUnsupported)
	}
	return "http://" + net.JoinHostPort(service, strconv.Itoa(port.Port)), nil
}

// modelConfigurationCredential reads the value of the declared authorization
// secret input: the confirmed Secret binding this runtime was already injected
// from. The interface authorizes with exactly the value the application holds, so
// neither side invents a second credential and this boundary stores none. A
// declared input the runtime carries no confirmed binding for has no value to
// present, so the interface is refused instead of being authorized with a guessed
// one.
func (p *Executor) modelConfigurationCredential(ctx context.Context, input WorkspaceApplicationRuntimeInput, contract ModelConfigurationContract) (string, error) {
	name := strings.TrimSpace(contract.AuthorizationSecretInputName)
	if !declaresSecretInput(input.Revision, name) {
		return "", fmt.Errorf("%w: the application revision declares no secret input named %q", ErrModelConfigurationUnsupported, name)
	}
	for _, binding := range input.SecretBindings {
		if binding.Name != name {
			continue
		}
		value, err := p.readApplicationBoundSecret(ctx, input, binding)
		if err != nil {
			return "", fmt.Errorf("%w: the declared secret input %q is not delivered: %w", ErrModelConfigurationUnsupported, name, err)
		}
		return value, nil
	}
	return "", fmt.Errorf("%w: the runtime carries no confirmed Secret binding for the declared secret input %q", ErrModelConfigurationUnsupported, name)
}

// validateModelConfigurationContract refuses an interface the boundary cannot
// execute as declared: an unknown protocol, a port or authorization the revision
// does not declare, a path that is not an absolute application path, or a field
// list that is not the contract's own.
func validateModelConfigurationContract(contract ModelConfigurationContract, revision contracts.WorkspaceApplicationRevision) error {
	if strings.TrimSpace(contract.Protocol) != ModelConfigurationProtocol {
		return fmt.Errorf("%w: protocol %q is not %s", ErrModelConfigurationUnsupported, contract.Protocol, ModelConfigurationProtocol)
	}
	if _, declared := declaredRevisionPort(revision, contract.PortName); !declared {
		return fmt.Errorf("%w: the application revision declares no port named %q", ErrModelConfigurationUnsupported, contract.PortName)
	}
	for _, declaredPath := range []string{contract.ApplyPath, contract.ReadbackPath} {
		if !declaredApplicationPath(declaredPath) {
			return fmt.Errorf("%w: %q is not an absolute application path", ErrModelConfigurationUnsupported, declaredPath)
		}
	}
	if !sameFields(contract.RequestFields, "version", "selections") {
		return fmt.Errorf("%w: the declared request fields are not version and selections", ErrModelConfigurationUnsupported)
	}
	if !sameFields(contract.ReadbackFields, "appliedVersion", "selections") {
		return fmt.Errorf("%w: the declared readback fields are not appliedVersion and selections", ErrModelConfigurationUnsupported)
	}
	if !declaresSecretInput(revision, contract.AuthorizationSecretInputName) {
		return fmt.Errorf("%w: the application revision declares no secret input named %q", ErrModelConfigurationUnsupported, contract.AuthorizationSecretInputName)
	}
	return nil
}

// declaredApplicationPath reports whether one declared interface path is an
// absolute, non-escaping application path. A relative or traversing path is
// refused rather than normalized into an address the publisher never declared.
func declaredApplicationPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.Contains(value, "..") {
		return false
	}
	return path.Clean(value) == value
}

func declaredRevisionPort(revision contracts.WorkspaceApplicationRevision, name string) (contracts.WorkspaceApplicationPort, bool) {
	for _, port := range revision.Ports {
		if port.Name == name && port.Protocol == "TCP" {
			return port, true
		}
	}
	return contracts.WorkspaceApplicationPort{}, false
}

func declaresSecretInput(revision contracts.WorkspaceApplicationRevision, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, secret := range revision.SecretInputs {
		if secret.Name == name {
			return true
		}
	}
	return false
}

func declaresField(fields []string, name string) bool {
	for _, field := range fields {
		if field == name {
			return true
		}
	}
	return false
}

func sameFields(fields []string, expected ...string) bool {
	if len(fields) != len(expected) {
		return false
	}
	for _, name := range expected {
		if !declaresField(fields, name) {
			return false
		}
	}
	return true
}

func modelSelectionWires(selections []ModelSelection) []modelSelectionWire {
	wires := make([]modelSelectionWire, 0, len(selections))
	for _, selection := range selections {
		wires = append(wires, modelSelectionWire{Slot: selection.Slot, ModelID: selection.ModelID})
	}
	sort.Slice(wires, func(i, j int) bool { return wires[i].Slot < wires[j].Slot })
	return wires
}

func modelSelections(wires []modelSelectionWire) []ModelSelection {
	selections := make([]ModelSelection, 0, len(wires))
	for _, wire := range wires {
		selections = append(selections, ModelSelection{Slot: wire.Slot, ModelID: wire.ModelID})
	}
	return selections
}

// sameModelSelections compares two selection sets by their declared slots, which is
// the identity one model configuration version names.
func sameModelSelections(a, b []ModelSelection) bool {
	if len(a) != len(b) {
		return false
	}
	index := make(map[string]string, len(a))
	for _, selection := range a {
		index[selection.Slot] = selection.ModelID
	}
	for _, selection := range b {
		if index[selection.Slot] != selection.ModelID {
			return false
		}
	}
	return true
}
