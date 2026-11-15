package build

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// The workspace image trust source is the admission decision for the images a
// workspace may start. The Control Plane owns it, so a publisher registers the
// repository it published to once and every provider admits that repository —
// no allowlist file and no environment edit is made by a human.
//
// The Build owner is the producer of that fact: it is the only owner that knows
// a repository was published by one of its own jobs. The producer records its
// admission intent in the same transaction that records the artifact, so a
// confirmed artifact always carries exactly one intent and a failed build never
// carries one.

const (
	// trustSourceCredentialMissing is a producer that holds no shared credential.
	trustSourceCredentialMissing = "trust_source_credential_missing"
	// trustSourceUnauthorized is a credential the Control Plane refused.
	trustSourceUnauthorized = "trust_source_unauthorized"
	// trustSourceConflict is a trust source revision that moved under the producer.
	trustSourceConflict = "trust_source_conflict"
	// trustSourceRequestRejected is a request the Control Plane refused on its facts.
	trustSourceRequestRejected = "trust_source_request_rejected"
	// trustSourceUnavailable is a Control Plane that could not be reached or failed.
	trustSourceUnavailable = "trust_source_unavailable"
	// trustSourceResponseInvalid is a response this producer cannot trust.
	trustSourceResponseInvalid = "trust_source_response_invalid"
	// trustSourceRegistrationFailed is the code the build job carries while its
	// admission intent is still open.
	trustSourceRegistrationFailed = "trust_source_registration_failed"
)

// trustSourceFailure names the failure an admission attempt hit. The code is
// the durable signal the build job carries and the registration row keeps, so
// it names what happened instead of a transport detail.
type trustSourceFailure struct {
	code   string
	status int
}

func (e *trustSourceFailure) Error() string { return e.code }

// TrustSourceRegistrar publishes admission decisions to the Control Plane that
// owns the workspace image trust source.
type TrustSourceRegistrar interface {
	// TrustSource reads the registered trust source, including its revision.
	TrustSource(ctx context.Context) (contracts.WorkspaceImageTrustSource, error)
	// RegisterRepository registers one release repository and returns the trust
	// source that carries it.
	RegisterRepository(ctx context.Context, request contracts.WorkspaceImageReleaseActivationRequest) (contracts.WorkspaceImageTrustSource, error)
}

// trustSourceClient speaks to the Control Plane's trust source route.
type trustSourceClient struct {
	url    string
	token  string
	client *http.Client
}

// NewTrustSourceRegistrar returns the producer that publishes this owner's
// admission decisions to the Control Plane that owns the trust source.
func NewTrustSourceRegistrar(getenv func(string) string) TrustSourceRegistrar {
	return newTrustSourceClient(getenv)
}

// newTrustSourceClient resolves the Control Plane's address and the shared
// credential. Both come from the installation environment, the same way the
// Console BFF, the Workspace owner and the Fabric owner resolve their Control
// Plane address, so an installation that moves the Control Plane moves every
// owner with it. The address falls back to the deployment topology's default
// when the installation does not name one.
//
// The environment names are spelled out literally here because the owner
// topology declaration is derived from the literal reads in the source.
func newTrustSourceClient(getenv func(string) string) *trustSourceClient {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	url := strings.TrimSpace(getenv("OPL_CONTROL_PLANE_URL"))
	if url == "" {
		url = contracts.WorkspaceImageTrustSourceURLDefault
	}
	return &trustSourceClient{
		url:    strings.TrimSuffix(url, "/"),
		token:  strings.TrimSpace(getenv("OPL_FABRIC_CAPABILITY_KEY")),
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

// TrustSource reads the registered trust source from the Control Plane.
func (c *trustSourceClient) TrustSource(ctx context.Context) (contracts.WorkspaceImageTrustSource, error) {
	if c == nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	if c.token == "" {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceCredentialMissing}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+contracts.WorkspaceImageTrustSourcePath, nil)
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	request.Header.Set(contracts.WorkspaceImageTrustSourceTokenHeader, "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return contracts.WorkspaceImageTrustSource{}, trustSourceStatusFailure(response.StatusCode)
	}
	return decodeTrustSource(response.Body)
}

// RegisterRepository registers one release repository. The Control Plane owns
// the revision: the request states the revision this producer read, and the
// Control Plane refuses a request whose revision moved.
func (c *trustSourceClient) RegisterRepository(ctx context.Context, request contracts.WorkspaceImageReleaseActivationRequest) (contracts.WorkspaceImageTrustSource, error) {
	if c == nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	if c.token == "" {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceCredentialMissing}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceRequestRejected}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, c.url+contracts.WorkspaceImageTrustSourcePath, bytes.NewReader(body))
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(contracts.WorkspaceImageTrustSourceTokenHeader, "Bearer "+c.token)
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceUnavailable}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return contracts.WorkspaceImageTrustSource{}, trustSourceStatusFailure(response.StatusCode)
	}
	return decodeTrustSource(response.Body)
}

