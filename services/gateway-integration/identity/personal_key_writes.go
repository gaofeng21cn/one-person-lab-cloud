package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const (
	// personalKeyNameLimit is the longest personal Key name this owner accepts.
	personalKeyNameLimit = 100
	// personalKeyIPListLimit is the longest IP list this owner accepts.
	personalKeyIPListLimit = 100
)

// personalKeyWritePayload decodes the provider's answer to a personal Key
// command. It carries the raw secret, which only a create or an explicit
// reveal may hand back to a caller. The ordinary personal projection never
// carries a secret.
type personalKeyWritePayload struct {
	personalKeyPayload
	Key string `json:"key"`
}

// personalUSDJSON renders USD micros as the provider's decimal string. Zero is
// a real amount here: a personal Key without a quota spends against the user's
// own balance, and the provider records that as zero.
func personalUSDJSON(micros int64) (json.RawMessage, error) {
	if micros < 0 {
		return nil, errors.New("a non-negative USD micros amount is required")
	}
	return json.RawMessage(fmt.Sprintf("%d.%06d", micros/1_000_000, micros%1_000_000)), nil
}

// normalizePersonalIPList canonicalizes the addresses and prefixes of one
// personal Key IP list. An unparseable entry is refused rather than dropped,
// so a caller never receives a Key bound to fewer addresses than it asked for.
func normalizePersonalIPList(values []string) ([]string, bool) {
	if len(values) > personalKeyIPListLimit {
		return nil, false
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, false
		}
		if address, err := netip.ParseAddr(value); err == nil {
			value = address.String()
		} else if prefix, err := netip.ParsePrefix(value); err == nil {
			value = prefix.Masked().String()
		} else {
			return nil, false
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, true
}

// personalKeyGroupID resolves the Group a personal Key belongs to. The Group
// id is a provider identity, so it must be the exact positive number the
// provider issued.
func personalKeyGroupID(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, status.Error(codes.InvalidArgument, "a positive Gateway group id is required")
	}
	return id, nil
}

// reservedPersonalKeyName refuses a personal Key name that could be mistaken
// for a Workspace-managed Key. The managed naming convention belongs to this
// owner, so the personal path reads it from the same fact instead of repeating
// a literal of its own.
func reservedPersonalKeyName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), managedKeyExactNamePrefix)
}

// mapPersonalKeyWriteError reports a refused or failed personal Key command. A
// rejected credential is dropped so the next call asks for a fresh login. Any
// other failure is an unknown outcome: the command may or may not have reached
// the provider, so it never reports success.
func (s *Service) mapPersonalKeyWriteError(credential *delegatedCredential, err error) error {
	var upstream *gatewayHTTPError
	if errors.As(err, &upstream) {
		switch upstream.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			s.forgetDelegatedSession(credential.key.session)
			return reauthenticationRequired()
		case http.StatusNotFound:
			return status.Error(codes.NotFound, "Gateway personal key not found")
		case http.StatusConflict:
			return status.Error(codes.AlreadyExists, "Gateway refused the personal key")
		case http.StatusTooManyRequests:
			return status.Error(codes.ResourceExhausted, "Gateway personal key writes rate limited")
		}
	}
	return status.Error(codes.Unavailable, "Gateway personal key write outcome is unknown")
}

func invalidPersonalWriteResponse() error {
	return status.Error(codes.Unavailable, "invalid Gateway personal key write response")
}

