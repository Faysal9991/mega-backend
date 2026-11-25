//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	agencypg "github.com/Faysal9991/mega-backend/internal/modules/agency/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationAgencyActivityContextReset(t *testing.T) {
	app := isolated(t, "TEST_", "biho_live_test_app")
	migration := isolated(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cc, e := platform.ConnectionConfig(migration)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal("isolated operator connection")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("actual target guard")
	}
	pool, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	host, agencyA, agencyB := access.ID(), access.ID(), access.ID()
	ids := []string{host, agencyA, agencyB}
	roomID, sessionID := access.ID(), access.ID()
	associationA, classificationA, associationB := access.ID(), access.ID(), access.ID()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for i, sql := range []string{"DELETE FROM app.live_presence WHERE account_id=ANY($1::text[])", "DELETE FROM app.live_rooms WHERE owner_id=ANY($1::text[])", "DELETE FROM app.agency_activity_contexts WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])", "DELETE FROM app.agency_host_associations WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])"} {
			if _, e := operator.Exec(cleanup, sql, ids); e != nil {
				t.Errorf("tracked activity-context cleanup step %d failed", i)
			}
		}
	}()
	for _, id := range ids {
		if _, e := operator.Exec(ctx, "INSERT INTO app.identity_accounts(id) VALUES($1)", id); e != nil {
			t.Fatal(e)
		}
	}
	clock, e := livesql.New(pool).Clock(ctx)
	if e != nil {
		t.Fatal(e)
	}
	at := clock.Time.UTC()
	base := at.Add(-20 * time.Minute)
	if _, e := operator.Exec(ctx, "INSERT INTO app.live_rooms(id,session_id,owner_id,kind,title,state,capacity,seats,created_at,closed_at) VALUES($1,$2,$3,'audio','Synthetic context history','closed',10,3,$4,$5)", roomID, sessionID, host, base, at); e != nil {
		t.Fatal(e)
	}
	txs, e := platform.NewTransactions(operator, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	transition := func(fn func(*agencypg.Writer) error) {
		t.Helper()
		if e := txs.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(772341909)"); e != nil {
				return e
			}
			return fn(agencypg.Bind(tx))
		}); e != nil {
			t.Fatal(e)
		}
	}
	transition(func(w *agencypg.Writer) error {
		return w.InsertAt(ctx, agencypg.Record{ID: associationA, AccountID: host, AgencyID: agencyA, Classification: agency.AudioHost, From: base})
	})
	insertSegment := func(id, association, classification string, start, end time.Time) {
		t.Helper()
		if _, e := operator.Exec(ctx, "INSERT INTO app.live_presence(participant_sid,room_id,account_id,association_id,classification,started_at,ended_at,qualifying) VALUES($1,$2,$3,$4,$5,$6,$7,true)", id, roomID, host, association, classification, start, end); e != nil {
			t.Fatal(e)
		}
	}
	oldAudio, oldVideo, newAudio := "PA_context_audio_"+host, "PA_context_video_"+host, "PA_context_new_"+host
	insertSegment(oldAudio, associationA, agency.AudioHost, base.Add(time.Minute), base.Add(3*time.Minute))
	limits := live.Limits{Capacity: 20, Seats: 5, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: 24 * time.Hour, DatabaseTimeout: 30 * time.Second}
	repo, e := New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	if e := repo.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	sessions := func() []agency.Session {
		t.Helper()
		s, e := repo.Sessions(ctx, host, base.Add(-time.Minute), at)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	if len(sessions()) != 1 {
		t.Fatal("initial context did not include verified interval")
	}
	transition(func(w *agencypg.Writer) error {
		if e := w.CloseAt(ctx, host, base.Add(5*time.Minute)); e != nil {
			return e
		}
		return w.InsertAt(ctx, agencypg.Record{ID: classificationA, AccountID: host, AgencyID: agencyA, Classification: agency.VideoHost, From: base.Add(5 * time.Minute)})
	})
	insertSegment(oldVideo, classificationA, agency.VideoHost, base.Add(6*time.Minute), base.Add(8*time.Minute))
	if len(sessions()) != 2 {
		t.Fatal("same-agency classification change reset context")
	}
	transition(func(w *agencypg.Writer) error { return w.LeaveAt(ctx, host, base.Add(10*time.Minute)) })
	if len(sessions()) != 0 {
		t.Fatal("leave retained active qualifying context")
	}
	var historical int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.live_presence WHERE account_id=$1", host).Scan(&historical); e != nil || historical != 2 {
		t.Fatal("leave erased verified history", e)
	}
	transition(func(w *agencypg.Writer) error {
		return w.InsertAt(ctx, agencypg.Record{ID: associationB, AccountID: host, AgencyID: agencyB, Classification: agency.AudioHost, From: base.Add(12 * time.Minute)})
	})
	if len(sessions()) != 0 {
		t.Fatal("new agency inherited previous day/minute data")
	}
	insertSegment(newAudio, associationB, agency.AudioHost, base.Add(13*time.Minute), base.Add(15*time.Minute))
	current := sessions()
	if len(current) != 1 || current[0].ID != newAudio || current[0].End.Sub(current[0].Start) != 2*time.Minute {
		t.Fatal("new context included previous agency duration")
	}
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.live_presence WHERE account_id=$1", host).Scan(&historical); e != nil || historical != 3 {
		t.Fatal("old verified history not retained", e)
	}
	var contexts int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.agency_activity_contexts WHERE account_id=$1", host).Scan(&contexts); e != nil || contexts != 3 {
		t.Fatal("context history/classification boundary", e)
	}
	for _, sql := range []string{"DELETE FROM app.agency_activity_contexts WHERE account_id=$1", "UPDATE app.agency_activity_contexts SET started_at=clock_timestamp() WHERE account_id=$1"} {
		if _, e := pool.Exec(ctx, sql, host); e == nil {
			t.Fatal("runtime rewrote context history")
		}
	}
}
