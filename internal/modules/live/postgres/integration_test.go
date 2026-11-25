//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	walletpg "github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func isolated(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("isolated test acknowledgement required")
	}
	c, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
	if e != nil || c.Name != "biho_live_test" || c.User != role {
		t.Fatal("guarded isolated configuration required")
	}
	c.MaxConns = 5
	return c
}

type controlledProvider struct {
	mu              sync.Mutex
	fail            bool
	created, closed int
	people          []live.Participant
}

func (p *controlledProvider) Create(ctx context.Context, r live.Room) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.created++
	if p.fail {
		return errors.New("synthetic provider failure")
	}
	return ctx.Err()
}
func (p *controlledProvider) Close(ctx context.Context, r live.Room) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed++
	return ctx.Err()
}
func (p *controlledProvider) Participant(ctx context.Context, r live.Room, m live.Member) error {
	return ctx.Err()
}
func (p *controlledProvider) Snapshot(ctx context.Context, r live.Room) (live.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return live.Snapshot{Exists: true, Participants: append([]live.Participant{}, p.people...)}, ctx.Err()
}
func (p *controlledProvider) Token(live.Grant, time.Duration) (string, error) {
	return "synthetic-token", nil
}

type auditFailUnit struct{ access.Unit }

func (auditFailUnit) Audit(context.Context, access.Audit) error {
	return errors.New("synthetic audit failure")
}

type auditFailGuard struct{ Guard }