// personalEnvelope performs one personal Key command and returns the
// provider's data member. A command answers with an explicit success code;
// anything else is an unknown outcome, never a success.
func (s *Service) personalEnvelope(ctx context.Context, credential *delegatedCredential, method, path string, body any, headers map[string]string) (json.RawMessage, error) {
	data, err := s.personalCall(ctx, credential, method, path, body, headers)
	if err != nil {
		return nil, s.mapPersonalKeyWriteError(credential, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code == nil || *envelope.Code != 0 {
		return nil, invalidPersonalWriteResponse()
	}
	return envelope.Data, nil
}

// recordPersonalKeyAudit appends one immutable audit row for a personal Key
// command. It carries the Key identity and the outcome, never the raw secret.
func (s *Service) recordPersonalKeyAudit(ctx context.Context, session *api.Session, action, keyID, requestID string, details map[string]any, outcome string) error {
	if details == nil {
		details = map[string]any{}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return persistence(err)
	}
	defer tx.Rollback()
	if err = s.recordAudit(ctx, tx, auditEvent{tenantID: session.GetTenantId(), actorID: session.GetActorId(), action: action, resourceType: "gateway.personal_key", resourceID: keyID, requestID: requestID, details: details, outcome: outcome}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return persistence(err)
	}
	return nil
}

// CreatePersonalGatewayKey creates one personal Gateway Key for the caller's
// own account and returns the new Key's summary. Sub2API remains the truth for
// the Key, its Group and its limits; this owner carries the command and
// confirms the provider's own readback before it reports the new Key.
//
// The answer carries no plaintext. The provider's create answer does carry
// the secret, and this owner discards it at the boundary: the customer takes
// the secret from the explicit reveal command, which audits before the secret
// leaves.
func (s *Service) CreatePersonalGatewayKey(ctx context.Context, r *api.CreatePersonalGatewayKeyRpcRequest) (*api.PersonalGatewayKey, error) {
	body := r.GetBody()
	name := strings.TrimSpace(body.GetName())
	if name == "" || utf8.RuneCountInString(name) > personalKeyNameLimit || reservedPersonalKeyName(name) {
		return nil, status.Error(codes.InvalidArgument, "a valid personal key name is required")
	}
	groupID, err := personalKeyGroupID(body.GetGroupId())
	if err != nil {
		return nil, err
	}
	whitelist, ok := normalizePersonalIPList(body.GetIpWhitelist())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "a valid personal key IP whitelist is required")
	}
	blacklist, ok := normalizePersonalIPList(body.GetIpBlacklist())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "a valid personal key IP blacklist is required")
	}
	if body.GetQuotaUsdMicros() < 0 || body.GetRateLimit5HUsdMicros() < 0 || body.GetRateLimit1DUsdMicros() < 0 || body.GetRateLimit7DUsdMicros() < 0 {
		return nil, status.Error(codes.InvalidArgument, "personal key limits cannot be negative")
	}
	var expiresInDays *int
	if body.ExpiresInDays != nil {
		days := int(body.GetExpiresInDays())
		if days <= 0 {
			return nil, status.Error(codes.InvalidArgument, "a positive personal key expiry is required")
		}
		expiresInDays = &days
	}
	idempotencyKey := strings.TrimSpace(r.GetContext().GetIdempotencyKey())
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "an idempotency key is required")
	}
	session, credential, err := s.personalSession(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	request := map[string]any{"name": name, "group_id": groupID}
	quota, err := personalUSDJSON(body.GetQuotaUsdMicros())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "a valid personal key quota is required")
	}
	request["quota"] = quota
	if len(whitelist) > 0 {
		request["ip_whitelist"] = whitelist
	}
	if len(blacklist) > 0 {
		request["ip_blacklist"] = blacklist
	}
	if expiresInDays != nil {
		request["expires_in_days"] = *expiresInDays
	}
	for field, micros := range map[string]int64{
		"rate_limit_5h": body.GetRateLimit5HUsdMicros(),
		"rate_limit_1d": body.GetRateLimit1DUsdMicros(),
		"rate_limit_7d": body.GetRateLimit7DUsdMicros(),
	} {
		if micros <= 0 {
			continue
		}
		encoded, err := personalUSDJSON(micros)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "a valid personal key rate limit is required")
		}
		request[field] = encoded
	}
	// The command is registered before the provider write. A retry of the same
	// command replays the recorded answer instead of minting a second Key, and a
	// different request under the same idempotency key is refused.
	reservation := PersonalKeyCommandReservation{
		ActorID:            session.GetActorId(),
		ProviderSubject:    credential.key.subject,
		OperationName:      personalKeyCreateOperation,
		IdempotencyKey:     idempotencyKey,
		CommandFingerprint: personalKeyCommandFingerprint(personalKeyCreateOperation, session.GetActorId(), credential.key.subject, "", request),
		ResourceID:         "personal_key_create:" + session.GetActorId(),
	}
	record, existing, err := s.GatewayStore.ReservePersonalKeyCommand(ctx, reservation)
	if errors.Is(err, ErrPersonalKeyCommandConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different personal Gateway key command"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the personal Gateway key command registration is unavailable")
	}
	if existing {
		answer := &api.PersonalGatewayKey{}
		if err := personalKeyCommandAnswer(record, answer); err != nil {
			return nil, err
		}
		return answer, nil
	}
	data, err := s.personalEnvelope(ctx, credential, http.MethodPost, "/api/v1/keys", request, map[string]string{"Idempotency-Key": idempotencyKey})
	if err != nil {
		// A definite refusal settles the command. An unconfirmed outcome leaves it
		// pending, so no second Key is minted for one accepted command.
		if definitePersonalKeyRefusal(err) {
			refusePersonalKeyCommand(ctx, s, reservation, err)
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, invalidPersonalWriteResponse()
	}
	var created personalKeyWritePayload
	if json.Unmarshal(data, &created) != nil {
		return nil, invalidPersonalWriteResponse()
	}
	if created.ID <= 0 || strconv.FormatInt(created.UserID, 10) != credential.key.subject {
		return nil, invalidPersonalWriteResponse()
	}
	key, err := s.readPersonalKey(ctx, credential, strconv.FormatInt(created.ID, 10))
	if err != nil {
		return nil, err
	}
	if !personalKeyCreateConfirmed(key, name, groupID, whitelist, blacklist, body.GetQuotaUsdMicros(), body.GetRateLimit5HUsdMicros(), body.GetRateLimit1DUsdMicros(), body.GetRateLimit7DUsdMicros(), expiresInDays != nil) {
		return nil, status.Error(codes.Unavailable, "the created Gateway personal key does not match the request")
	}
	if err = s.recordPersonalKeyAudit(ctx, session, "gateway.personal_key_create", key.GetId(), r.GetContext().GetRequestId(), map[string]any{"name": name, "groupId": strconv.FormatInt(groupID, 10)}, "confirmed"); err != nil {
		return nil, err
	}
	if err = settlePersonalKeyCommand(ctx, s, reservation, key, key.GetId()); err != nil {
		return nil, err
	}
	return key, nil
}

