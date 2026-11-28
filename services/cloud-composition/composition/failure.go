package composition

import (
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

// CompositionSubject names the composition itself as the subject of a failure
// that belongs to no single owner, such as a missing or unreadable owner
// topology contract.
const CompositionSubject = "composition"

// Failure is a composition error the entrypoint can classify. It carries the
// canonical code and the owner the failure belongs to, so the composed backend
// prints one machine-readable line next to its human message and an
// installation can read the reason without reading a free-text message.
type Failure struct {
	Code  contracts.CompositionFailureCode
	Owner string
	Err   error
}

func (f *Failure) Error() string { return f.Err.Error() }

func (f *Failure) Unwrap() error { return f.Err }

// MaterialError is the error the certificate material check returns. It is
// classified separately from an owner that refuses to start: an installation
// that cannot provide the owner certificates needs a different action than an
// owner whose own configuration it rejects.
type MaterialError struct {
	Owner string
	Err   error
}

func (e *MaterialError) Error() string { return e.Err.Error() }

func (e *MaterialError) Unwrap() error { return e.Err }

// fail classifies an error as a composition failure.
func fail(code contracts.CompositionFailureCode, owner string, err error) error {
	return &Failure{Code: code, Owner: owner, Err: err}
}

// NewFailure classifies an error as a composition failure. The entrypoint uses
// it for the failures it owns itself, such as a missing owner topology
// contract.
func NewFailure(code contracts.CompositionFailureCode, owner string, err error) error {
	return fail(code, owner, err)
}

// classifyStartFailure classifies the error that kept one owner from starting.
// A missing certificate is the installation's own gap, every other start
// failure belongs to the owner that refused.
func classifyStartFailure(process Process, err error) error {
	var material *MaterialError
	if errors.As(err, &material) {
		return fail(contracts.CompositionFailureOwnerCertificateMissing, material.Owner, err)
	}
	return fail(contracts.CompositionFailureOwnerStartFailed, process.Service, err)
}
