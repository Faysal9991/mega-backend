//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

// Verifies the provider receives its full independent budget after local SQL.
type deadlineProbeProvider struct {
	*controlledProvider
	ttl     time.Duration
	members []live.Member
}

func (p *deadlineProbeProvider) Participant(ctx context.Context, _ live.Room, m live.Member) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < p.ttl*9/10 {
		return errors.New("provider budget consumed before remote call")
	}
	p.members = append(p.members, m)
	return ctx.Err()
}

func TestIntegrationLiveReconciliationBatch(t *testing.T) {
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
	actor, host, audience := access.ID(), access.ID(), access.ID()
	ids := []string{actor, host, audience}
	roomID, sessionID, roleID, assocID := access.ID(), access.ID(), access.ID(), access.ID()
	credential := access.Credentials{Access: strings.Repeat("z", 11) + actor, Installation: actor}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for i, statement := range []string{
			"DELETE FROM app.live_presence WHERE account_id=ANY($1::text[])", "DELETE FROM app.live_operations WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))", "DELETE FROM app.live_members WHERE account_id=ANY($1::text[])", "DELETE FROM app.live_rooms WHERE owner_id=ANY($1::text[])", "DELETE FROM app.agency_host_associations WHERE account_id=ANY($1::text[])", "DELETE FROM app.agencies WHERE id=ANY($1::text[])", "DELETE FROM app.access_audit WHERE actor=ANY($1::text[])", "DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])", "DELETE FROM app.live_account_restrictions WHERE account_id=ANY($1::text[])", "DELETE FROM app.profiles WHERE account_id=ANY($1::text[])", "DELETE FROM app.agency_activity_contexts WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])", "DELETE FROM app.identity_installations WHERE id=ANY($1::text[])",
		} {
			if _, e := operator.Exec(cleanup, statement, ids); e != nil {
				t.Errorf("tracked reconciliation cleanup step %d failed", i)
			}
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.access_roles WHERE id=$1", roleID); e != nil {
			t.Error("tracked role cleanup")
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=$1", identity.Digest("profiles-access:"+actor)); e != nil {
			t.Error("tracked rate cleanup")
		}
	}()
	for _, id := range ids {
		if _, e := operator.Exec(ctx, "INSERT INTO app.identity_accounts(id) VALUES($1)", id); e != nil {
			t.Fatal(e)
		}
		if _, e := operator.Exec(ctx, "INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'Synthetic reconciliation','')", id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.identity_installations(id) VALUES($1)", actor); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$1,$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", actor, identity.Digest(credential.Access)); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.access_roles(id,name) VALUES($1,$2)", roleID, "reconcile-batch-"+actor); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.access_role_permissions(role_id,permission) VALUES($1,'live.rooms.moderate')", roleID); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.access_assignments(account_id,role_id) VALUES($1,$2)", actor, roleID); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.agencies(id,name,status) VALUES($1,'Synthetic reconciliation','active')", actor); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.agency_host_associations(id,account_id,agency_id,classification,effective_from) VALUES($1,$2,$3,'audio_host',clock_timestamp()-interval '1 hour')", assocID, host, actor); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.live_rooms(id,session_id,owner_id,kind,title,state,capacity,seats) VALUES($1,$2,$3,'audio','Synthetic reconciliation','open',10,3)", roomID, sessionID, host); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.live_members(room_id,account_id,role,state) VALUES($1,$2,'host','active'),($1,$3,'audience','active')", roomID, host, audience); e != nil {
		t.Fatal(e)
	}
	presentSID, missingSID := "PA_present_"+host, "PA_missing_"+host
	if _, e := operator.Exec(ctx, "INSERT INTO app.live_presence(participant_sid,room_id,account_id,association_id,classification,started_at,qualifying) VALUES($1,$3,$4,$5,'audio_host',clock_timestamp()-interval '2 minutes',true),($2,$3,$4,$5,'audio_host',clock_timestamp()-interval '3 minutes',true)", presentSID, missingSID, roomID, host, assocID); e != nil {
		t.Fatal(e)
	}
	limits := live.Limits{Capacity: 20, Seats: 5, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: 24 * time.Hour, DatabaseTimeout: 30 * time.Second, Compatibility: live.Compatibility{AudioHostAudio: true}}
	repo, e := New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	if e := repo.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	unknown := access.ID()
	snapshotProvider := &controlledProvider{people: []live.Participant{{Identity: host, SID: presentSID}, {Identity: audience, SID: "PA_audience_" + audience}, {Identity: unknown, SID: "PA_unknown_" + unknown}}}
	provider := &deadlineProbeProvider{controlledProvider: snapshotProvider, ttl: limits.ProviderTimeout}
	if e := repo.Reconcile(ctx, credential, roomID, provider); e != nil {
		t.Fatal(e)
	}
	q := livesql.New(pool)
	operations, e := q.OperationPreview(ctx, roomID)
	if e != nil || len(operations) != 1 || operations[0].Target != unknown {
		t.Fatal("known members removed or unknown member retained", e)
	}
	present, e := q.Presence(ctx, presentSID)
	if e != nil || present.EndedAt.Valid {
		t.Fatal("present connection abandoned", e)
	}
	missing, e := q.Presence(ctx, missingSID)
	if e != nil || !missing.EndedAt.Valid || !missing.EndedAt.Time.Equal(missing.StartedAt.Time) {
		t.Fatal("missing disconnect fabricated duration", e)
	}
	if e := repo.Flush(ctx, roomID, provider); e != nil {
		t.Fatal(e)
	}
	pending, e := q.Pending(ctx, roomID)
	if e != nil || pending {
		t.Fatal("unknown provider removal did not converge", e)
	}
	for _, restricted := range []bool{false, true} {
		if restricted {
			if _, e := operator.Exec(ctx, "UPDATE app.agencies SET status='restricted' WHERE id=$1", actor); e != nil {
				t.Fatal(e)
			}
		}
		if e := q.Enqueue(ctx, livesql.EnqueueParams{ID: access.ID(), RoomID: roomID, Kind: "participant", Target: host}); e != nil {
			t.Fatal(e)
		}
		if e := repo.Flush(ctx, roomID, provider); e != nil {
			t.Fatal("eligible/restricted publisher provider operation did not converge", e)
		}
		last := provider.members[len(provider.members)-1]
		if last.AccountID != host || (last.State == "active") == restricted {
			t.Fatal("desired participant authorization mismatch")
		}
	}
	if len(provider.members) != 3 {
		t.Fatal("provider deadline probe missing operation")
	}
}
