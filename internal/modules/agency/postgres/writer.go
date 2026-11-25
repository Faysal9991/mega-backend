// Package postgres owns transaction-bound agency association and activity-context
// writes. Consumers authorize and audit within the existing security transaction.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency/postgres/agencysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Writer struct{ q *agencysql.Queries }

func Bind(tx pgx.Tx) *Writer { return &Writer{q: agencysql.New(tx)} }

type Record struct {
	ID, AccountID, AgencyID, Classification string
	From                                    time.Time
	Until                                   *time.Time
}

func timestamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t.UTC(), Valid: true} }
func (w *Writer) CloseAt(ctx context.Context, account string, at time.Time) error {
	return w.q.CloseAt(ctx, agencysql.CloseAtParams{AccountID: account, EffectiveUntil: timestamp(at)})
}
func (w *Writer) InsertAt(ctx context.Context, r Record) error {
	var until pgtype.Timestamptz
	if r.Until != nil {
		until = timestamp(*r.Until)
	}
	if e := w.q.InsertAt(ctx, agencysql.InsertAtParams{ID: r.ID, AccountID: r.AccountID, AgencyID: r.AgencyID, Classification: r.Classification, EffectiveFrom: timestamp(r.From), EffectiveUntil: until}); e != nil {
		return e
	}
	if r.Until != nil {
		return w.clear(ctx, r.AccountID, *r.Until)
	}
	return w.begin(ctx, r.AccountID, r.AgencyID, r.From)
}
func (w *Writer) CloseNow(ctx context.Context, account string) error {
	return w.q.CloseNow(ctx, account)
}
func (w *Writer) InsertNow(ctx context.Context, id, account, agency, classification string) error {
	at, e := w.q.InsertNow(ctx, agencysql.InsertNowParams{ID: id, AccountID: account, AgencyID: agency, Classification: classification})
	if e != nil {
		return e
	}
	return w.begin(ctx, account, agency, at.Time)
}

// Classification changes inside the same agency preserve its activity context.
// Joining after a leave (NULL context), or switching agency, starts a fresh one.
func (w *Writer) begin(ctx context.Context, account, agency string, at time.Time) error {
	current, e := w.q.CurrentContext(ctx, account)
	if e == nil && current.AgencyID.Valid && current.AgencyID.String == agency {
		return nil
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	return w.q.RecordContext(ctx, agencysql.RecordContextParams{ID: access.ID(), AccountID: account, AgencyID: agency, StartedAt: timestamp(at)})
}
func (w *Writer) clear(ctx context.Context, account string, at time.Time) error {
	return w.q.RecordContext(ctx, agencysql.RecordContextParams{ID: access.ID(), AccountID: account, StartedAt: timestamp(at)})
}
func (w *Writer) LeaveAt(ctx context.Context, account string, at time.Time) error {
	if e := w.CloseAt(ctx, account, at); e != nil {
		return e
	}
	return w.clear(ctx, account, at)
}
func (w *Writer) LeaveNow(ctx context.Context, account string) error {
	at, e := w.q.Clock(ctx)
	if e != nil {
		return e
	}
	return w.LeaveAt(ctx, account, at.Time)
}