// trustSourceStatusFailure names the outcome a status code carries.
func trustSourceStatusFailure(status int) error {
	switch {
	case status == http.StatusConflict:
		return &trustSourceFailure{code: trustSourceConflict, status: status}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &trustSourceFailure{code: trustSourceUnauthorized, status: status}
	case status >= 400 && status < 500:
		return &trustSourceFailure{code: trustSourceRequestRejected, status: status}
	default:
		return &trustSourceFailure{code: trustSourceUnavailable, status: status}
	}
}

// decodeTrustSource reads the source envelope and validates the source it
// carries. An envelope this producer cannot decode is not a trust decision,
// and an envelope that carries no source at all is not an empty trust source:
// the stored value's own empty form is an installation fact, a missing field in
// a response is a response this producer cannot read.
func decodeTrustSource(reader io.Reader) (contracts.WorkspaceImageTrustSource, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(reader).Decode(&envelope); err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceResponseInvalid}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceResponseInvalid}
	}
	source, err := contracts.DecodeWorkspaceImageTrustSource(string(envelope.Data))
	if err != nil {
		return contracts.WorkspaceImageTrustSource{}, &trustSourceFailure{code: trustSourceResponseInvalid}
	}
	return source, nil
}

// trustSourceErrorCode names the failure an attempt hit.
func trustSourceErrorCode(err error) string {
	var failure *trustSourceFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return trustSourceUnavailable
}

// registrationIntent is one admission the Build owner owes the trust source.
type registrationIntent struct {
	ID                string
	BuildJobID        string
	PackageVersionID  string
	ReleaseRepository string
	ReleaseVersion    string
	AttemptCount      int
	LastErrorCode     string
	JobStage          string
}

// registrationReason is the auditable sentence the Control Plane records with
// the registration. It names the build job and the package version it
// published, so an audit row can be traced back to the publication.
func registrationReason(intent registrationIntent) string {
	return fmt.Sprintf("build job %s published package version %s", intent.BuildJobID, intent.PackageVersionID)
}