// personalKeyCreateConfirmed checks the provider's own readback against the
// requested create. The readback is the only proof that the provider accepted
// the requested Group, limits and address lists.
func personalKeyCreateConfirmed(key *api.PersonalGatewayKey, name string, groupID int64, whitelist, blacklist []string, quota, rate5h, rate1d, rate7d int64, expiring bool) bool {
	return key.GetName() == name &&
		key.GetStatus() == api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_ACTIVE &&
		key.GetGroupId() == strconv.FormatInt(groupID, 10) &&
		slices.Equal(key.GetIpWhitelist(), whitelist) &&
		slices.Equal(key.GetIpBlacklist(), blacklist) &&
		key.GetQuotaUsdMicros() == quota &&
		key.GetRateLimit5HUsdMicros() == rate5h &&
		key.GetRateLimit1DUsdMicros() == rate1d &&
		key.GetRateLimit7DUsdMicros() == rate7d &&
		(!expiring || key.GetExpiresAt() != nil)
}

// personalKeyUpdateFacts are the facts one update command must see in the
// provider's readback. A fact the command did not name is absent and is not
// compared: an unrelated fact such as the consumed quota can move while the
// Key is in use, and that is not a contradiction of the command.
type personalKeyUpdateFacts struct {
	name      *string
	groupID   *string
	whitelist *[]string
	blacklist *[]string
	quota     *int64
	expiresAt *time.Time
	rate5h    *int64
	rate1d    *int64
	rate7d    *int64
	status    *api.PersonalGatewayKeyStatusEnum
	quotaUsed *int64
	usage5h   *int64
	usage1d   *int64
	usage7d   *int64
	details   map[string]any
}

