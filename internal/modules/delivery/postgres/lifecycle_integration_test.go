//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingPush struct {
	entered chan struct{}
	once    sync.Once
}

func (p *blockingPush) Send(ctx context.Context, _ delivery.Push) error {
	p.once.Do(func() { close(p.entered) })
	<-ctx.Done()
	return ctx.Err()
}
func TestIntegrationDispatcherInflightShutdownRecovery(t *testing.T) {
	app, mc := settings(t, "TEST_", "biho_live_test_app"), settings(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cc, e := platform.ConnectionConfig(mc)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal("guarded operator connection")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e = operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("authenticated target guard")
	}
	pool, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	id := access.ID()
	notification := access.ID()
	creds := access.Credentials{Access: strings.Repeat("q", 11) + id, Installation: id}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, sql := range []string{"DELETE FROM app.delivery_outbox WHERE topic='push' AND payload->>'notification_id' IN(SELECT id FROM app.notifications WHERE account_id=$1)", "DELETE FROM app.notifications WHERE account_id=$1", "DELETE FROM app.push_devices WHERE account_id=$1", "DELETE FROM app.notification_preferences WHERE account_id=$1", "DELETE FROM app.identity_sessions WHERE account_id=$1", "DELETE FROM app.profiles WHERE account_id=$1", "DELETE FROM app.identity_accounts WHERE id=$1", "DELETE FROM app.identity_installations WHERE id=$1"} {
			if _, e := operator.Exec(cleanup, sql, id); e != nil {
				t.Error("tracked lifecycle cleanup", e)
			}
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=$1", identity.Digest("profiles-access:"+id)); e != nil {
			t.Error(e)
		}
	}()
	for _, sql := range []string{"INSERT INTO app.identity_accounts(id) VALUES($1)", "INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'Synthetic lifecycle','')", "INSERT INTO app.identity_installations(id) VALUES($1)"} {
		if _, e = operator.Exec(ctx, sql, id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$1,$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", id, identity.Digest(creds.Access)); e != nil {
		t.Fatal(e)
	}
	repo, e := New(pool, nil, nil, config.DeliveryConfig{DatabaseTimeout: 30 * time.Second, TokenKey: config.Secret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = repo.Preferences(ctx, creds, &delivery.Preferences{RoomInvitation: true}); e != nil {
		t.Fatal(e)
	}
	if e = repo.Register(ctx, creds, "android", "synthetic-lifecycle-token-"+id); e != nil {
		t.Fatal(e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = deliverysql.New(tx).Notification(ctx, deliverysql.NotificationParams{ID: notification, AccountID: id, Kind: "room_invitation", Reference: access.ID()}); e != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = deliverysql.New(tx).QueuePush(ctx, notification); e != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	runCtx, stop := context.WithCancel(ctx)
	blocked := &blockingPush{entered: make(chan struct{})}
	d := &Dispatcher{Repo: repo, Push: blocked, Workers: 2}
	done := make(chan error, 1)
	go func() { done <- d.Run(runCtx) }()
	select {
	case <-blocked.entered:
	case <-time.After(15 * time.Second):
		stop()
		<-done
		t.Fatal("dispatcher never reached controlled provider")
	}
	stop()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("in-flight worker did not join")
	}
	// Only this tracked intent's clock is advanced to simulate crash/lease expiry.
	if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET available_at=clock_timestamp(),lease_until=clock_timestamp()-interval '1 second' WHERE topic='push' AND payload->>'notification_id'=$1", notification); e != nil {
		t.Fatal(e)
	}
	accepted := &pushSink{}
	restart := &Dispatcher{Repo: repo, Push: accepted, Workers: 1}
	worked, e := restart.Step(ctx, []string{"push"})
	if e != nil || !worked || accepted.calls.Load() != 1 {
		t.Fatal("restart failed to recover canceled intent", e)
	}
	var state string
	var attempts int
	if e = operator.QueryRow(ctx, "SELECT state,attempts FROM app.delivery_outbox WHERE topic='push' AND payload->>'notification_id'=$1", notification).Scan(&state, &attempts); e != nil || state != "delivered" || attempts != 2 {
		t.Fatal("canceled intent lost/reduplicated", e)
	}
}
