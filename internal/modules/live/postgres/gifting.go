package postgres

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

// ValidateGift is called inside Wallet's existing guarded transaction. It uses
// only PostgreSQL business authority and previously verified provider presence.
// It never calls a provider or posts money.
func (r *Repository) ValidateGift(ctx context.Context, tx pgx.Tx, roomID, session, sender, recipient string) error {
	q := livesql.New(tx)
	v, e := q.Room(ctx, roomID)
	if e != nil {
		return safe(e)
	}
	if v.State != "open" || v.SessionID != session || sender == recipient {
		return live.Fail(apperror.Conflict)
	}
	limits, e := r.effective(ctx, q)
	if e != nil {
		return safe(e)
	}
	for _, id := range []string{sender, recipient} {
		m, e := q.DesiredParticipant(ctx, livesql.DesiredParticipantParams{RoomID: roomID, AccountID: id})
		if e != nil {
			return safe(e)
		}
		if m.State != "active" || !m.AccountActive {
			return live.Fail(apperror.Forbidden)
		}
		if id == recipient {
			member := live.Member{AccountID: m.AccountID, Role: m.Role, State: m.State, Muted: m.Muted, Seat: m.Seat.Int32}
			if !live.CanPublish(room(v), member) || !m.AgencyActive || !limits.Compatibility.Allows(m.Classification, v.Kind) {
				return live.Fail(apperror.Forbidden)
			}
		}
	}
	present, e := q.GiftPresence(ctx, livesql.GiftPresenceParams{RoomID: roomID, Sender: sender, Recipient: recipient})
	if e != nil {
		return safe(e)
	}
	if len(present) != 2 {
		return live.Fail(apperror.Forbidden)
	}
	return nil
}