// personalKeyUpdateRequest builds the provider's update body together with the
// facts its readback must confirm.
func personalKeyUpdateRequest(body *api.UpdatePersonalGatewayKeyRequest) (map[string]any, personalKeyUpdateFacts, error) {
	request := map[string]any{}
	expected := personalKeyUpdateFacts{details: map[string]any{}}
	if body.Name != nil {
		name := strings.TrimSpace(body.GetName())
		if name == "" || utf8.RuneCountInString(name) > personalKeyNameLimit || reservedPersonalKeyName(name) {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key name is required")
		}
		request["name"] = name
		expected.name = &name
		expected.details["name"] = name
	}
	if body.GroupId != nil {
		groupID, err := personalKeyGroupID(body.GetGroupId())
		if err != nil {
			return nil, expected, err
		}
		id := strconv.FormatInt(groupID, 10)
		request["group_id"] = groupID
		expected.groupID = &id
		expected.details["groupId"] = id
	}
	if body.GetIpWhitelist() != nil {
		values, ok := normalizePersonalIPList(body.GetIpWhitelist().GetValues())
		if !ok {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key IP whitelist is required")
		}
		request["ip_whitelist"] = values
		expected.whitelist = &values
		expected.details["ipWhitelist"] = values
	}
	if body.GetIpBlacklist() != nil {
		values, ok := normalizePersonalIPList(body.GetIpBlacklist().GetValues())
		if !ok {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key IP blacklist is required")
		}
		request["ip_blacklist"] = values
		expected.blacklist = &values
		expected.details["ipBlacklist"] = values
	}
	if body.QuotaUsdMicros != nil {
		micros := *body.QuotaUsdMicros
		if micros < 0 {
			return nil, expected, status.Error(codes.InvalidArgument, "personal key limits cannot be negative")
		}
		encoded, err := personalUSDJSON(micros)
		if err != nil {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key quota is required")
		}
		request["quota"] = encoded
		expected.quota = &micros
		expected.details["quotaUsdMicros"] = micros
	}
	if body.ExpiresAt != nil {
		stamp := body.ExpiresAt
		if stamp.CheckValid() != nil {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key expiry is required")
		}
		instant := stamp.AsTime().UTC().Truncate(time.Second)
		if !instant.After(time.Unix(0, 0)) {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key expiry is required")
		}
		request["expires_at"] = instant.Format(time.RFC3339)
		expected.expiresAt = &instant
		expected.details["expiresAt"] = instant.Format(time.RFC3339)
	}
	if body.RateLimit5HUsdMicros != nil {
		micros := *body.RateLimit5HUsdMicros
		if micros < 0 {
			return nil, expected, status.Error(codes.InvalidArgument, "personal key limits cannot be negative")
		}
		encoded, err := personalUSDJSON(micros)
		if err != nil {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key rate limit is required")
		}
		request["rate_limit_5h"] = encoded
		expected.rate5h = &micros
		expected.details["rateLimit5hUsdMicros"] = micros
	}
	if body.RateLimit1DUsdMicros != nil {
		micros := *body.RateLimit1DUsdMicros
		if micros < 0 {
			return nil, expected, status.Error(codes.InvalidArgument, "personal key limits cannot be negative")
		}
		encoded, err := personalUSDJSON(micros)
		if err != nil {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key rate limit is required")
		}
		request["rate_limit_1d"] = encoded
		expected.rate1d = &micros
		expected.details["rateLimit1dUsdMicros"] = micros
	}
	if body.RateLimit7DUsdMicros != nil {
		micros := *body.RateLimit7DUsdMicros
		if micros < 0 {
			return nil, expected, status.Error(codes.InvalidArgument, "personal key limits cannot be negative")
		}
		encoded, err := personalUSDJSON(micros)
		if err != nil {
			return nil, expected, status.Error(codes.InvalidArgument, "a valid personal key rate limit is required")
		}
		request["rate_limit_7d"] = encoded
		expected.rate7d = &micros
		expected.details["rateLimit7dUsdMicros"] = micros
	}
	if body.ResetQuota != nil && *body.ResetQuota {
		request["reset_quota"] = true
		zero := int64(0)
		expected.quotaUsed = &zero
		expected.details["resetQuota"] = true
	}
	if body.ResetRateLimitUsage != nil && *body.ResetRateLimitUsage {
		request["reset_rate_limit_usage"] = true
		zero := int64(0)
		expected.usage5h, expected.usage1d, expected.usage7d = &zero, &zero, &zero
		expected.details["resetRateLimitUsage"] = true
	}
	if body.Enabled != nil {
		enabled := *body.Enabled
		state := api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED
		providerState := "inactive"
		if enabled {
			state = api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_ACTIVE
			providerState = "active"
		}
		request["status"] = providerState
		expected.status = &state
		expected.details["enabled"] = enabled
	}
	if len(request) == 0 {
		return nil, expected, status.Error(codes.InvalidArgument, "a personal key change is required")
	}
	return request, expected, nil
}

