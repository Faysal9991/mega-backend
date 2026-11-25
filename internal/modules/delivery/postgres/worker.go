package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Dispatcher struct {
	Repo    *Repository
	Rooms   delivery.RoomProvider
	Push    delivery.PushProvider
	Workers int
	Logger  *slog.Logger
}

func Backoff(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		attempt = 9
	}
	base := time.Second * time.Duration(1<<uint(attempt-1))
	return min(5*time.Minute, base+time.Duration(rand.Int64N(int64(base/4+1))))
}

// Run owns and joins every worker. No process-local queue is authoritative.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d.Repo == nil || d.Workers < 1 || d.Workers > 4 {
		return errors.New("invalid dispatcher configuration")
	}
	topics := []string{}
	if d.Rooms != nil {
		topics = append(topics, "gift")
	}
	if d.Push != nil {
		topics = append(topics, "push")
	}
	if d.Repo.cfg.MediaEnabled {
		topics = append(topics, "media_gc")
	}
	if len(topics) == 0 {
		<-ctx.Done()
		return nil
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				maintenance, done := context.WithTimeout(ctx, 5*time.Second)
				if e := d.Maintenance(maintenance); e != nil && d.Logger != nil {
					d.Logger.Warn("delivery.maintenance_failed", slog.String("failure_code", "DELIVERY_DATABASE_UNAVAILABLE"))
				}
				done()
			}
		}
	})
	for range d.Workers {
		wg.Go(func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
					worked, e := d.Step(jobCtx, topics)
					cancel()
					if e != nil && d.Logger != nil {
						d.Logger.Warn("delivery.dispatch_failed", slog.String("failure_code", "DELIVERY_DATABASE_UNAVAILABLE"))
					}
					if e != nil || !worked {
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Second):
						}
					}
				}
			}
		})
	}
	wg.Wait()
	return nil
}

