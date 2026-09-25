package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// availability collapses the stored plan status and the plan's own validity
// window into the single customer-facing availability the contract fixes:
// revoked/deprecated publish as retired; an approved plan inside its validity
// window is available; anything else is unavailable. This projection has one
// owner so a plan row and its published price can never disagree.
func availability(stored string, validFrom, validUntil, now time.Time) (string, error) {
	switch stored {
	case "revoked", "deprecated":
		return "retired", nil
	case "approved":
		if now.Before(validFrom) || (!validUntil.IsZero() && !now.Before(validUntil)) {
			return "unavailable", nil
		}
		return "available", nil
	default:
		return "", status.Errorf(codes.DataLoss, "unknown stored plan status %q", stored)
	}
}

func computeAvailabilityEnum(v string) api.ComputePlanAvailabilityEnum {
	return api.ComputePlanAvailabilityEnum(api.ComputePlanAvailabilityEnum_value["COMPUTE_PLAN_AVAILABILITY_ENUM_"+upper(v)])
}

func storageAvailabilityEnum(v string) api.StoragePlanAvailabilityEnum {
	return api.StoragePlanAvailabilityEnum(api.StoragePlanAvailabilityEnum_value["STORAGE_PLAN_AVAILABILITY_ENUM_"+upper(v)])
}

func computeBillingEnum(v string) api.ComputePlanBillingModeEnum {
	return api.ComputePlanBillingModeEnum(api.ComputePlanBillingModeEnum_value["COMPUTE_PLAN_BILLING_MODE_ENUM_"+upper(v)])
}

func storageBillingEnum(v string) api.StoragePlanBillingModeEnum {
	return api.StoragePlanBillingModeEnum(api.StoragePlanBillingModeEnum_value["STORAGE_PLAN_BILLING_MODE_ENUM_"+upper(v)])
}

type rowScanner interface{ Scan(...any) error }

func nullOrZero(v sql.NullTime) time.Time {
	if v.Valid {
		return v.Time
	}
	return time.Time{}
}

const computePlanCols = `p.id,p.name,p.vcpus,p.memory_mib,p.status,p.billing_mode,p.provider_capability_version,p.valid_from,p.valid_until,p.created_at`

func scanComputePlan(row rowScanner, now time.Time) (*api.ComputePlan, error) {
	var (
		plan                 api.ComputePlan
		st, billing          string
		validFrom, createdAt time.Time
		validUntil           sql.NullTime
	)
	if err := row.Scan(&plan.Id, &plan.Name, &plan.Vcpus, &plan.MemoryMiB, &st, &billing, &plan.ProviderCapabilityVersion, &validFrom, &validUntil, &createdAt); err != nil {
		return nil, err
	}
	raw, err := availability(st, validFrom, nullOrZero(validUntil), now)
	if err != nil {
		return nil, err
	}
	plan.Availability = computeAvailabilityEnum(raw)
	plan.BillingMode = computeBillingEnum(billing)
	plan.ValidFrom = timestamppb.New(validFrom.UTC())
	if validUntil.Valid {
		plan.ValidUntil = timestamppb.New(validUntil.Time.UTC())
	}
	plan.CreatedAt = timestamppb.New(createdAt.UTC())
	return &plan, nil
}