// personalKeyUpdateConfirmed compares the provider's readback with the facts
// the command named.
func personalKeyUpdateConfirmed(key *api.PersonalGatewayKey, expected personalKeyUpdateFacts) bool {
	if expected.name != nil && key.GetName() != *expected.name {
		return false
	}
	if expected.groupID != nil && key.GetGroupId() != *expected.groupID {
		return false
	}
	if expected.whitelist != nil && !slices.Equal(key.GetIpWhitelist(), *expected.whitelist) {
		return false
	}
	if expected.blacklist != nil && !slices.Equal(key.GetIpBlacklist(), *expected.blacklist) {
		return false
	}
	if expected.quota != nil && key.GetQuotaUsdMicros() != *expected.quota {
		return false
	}
	if expected.expiresAt != nil {
		if key.GetExpiresAt() == nil || !key.GetExpiresAt().AsTime().UTC().Truncate(time.Second).Equal(*expected.expiresAt) {
			return false
		}
	}
	if expected.rate5h != nil && key.GetRateLimit5HUsdMicros() != *expected.rate5h {
		return false
	}
	if expected.rate1d != nil && key.GetRateLimit1DUsdMicros() != *expected.rate1d {
		return false
	}
	if expected.rate7d != nil && key.GetRateLimit7DUsdMicros() != *expected.rate7d {
		return false
	}
	if expected.status != nil && key.GetStatus() != *expected.status {
		return false
	}
	if expected.quotaUsed != nil && key.GetQuotaUsedUsdMicros() != *expected.quotaUsed {
		return false
	}
	if expected.usage5h != nil && (key.GetUsage5HUsdMicros() != *expected.usage5h || key.GetUsage1DUsdMicros() != *expected.usage1d || key.GetUsage7DUsdMicros() != *expected.usage7d) {
		return false
	}
	return true
}

// UpdatePersonalGatewayKey changes one personal Gateway Key the caller owns.
// A Workspace-managed Key is not a personal Key: its lifecycle belongs to the
// managed-Key coordination path, so this command refuses it instead of
// becoming a second writer for it.
func (s *Service) UpdatePersonalGatewayKey(ctx context.Context, r *api.UpdatePersonalGatewayKeyRpcRequest) (*api.PersonalGatewayKey, error) {
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	request, expected, err := personalKeyUpdateRequest(r.GetBody())
	if err != nil {
		return nil, err
	}
	// An update that names nothing is refused at the owner boundary. A provider
	// write with no instruction would still reset nothing, but it would answer
	// a command the caller never stated.
	if len(request) == 0 {
		return nil, status.Error(codes.InvalidArgument, "a personal Gateway key update requires at least one field")
	}
	idempotencyKey := strings.TrimSpace(r.GetContext().GetIdempotencyKey())
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "an idempotency key is required")
	}
	session, credential, err := s.personalSession(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	// The command is registered before the provider write. A retry replays the
	// recorded answer, so an update that resets the Key's usage is never sent to
	// the provider twice and never clears the usage that accrued after the first
	// request.
	reservation := PersonalKeyCommandReservation{
		ActorID:            session.GetActorId(),
		ProviderSubject:    credential.key.subject,
		OperationName:      personalKeyUpdateOperation,
		IdempotencyKey:     idempotencyKey,
		CommandFingerprint: personalKeyCommandFingerprint(personalKeyUpdateOperation, session.GetActorId(), credential.key.subject, id, request),
		ResourceID:         id,
	}
	record, existing, err := s.GatewayStore.ReservePersonalKeyCommand(ctx, reservation)
	if errors.Is(err, ErrPersonalKeyCommandConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different personal Gateway key command"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the personal Gateway key command registration is unavailable")
	}
	if existing {
		if personalKeyCommandPending(record) {
			// The provider write's outcome was never confirmed. A usage reset
			// cannot be read back - the usage that accrued after the first
			// request is indistinguishable from a usage that was never reset - so
			// such a command stays unresolved and is never dispatched again.
			if personalKeyCommandUsageReset(request) {
				return nil, status.Error(codes.Unavailable, "the personal Gateway key command outcome is unknown")
			}
			// Otherwise the owner's own readback is the evidence that settles it:
			// a Key whose configuration matches was updated.
			current, readErr := s.readPersonalKey(ctx, credential, id)
			if readErr != nil || !personalKeyUpdateConfirmed(current, expected) {
				return nil, status.Error(codes.Unavailable, "the personal Gateway key command outcome is unknown")
			}
			if err = settlePersonalKeyCommand(ctx, s, reservation, current, id); err != nil {
				return nil, err
			}
			return current, nil
		}
		answer := &api.PersonalGatewayKey{}
		if err := personalKeyCommandAnswer(record, answer); err != nil {
			return nil, err
		}
		return answer, nil
	}
	current, err := s.readPersonalKey(ctx, credential, id)
	if err != nil {
		return nil, err
	}
	if current.GetKind() == api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE {
		refusePersonalKeyCommand(ctx, s, reservation, status.Error(codes.FailedPrecondition, "the Gateway key is managed by a Workspace"))
		return nil, status.Error(codes.FailedPrecondition, "the Gateway key is managed by a Workspace")
	}
	if _, err = s.personalEnvelope(ctx, credential, http.MethodPut, "/api/v1/keys/"+id, request, nil); err != nil {
		if definitePersonalKeyRefusal(err) {
			refusePersonalKeyCommand(ctx, s, reservation, err)
		}
		return nil, err
	}
	updated, err := s.readPersonalKey(ctx, credential, id)
	if err != nil {
		return nil, err
	}
	if !personalKeyUpdateConfirmed(updated, expected) {
		return nil, status.Error(codes.Unavailable, "the updated Gateway personal key does not match the request")
	}
	if err = s.recordPersonalKeyAudit(ctx, session, "gateway.personal_key_update", id, r.GetContext().GetRequestId(), map[string]any{"fields": expected.details}, "confirmed"); err != nil {
		return nil, err
	}
	if err = settlePersonalKeyCommand(ctx, s, reservation, updated, id); err != nil {
		return nil, err
	}
	return updated, nil
}