// Step claims one fenced lease; all provider work occurs after claim autocommit.
func (d *Dispatcher) Step(ctx context.Context, topics []string) (bool, error) {
	q := deliverysql.New(d.Repo.pool)
	job, e := q.Claim(ctx, deliverysql.ClaimParams{LeaseID: access.ID(), Column2: topics})
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, safe(e)
	}
	state, code, e := d.deliver(ctx, job)
	delay := int32(0)
	if e != nil {
		if errors.Is(e, delivery.ErrPermanent) {
			state = "failed"
			if code == "" {
				code = "PROVIDER_REJECTED"
			}
		} else if job.Attempts >= 10 {
			state, code = "failed", "RETRY_EXHAUSTED"
		} else {
			state = "pending"
			if code == "" {
				code = "PROVIDER_UNAVAILABLE"
			}
			delay = int32((Backoff(job.Attempts) + time.Second - 1) / time.Second)
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, ackErr := q.Ack(cleanup, deliverysql.AckParams{ID: job.ID, LeaseID: job.LeaseID, State: state, ErrorCode: code, DelaySeconds: delay})
	if ackErr != nil {
		return true, safe(ackErr)
	}
	return true, nil
}
func (d *Dispatcher) deliver(ctx context.Context, job deliverysql.ClaimRow) (string, string, error) {
	q := deliverysql.New(d.Repo.pool)
	switch job.Topic {
	case "gift":
		var event struct {
			Room    string `json:"room_id"`
			Session string `json:"session_id"`
		}
		if e := json.Unmarshal(job.Payload, &event); e != nil || !access.ValidID(event.Room) || !access.ValidID(event.Session) || len(job.Payload) > 4096 {
			return "failed", "INVALID_INTENT", delivery.ErrPermanent
		}
		gate, e := q.LiveSession(ctx, deliverysql.LiveSessionParams{ID: event.Room, SessionID: event.Session})
		if errors.Is(e, pgx.ErrNoRows) || e == nil && (!gate.Active.Valid || !gate.Active.Bool) {
			return "skipped", "ROOM_CLOSED", nil
		}
		if e != nil {
			return "", "", e
		}
		if gate.Synchronizing {
			return "pending", "ROOM_SYNCHRONIZING", errors.New("room provider corrections pending")
		}
		if d.Rooms == nil {
			return "", "", errors.New("provider unavailable")
		}
		remote, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		e = d.Rooms.SendGift(remote, event.Session, job.Payload)
		return "delivered", "", e
	case "push":
		var payload struct {
			Notification string `json:"notification_id"`
			Installation string `json:"installation_id"`
			Revision     int64  `json:"revision"`
		}
		if e := json.Unmarshal(job.Payload, &payload); e != nil || !access.ValidID(payload.Notification) || !access.ValidID(payload.Installation) || payload.Revision < 1 {
			return "failed", "INVALID_INTENT", delivery.ErrPermanent
		}
		v, e := q.ResolvePush(ctx, deliverysql.ResolvePushParams{ID: payload.Notification, InstallationID: payload.Installation, Revision: pgtype.Int8{Int64: payload.Revision, Valid: true}})
		if errors.Is(e, pgx.ErrNoRows) {
			return "skipped", "BINDING_OR_PREFERENCE_INACTIVE", nil
		}
		if e != nil {
			return "", "", e
		}
		token, e := delivery.Open(d.Repo.key, v.TokenCipher, payload.Installation)
		if e != nil {
			return "failed", "TOKEN_KEY_UNAVAILABLE", delivery.ErrPermanent
		}
		if d.Push == nil {
			return "", "", errors.New("provider unavailable")
		}
		remote, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = d.Push.Send(remote, delivery.Push{Token: token, Kind: v.Kind, EventID: v.ID, Template: v.Template})
		cancel()
		if errors.Is(e, delivery.ErrInvalidToken) {
			if e := q.InvalidDevice(ctx, deliverysql.InvalidDeviceParams{InstallationID: payload.Installation, Revision: pgtype.Int8{Int64: payload.Revision, Valid: true}}); e != nil {
				return "", "", e
			}
			return "skipped", "INVALID_REGISTRATION", nil
		}
		return "delivered", "", e
	case "media_gc":
		var payload struct {
			ID string `json:"asset_id"`
		}
		if e := json.Unmarshal(job.Payload, &payload); e != nil || !access.ValidID(payload.ID) {
			return "failed", "INVALID_INTENT", delivery.ErrPermanent
		}
		return d.cleanupAsset(ctx, payload.ID)
	}
	return "failed", "INVALID_TOPIC", delivery.ErrPermanent
}
func (d *Dispatcher) cleanupAsset(ctx context.Context, id string) (string, string, error) {
	r := d.Repo
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return "", "", e
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer done()
		_ = tx.Rollback(cleanup)
	}()
	q := deliverysql.New(tx)
	if e := accesssql.New(tx).LockSecurity(ctx); e != nil {
		return "", "", e
	}
	v, e := q.Asset(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return "skipped", "ASSET_MISSING", nil
	}
	if e != nil {
		return "", "", e
	}
	now, e := q.Clock(ctx)
	if e != nil {
		return "", "", e
	}
	if now.Time.Before(v.ExpiresAt.Time.Add(time.Minute)) || v.State == "processing" && v.LeaseUntil.Valid && v.LeaseUntil.Time.After(now.Time) {
		return "", "", errors.New("asset still active")
	}
	referenced, e := q.AssetReferenced(ctx, id)
	if e != nil {
		return "", "", e
	}
	if !referenced.Bool {
		if e := q.RetireAsset(ctx, id); e != nil {
			return "", "", e
		}
	}
	if e := tx.Commit(ctx); e != nil {
		return "", "", e
	}
	// Retirement prevents a concurrent attachment while public objects are deleted.
	keys := []string{v.ObjectKey}
	if !referenced.Bool {
		keys = append(keys, v.ApprovedKey, v.ThumbnailKey)
		if v.ApprovedKey == "" {
			keys = append(keys, r.cfg.Prefix+"/approved/"+id+"/image.png", r.cfg.Prefix+"/approved/"+id+"/thumbnail.png")
		}
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if !strings.HasPrefix(key, r.cfg.Prefix+"/") {
			return "failed", "OBJECT_PREFIX_MISMATCH", delivery.ErrPermanent
		}
		remote, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = r.store.Delete(remote, key)
		cancel()
		if e != nil {
			return "", "", e
		}
	}
	if !referenced.Bool {
		if e := deliverysql.New(r.pool).DeleteAsset(ctx, id); e != nil {
			return "", "", e
		}
	}
	return "delivered", "", nil
}

// Maintenance is bounded and does not scan global tokens in application memory.
func (d *Dispatcher) Maintenance(ctx context.Context) error {
	q := deliverysql.New(d.Repo.pool)
	if e := q.ExpireExhausted(ctx); e != nil {
		return safe(e)
	}
	return safe(q.CleanupDevices(ctx))
}