// trustSourceRetryDelay is how long a producer waits before it tries again. A
// credential that was refused and a Control Plane that was unreachable are
// both worth another attempt, but not on every tick.
func trustSourceRetryDelay(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return 5 * time.Second
	case attempts == 2:
		return 15 * time.Second
	case attempts == 3:
		return time.Minute
	case attempts == 4:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// registrationIntentColumns is the projection every admission intent is read
// with: the intent itself, the package version it published, and the stage the
// build job is in.
const registrationIntentColumns = `r.id,r.build_job_id,j.package_version_id,r.release_repository,r.release_version,r.attempt_count,COALESCE(r.last_error_code,''),j.stage`

// scanRegistrationIntent reads one admission intent row.
func scanRegistrationIntent(row interface{ Scan(...any) error }) (registrationIntent, error) {
	var intent registrationIntent
	err := row.Scan(&intent.ID, &intent.BuildJobID, &intent.PackageVersionID, &intent.ReleaseRepository, &intent.ReleaseVersion, &intent.AttemptCount, &intent.LastErrorCode, &intent.JobStage)
	return intent, err
}

// dueRegistrations reads the admission intents this producer still owes. The
// trust source itself decides what is already admitted, so a producer that
// restarts re-reads the same intents until the Control Plane confirms them.
func dueRegistrations(ctx context.Context, db *sql.DB) ([]registrationIntent, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+registrationIntentColumns+` FROM build.build_trust_source_registrations r JOIN build.build_jobs j ON j.id=r.build_job_id WHERE r.status<>'registered' AND r.next_attempt_at<=now() ORDER BY r.next_attempt_at,r.id LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var intents []registrationIntent
	for rows.Next() {
		intent, err := scanRegistrationIntent(rows)
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// registrationIntentFor reads one build job's open admission intent. It is
// read by its build job rather than by the due scan, because a fresh
// confirmation is a new fact to publish even when an earlier attempt is
// waiting out its backoff.
func registrationIntentFor(ctx context.Context, db *sql.DB, buildJobID string) (*registrationIntent, error) {
	row := db.QueryRowContext(ctx, `SELECT `+registrationIntentColumns+` FROM build.build_trust_source_registrations r JOIN build.build_jobs j ON j.id=r.build_job_id WHERE r.build_job_id=$1 AND r.status<>'registered'`, buildJobID)
	intent, err := scanRegistrationIntent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &intent, nil
}

// publishTrustSource publishes one build job's admission intent now. A refused
// or unreachable Control Plane leaves the intent open with its named failure
// and the build job carrying the open item; the retry publishes it later.
func (s *Service) publishTrustSource(ctx context.Context, buildJobID string) {
	if s.trustSource == nil {
		return
	}
	intent, err := registrationIntentFor(ctx, s.store.DB(), buildJobID)
	if err != nil || intent == nil {
		return
	}
	_ = registerOneTrustSource(ctx, s.store.DB(), s.trustSource, *intent)
}

// registerTrustSources publishes the admission intents this owner owes. The
// Control Plane is the only writer of the trust source; this producer is the
// only owner that knows a repository was published by one of its own jobs.
func (s *Service) registerTrustSources(ctx context.Context) error {
	return registerPendingTrustSources(ctx, s.store.DB(), s.trustSource)
}

// registerPendingTrustSources publishes the admission intents this owner owes.
// A failure is recorded and retried; it never fails the build that produced
// the artifact, because the artifact and the admission are two facts.
func registerPendingTrustSources(ctx context.Context, db *sql.DB, registrar TrustSourceRegistrar) error {
	if registrar == nil {
		return nil
	}
	intents, err := dueRegistrations(ctx, db)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if err := registerOneTrustSource(ctx, db, registrar, intent); err != nil {
			return err
		}
	}
	return nil
}

// registerOneTrustSource publishes one admission intent.
func registerOneTrustSource(ctx context.Context, db *sql.DB, registrar TrustSourceRegistrar, intent registrationIntent) error {
	source, err := registrar.TrustSource(ctx)
	if err != nil {
		return recordTrustSourceFailure(ctx, db, intent, trustSourceErrorCode(err))
	}
	if source.TrustsRepository(intent.ReleaseRepository) {
		// The repository is already admitted: the same admission twice is the same
		// fact, so the trust source keeps the revision it already had.
		return recordTrustSourceSuccess(ctx, db, intent, source.Revision)
	}
	request := contracts.WorkspaceImageReleaseActivationRequest{
		ReleaseVersion:    intent.ReleaseVersion,
		ReleaseRepository: intent.ReleaseRepository,
		ExpectedRevision:  source.Revision,
		Reason:            registrationReason(intent),
	}
	updated, err := registrar.RegisterRepository(ctx, request)
	if err != nil {
		return recordTrustSourceFailure(ctx, db, intent, trustSourceErrorCode(err))
	}
	return recordTrustSourceSuccess(ctx, db, intent, updated.Revision)
}

// recordTrustSourceSuccess closes an admission intent and clears the open item
// the build job carried while it was open.
func recordTrustSourceSuccess(ctx context.Context, db *sql.DB, intent registrationIntent, revision int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE build.build_trust_source_registrations SET status='registered',trust_source_revision=$2,attempt_count=attempt_count+1,last_error_code=NULL,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND status<>'registered'`, intent.ID, revision); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE build.build_jobs SET error_code=NULL,updated_at=now() WHERE id=$1 AND error_code=$2`, intent.BuildJobID, trustSourceRegistrationFailed); err != nil {
		return err
	}
	return tx.Commit()
}

// recordTrustSourceFailure keeps an admission intent open, names the failure,
// and leaves the build job carrying the open item. The intent is the landing
// point: the next attempt reads it again until the Control Plane confirms it.
func recordTrustSourceFailure(ctx context.Context, db *sql.DB, intent registrationIntent, code string) error {
	attempts := intent.AttemptCount + 1
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE build.build_trust_source_registrations SET attempt_count=$2,last_error_code=$3,next_attempt_at=now()+($4::double precision*interval '1 second'),updated_at=now() WHERE id=$1`, intent.ID, attempts, code, trustSourceRetryDelay(attempts).Seconds()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE build.build_jobs SET error_code=$2,updated_at=now() WHERE id=$1 AND error_code IS DISTINCT FROM $2`, intent.BuildJobID, trustSourceRegistrationFailed); err != nil {
		return err
	}
	if intent.LastErrorCode != code {
		message := fmt.Sprintf("registering repository %s in the workspace image trust source failed: %s", intent.ReleaseRepository, code)
		if _, err = tx.ExecContext(ctx, `INSERT INTO build.build_logs(id,build_job_id,sequence,stage,level,message) SELECT $1,$2,COALESCE(MAX(sequence),-1)+1,$3,'error',$4 FROM build.build_logs WHERE build_job_id=$2`, id("log_"), intent.BuildJobID, intent.JobStage, message); err != nil {
			return err
		}
	}
	return tx.Commit()
}
