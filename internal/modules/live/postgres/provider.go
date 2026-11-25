package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/access/postgres/accesssql"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

// Flush claims just one ordered intent. A bounded provider call runs with no
// PostgreSQL transaction/connection held. Failed/ambiguous calls stay retryable.
func (r *Repository) Flush(ctx context.Context, id string, p live.Provider) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q := livesql.New(r.pool)
	op, e := q.Claim(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		pending, err := q.Pending(ctx, id)
		if err != nil {
			return safe(err)
		}
		if pending {
			return live.Fail(apperror.ServiceUnavailable)
		}
		return nil
	}
	if e != nil {
		return safe(e)
	}
	limits, e := r.effective(ctx, q)
	if e != nil {
		return safe(e)
	}
	v, e := q.Room(ctx, id)
	if e != nil {
		return safe(e)
	}
	// Start the provider clock only after local reads have finished. Database
	// latency must not consume the remote call's independent timeout.
	call := func(fn func(context.Context) error) error {
		remote, done := context.WithTimeout(ctx, r.limits.ProviderTimeout)
		defer done()
		return fn(remote)
	}
	switch op.Kind {
	case "create":
		if v.State == "opening" {
			e = call(func(remote context.Context) error { return p.Create(remote, room(v)) })
		}
	case "close":
		e = call(func(remote context.Context) error { return p.Close(remote, room(v)) })
	case "participant":
		m := livesql.AppLiveMember{AccountID: op.Target, State: "left"}
		state, readErr := q.DesiredParticipant(ctx, livesql.DesiredParticipantParams{RoomID: id, AccountID: op.Target})
		if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
			e = readErr
		} else {
			if readErr == nil {
				m = livesql.AppLiveMember{RoomID: state.RoomID, AccountID: state.AccountID, Role: state.Role, State: state.State, Muted: state.Muted, Seat: state.Seat, InvitedSeat: state.InvitedSeat, UpdatedAt: state.UpdatedAt}
				if !state.AccountActive || live.CanPublish(room(v), member(m)) && (!state.AgencyActive || !limits.Compatibility.Allows(state.Classification, v.Kind)) {
					m.State = "left"
				}
			}
			e = call(func(remote context.Context) error { return p.Participant(remote, room(v), member(m)) })
		}
	}
	if e != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer done()
		if retryErr := q.Retry(cleanup, op.ID); retryErr != nil {
			return safe(retryErr)
		}
		return live.Fail(apperror.ServiceUnavailable)
	}
	// If provider succeeded and commit fails the lease expires and an idempotent
	// retry converges. Closing/closed rooms can never be reopened by create retry.
	e = r.tx.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		if e := accesssql.New(tx).LockSecurity(ctx); e != nil {
			return e
		}
		q := livesql.New(tx)
		current, e := q.Room(ctx, id)
		if e != nil {
			return e
		}
		if op.Kind == "create" && current.State == "opening" {
			if e := q.RoomState(ctx, livesql.RoomStateParams{ID: id, State: "open"}); e != nil {
				return e
			}
		}
		if op.Kind == "close" {
			if e := q.RoomState(ctx, livesql.RoomStateParams{ID: id, State: "closed"}); e != nil {
				return e
			}
		}
		return q.Complete(ctx, op.ID)
	})
	if e != nil {
		return safe(e)
	}
	pending, e := q.Pending(ctx, id)
	if e != nil {
		return safe(e)
	}
	if pending {
		return live.Fail(apperror.ServiceUnavailable)
	}
	return nil
}
func (r *Repository) Reconcile(ctx context.Context, c access.Credentials, id string, p live.Provider) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var v live.Room
	// Reconciliation is a privileged mutation, authorized/audited before calls.
	e := r.guard.Authorized(ctx, c, live.RoomModerate, false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := livesql.New(tx)
		row, e := q.Room(ctx, id)
		if e != nil {
			return e
		}
		v = room(row)
		if e := q.ResetExhausted(ctx, id); e != nil {
			return e
		}
		return audit(ctx, u, id, "room.reconcile", "Reconcile durable provider state")
	})
	if e != nil {
		return safe(e)
	}
	if e := r.Flush(ctx, id, p); e != nil {
		return e
	}
	remote, done := context.WithTimeout(ctx, r.limits.ProviderTimeout)
	snapshot, e := p.Snapshot(remote, v)
	done()
	if e != nil {
		return live.Fail(apperror.ServiceUnavailable)
	}
	participants := snapshot.Participants
	if len(participants) > 500 {
		return live.Fail(apperror.ServiceUnavailable)
	}
	return safe(r.guard.Authorized(ctx, c, live.RoomModerate, false, "", func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := livesql.New(tx)
		current, e := q.Room(ctx, id)
		if e != nil {
			return e
		}
		if !snapshot.Exists {
			if e := closeVerifiedRoom(ctx, q, id); e != nil {
				return e
			}
			return audit(ctx, u, id, "room.reconciled", "Verified provider room absence; no duration inferred")
		}
		if current.State == "closed" {
			if e := q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: id, Kind: "close"}); e != nil {
				return e
			}
		}
		open, e := q.OpenPresence(ctx, id)
		if e != nil {
			return e
		}
		seen := map[string]string{}
		for _, p := range participants {
			seen[p.SID] = p.Identity
		}
		missing := []string{}
		for _, v := range open {
			if seen[v.ParticipantSid] != v.AccountID {
				missing = append(missing, v.ParticipantSid)
			}
		}
		if e := q.AbandonPresences(ctx, missing); e != nil {
			return e
		}

		// Provider snapshots never establish past duration. New join evidence is
		// required to restart qualifying activity after an abandoned connection.
		active, e := q.Members(ctx, id)
		if e != nil {
			return e
		}
		members := make(map[string]bool, len(active))
		for _, m := range active {
			members[m.AccountID] = true
		}
		corrections := []livesql.EnqueueMembersParams{}
		for _, p := range participants {
			if !members[p.Identity] && live.Text(p.Identity, 128) {
				corrections = append(corrections, livesql.EnqueueMembersParams{ID: access.ID(), RoomID: id, Kind: "participant", Target: p.Identity})
			}
		}
		if _, e := q.EnqueueMembers(ctx, corrections); e != nil {
			return e
		}

		return audit(ctx, u, id, "room.reconciled", "Verified provider snapshot; unverifiable gaps abandoned")
	}))
}

func (r *Repository) FlushSession(ctx context.Context, session string, p live.Provider) error {
	v, e := livesql.New(r.pool).SessionRoom(ctx, session)
	if e != nil {
		return safe(e)
	}
	return r.Flush(ctx, v.ID, p)
}
