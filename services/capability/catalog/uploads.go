package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

func (s *Service) CreateUpload(ctx context.Context, r *api.CreateUploadRpcRequest) (*api.UploadSession, error) {
	if e := s.auth(ctx, r.GetContext(), "CreateUpload", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.PackageId); e != nil {
		return nil, e
	}
	b := r.GetBody()
	if e := checkName(b.GetVersionLabel()); e != nil {
		return nil, e
	}
	if b.GetSizeBytes() <= 0 || b.SizeBytes > s.Objects.Policy.MaxBytes || !digestRE.MatchString(b.GetSha256()) || !strings.HasSuffix(strings.ToLower(b.FileName), ".zip") {
		return nil, status.Error(codes.InvalidArgument, "ZIP file, exact digest and permitted size are required")
	}
	out := &api.UploadSession{}
	// Package identity participates in the idempotency request; context does not.
	request := &api.CreateUploadRpcRequest{Body: b, PackageId: r.PackageId}
	e := s.command(ctx, r.Context, "CreateUpload", request, out, func(tx *sql.Tx) error {
		var pkg string
		e := tx.QueryRowContext(ctx, `SELECT p.id FROM capability.packages p JOIN capability.namespaces n ON n.id=p.namespace_id WHERE p.id=$1 AND n.tenant_id=$2 AND p.status='active' AND n.status='active' FOR SHARE OF p,n`, r.PackageId, tenant(r.Context)).Scan(&pkg)
		if e != nil {
			return dbError(e)
		}
		out.Id = id("upload")
		out.PackageVersionId = id("pv")
		out.SizeBytes = b.SizeBytes
		out.Sha256 = b.Sha256
		out.PartSizeBytes = s.Objects.Policy.PartBytes
		out.Status = api.UploadSessionStatusEnum_UPLOAD_SESSION_STATUS_ENUM_UPLOADING
		out.ExpiresAt = timestamppb.New(time.Now().Add(s.Objects.Policy.TTL))
		if _, e = tx.ExecContext(ctx, `INSERT INTO capability.package_versions(id,package_id,version_label,sha256,size_bytes,created_by) VALUES($1,$2,$3,$4,$5,$6)`, out.PackageVersionId, pkg, b.VersionLabel, b.Sha256, b.SizeBytes, r.Context.ActorId); e != nil {
			return dbError(e)
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO capability.upload_sessions(id,package_version_id,part_size_bytes,object_ref,provider_upload_ref,expires_at) VALUES($1,$2,$3,$4,$1,$5)`, out.Id, out.PackageVersionId, out.PartSizeBytes, b.Sha256, out.ExpiresAt.AsTime())
		return dbError(e)
	})
	return out, e
}
func (s *Service) readUpload(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, upload, tid string) (*api.UploadSession, error) {
	v := &api.UploadSession{}
	var st string
	var expires time.Time
	e := q.QueryRowContext(ctx, `SELECT u.id,u.package_version_id,u.status,v.size_bytes,v.sha256,u.part_size_bytes,u.expires_at FROM capability.upload_sessions u JOIN capability.package_versions v ON v.id=u.package_version_id JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces n ON n.id=p.namespace_id WHERE u.id=$1 AND n.tenant_id=$2`, upload, tid).Scan(&v.Id, &v.PackageVersionId, &st, &v.SizeBytes, &v.Sha256, &v.PartSizeBytes, &expires)
	if e != nil {
		return nil, dbError(e)
	}
	if st == "uploading" && !expires.After(time.Now()) {
		st = "expired"
	}
	v.Status = api.UploadSessionStatusEnum(api.UploadSessionStatusEnum_value["UPLOAD_SESSION_STATUS_ENUM_"+upper(st)])
	v.ExpiresAt = timestamppb.New(expires)
	rows, e := q.QueryContext(ctx, `SELECT part_number,etag,size_bytes,sha256 FROM capability.upload_chunks WHERE upload_session_id=$1 AND observation_result='confirmed' ORDER BY part_number`, upload)
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	for rows.Next() {
		p := &api.UploadPart{}
		if e = rows.Scan(&p.PartNumber, &p.Etag, &p.SizeBytes, &p.Sha256); e != nil {
			return nil, dbError(e)
		}
		v.CompletedParts = append(v.CompletedParts, p)
	}
	return v, dbError(rows.Err())
}
func (s *Service) GetUpload(ctx context.Context, r *api.GetUploadRpcRequest) (*api.UploadSession, error) {
	if e := s.auth(ctx, r.GetContext(), "GetUpload", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.UploadId); e != nil {
		return nil, e
	}
	return s.readUpload(ctx, s.DB, r.UploadId, tenant(r.Context))
}
func (s *Service) CreateUploadPart(ctx context.Context, r *api.CreateUploadPartRpcRequest) (*api.UploadPartAuthorization, error) {
	if e := s.auth(ctx, r.GetContext(), "CreateUploadPart", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.UploadId); e != nil {
		return nil, e
	}
	u, e := s.readUpload(ctx, s.DB, r.UploadId, tenant(r.Context))
	if e != nil {
		return nil, e
	}
	if u.Status != api.UploadSessionStatusEnum_UPLOAD_SESSION_STATUS_ENUM_UPLOADING {
		return nil, status.Error(codes.FailedPrecondition, "upload is not active")
	}
	b := r.GetBody()
	count := (u.SizeBytes + u.PartSizeBytes - 1) / u.PartSizeBytes
	if b.GetPartNumber() < 1 || int64(b.PartNumber) > count || !digestRE.MatchString(b.GetSha256()) {
		return nil, status.Error(codes.InvalidArgument, "invalid part")
	}
	size := u.PartSizeBytes
	if int64(b.PartNumber) == count {
		size = u.SizeBytes - u.PartSizeBytes*(count-1)
	}
	if b.SizeBytes != size {
		return nil, status.Error(codes.InvalidArgument, "part length differs from upload policy")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, dbError(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO capability.upload_chunks(id,upload_session_id,size_bytes,sha256,provider_part_ref,observation_result,part_number) VALUES($1,$2,$3,$4,$5,'unknown',$6) ON CONFLICT(upload_session_id,part_number) DO NOTHING`, id("part"), u.Id, b.SizeBytes, b.Sha256, fmt.Sprintf("%s/%d", u.Id, b.PartNumber), b.PartNumber); e != nil {
		return nil, dbError(e)
	}
	var sha string
	var actual int64
	if e = tx.QueryRowContext(ctx, `SELECT size_bytes,sha256 FROM capability.upload_chunks WHERE upload_session_id=$1 AND part_number=$2`, u.Id, b.PartNumber).Scan(&actual, &sha); e != nil {
		return nil, dbError(e)
	}
	if sha != b.Sha256 || actual != b.SizeBytes {
		return nil, status.Error(codes.AlreadyExists, "part identity is immutable")
	}
	if e = tx.Commit(); e != nil {
		return nil, dbError(e)
	}
	p := partPermit{Upload: u.Id, Part: b.PartNumber, Size: b.SizeBytes, Digest: b.Sha256, Expires: u.ExpiresAt.AsTime().Unix()}
	return &api.UploadPartAuthorization{UploadId: u.Id, PartNumber: b.PartNumber, Method: api.UploadPartAuthorizationMethodEnum_UPLOAD_PART_AUTHORIZATION_METHOD_ENUM_PUT, Url: s.Objects.PublicURL + "/parts?permit=" + s.Objects.permit(p), ContentType: "application/octet-stream", RequiredChecksumHeaderName: "X-OPL-SHA256", RequiredChecksumHeaderValue: b.Sha256, ExpiresAt: u.ExpiresAt}, nil
}
func (s *Service) CompleteUpload(ctx context.Context, r *api.CompleteUploadRpcRequest) (*api.Operation, error) {
	if e := s.auth(ctx, r.GetContext(), "CompleteUpload", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.UploadId); e != nil {
		return nil, e
	}
	out := &api.Operation{}
	e := s.command(ctx, r.Context, "CompleteUpload", &api.CompleteUploadRpcRequest{Body: r.GetBody(), UploadId: r.UploadId}, out, func(tx *sql.Tx) error {
		var lock string
		if e := tx.QueryRowContext(ctx, `SELECT id FROM capability.upload_sessions WHERE id=$1 FOR UPDATE`, r.UploadId).Scan(&lock); e != nil {
			return dbError(e)
		}
		u, e := s.readUpload(ctx, tx, r.UploadId, tenant(r.Context))
		if e != nil {
			return e
		}
		if u.Status == api.UploadSessionStatusEnum_UPLOAD_SESSION_STATUS_ENUM_COMPLETED {
			var saved, accepted []byte
			e = tx.QueryRowContext(ctx, `SELECT result,accepted_input FROM capability.operations WHERE resource_id=$1 AND kind='complete_upload' ORDER BY created_at LIMIT 1`, u.PackageVersionId).Scan(&saved, &accepted)
			if e != nil {
				return dbError(e)
			}
			original := &api.CompleteUploadRequest{}
			if e = protojson.Unmarshal(accepted, original); e != nil {
				return dbError(e)
			}
			if !proto.Equal(original, r.GetBody()) {
				return status.Error(codes.AlreadyExists, "completed upload has different parts")
			}
			return protojson.Unmarshal(saved, out)
		}
		if u.Status != api.UploadSessionStatusEnum_UPLOAD_SESSION_STATUS_ENUM_UPLOADING {
			return status.Error(codes.FailedPrecondition, "upload expired")
		}
		parts := r.GetBody().GetParts()
		if len(parts) != len(u.CompletedParts) || int64(len(parts)) != (u.SizeBytes+u.PartSizeBytes-1)/u.PartSizeBytes {
			return status.Error(codes.FailedPrecondition, "all confirmed parts are required")
		}
		for i, p := range parts {
			actual := u.CompletedParts[i]
			if p.PartNumber != actual.PartNumber || p.SizeBytes != actual.SizeBytes || p.Sha256 != actual.Sha256 || p.Etag != actual.Etag {
				return status.Error(codes.InvalidArgument, "parts differ from owner readback")
			}
		}
		f, e := os.CreateTemp(filepath.Join(s.Objects.Root, "pending"), "complete-")
		if e != nil {
			return status.Error(codes.Unavailable, "object store unavailable")
		}
		defer os.Remove(f.Name())
		h := sha256.New()
		var size int64
		for _, p := range parts {
			part, e := os.Open(filepath.Join(s.Objects.Root, "parts", u.Id, strconv.Itoa(int(p.PartNumber))))
			if e != nil {
				f.Close()
				return status.Error(codes.Unavailable, "part unavailable")
			}
			n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(part, p.SizeBytes+1))
			part.Close()
			if e != nil || n != p.SizeBytes {
				f.Close()
				return status.Error(codes.DataLoss, "part integrity failed")
			}
			size += n
		}
		if e = f.Close(); e != nil {
			return status.Error(codes.Unavailable, "object write failed")
		}
		validation := ""
		var manifest []byte
		if size != u.SizeBytes || "sha256:"+hex.EncodeToString(h.Sum(nil)) != u.Sha256 {
			validation = "PACKAGE_DIGEST_MISMATCH"
		} else {
			manifest, e = s.Objects.validateArchive(f.Name())
			if e != nil {
				validation = "PACKAGE_VALIDATION_FAILED"
			}
		}
		now := time.Now()
		out.OperationId = id("op")
		out.Owner = api.OperationOwnerEnum_OPERATION_OWNER_ENUM_CAPABILITY
		out.Kind = api.OperationKindEnum_OPERATION_KIND_ENUM_COMPLETE_UPLOAD
		out.ResourceId = u.PackageVersionId
		out.Status = api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED
		out.Stage = api.OperationStageEnum_OPERATION_STAGE_ENUM_UPLOAD_VERIFICATION
		out.RequestId = r.Context.RequestId
		out.CreatedAt = timestamppb.New(now)
		out.UpdatedAt = timestamppb.New(now)
		st := "succeeded"
		obs := "confirmed"
		if validation != "" {
			st = "failed"
			obs = "rejected"
			out.Status = api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED
			code := api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED
			out.ErrorCode = &code
			if _, e = tx.ExecContext(ctx, `UPDATE capability.package_versions SET status='rejected',validation_error_code=$1,updated_at=now() WHERE id=$2`, validation, u.PackageVersionId); e != nil {
				return dbError(e)
			}
		} else {
			if e = s.Objects.putImmutable(f.Name(), u.Sha256); e != nil {
				return status.Error(codes.Unavailable, "immutable object write failed")
			}
			if _, e = tx.ExecContext(ctx, `UPDATE capability.package_versions SET status='uploaded',object_ref=$1,manifest=$2,verified_at=now(),updated_at=now() WHERE id=$3 AND status='upload_pending'`, u.Sha256, manifest, u.PackageVersionId); e != nil {
				return dbError(e)
			}
			var pkg string
			if e = tx.QueryRowContext(ctx, `SELECT package_id FROM capability.package_versions WHERE id=$1`, u.PackageVersionId).Scan(&pkg); e != nil {
				return dbError(e)
			}
			payload := jsonBytes(&api.PackageUploadedEvent{PackageVersionId: u.PackageVersionId, PackageId: pkg, Sha256: u.Sha256, SizeBytes: u.SizeBytes})
			if e = s.Store.AppendEvent(ctx, tx, ownerstore.Event{ID: id("evt"), EventType: "package.uploaded.v1", SchemaVersion: 1, AggregateType: "package_version", AggregateID: u.PackageVersionId, AggregateRevision: 1, TenantID: tenant(r.Context), CorrelationID: r.Context.RequestId, Payload: payload, OccurredAt: now}); e != nil {
				return dbError(e)
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE capability.upload_sessions SET status='completed',updated_at=now() WHERE id=$1`, u.Id); e != nil {
			return dbError(e)
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO capability.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,error_code,observation_result,request_id,accepted_input,result,started_at,completed_at) VALUES($1,$2,$3,'complete_upload',$4,$5,'upload_verification',$6,$7,$8,$9,$10,now(),now())`, out.OperationId, tenant(r.Context), r.Context.ActorId, u.PackageVersionId, st, nullable(validation), obs, r.Context.RequestId, jsonBytes(r.Body), jsonBytes(out))
		return dbError(e)
	})
	return out, e
}