func (s *Service) ListComputePlans(ctx context.Context, r *api.ListComputePlansRpcRequest) (*api.ComputePlanPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListComputePlans", false); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	now := time.Now().UTC()
	rows, err := s.DB.QueryContext(ctx, `SELECT `+computePlanCols+` FROM resource_catalog.compute_plans p WHERE p.id>$1 ORDER BY p.id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.ComputePlanPage{}
	for rows.Next() {
		plan, err := scanComputePlan(rows, now)
		if err != nil {
			return nil, dbError(err)
		}
		out.Items = append(out.Items, plan)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) CreateComputePlan(ctx context.Context, r *api.CreateComputePlanRpcRequest) (*api.ComputePlan, error) {
	if err := s.auth(ctx, r.GetContext(), "CreateComputePlan", true); err != nil {
		return nil, err
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetName()) || b.GetVcpus() <= 0 || b.GetMemoryMiB() <= 0 ||
		b.GetProviderProfileId() == "" || b.GetProviderSkuId() == "" || !tsValid(b.GetValidFrom()) {
		return nil, status.Error(codes.InvalidArgument, "compute plan requires a name, positive vcpus and memory, provider profile/sku and validFrom")
	}
	if b.GetValidUntil() != nil && (b.GetValidUntil().CheckValid() != nil || !b.GetValidUntil().AsTime().After(b.GetValidFrom().AsTime())) {
		return nil, status.Error(codes.InvalidArgument, "validUntil must be after validFrom")
	}
	// The instance profile is the single authority for which provider capability
	// version this catalog has been approved to price. The administrator confirms
	// it; a request naming another capability is refused rather than stored.
	if b.GetProviderCapabilityVersion() != s.Profile.ProviderCapabilityVersion {
		return nil, status.Errorf(codes.FailedPrecondition, "provider capability version %q is not the instance-approved %q", b.GetProviderCapabilityVersion(), s.Profile.ProviderCapabilityVersion)
	}
	out := &api.ComputePlan{}
	err := s.command(ctx, r.GetContext(), "CreateComputePlan", b, out, func(tx *sql.Tx) error {
		out.Id = id("compute")
		out.Name = b.Name
		out.Vcpus = b.Vcpus
		out.MemoryMiB = b.MemoryMiB
		out.ProviderCapabilityVersion = s.Profile.ProviderCapabilityVersion
		out.Availability = computeAvailabilityEnum("available")
		out.BillingMode = computeBillingEnum(s.Profile.BillingMode)
		out.ValidFrom = timestamppb.New(b.ValidFrom.AsTime().UTC())
		if b.ValidUntil != nil {
			out.ValidUntil = timestamppb.New(b.ValidUntil.AsTime().UTC())
		}
		out.CreatedAt = timestamppb.Now()
		spec, err := marshalProviderSpec(b.GetProviderProfileId(), b.GetProviderSkuId())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO resource_catalog.compute_plans(id,name,version_label,provider,provider_profile_ref,region,status,billing_mode,valid_from,valid_until,published_by,created_at,updated_at,provider_capability_version,provider_specification,vcpus,memory_mib) VALUES($1,$2,$3,$4,$5,$6,'approved',$7,$8,$9,$10,now(),now(),$11,$12,$13,$14)`,
			out.Id, out.Name, out.Id, s.Profile.Provider, b.ProviderProfileId, s.Profile.Region, s.Profile.BillingMode, b.ValidFrom.AsTime().UTC(), tsOrNil(b.ValidUntil), r.GetContext().GetActorId(), s.Profile.ProviderCapabilityVersion, spec, out.Vcpus, out.MemoryMiB)
		return dbError(err)
	})
	return out, err
}

func (s *Service) SetComputePlanAvailability(ctx context.Context, r *api.SetComputePlanAvailabilityRpcRequest) (*api.ComputePlan, error) {
	if err := s.auth(ctx, r.GetContext(), "SetComputePlanAvailability", true); err != nil {
		return nil, err
	}
	stored, err := requestedStatus(r.GetBody())
	if err != nil {
		return nil, err
	}
	if r.GetPlanId() == "" {
		return nil, status.Error(codes.InvalidArgument, "plan id is required")
	}
	out := &api.ComputePlan{}
	err = s.command(ctx, r.GetContext(), "SetComputePlanAvailability", &api.SetComputePlanAvailabilityRpcRequest{Body: r.Body, PlanId: r.PlanId}, out, func(tx *sql.Tx) error {
		plan, err := scanComputePlan(tx.QueryRowContext(ctx, `UPDATE resource_catalog.compute_plans p SET status=$1,updated_at=now() WHERE p.id=$2 RETURNING `+computePlanCols, stored, r.PlanId), time.Now().UTC())
		if err == nil {
			proto.Merge(out, plan)
		}
		return err
	})
	return out, err
}