func (g auditFailGuard) Authorized(ctx context.Context, c access.Credentials, p string, o bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	return g.Guard.Authorized(ctx, c, p, o, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error { return fn(ctx, tx, auditFailUnit{u}) })
}
func decode[T any](t *testing.T, v any) T {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out T
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestIntegrationLiveAgency(t *testing.T) {
	app := isolated(t, "TEST_", "biho_live_test_app")
	migration := isolated(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cc, e := platform.ConnectionConfig(migration)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal("isolated operator connection failed")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e := operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("actual identity guard")
	}
	pool, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	other, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	limits := live.Limits{DatabaseTimeout: 30 * time.Second, Capacity: 20, Seats: 5, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: 24 * time.Hour, Compatibility: live.Compatibility{AudioHostAudio: true, VideoHostVideo: true}}
	repo, e := New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	repo2, e := New(other, limits)
	if e != nil {
		t.Fatal(e)
	}
	if e := repo.Ready(ctx); e != nil {
		t.Fatal("migration 5 required", e)
	}
	repo.guard = diagnosticGuard{repo.guard, t}
	repo2.guard = diagnosticGuard{repo2.guard, t}
	ids := []string{}
	policyIDs := []string{}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		statements := []string{
			"DELETE FROM app.live_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.live_policies WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.live_events WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.live_presence WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_operations WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.live_members WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.live_rooms WHERE owner_id=ANY($1::text[])",
			"DELETE FROM app.host_applications WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.agency_managers WHERE agency_id=ANY($1::text[])",
			"DELETE FROM app.agencies WHERE id=ANY($1::text[])",
			"DELETE FROM app.agency_host_associations WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.economy_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])",
			"DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.access_roles WHERE name=ANY($1::text[])",
			"DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.wallet_accounts WHERE owner_id=ANY($1::text[])",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.profiles WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_account_restrictions WHERE account_id=ANY($1::text[])", "DELETE FROM app.agency_activity_contexts WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])",
			"DELETE FROM app.identity_installations WHERE id=ANY($1::text[])",
		}
		for i := len(policyIDs) - 1; i >= 0; i-- {
			if _, e := operator.Exec(cleanup, "DELETE FROM app.economy_policies WHERE id=$1", policyIDs[i]); e != nil {
				t.Error("tracked policy cleanup", e)
			}
		}
		for _, sql := range statements {
			if _, e := operator.Exec(cleanup, sql, ids); e != nil {
				t.Error("tracked synthetic cleanup", e)
				return
			}
		}
		hashes := []string{}
		for _, id := range ids {
			hashes = append(hashes, identity.Digest("profiles-access:"+id))
		}
		if _, e := operator.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=ANY($1::text[])", hashes); e != nil {
			t.Error("tracked rate cleanup")
		}
	}()
	signup := func() (string, access.Credentials) {
		id := access.ID()
		ids = append(ids, id)
		c := access.Credentials{Access: strings.Repeat("x", 11) + id, Installation: id}
		for _, sql := range []string{"INSERT INTO app.identity_accounts(id) VALUES($1)", "INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'Synthetic fixture','')", "INSERT INTO app.identity_google(account_id,subject) VALUES($1,$1)", "INSERT INTO app.identity_installations(id) VALUES($1)", "INSERT INTO app.wallet_accounts(id,owner_id,kind) VALUES('user:'||$1,$1,'user'),('held:'||$1,$1,'held')"} {
			if _, e := operator.Exec(ctx, sql, id); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$1,$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", id, identity.Digest(c.Access)); e != nil {
			t.Fatal(e)
		}
		return id, c
	}
	admin, ac := signup()
	agencyID, gc := signup()
	host, hc := signup()
	guest, xc := signup()
	secondHost, sc := signup()
	outsider, oc := signup()
	_ = outsider
	// Dedicated scoped staff avoids creating/replacing any protected owner.
	roleID := access.ID()
	if _, e := operator.Exec(ctx, "INSERT INTO app.access_roles(id,name) VALUES($1,$2)", roleID, admin); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{live.AgencyManage, live.HostManage, live.RoomModerate, live.ActivityRead, live.PolicyManage, access.Restrict} {
		if _, e := operator.Exec(ctx, "INSERT INTO app.access_role_permissions(role_id,permission) VALUES($1,$2)", roleID, p); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := operator.Exec(ctx, "INSERT INTO app.access_assignments(account_id,role_id) VALUES($1,$2)", admin, roleID); e != nil {
		t.Fatal(e)
	}
	command := func(kind string) live.Command {
		return live.Command{Kind: kind, Key: access.ID(), Reason: "Synthetic live verification"}
	}
	execute := func(tb *testing.T, c access.Credentials, in live.Command) live.Result {
		tb.Helper()
		v, e := repo.Execute(ctx, c, in)
		if e != nil {
			tb.Fatal(in.Kind, e)
		}
		return v
	}
	expect := func(tb *testing.T, e error, code apperror.Code) {
		tb.Helper()
		if e == nil || apperror.Public(e).Code() != code {
			tb.Fatalf("wanted %s, got %v", code, e)
		}
	}
	createAgency := command("agency.create")
	createAgency.Name = "Fixture agency"
	execute(t, gc, createAgency)
	approveAgency := command("agency.update")
	approveAgency.ID = agencyID
	approveAgency.Name = "Fixture agency"
	approveAgency.Status = "active"
	execute(t, ac, approveAgency)
	_, e = repo.Execute(ctx, oc, approveAgency)
	expect(t, e, apperror.Forbidden)
	apply := func(id string, c access.Credentials) live.Application {
		in := command("host.apply")
		in.ID = agencyID
		in.Classification = agency.AudioHost
		v := execute(t, c, in)
		return decode[live.Application](t, v.Data)
	}
	a := apply(host, hc)
	decision := command("host.decide")
	decision.ID = a.ID
	decision.Status = "approved"
	execute(t, gc, decision)
	a2 := apply(secondHost, sc)
	decision.Key = access.ID()
	decision.ID = a2.ID
	execute(t, gc, decision)
	t.Run("membership conflict and immutable history", func(t *testing.T) {
		in := command("host.apply")
		in.ID = agencyID
		in.Classification = agency.VideoHost
		_, e := repo.Execute(ctx, hc, in)
		expect(t, e, apperror.Conflict)
		if _, e := pool.Exec(ctx, "UPDATE app.agency_host_associations SET classification='video_host' WHERE account_id=$1", host); e == nil {
			t.Fatal("runtime rewrote historical classification")
		}
	})
	t.Run("concurrent membership approval", func(t *testing.T) {
		applicant, pc := signup()
		_ = applicant
		in := command("host.apply")
		in.ID = agencyID
		in.Classification = agency.AudioHost
		first := execute(t, pc, in)
		application := decode[live.Application](t, first.Data)
		approve := command("host.decide")
		approve.ID = application.ID
		approve.Status = "approved"
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		wg.Go(func() { _, e := repo.Execute(ctx, gc, approve); outcomes <- e })
		otherApprove := approve
		otherApprove.Key = access.ID()
		wg.Go(func() { _, e := repo2.Execute(ctx, gc, otherApprove); outcomes <- e })
		wg.Wait()
		success := 0
		for range 2 {
			if e := <-outcomes; e == nil {
				success++
			} else if apperror.Public(e).Code() != apperror.Conflict {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("conflicting active membership")
		}
	})
	t.Run("live policy CAS and scoped administration", func(t *testing.T) {
		in := command("policy.publish")
		in.Policy = &live.Policy{Capacity: 20, Seats: 5, Compatibility: limits.Compatibility}
		_, e := repo.Execute(ctx, oc, in)
		expect(t, e, apperror.Forbidden)
		v := execute(t, ac, in)
		published := decode[live.Policy](t, v.Data)
		if published.ID == "" || published.CreatedAt.IsZero() {
			t.Fatal("policy identity")
		}
		in.Key = access.ID()
		_, e = repo.Execute(ctx, ac, in)
		expect(t, e, apperror.Conflict)
	})
	create := command("room.create")
	create.Name = "Fixture room"
	create.Status = "audio"
	create.Capacity = 10
	create.Seats = 3
	var created live.Result
	t.Run("concurrent create and fingerprint", func(t *testing.T) {
		results := make(chan live.Result, 2)
		failures := make(chan error, 2)
		var wg sync.WaitGroup
		for _, r := range []*Repository{repo, repo2} {
			wg.Go(func() { v, e := r.Execute(ctx, hc, create); results <- v; failures <- e })
		}
		wg.Wait()
		for range 2 {
			v := <-results
			if e := <-failures; e != nil {
				t.Fatal(e)
			}
			if created.RoomID != "" && created.RoomID != v.RoomID {
				t.Fatal("duplicate room")
			}
			created = v
		}
		changed := create
		changed.Name = "Different payload"
		_, e := repo.Execute(ctx, hc, changed)
		expect(t, e, apperror.Conflict)
		another := create
		another.Key = access.ID()
		_, e = repo.Execute(ctx, hc, another)
		expect(t, e, apperror.Conflict)
	})
	var auditCount int
	if e := operator.QueryRow(ctx, "SELECT count(*) FROM app.access_audit WHERE actor=$1 AND target=$2 AND action='room.create' AND EXISTS(SELECT 1 FROM unnest(fields) f WHERE f LIKE 'command:%')", host, created.RoomID).Scan(&auditCount); e != nil || auditCount != 1 {
		t.Fatal("room audit target/fingerprint not durable", e)
	}
	provider := &controlledProvider{fail: true}
	if e := repo.Flush(ctx, created.RoomID, provider); e == nil {
		t.Fatal("provider failure hidden")
	}
	_, e = repo.Grant(ctx, hc, created.RoomID)
	expect(t, e, apperror.Conflict)
	drain := func(id string) {
		t.Helper()
		for range 20 {
			pending, e := livesql.New(pool).Pending(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			if !pending {
				return
			}
			if e := repo.Flush(ctx, id, provider); e != nil && apperror.Public(e).Code() != apperror.ServiceUnavailable {
				t.Fatal(e)
			}
		}
		t.Fatal("durable provider backlog did not converge")
	}
	provider.fail = false
	if e := repo.Flush(ctx, created.RoomID, provider); e != nil {
		t.Fatal(e)
	}
	g, e := repo.Grant(ctx, hc, created.RoomID)
	if e != nil || !live.CanPublish(g.Room, g.Member) || g.Identity != host {
		t.Fatal("host grant", e)
	}
	join := command("room.join")
	join.ID = created.RoomID
	execute(t, xc, join)
	join.Key = access.ID()
	execute(t, sc, join)
	if _, e := repo.Grant(ctx, oc, created.RoomID); e == nil {
		t.Fatal("nonmember token")
	}
	if g, e := repo.Grant(ctx, xc, created.RoomID); e != nil || live.CanPublish(g.Room, g.Member) {
		t.Fatal("audience publishing", e)
	}
	invite := command("seat.invite")
	invite.ID = created.RoomID
	invite.Target = guest
	invite.Seat = 1
	_, e = repo.Execute(ctx, hc, invite)
	expect(t, e, apperror.NotFound) // No approved host association.
	invite.Target = secondHost
	execute(t, hc, invite)
	// A second eligible audience races for the same unique seat.
	third, tc := signup()
	a3 := apply(third, tc)
	decision.Key = access.ID()
	decision.ID = a3.ID
	execute(t, gc, decision)
	join.Key = access.ID()
	execute(t, tc, join)
	invite.Key = access.ID()
	invite.Target = third
	execute(t, hc, invite)
	t.Run("capacity race", func(t *testing.T) {
		members, e := livesql.New(pool).Members(ctx, created.RoomID)
		if e != nil {
			t.Fatal(e)
		}
		before := len(members)
		if _, e := operator.Exec(ctx, "UPDATE app.live_rooms SET capacity=$2 WHERE id=$1", created.RoomID, before+1); e != nil {
			t.Fatal(e)
		}
		request := command("room.join")
		request.ID = created.RoomID
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, e := repo.Execute(ctx, ac, request); results <- e })
		other := request
		other.Key = access.ID()
		wg.Go(func() { _, e := repo2.Execute(ctx, oc, other); results <- e })
		wg.Wait()
		success := 0
		for range 2 {
			if e := <-results; e == nil {
				success++
			} else if apperror.Public(e).Code() != apperror.Conflict {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("capacity exceeded")
		}
		members, e = livesql.New(pool).Members(ctx, created.RoomID)
		if e != nil || len(members) != before+1 {
			t.Fatal("capacity reservation race", e)
		}
		if _, e := operator.Exec(ctx, "UPDATE app.live_rooms SET capacity=10 WHERE id=$1", created.RoomID); e != nil {
			t.Fatal(e)
		}
	})
	accept := command("seat.accept")
	accept.ID = created.RoomID
	t.Run("seat race", func(t *testing.T) {
		results := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, e := repo.Execute(ctx, sc, accept); results <- e })
		wg.Go(func() { _, e := repo2.Execute(ctx, tc, accept); results <- e })
		wg.Wait()
		success := 0
		for range 2 {
			if e := <-results; e == nil {
				success++
			} else if apperror.Public(e).Code() != apperror.Conflict {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("seat double allocation")
		}
	})
	// Flush outstanding seat operations; admission stays closed until synchronized.
	drain(created.RoomID)

	event := func(kind, sid, account string) live.Event {
		return live.Event{ID: access.ID(), Type: kind, SessionID: g.Room.SessionID, RoomSID: "RM_fixture", AccountID: account, ParticipantSID: sid, At: time.Now().UTC()}
	}
	t.Run("duplicate out of order reconnect and missing disconnect", func(t *testing.T) {
		joined := event("participant_joined", "PA_host", host)
		if e := repo.Ingest(ctx, joined); e != nil {
			t.Fatal(e)
		}
		if e := repo2.Ingest(ctx, joined); e != nil {
			t.Fatal(e)
		}
		left := event("participant_left", "PA_host", host)
		if e := repo.Ingest(ctx, left); e != nil {
			t.Fatal(e)
		}
		if e := repo.Ingest(ctx, left); e != nil {
			t.Fatal(e)
		}
		if e := repo.Ingest(ctx, joined); e != nil {
			t.Fatal(e)
		}
		bad := joined
		bad.AccountID = guest
		expect(t, repo.Ingest(ctx, bad), apperror.Conflict)
		leftFirst := event("participant_left", "PA_late", host)
		if e := repo.Ingest(ctx, leftFirst); e != nil {
			t.Fatal(e)
		}
		late := event("participant_joined", "PA_late", host)
		if e := repo.Ingest(ctx, late); e != nil {
			t.Fatal(e)
		}
		p, e := livesql.New(pool).Presence(ctx, "PA_late")
		if e != nil || p.StartedAt.Valid {
			t.Fatal("late join reopened tombstone", e)
		}
		reconnect := event("participant_joined", "PA_reconnect", host)
		if e := repo.Ingest(ctx, reconnect); e != nil {
			t.Fatal(e)
		}
		drain(created.RoomID)
		if e := repo.Reconcile(ctx, ac, created.RoomID, provider); e != nil {
			t.Fatal(e)
		}
		p, e = livesql.New(pool).Presence(ctx, "PA_reconnect")
		if e != nil || !p.EndedAt.Valid || !p.StartedAt.Time.Equal(p.EndedAt.Time) {
			t.Fatal("missing disconnect fabricated time", e)
		}
		stale := event("participant_joined", "PA_stale", host)
		stale.At = time.Now().Add(-25 * time.Hour)
		expect(t, repo.Ingest(ctx, stale), apperror.Conflict)
	})
	t.Run("ban mute role and protected audit rollback", func(t *testing.T) {
		ban := command("member.ban")
		ban.ID = created.RoomID
		ban.Target = guest
		_, e := repo.Execute(ctx, xc, ban)
		expect(t, e, apperror.Forbidden)
		execute(t, hc, ban)
		_, e = repo.Grant(ctx, xc, created.RoomID)
		expect(t, e, apperror.Forbidden)
		join.Key = access.ID()
		_, e = repo.Execute(ctx, xc, join)
		expect(t, e, apperror.Forbidden)
		failed := *repo
		failed.guard = auditFailGuard{repo.guard}
		unban := command("member.unban")
		unban.ID = created.RoomID
		unban.Target = guest
		_, e = failed.Execute(ctx, hc, unban)
		expect(t, e, apperror.ServiceUnavailable)
		m, e := livesql.New(pool).Member(ctx, livesql.MemberParams{RoomID: created.RoomID, AccountID: guest})
		if e != nil || m.State != "banned" {
			t.Fatal("audit failure committed mutation")
		}
		mute := command("member.mute")
		mute.ID = created.RoomID
		mute.Target = secondHost
		mute.Muted = true
		execute(t, hc, mute)
		role := command("member.role")
		role.ID = created.RoomID
		role.Target = secondHost
		role.Role = "co_host"
		execute(t, hc, role)
	})
	t.Run("trusted source and Wallet eligibility integration", func(t *testing.T) {
		sessions, e := repo.Sessions(ctx, host, time.Now().Add(-time.Hour), time.Now())
		if e != nil {
			t.Fatal(e)
		}
		for _, s := range sessions {
			if s.ID == "PA_late" || s.ID == "PA_reconnect" {
				t.Fatal("unverified interval credited")
			}
		}
		wp, e := walletpg.New(pool, repo)
		if e != nil {
			t.Fatal(e)
		}
		engine := wallet.New(wp)
		out, e := engine.Read(ctx, hc, wallet.Query{Kind: "eligibility"})
		if e != nil {
			t.Fatal(e)
		}
		if decode[wallet.Eligibility](t, out.Data).Eligible {
			t.Fatal("unconfigured withdrawal allowed")
		}
		now, e := livesql.New(pool).Clock(ctx)
		if e != nil {
			t.Fatal(e)
		}
		start := now.Time.Add(-3 * time.Minute)
		end := now.Time.Add(-time.Minute)
		// Migrator-only synthetic completed provider history; no live E2E claim.
		var assoc string
		if e := operator.QueryRow(ctx, "SELECT id FROM app.agency_host_associations WHERE account_id=$1 AND effective_until IS NULL", host).Scan(&assoc); e != nil {
			t.Fatal(e)
		}
		if _, e := operator.Exec(ctx, "UPDATE app.agency_host_associations SET effective_from=$2 WHERE id=$1", assoc, start.Add(-time.Hour)); e != nil {
			t.Fatal(e)
		}
		if _, e := operator.Exec(ctx, "INSERT INTO app.live_presence(participant_sid,room_id,account_id,association_id,classification,started_at,ended_at,qualifying) VALUES($1,$2,$3,$4,'audio_host',$5,$6,true)", "PA_synthetic_"+host, created.RoomID, host, assoc, start, end); e != nil {
			t.Fatal(e)
		}
		activity := agency.Policy{Version: access.ID(), Scope: agency.AudioHost, Timezone: "Etc/UTC", Period: agency.CurrentCalendarMonth, MinimumQualifyingDays: 1, MinimumDailyLiveMinutes: 1, EffectiveAt: start.Add(-time.Hour)}
		for _, p := range []wallet.Policy{{ID: access.ID(), Kind: "withdrawal", Scope: "all", Enabled: true, ReviewOnly: true, Min: 1, Max: 10, Fee: wallet.Fee{Rounding: "ceil"}, EffectiveAt: start.Add(-time.Hour)}, {ID: activity.Version, Kind: "activity", Scope: agency.AudioHost, Enabled: true, Activity: &activity, EffectiveAt: activity.EffectiveAt}} {
			if e := p.Validate(); e != nil {
				t.Fatal("synthetic immutable policy", e)
			}
			raw, e := json.Marshal(p)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := operator.Exec(ctx, "INSERT INTO app.economy_policies(id,kind,scope,effective_at,actor_id,config) VALUES($1,$2,$3,$4,$5,$6::jsonb)", p.ID, p.Kind, p.Scope, p.EffectiveAt, admin, string(raw)); e != nil {
				t.Fatal(e)
			}
			policyIDs = append(policyIDs, p.ID)
		}
		out, e = engine.Read(ctx, hc, wallet.Query{Kind: "eligibility"})
		if e != nil {
			t.Fatal(e)
		}
		eligibility := decode[wallet.Eligibility](t, out.Data)
		if eligibility.Activity == nil {
			t.Fatal("live source not evaluated by wallet")
		}
		if eligibility.Eligible {
			t.Fatal("empty wallet permitted withdrawal")
		}
		// Near a month boundary the interval may fall outside the window; require
		// exact agreement with the same approved calendar calculator in either case.
		expected, e := agency.Eligibility(ctx, repo, host, activity, now.Time)
		if e != nil {
			t.Fatal(e)
		}
		if eligibility.Activity.QualifyingDays != expected.QualifyingDays || eligibility.Activity.Eligible != expected.Eligible {
			t.Fatal("wallet/source calendar disagreement")
		}
		unavailable, e := walletpg.New(pool, nil)
		if e != nil {
			t.Fatal(e)
		}
		missing, e := wallet.New(unavailable).Read(ctx, hc, wallet.Query{Kind: "eligibility"})
		if e != nil {
			t.Fatal(e)
		}
		closed := decode[wallet.Eligibility](t, missing.Data)
		if closed.Activity == nil || closed.Activity.Eligible || closed.Activity.Reason != agency.ReasonUnavailable {
			t.Fatal("missing source did not fail closed")
		}
		_, e = engine.Execute(ctx, hc, wallet.Command{Kind: "withdrawal", Amount: 1, Key: access.ID(), Correlation: "live-fixture"})
		if e == nil {
			t.Fatal("unfunded withdrawal accepted")
		}
	})
	t.Run("account restriction cuts activity and rolls back atomically", func(t *testing.T) {
		started := event("participant_joined", "PA_restricted", host)
		if e := repo.Ingest(ctx, started); e != nil {
			t.Fatal(e)
		}
		drain(created.RoomID)
		ar, e := accesspg.New(pool)
		if e != nil {
			t.Fatal(e)
		}
		before, e := livesql.New(pool).RestrictionHistory(ctx, host)
		if e != nil {
			t.Fatal(e)
		}
		e = ar.Authorized(ctx, ac, access.Restrict, false, host, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
			if e := u.Restrict(ctx, host, true); e != nil {
				return e
			}
			return errors.New("synthetic audit failure after restriction")
		})
		expect(t, e, apperror.ServiceUnavailable)
		after, e := livesql.New(pool).RestrictionHistory(ctx, host)
		if e != nil || len(after) != len(before) {
			t.Fatal("restriction evidence escaped rollback", e)
		}
		as, e := access.New(ar)
		if e != nil {
			t.Fatal(e)
		}
		if e := as.SetRestriction(ctx, ac, host, true, "Synthetic account restriction", "live-restriction"); e != nil {
			t.Fatal(e)
		}
		history, e := livesql.New(pool).RestrictionHistory(ctx, host)
		if e != nil || len(history) != len(before)+1 {
			t.Fatal("missing immutable restriction ceiling", e)
		}
		cutoff := history[len(history)-1].RestrictedAt.Time
		for _, statement := range []string{"UPDATE app.live_account_restrictions SET restricted_at=clock_timestamp() WHERE account_id=$1", "DELETE FROM app.live_account_restrictions WHERE account_id=$1"} {
			if _, e := pool.Exec(ctx, statement, host); e == nil {
				t.Fatal("runtime rewrote immutable restriction evidence")
			}
		}
		inspection, e := livesql.New(pool).InspectActivity(ctx, livesql.InspectActivityParams{AccountID: host, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		cutoffVisible := false
		for _, v := range inspection {
			if v.ParticipantSid == "PA_restricted" && v.RestrictedAt.Valid && v.RestrictedAt.Time.Equal(cutoff) {
				cutoffVisible = true
			}
		}
		if !cutoffVisible {
			t.Fatal("activity inspection omitted restriction evidence")
		}

		if _, e := repo.Grant(ctx, hc, created.RoomID); e == nil {
			t.Fatal("restricted account obtained media grant")
		}
		m, e := livesql.New(pool).Member(ctx, livesql.MemberParams{RoomID: created.RoomID, AccountID: host})
		if e != nil || m.State != "left" {
			t.Fatal("restriction retained active membership", e)
		}
		drain(created.RoomID)
		ended := event("participant_left", "PA_restricted", host)
		if e := repo.Ingest(ctx, ended); e != nil {
			t.Fatal(e)
		}
		sessions, e := repo.Sessions(ctx, host, time.Now().Add(-time.Hour), time.Now())
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, v := range sessions {
			if v.ID == "PA_restricted" {
				found = true
				if !v.End.Equal(cutoff) {
					t.Fatal("restricted interval was not clipped at server cutoff")
				}
			}
		}
		if !found {
			t.Fatal("verified prefix not retained")
		}
		if e := as.SetRestriction(ctx, ac, host, false, "Lift synthetic restriction", "live-unrestriction"); e != nil {
			t.Fatal(e)
		}
		if _, e := repo.Grant(ctx, hc, created.RoomID); e == nil {
			t.Fatal("unrestrict revived revoked session")
		}
		// Fresh synthetic reauthentication; production sessions are never unrevoked.
		hc.Access = strings.Repeat("y", 11) + access.ID()
		if _, e := operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", access.ID(), host, hc.Installation, identity.Digest(hc.Access)); e != nil {
			t.Fatal(e)
		}
		rejoin := command("room.join")
		rejoin.ID = created.RoomID
		execute(t, hc, rejoin)
		sessions, e = repo.Sessions(ctx, host, time.Now().Add(-time.Hour), time.Now())
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range sessions {
			if v.ID == "PA_restricted" && !v.End.Equal(cutoff) {
				t.Fatal("unrestriction rewrote completed activity")
			}
		}
	})
	t.Run("classification and association history", func(t *testing.T) {
		change := command("host.classify")
		change.Target = host
		change.Classification = agency.VideoHost
		execute(t, ac, change)
		q := livesql.New(pool)
		history, e := q.History(ctx, livesql.HistoryParams{AccountID: host, Limit: 10})
		if e != nil || len(history) != 2 {
			t.Fatal("history lost", e)
		}
		_, e = repo.Grant(ctx, hc, created.RoomID)
		if e == nil {
			t.Fatal("incompatible classification allowed token")
		}
		leave := command("host.leave")
		execute(t, hc, leave)
		if _, e := q.Association(ctx, host); !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal("leave preserved active association")
		}
	})
	t.Run("provider absence and unexpected room reincarnation", func(t *testing.T) {
		request := create
		request.Key = access.ID()
		request.Name = "Absent provider room"
		createdMissing := execute(t, sc, request)
		drain(createdMissing.RoomID)
		if e := repo.Reconcile(ctx, ac, createdMissing.RoomID, absentProvider{provider}); e != nil {
			t.Fatal(e)
		}
		missing, e := livesql.New(pool).Room(ctx, createdMissing.RoomID)
		if e != nil || missing.State != "closed" {
			t.Fatal("provider absence left admission open", e)
		}
		request.Key = access.ID()
		request.Name = "Unexpected provider incarnation"
		recreated := execute(t, tc, request)
		drain(recreated.RoomID)
		row, e := livesql.New(pool).Room(ctx, recreated.RoomID)
		if e != nil {
			t.Fatal(e)
		}
		initial := live.Event{ID: access.ID(), Type: "room_started", SessionID: row.SessionID, RoomSID: "RM_original", At: time.Now().UTC()}
		if e := repo.Ingest(ctx, initial); e != nil {
			t.Fatal(e)
		}
		unexpected := live.Event{ID: access.ID(), Type: "participant_joined", SessionID: row.SessionID, RoomSID: "RM_reincarnated", AccountID: third, ParticipantSID: "PA_unexpected", At: time.Now().UTC()}
		if e := repo.Ingest(ctx, unexpected); e != nil {
			t.Fatal(e)
		}
		row, e = livesql.New(pool).Room(ctx, recreated.RoomID)
		if e != nil || row.State != "closed" {
			t.Fatal("unexpected provider SID reopened session", e)
		}
		if _, e := livesql.New(pool).Presence(ctx, "PA_unexpected"); !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal("unexpected incarnation established activity")
		}
		drain(recreated.RoomID)
	})
	t.Run("concurrent close and cleanup", func(t *testing.T) {
		close := command("room.close")
		close.ID = created.RoomID
		adminClose := close
		adminClose.Admin = true
		_, err := repo.Execute(ctx, hc, adminClose)
		expect(t, err, apperror.Forbidden)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, r := range []*Repository{repo, repo2} {
			wg.Go(func() { _, e := r.Execute(ctx, hc, close); results <- e })
		}
		wg.Wait()
		for range 2 {
			if e := <-results; e != nil {
				t.Fatal(e)
			}
		}
		drain(created.RoomID)
		v, e := livesql.New(pool).Room(ctx, created.RoomID)
		if e != nil || v.State != "closed" {
			t.Fatal("close not reconciled", e)
		}
	})
	// Keep compile-time evidence that the shared Access adapter remains compatible.
	var _ Guard = (*accesspg.Repository)(nil)
}

type diagnosticGuard struct {
	Guard
	t *testing.T
}

func (g diagnosticGuard) Authorized(ctx context.Context, c access.Credentials, p string, o bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	entered, success := false, false
	start := time.Now()
	e := g.Guard.Authorized(ctx, c, p, o, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		entered = true
		e := fn(ctx, tx, u)
		success = e == nil
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			g.t.Logf("callback SQLSTATE=%s message=%s", pg.Code, pg.Message)
		}
		if errors.Is(e, context.DeadlineExceeded) {
			g.t.Log("callback deadline exceeded")
		}
		return e
	})
	if e != nil {
		g.t.Logf("guard entered=%t success=%t elapsed=%s context_done=%t safe_code=%s", entered, success, time.Since(start), ctx.Err() != nil, apperror.Public(e).Code())
	}
	return e
}

type absentProvider struct{ *controlledProvider }

func (p absentProvider) Snapshot(context.Context, live.Room) (live.Snapshot, error) {
	return live.Snapshot{Exists: false, Participants: []live.Participant{}}, nil
}
