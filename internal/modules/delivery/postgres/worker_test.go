package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
)

func TestBoundedBackoffAndDisabledShutdown(t *testing.T) {
	for attempt := int32(-1); attempt <= 12; attempt++ {
		delay := Backoff(attempt)
		if delay < time.Second || delay > 5*time.Minute {
			t.Fatal("unbounded retry delay")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := (&Dispatcher{Repo: &Repository{}, Workers: 2}).Run(ctx); e != nil {
		t.Fatal(e)
	}
	if e := (&Dispatcher{Repo: &Repository{}, Workers: 100}).Run(ctx); e == nil {
		t.Fatal("unbounded worker count")
	}
}
func TestMalformedIntentNeverReachesProvider(t *testing.T) {
	d := &Dispatcher{Repo: &Repository{}}
	state, code, e := d.deliver(context.Background(), deliverysql.ClaimRow{Topic: "gift", Payload: []byte(`{"room_id":"caller-owned"}`)})
	if state != "failed" || code != "INVALID_INTENT" || e != delivery.ErrPermanent {
		t.Fatal("malformed event accepted")
	}
}