// requestedStatus maps the administrator's requested availability onto the stored
// catalog status the contract defines. "retired" maps to revoked. "available"
// maps to approved; a plan whose validity window has passed still projects as
// unavailable, so the request is not a promise about the clock. "unavailable" has
// no distinct stored state in the fixed schema: it is refused with the gap named
// instead of being silently stored as a different projection.
func requestedStatus(body *api.PlanAvailabilityRequest) (string, error) {
	if body.GetAvailability() == api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_UNSPECIFIED || len(body.GetReason()) == 0 || len(body.GetReason()) > 1024 {
		return "", status.Error(codes.InvalidArgument, "availability and a non-empty reason are required")
	}
	switch body.GetAvailability() {
	case api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_AVAILABLE:
		return "approved", nil
	case api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_RETIRED:
		return "revoked", nil
	case api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_UNAVAILABLE:
		return "", status.Error(codes.FailedPrecondition, "the fixed catalog stores no distinct temporarily-unavailable plan state; retire the plan or narrow its validity window")
	default:
		return "", status.Error(codes.InvalidArgument, "unknown availability")
	}
}

const storagePlanCols = `s.id,s.name,s.capacity_gib,s.status,s.billing_mode,s.shrink_supported,s.valid_from,s.valid_until,s.created_at`

func scanStoragePlan(row rowScanner, now time.Time) (*api.StoragePlan, error) {
	var (
		plan                 api.StoragePlan
		st, billing          string
		validFrom, createdAt time.Time
		validUntil           sql.NullTime
	)
	if err := row.Scan(&plan.Id, &plan.Name, &plan.CapacityGiB, &st, &billing, &plan.ShrinkSupported, &validFrom, &validUntil, &createdAt); err != nil {
		return nil, err
	}
	raw, err := availability(st, validFrom, nullOrZero(validUntil), now)
	if err != nil {
		return nil, err
	}
	plan.Availability = storageAvailabilityEnum(raw)
	plan.BillingMode = storageBillingEnum(billing)
	plan.ValidFrom = timestamppb.New(validFrom.UTC())
	if validUntil.Valid {
		plan.ValidUntil = timestamppb.New(validUntil.Time.UTC())
	}
	plan.CreatedAt = timestamppb.New(createdAt.UTC())
	return &plan, nil
}

