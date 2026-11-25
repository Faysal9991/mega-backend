// Package intents owns transaction-bound notification and outbox inserts.
package intents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/jackc/pgx/v5/pgtype"
)

func Enqueue(ctx context.Context, db deliverysql.DBTX, id, topic, aggregate string, payload any, at time.Time) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	if len(b) > 4096 {
		return delivery.Fail("VALIDATION_FAILED")
	}
	return deliverysql.New(db).Enqueue(ctx, deliverysql.EnqueueParams{ID: id, Topic: topic, AggregateID: aggregate, Payload: string(b), AvailableAt: pgtype.Timestamptz{Time: at, Valid: true}})
}
func Notify(ctx context.Context, db deliverysql.DBTX, account, kind, reference string) error {
	if _, _, ok := delivery.Template(kind); !ok {
		return delivery.Fail("VALIDATION_FAILED")
	}
	q := deliverysql.New(db)
	id := access.ID()
	if e := q.Notification(ctx, deliverysql.NotificationParams{ID: id, AccountID: account, Kind: kind, Reference: reference}); e != nil {
		return e
	}
	return q.QueuePush(ctx, id)
}
