package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/intents"
	profilepg "github.com/Faysal9991/mega-backend/internal/modules/profiles/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) Upload(ctx context.Context, c access.Credentials, key string, in delivery.Upload) (delivery.SignedUpload, error) {
	if e := in.Validate(); e != nil {
		return delivery.SignedUpload{}, e
	}
	if !r.cfg.MediaEnabled {
		return delivery.SignedUpload{}, delivery.Fail(apperror.ServiceUnavailable)
	}
	p := ""
	if in.Purpose != "avatar" {
		p = delivery.MediaManage
	}
	out, e := r.command(ctx, c, p, "media.intent", key, "Create private image upload", in, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		q := deliverysql.New(tx)
		target := in.Target
		if in.Purpose == "avatar" {
			if target != "" && target != u.Actor() {
				return nil, "", delivery.Fail(apperror.Forbidden)
			}
			target = u.Actor()
		} else {
			if target == "" {
				target = u.Actor()
			}
			if !access.ValidID(target) {
				return nil, "", delivery.Fail(apperror.ValidationFailed)
			}
			if in.Purpose == "agency_logo" {
				if !has(u, "agencies.manage") {
					return nil, "", delivery.Fail(apperror.Forbidden)
				}
				if _, e := q.AgencyExists(ctx, target); e != nil {
					return nil, "", e
				}
			}
		}
		quota, e := q.AssetQuota(ctx, u.Actor())
		if e != nil {
			return nil, "", e
		}
		if quota.Count >= 100 || quota.Total > 100*delivery.MaxImageBytes-in.Size {
			return nil, "", delivery.Fail(apperror.RateLimited)
		}
		id := access.ID()
		objectKey := r.cfg.Prefix + "/pending/" + id
		if e := q.InsertAsset(ctx, deliverysql.InsertAssetParams{ID: id, OwnerID: u.Actor(), Purpose: in.Purpose, TargetID: target, ObjectKey: objectKey, DeclaredType: in.ContentType, DeclaredSize: in.Size, Checksum: in.SHA256}); e != nil {
			return nil, "", e
		}
		v, e := q.Asset(ctx, id)
		if e != nil {
			return nil, "", e
		}
		if e := intents.Enqueue(ctx, tx, access.ID(), "media_gc", id, map[string]string{"asset_id": id}, v.CreatedAt.Time.Add(24*time.Hour)); e != nil {
			return nil, "", e
		}
		return asset(v), id, nil
	})
	if e != nil {
		return delivery.SignedUpload{}, e
	}
	b, e := json.Marshal(out)
	if e != nil {
		return delivery.SignedUpload{}, safe(e)
	}
	var a delivery.Asset
	if e = json.Unmarshal(b, &a); e != nil {
		return delivery.SignedUpload{}, safe(e)
	}
	result := delivery.SignedUpload{Asset: a}
	if time.Now().After(a.ExpiresAt) {
		return result, delivery.Fail(apperror.Conflict)
	}
	remote, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	ttl := min(5*time.Minute, time.Until(a.ExpiresAt))
	url, headers, e := r.store.Presign(remote, r.cfg.Prefix+"/pending/"+a.ID, in.ContentType, in.Size, in.SHA256, ttl)
	if e != nil {
		return result, delivery.Fail(apperror.ServiceUnavailable)
	}
	result.URL = url
	result.Headers = headers
	result.ExpiresAt = time.Now().UTC().Add(ttl)
	if result.ExpiresAt.After(a.ExpiresAt) {
		result.ExpiresAt = a.ExpiresAt
	}
	return result, nil
}
func has(u access.Unit, p string) bool {
	for _, v := range u.Effective().Permissions {
		if v == p {
			return true
		}
	}
	return false
}
func (r *Repository) Asset(ctx context.Context, c access.Credentials, id string) (out delivery.Asset, err error) {
	if !access.ValidID(id) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		v, e := deliverysql.New(tx).Asset(ctx, id)
		if e != nil {
			return e
		}
		if v.OwnerID != u.Actor() && !has(u, delivery.MediaManage) {
			return delivery.Fail(apperror.NotFound)
		}
		out = asset(v)
		return nil
	})
	return
}
func (r *Repository) Complete(ctx context.Context, c access.Credentials, id string) (out delivery.Asset, err error) {
	if !access.ValidID(id) {
		return out, delivery.Fail(apperror.ValidationFailed)
	}
	if !r.cfg.MediaEnabled {
		return out, delivery.Fail(apperror.ServiceUnavailable)
	}
	select {
	case r.processors <- struct{}{}:
		defer func() { <-r.processors }()
	default:
		return out, delivery.Fail(apperror.RateLimited)
	}
	var original deliverysql.AppDeliveryAsset
	lease := access.ID()
	already := false
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		v, e := q.Asset(ctx, id)
		if e != nil {
			return e
		}
		if v.OwnerID != u.Actor() {
			return delivery.Fail(apperror.Forbidden)
		}
		if v.Purpose != "avatar" && !has(u, delivery.MediaManage) {
			return delivery.Fail(apperror.Forbidden)
		}
		if v.State == "ready" {
			out = asset(v)
			already = true
			return nil
		}
		original, e = q.ClaimAsset(ctx, deliverysql.ClaimAssetParams{ID: id, LeaseID: lease})
		if errors.Is(e, pgx.ErrNoRows) {
			return delivery.Fail(apperror.Conflict)
		}
		if e == nil && original.Purpose != "avatar" {
			return audit(ctx, u, id, "media.complete.claim", "Validate administrative asset upload")
		}
		return e
	})
	if err != nil || already {
		return
	}
	// All remote I/O and decoding happen after the claim transaction has closed.
	var processed delivery.ProcessedImage
	remote, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	obj, e := r.store.Head(remote, original.ObjectKey)
	validation := false
	if e == nil && (obj.Size != original.DeclaredSize || obj.ContentType != original.DeclaredType || obj.ETag == "") {
		e = delivery.Fail(apperror.ValidationFailed)
		validation = true
	}
	var data []byte
	if e == nil {
		data, e = r.store.Read(remote, original.ObjectKey, obj.ETag, delivery.MaxImageBytes)
	}
	if e == nil {
		processed, e = delivery.ProcessImage(data, original.DeclaredType, original.Checksum)
		validation = e != nil
	}
	mainKey := r.cfg.Prefix + "/approved/" + id + "/image.png"
	thumbKey := r.cfg.Prefix + "/approved/" + id + "/thumbnail.png"
	if e == nil {
		e = r.store.Put(remote, mainKey, processed.Main, "image/png")
	}
	if e == nil {
		e = r.store.Put(remote, thumbKey, processed.Thumbnail, "image/png")
	}
	if e != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer done()
		state, code := "pending", "STORAGE_UNAVAILABLE"
		if validation {
			state, code = "failed", "INVALID_IMAGE"
		}
		if failure := deliverysql.New(r.pool).FailAsset(cleanup, deliverysql.FailAssetParams{ID: id, LeaseID: lease, State: state, ErrorCode: code}); failure != nil {
			return out, safe(failure)
		}
		if validation {
			return out, delivery.Fail(apperror.ValidationFailed)
		}
		return out, delivery.Fail(apperror.ServiceUnavailable)
	}
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := deliverysql.New(tx)
		if original.OwnerID != u.Actor() || original.Purpose != "avatar" && !has(u, delivery.MediaManage) {
			return delivery.Fail(apperror.Forbidden)
		}
		n, e := q.FinishAsset(ctx, deliverysql.FinishAssetParams{ID: id, LeaseID: lease, ApprovedKey: mainKey, ThumbnailKey: thumbKey, PublicUrl: r.cfg.PublicURL + "/" + mainKey, Width: int32(processed.Width), Height: int32(processed.Height), SizeBytes: int64(len(processed.Main))})
		if e != nil {
			return e
		}
		if n != 1 {
			return delivery.Fail(apperror.Conflict)
		}
		v, e := q.Asset(ctx, id)
		out = asset(v)
		if e == nil && original.Purpose != "avatar" {
			return audit(ctx, u, id, "media.complete.approved", "Approve validated administrative image")
		}
		return e
	})
	return
}
func (r *Repository) Attach(ctx context.Context, c access.Credentials, id, expected, key string) (any, error) {
	if !access.ValidID(id) || expected != "" && !access.ValidID(expected) {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	return r.command(ctx, c, "", "media.attach", key, "Attach approved image", []string{id, expected}, func(ctx context.Context, tx pgx.Tx, u access.Unit) (any, string, error) {
		q := deliverysql.New(tx)
		v, e := q.Asset(ctx, id)
		if e != nil {
			return nil, "", e
		}
		if v.State != "ready" || v.Purpose == "gift_thumbnail" {
			return nil, "", delivery.Fail(apperror.Conflict)
		}
		if v.Purpose == "avatar" {
			if v.OwnerID != u.Actor() || v.TargetID != u.Actor() {
				return nil, "", delivery.Fail(apperror.Forbidden)
			}
		} else if !has(u, delivery.MediaManage) || !has(u, "agencies.manage") {
			return nil, "", delivery.Fail(apperror.Forbidden)
		}
		previous, e := q.AssetLink(ctx, deliverysql.AssetLinkParams{TargetID: v.TargetID, Purpose: v.Purpose})
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, "", e
		}
		if previous != expected {
			return nil, "", delivery.Fail(apperror.Conflict)
		}
		if e := q.LinkAsset(ctx, deliverysql.LinkAssetParams{TargetID: v.TargetID, Purpose: v.Purpose, AssetID: id}); e != nil {
			return nil, "", e
		}
		if v.Purpose == "avatar" {
			if e := profilepg.Bind(tx).AttachAvatar(ctx, v.TargetID, v.PublicUrl); e != nil {
				return nil, "", e
			}
		}
		if previous != "" && previous != id {
			now, e := q.Clock(ctx)
			if e != nil {
				return nil, "", e
			}
			if e := intents.Enqueue(ctx, tx, access.ID(), "media_gc", previous+":"+id, map[string]string{"asset_id": previous}, now.Time.Add(24*time.Hour)); e != nil {
				return nil, "", e
			}
		}
		return asset(v), v.TargetID, nil
	})
}

// Attachment discovers the current optimistic replacement token. Avatar links
// are self-only; agency-logo links expose only their approved public projection.
func (r *Repository) Attachment(ctx context.Context, c access.Credentials, purpose, target string) (out any, err error) {
	if !access.ValidID(target) || purpose != "avatar" && purpose != "agency_logo" {
		return nil, delivery.Fail(apperror.ValidationFailed)
	}
	err = r.within(ctx, c, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		if purpose == "avatar" && target != u.Actor() {
			return delivery.Fail(apperror.Forbidden)
		}
		q := deliverysql.New(tx)
		id, e := q.AssetLink(ctx, deliverysql.AssetLinkParams{TargetID: target, Purpose: purpose})
		if errors.Is(e, pgx.ErrNoRows) {
			out = map[string]string{"asset_id": "", "url": "", "thumbnail_url": ""}
			return nil
		}
		if e != nil {
			return e
		}
		v, e := q.Asset(ctx, id)
		if e != nil {
			return e
		}
		if v.State != "ready" {
			return delivery.Fail(apperror.NotFound)
		}
		a := asset(v)
		out = map[string]string{"asset_id": a.ID, "url": a.URL, "thumbnail_url": a.ThumbnailURL}
		return nil
	})
	return
}