// RevealPersonalGatewayKey returns one personal Gateway Key's secret to its
// owner. The secret is returned exactly once, in this answer, and this owner
// never stores it. The reveal is audited before the secret leaves, so a
// failed audit never hands the secret back.
func (s *Service) RevealPersonalGatewayKey(ctx context.Context, r *api.RevealPersonalGatewayKeyRpcRequest) (*api.PersonalGatewayKeySecret, error) {
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	session, credential, err := s.personalSession(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	var item personalKeyWritePayload
	if err = s.personalRequest(ctx, credential, "/api/v1/keys/"+id, &item); err != nil {
		return nil, err
	}
	if strconv.FormatInt(item.ID, 10) != id || strconv.FormatInt(item.UserID, 10) != credential.key.subject {
		return nil, invalidPersonalResponse()
	}
	if strings.TrimSpace(item.Key) == "" {
		return nil, status.Error(codes.Unavailable, "the Gateway did not return the personal key secret")
	}
	key, err := s.personalKey(ctx, item.personalKeyPayload, credential.key.subject)
	if err != nil {
		return nil, err
	}
	if key.GetKind() == api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE {
		return nil, status.Error(codes.FailedPrecondition, "the Gateway key is managed by a Workspace")
	}
	if err = s.recordPersonalKeyAudit(ctx, session, "gateway.personal_key_reveal", id, r.GetContext().GetRequestId(), map[string]any{"name": key.GetName()}, "confirmed"); err != nil {
		return nil, err
	}
	return &api.PersonalGatewayKeySecret{Key: key, Secret: item.Key}, nil
}

// RevokePersonalGatewayKey revokes one personal Gateway Key the caller owns.
// The revoke is idempotent: a Key the provider no longer holds is already
// revoked. A provider failure never reports success, and the Key's absence is
// confirmed by the provider's own readback before the revoke is accepted.
func (s *Service) RevokePersonalGatewayKey(ctx context.Context, r *api.RevokePersonalGatewayKeyRpcRequest) (*api.Operation, error) {
	id, err := personalKeyID(r.GetKeyId())
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(r.GetContext().GetIdempotencyKey())
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "an idempotency key is required")
	}
	session, credential, err := s.personalSession(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	// The command is registered before the provider write. A repeated revoke
	// replays the recorded answer instead of writing to the provider again.
	reservation := PersonalKeyCommandReservation{
		ActorID:            session.GetActorId(),
		ProviderSubject:    credential.key.subject,
		OperationName:      personalKeyRevokeOperation,
		IdempotencyKey:     idempotencyKey,
		CommandFingerprint: personalKeyCommandFingerprint(personalKeyRevokeOperation, session.GetActorId(), credential.key.subject, id, nil),
		ResourceID:         id,
	}
	record, existing, err := s.GatewayStore.ReservePersonalKeyCommand(ctx, reservation)
	if errors.Is(err, ErrPersonalKeyCommandConflict) {
		return nil, owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different personal Gateway key command"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "the personal Gateway key command registration is unavailable")
	}
	if existing {
		if personalKeyCommandPending(record) {
			// The provider write's outcome was never confirmed. A Key the provider
			// no longer holds was revoked, so the owner's readback settles the
			// command; a Key it still holds leaves the command unresolved, and it
			// is never dispatched a second time.
			if _, readErr := s.readPersonalKey(ctx, credential, id); readErr == nil || status.Code(readErr) != codes.NotFound {
				return nil, status.Error(codes.Unavailable, "the personal Gateway key command outcome is unknown")
			}
			return s.confirmPersonalKeyRevoke(ctx, session, r.GetContext().GetRequestId(), id, reservation, "revoked")
		}
		answer := &api.Operation{}
		if err := personalKeyCommandAnswer(record, answer); err != nil {
			return nil, err
		}
		return answer, nil
	}
	current, err := s.readPersonalKey(ctx, credential, id)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return nil, err
		}
		return s.confirmPersonalKeyRevoke(ctx, session, r.GetContext().GetRequestId(), id, reservation, "already absent")
	}
	if current.GetKind() == api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE {
		refusePersonalKeyCommand(ctx, s, reservation, status.Error(codes.FailedPrecondition, "the Gateway key is managed by a Workspace"))
		return nil, status.Error(codes.FailedPrecondition, "the Gateway key is managed by a Workspace")
	}
	if _, err = s.personalEnvelope(ctx, credential, http.MethodDelete, "/api/v1/keys/"+id, nil, nil); err != nil {
		if status.Code(err) != codes.NotFound {
			if definitePersonalKeyRefusal(err) {
				refusePersonalKeyCommand(ctx, s, reservation, err)
			}
			return nil, err
		}
		// The provider refused the delete. The revoke is only satisfied when the
		// provider's own readback proves the Key is gone.
	}
	if _, err = s.readPersonalKey(ctx, credential, id); err == nil {
		// The provider still holds the Key, so the revoke did not take effect.
		refusePersonalKeyCommand(ctx, s, reservation, status.Error(codes.Unavailable, "the Gateway personal key revocation is unconfirmed"))
		return nil, status.Error(codes.Unavailable, "the Gateway personal key revocation is unconfirmed")
	} else if status.Code(err) != codes.NotFound {
		// The readback itself did not settle the outcome. The command stays
		// pending: it is never dispatched a second time.
		return nil, status.Error(codes.Unavailable, "the Gateway personal key revocation is unconfirmed")
	}
	return s.confirmPersonalKeyRevoke(ctx, session, r.GetContext().GetRequestId(), id, reservation, "revoked")
}

// confirmPersonalKeyRevoke records the revoke evidence, settles the original
// command with its terminal answer and returns the operation that describes it.
func (s *Service) confirmPersonalKeyRevoke(ctx context.Context, session *api.Session, requestID, id string, reservation PersonalKeyCommandReservation, outcome string) (*api.Operation, error) {
	operation, err := s.revokedPersonalKey(ctx, session, requestID, id, outcome)
	if err != nil {
		return nil, err
	}
	if err = settlePersonalKeyCommand(ctx, s, reservation, operation, id); err != nil {
		return nil, err
	}
	return operation, nil
}

// revokedPersonalKey records the revoke evidence and answers with the
// operation that describes it.
func (s *Service) revokedPersonalKey(ctx context.Context, session *api.Session, requestID, id, outcome string) (*api.Operation, error) {
	if err := s.recordPersonalKeyAudit(ctx, session, "gateway.personal_key_revoke", id, requestID, map[string]any{"outcome": outcome}, "confirmed"); err != nil {
		return nil, err
	}
	now := timestamppb.Now()
	return &api.Operation{OperationId: id, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_REVOKE_KEY, ResourceId: id, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED, RequestId: requestID, CreatedAt: now, UpdatedAt: now}, nil
}