func (s *Service) ListStoragePlans(ctx context.Context, r *api.ListStoragePlansRpcRequest) (*api.StoragePlanPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListStoragePlans", false); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	now := time.Now().UTC()
	rows, err := s.DB.QueryContext(ctx, `SELECT `+storagePlanCols+` FROM resource_catalog.storage_plans s WHERE s.id>$1 ORDER BY s.id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.StoragePlanPage{}
	for rows.Next() {
		plan, err := scanStoragePlan(rows, now)
		if err != nil {
			return nil, dbError(err)
		}
		out.Items = append(out.Items, plan)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) CreateStoragePlan(ctx context.Context, r *api.CreateStoragePlanRpcRequest) (*api.StoragePlan, error) {
	if err := s.auth(ctx, r.GetContext(), "CreateStoragePlan", true); err != nil {
		return nil, err
	}
	b := r.GetBody()
	if !nameRE.MatchString(b.GetName()) || b.GetCapacityGiB() <= 0 || b.GetProviderProfileId() == "" || b.GetProviderSkuId() == "" || !tsValid(b.GetValidFrom()) {
		return nil, status.Error(codes.InvalidArgument, "storage plan requires a name, positive capacity, provider profile/sku and validFrom")
	}
	if b.GetValidUntil() != nil && (b.GetValidUntil().CheckValid() != nil || !b.GetValidUntil().AsTime().After(b.GetValidFrom().AsTime())) {
		return nil, status.Error(codes.InvalidArgument, "validUntil must be after validFrom")
	}
	out := &api.StoragePlan{}
	err := s.command(ctx, r.GetContext(), "CreateStoragePlan", b, out, func(tx *sql.Tx) error {
		out.Id = id("storage")
		out.Name = b.Name
		out.CapacityGiB = b.CapacityGiB
		out.ShrinkSupported = b.ShrinkSupported
		out.Availability = storageAvailabilityEnum("available")
		out.BillingMode = storageBillingEnum(s.Profile.BillingMode)
		out.ValidFrom = timestamppb.New(b.ValidFrom.AsTime().UTC())
		if b.ValidUntil != nil {
			out.ValidUntil = timestamppb.New(b.ValidUntil.AsTime().UTC())
		}
		out.CreatedAt = timestamppb.Now()
		spec, err := marshalProviderSpec(b.GetProviderProfileId(), b.GetProviderSkuId())
		if err != nil {
			return err
		}
		// The create request carries no provider capability version, so the
		// instance-approved profile supplies the one this catalog is approved
		// against. An unset profile capability is refused instead of fabricated.
		if s.Profile.ProviderCapabilityVersion == "" {
			return status.Error(codes.FailedPrecondition, "the instance profile declares no approved provider capability version")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO resource_catalog.storage_plans(id,name,version_label,provider,provider_profile_ref,region,status,billing_mode,valid_from,valid_until,published_by,created_at,updated_at,provider_capability_version,provider_specification,capacity_gib,shrink_supported) VALUES($1,$2,$3,$4,$5,$6,'approved',$7,$8,$9,$10,now(),now(),$11,$12,$13,$14)`,
			out.Id, out.Name, out.Id, s.Profile.Provider, b.ProviderProfileId, s.Profile.Region, s.Profile.BillingMode, b.ValidFrom.AsTime().UTC(), tsOrNil(b.ValidUntil), r.GetContext().GetActorId(), s.Profile.ProviderCapabilityVersion, spec, out.CapacityGiB, out.ShrinkSupported)
		return dbError(err)
	})
	return out, err
}

func (s *Service) SetStoragePlanAvailability(ctx context.Context, r *api.SetStoragePlanAvailabilityRpcRequest) (*api.StoragePlan, error) {
	if err := s.auth(ctx, r.GetContext(), "SetStoragePlanAvailability", true); err != nil {
		return nil, err
	}
	stored, err := requestedStatus(r.GetBody())
	if err != nil {
		return nil, err
	}
	if r.GetPlanId() == "" {
		return nil, status.Error(codes.InvalidArgument, "plan id is required")
	}
	out := &api.StoragePlan{}
	err = s.command(ctx, r.GetContext(), "SetStoragePlanAvailability", &api.SetStoragePlanAvailabilityRpcRequest{Body: r.Body, PlanId: r.PlanId}, out, func(tx *sql.Tx) error {
		plan, err := scanStoragePlan(tx.QueryRowContext(ctx, `UPDATE resource_catalog.storage_plans s SET status=$1,updated_at=now() WHERE s.id=$2 RETURNING `+storagePlanCols, stored, r.PlanId), time.Now().UTC())
		if err == nil {
			proto.Merge(out, plan)
		}
		return err
	})
	return out, err
}

func marshalProviderSpec(profileID, skuID string) ([]byte, error) {
	spec, err := json.Marshal(map[string]string{"providerProfileId": profileID, "providerSkuId": skuID})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid provider specification")
	}
	return spec, nil
}

// tsOrNil stores an optional validity bound. A nil or invalid timestamp is
// stored as SQL NULL rather than as the zero instant, so a plan with no stated
// end does not silently expire in 1970.
func tsOrNil(t *timestamppb.Timestamp) any {
	if t == nil || t.CheckValid() != nil {
		return nil
	}
	return t.AsTime().UTC()
}
