//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live/postgres/livesql"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	walletpg "github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func setting(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("explicit isolated ack")
	}
	v, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
	if e != nil || v.Name != "biho_live_test" || v.User != role {
		t.Fatal("test guard")
	}
	v.MaxConns = 3
	return v
}
func TestIntegrationConsolidatedCapabilitiesPackages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	app, mc := setting(t, "TEST_", "biho_live_test_app"), setting(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	cc, e := platform.ConnectionConfig(mc)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal("guarded operator")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e = operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != mc.Name || role != mc.User {
		t.Fatal("authenticated target guard")
	}
	m, e := platform.NewMigrator(mc)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Up(ctx); e != nil {
		m.Close()
		t.Fatal(e)
	}
	m.Close()
	pool, e := platform.Open(ctx, app)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	ar, e := accesspg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	as, e := access.New(ar)
	if e != nil {
		t.Fatal(e)
	}
	wr, e := walletpg.New(pool, nil)
	if e != nil {
		t.Fatal(e)
	}
	repo, e := New(pool, wr)
	if e != nil {
		t.Fatal(e)
	}
	limits := live.Limits{Capacity: 20, Seats: 4, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: time.Hour, DatabaseTimeout: 30 * time.Second, Compatibility: live.Compatibility{AudioHostAudio: true, VideoHostVideo: true}}
	lr, e := livepg.New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	other, e := livepg.New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{access.ID(), access.ID(), access.ID(), access.ID(), access.ID(), access.ID()}
	owner, mod, host1, host2, user, second := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	room1, room2, staffRole := access.ID(), access.ID(), access.ID()
	rooms := []string{room1, room2}
	creds := map[string]access.Credentials{}
	packageIDs := []string{}
	policyIDs := []string{}
	var originalPolicy string
	if e = operator.QueryRow(ctx, "SELECT policy_id FROM app.access_policy_head WHERE singleton").Scan(&originalPolicy); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 90*time.Second)
		defer done()
		tx, e := operator.Begin(cleanup)
		if e != nil {
			t.Error("cleanup transaction")
			return
		}
		defer tx.Rollback(context.Background())
		statements := []string{
			"UPDATE app.access_policy_head SET policy_id=$4,revision=revision+1 WHERE singleton AND policy_id=ANY($5::text[])",
			"DELETE FROM app.access_policy_versions WHERE id=ANY($5::text[])",
			"UPDATE app.wallet_accounts a SET balance=a.balance-d.delta FROM(SELECT e.account_id,sum(e.delta)::bigint AS delta FROM app.wallet_entries e JOIN app.wallet_operations o ON o.id=e.operation_id WHERE o.actor_id=ANY($1::text[]) GROUP BY e.account_id)d WHERE a.id=d.account_id",
			"DELETE FROM app.package_profit WHERE operation_id IN(SELECT id FROM app.wallet_operations WHERE actor_id=ANY($1::text[]))",
			"DELETE FROM app.package_purchases WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.package_revocations WHERE entitlement_id IN(SELECT id FROM app.package_entitlements WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.package_entitlements WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.package_commands WHERE actor_id=ANY($1::text[])",
			"UPDATE app.package_catalog SET current_version=NULL WHERE id=ANY($3::text[])",
			"DELETE FROM app.package_versions WHERE package_id=ANY($3::text[])",
			"DELETE FROM app.package_catalog WHERE id=ANY($3::text[])",
			"DELETE FROM app.wallet_entries WHERE operation_id IN(SELECT id FROM app.wallet_operations WHERE actor_id=ANY($1::text[]))",
			"DELETE FROM app.economy_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.wallet_operations WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.live_list_snapshots WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.access_overrides WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_global_bans WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_presence WHERE room_id=ANY($2::text[])",
			"DELETE FROM app.live_operations WHERE room_id=ANY($2::text[])",
			"DELETE FROM app.live_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.live_members WHERE room_id=ANY($2::text[])",
			"DELETE FROM app.live_rooms WHERE id=ANY($2::text[])",
			"DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.access_role_permissions WHERE role_id=$6",
			"DELETE FROM app.access_roles WHERE id=$6",
			"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])",
			"DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])",
			"DELETE FROM app.identity_installations WHERE id=ANY($1::text[])",
			"DELETE FROM app.identity_rate_limits WHERE scope_hash=ANY($7::text[])"}
		hashes := []string{}
		for _, id := range ids {
			hashes = append(hashes, identity.Digest("profiles-access:"+id))
		}
		for _, sql := range statements { // Every placeholder is typed even for statements using a subset.
			sql = "WITH fixture_args AS(SELECT $1::text[] AS accounts,$2::text[] AS rooms,$3::text[] AS packages,$4::text AS original_policy,$5::text[] AS policies,$6::text AS staff_role,$7::text[] AS rate_hashes) " + sql
			if _, e = tx.Exec(cleanup, sql, ids, rooms, packageIDs, originalPolicy, policyIDs, staffRole, hashes); e != nil {
				t.Error("tracked cleanup", e)
				return
			}
		}
		if e = tx.Commit(cleanup); e != nil {
			t.Error("cleanup commit", e)
		}
	}()
	b := &pgx.Batch{}
	for _, id := range ids {
		c := access.Credentials{Access: strings.Repeat("x", 11) + id, Installation: id}
		creds[id] = c
		b.Queue("INSERT INTO app.identity_accounts(id) VALUES($1)", id)
		b.Queue("INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'Synthetic consolidated','')", id)
		b.Queue("INSERT INTO app.identity_installations(id) VALUES($1)", id)
		b.Queue("INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$1,$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", id, identity.Digest(c.Access))
		b.Queue("INSERT INTO app.wallet_accounts(id,owner_id,kind) VALUES('user:'||$1,$1,'user'),('held:'||$1,$1,'held')", id)
	}
	b.Queue("INSERT INTO app.access_owners(account_id) VALUES($1)", owner)
	b.Queue("INSERT INTO app.access_roles(id,name) VALUES($1,$2)", staffRole, "Synthetic moderator "+staffRole)
	for _, p := range access.ModeratorDefaults() {
		b.Queue("INSERT INTO app.access_role_permissions(role_id,permission) VALUES($1,$2)", staffRole, p)
	}
	b.Queue("INSERT INTO app.access_assignments(account_id,role_id) VALUES($1,$2)", mod, staffRole)
	for i, id := range rooms {
		host := []string{host1, host2}[i]
		b.Queue("INSERT INTO app.live_rooms(id,session_id,owner_id,kind,title,state,capacity,seats) VALUES($1,$1,$2,'audio','Synthetic room','open',20,4)", id, host)
		b.Queue("INSERT INTO app.live_members(room_id,account_id,role,state) VALUES($1,$2,'host','active'),($1,$3,'audience','active'),($1,$4,'audience','active'),($1,$5,'audience','active')", id, host, mod, user, second)
	}
	b.Queue("INSERT INTO app.live_members(room_id,account_id,role,state) VALUES($1,$2,'audience','active')", room1, owner)
	results := operator.SendBatch(ctx, b)
	for range b.Len() {
		if _, e = results.Exec(); e != nil {
			results.Close()
			t.Fatal("synthetic setup", e)
		}
	}
	if e = results.Close(); e != nil {
		t.Fatal(e)
	}
	expect := func(t *testing.T, e error, code apperror.Code) {
		t.Helper()
		if e == nil || apperror.Public(e).Code() != code {
			t.Fatalf("want %s got %v %s", code, e, failureClass(e))
		}
	}
	command := func(kind, room, target string) live.Command {
		return live.Command{Kind: kind, ID: room, Target: target, Key: access.ID(), Reason: "Synthetic consolidated policy"}
	}
	execute := func(t *testing.T, c access.Credentials, in live.Command) live.Result {
		t.Helper()
		v, e := lr.Execute(ctx, c, in)
		if e != nil {
			t.Fatal(in.Kind, e, failureClass(e))
		}
		return v
	}
	oc, mc2, uc := creds[owner], creds[mod], creds[user]
	t.Run("global moderation hidden projections self seats and revocation", func(t *testing.T) {
		for _, room := range rooms {
			in := command("member.mute", room, user)
			in.Muted = true
			execute(t, mc2, in)
			in = command("seat.remove", room, user)
			execute(t, mc2, in)
		}
		execute(t, mc2, command("member.kick", room2, second))
		execute(t, oc, command("member.unban", room2, second))
		execute(t, creds[second], command("room.join", room2, ""))
		_, e := lr.Execute(ctx, uc, command("member.kick", room2, mod))
		expect(t, e, apperror.Forbidden)
		in := command("room.join", room1, "")
		in.Hidden = true
		execute(t, mc2, in)
		p, e := lr.Read(ctx, uc, live.Query{Kind: "room", ID: room1, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(p.Data)
		if strings.Contains(string(raw), mod) {
			t.Fatal("hidden public identity leak")
		}
		p, e = lr.Read(ctx, oc, live.Query{Kind: "admin.room", ID: room1, Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ = json.Marshal(p.Data)
		if !strings.Contains(string(raw), mod) {
			t.Fatal("hidden missing operational identity")
		}
		in = command("room.join", room1, "")
		execute(t, mc2, in)
		in = command("seat.self_join", room1, "")
		in.Seat = 1
		execute(t, mc2, in)
		execute(t, mc2, command("seat.release", room1, ""))
		if e = as.Assign(ctx, oc, mod, []string{}, "Revoke synthetic staff", access.ID()); e != nil {
			t.Fatal(e)
		}
		_, e = other.Execute(ctx, mc2, command("member.kick", room2, user))
		expect(t, e, apperror.Forbidden)
		if e = as.Assign(ctx, oc, mod, []string{staffRole}, "Restore synthetic staff", access.ID()); e != nil {
			t.Fatal(e)
		}
		in = command("live.ban", "", user)
		in.Muted = true
		execute(t, mc2, in)
		var active bool
		if e = operator.QueryRow(ctx, "SELECT status='active' FROM app.identity_accounts WHERE id=$1", user).Scan(&active); e != nil || !active {
			t.Fatal("live ban changed account")
		}
		_, e = lr.Execute(ctx, uc, command("room.join", room2, ""))
		expect(t, e, apperror.Forbidden)
		in = command("live.ban", "", user)
		in.Muted = false
		execute(t, mc2, in)
		execute(t, uc, command("room.join", room2, ""))
	})
	t.Run("video call allocation and room-wide seat admission lock", func(t *testing.T) {
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET kind='video',seats=0 WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		execute(t, oc, command("seat.lock", room2, ""))
		_, e = lr.Execute(ctx, mc2, command("seat.self_join", room2, ""))
		expect(t, e, apperror.Conflict)
		execute(t, oc, command("seat.unlock", room2, ""))
		execute(t, mc2, command("seat.self_join", room2, ""))
		var allocated bool
		if e = operator.QueryRow(ctx, "SELECT self_seated AND seat>0 FROM app.live_members WHERE room_id=$1 AND account_id=$2", room2, mod).Scan(&allocated); e != nil || !allocated {
			t.Fatal("video allocation", e)
		}
		execute(t, mc2, command("seat.release", room2, ""))
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET kind='audio',seats=4 WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("private entry revocation and effective participation restrictions", func(t *testing.T) {
		execute(t, mc2, command("room.leave", room2, ""))
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET visibility='private' WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		execute(t, mc2, command("room.join", room2, ""))
		if e = as.Assign(ctx, oc, mod, []string{}, "Synthetic private entry revoke", access.ID()); e != nil {
			t.Fatal(e)
		}
		powers, e := ar.Powers(ctx, mc2, room2)
		if e != nil || powers.Allows("room.enter") {
			t.Fatal("revoked private entry retained", e)
		}
		_, e = other.Read(ctx, mc2, live.Query{Kind: "room", ID: room2, Limit: 50})
		expect(t, e, apperror.Forbidden)
		if _, _, e = lr.RecoverCapabilities(ctx, "", "", nil); e != nil {
			t.Fatal(e)
		}
		var left bool
		if e = operator.QueryRow(ctx, "SELECT state='left' AND NOT private_entry FROM app.live_members WHERE room_id=$1 AND account_id=$2", room2, mod).Scan(&left); e != nil || !left {
			t.Fatal("private privilege recovery", e)
		}
		if e = as.Assign(ctx, oc, mod, []string{staffRole}, "Synthetic private staff restore", access.ID()); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET visibility='public' WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		execute(t, mc2, command("room.join", room2, ""))
	})
	t.Run("direct denies scoped grants dynamic room policy and protected authority", func(t *testing.T) {
		// The preceding global-ban scenario intentionally ended every membership.
		// Restore this scenario's target before testing moderation authority.
		execute(t, uc, command("room.join", room1, ""))
		denied := access.OverrideChange{Action: "room.participant.mute", RoomID: room2, Allowed: false, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic explicit deny"}
		if e = ar.Override(ctx, oc, mod, denied); e != nil {
			t.Fatal(e)
		}
		_, e = other.Execute(ctx, mc2, command("member.mute", room2, user))
		expect(t, e, apperror.Forbidden)
		execute(t, mc2, command("member.mute", room1, user))
		denied.Remove = true
		if e = ar.Override(ctx, oc, mod, denied); e != nil {
			t.Fatal(e)
		}
		titleGrant := access.OverrideChange{Action: "room.title.update", RoomID: room1, Allowed: true, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic title-only authority"}
		if e = ar.Override(ctx, oc, user, titleGrant); e != nil {
			t.Fatal(e)
		}
		title := command("room.settings", room1, "")
		title.TitleSet = true
		title.Name = "Scoped title"
		execute(t, uc, title)
		title.Key = access.ID()
		title.VisibilitySet = true
		title.Visibility = "private"
		_, e = lr.Execute(ctx, uc, title)
		expect(t, e, apperror.Forbidden)
		titleGrant.Remove = true
		if e = ar.Override(ctx, oc, user, titleGrant); e != nil {
			t.Fatal(e)
		}
		grant := access.OverrideChange{Action: "room.participant.mute", RoomID: room1, Allowed: true, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic scoped grant"}
		if e = ar.Override(ctx, oc, user, grant); e != nil {
			t.Fatal(e)
		}
		execute(t, uc, command("member.mute", room1, second))
		_, e = lr.Execute(ctx, uc, command("member.mute", room2, second))
		expect(t, e, apperror.Forbidden)
		_, e = ar.CapabilityPolicy(ctx, uc, nil)
		expect(t, e, apperror.Forbidden)
		legacy := append(access.ModeratorDefaults(), live.RoomModerate)
		if _, e = as.PutRole(ctx, oc, staffRole, "Synthetic moderator "+staffRole, legacy, false, "Synthetic legacy role", access.ID()); e != nil {
			t.Fatal(e)
		}
		legacyDeny := access.OverrideChange{Action: live.RoomModerate, Allowed: false, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic legacy deny"}
		if e = ar.Override(ctx, oc, mod, legacyDeny); e != nil {
			t.Fatal(e)
		}
		_, e = other.Execute(ctx, mc2, command("room.close", room2, ""))
		expect(t, e, apperror.Forbidden)
		legacyDeny.Remove = true
		if e = ar.Override(ctx, oc, mod, legacyDeny); e != nil {
			t.Fatal(e)
		}
		if _, e = as.PutRole(ctx, oc, staffRole, "Synthetic moderator "+staffRole, access.ModeratorDefaults(), false, "Synthetic role restore", access.ID()); e != nil {
			t.Fatal(e)
		}
		roles := access.RoomDefaults()
		roles["audience"] = append(roles["audience"], "room.participant.mute")
		v, e := ar.CapabilityPolicy(ctx, oc, &access.PolicyChange{Previous: originalPolicy, Roles: roles, Reason: "Synthetic dynamic room policy"})
		if e != nil {
			t.Fatal(e)
		}
		version := v.(map[string]any)["policy_id"].(string)
		policyIDs = append(policyIDs, version)
		execute(t, uc, command("member.mute", room2, second))
		_, e = ar.CapabilityPolicy(ctx, oc, &access.PolicyChange{Previous: originalPolicy, Roles: roles, Reason: "Synthetic stale CAS"})
		expect(t, e, apperror.Conflict)
		if e = ar.Override(ctx, oc, owner, grant); e == nil {
			t.Fatal("protected owner override accepted")
		}
		_, e = lr.Execute(ctx, mc2, command("member.kick", room1, owner))
		expect(t, e, apperror.Forbidden)
	})
	save := func(t *testing.T, d packages.Definition) packages.Definition {
		t.Helper()
		id := d.ID
		d.ID = ""
		d.Version = ""
		v, e := repo.Execute(ctx, oc, packages.Command{Kind: "save", ID: id, Definition: &d, Reason: "Synthetic package configuration", Key: access.ID()})
		if e != nil {
			t.Fatal(e, failureClass(e))
		}
		out := v.(packages.Definition)
		if id == "" {
			packageIDs = append(packageIDs, out.ID)
		}
		return out
	}
	makePackage := func(t *testing.T, name string, priority int32) packages.Definition {
		t.Helper()
		return save(t, packages.Definition{Code: strings.ToLower(name) + "_" + access.ID()[:8], Name: name, Price: 100, Duration: 3600, Powers: []string{"benefit.badge", "room.seat.self_join"}, Priority: priority, Active: true})
	}
	celebrity := makePackage(t, "Celebrity", 90)
	king := makePackage(t, "King", 1000)
	queen := makePackage(t, "Queen", 50)
	grantPackage := func(t *testing.T, p, account string) packages.Entitlement {
		t.Helper()
		v, e := repo.Execute(ctx, oc, packages.Command{Kind: "grant", ID: p, Target: account, Key: access.ID(), Reason: "Synthetic direct entitlement"})
		if e != nil {
			t.Fatal(e)
		}
		return v.(packages.Entitlement)
	}
	t.Run("package policy updates expiry revocation and seat race", func(t *testing.T) {
		ent := grantPackage(t, celebrity.ID, user)
		powers, e := ar.Powers(ctx, uc, room2)
		if e != nil || !powers.Allows("room.seat.self_join") {
			t.Fatal("missing package power", e)
		}
		grantPackage(t, queen.ID, second)
		in := command("seat.self_join", room2, "")
		in.Seat = 2
		in2 := in
		in2.Key = access.ID()
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, v := range []struct {
			c  access.Credentials
			in live.Command
		}{{uc, in}, {creds[second], in2}} {
			wg.Go(func() { _, e := lr.Execute(ctx, v.c, v.in); errs <- e })
		}
		wg.Wait()
		close(errs)
		success := 0
		for e := range errs {
			if e == nil {
				success++
			} else if apperror.Public(e).Code() != apperror.Conflict {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("seat race", success)
		}
		execute(t, uc, command("seat.release", room2, ""))
		execute(t, creds[second], command("seat.release", room2, ""))
		deterministic := command("seat.self_join", room2, "")
		deterministic.Seat = 2
		execute(t, uc, deterministic)
		updated := celebrity
		updated.Previous = celebrity.Version
		updated.Powers = []string{"benefit.badge"}
		celebrity = save(t, updated)
		powers, e = ar.Powers(ctx, uc, room2)
		if e != nil || powers.Allows("room.seat.self_join") {
			t.Fatal("existing subscriber retained removed power", e)
		}
		_, e = repo.Execute(ctx, oc, packages.Command{Kind: "revoke", ID: ent.ID, Key: access.ID(), Reason: "Synthetic revoke"})
		if e != nil {
			t.Fatal(e)
		}
		powers, e = ar.Powers(ctx, uc, room2)
		if e != nil || powers.Allows("benefit.badge") {
			t.Fatal("revoked package power", e)
		}
		expired := grantPackage(t, king.ID, user)
		if _, e = operator.Exec(ctx, "UPDATE app.package_entitlements SET starts_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", expired.ID); e != nil {
			t.Fatal(e)
		}
		powers, e = ar.Powers(ctx, uc, room2)
		if e != nil || powers.Allows("benefit.badge") {
			t.Fatal("expired package power", e)
		}
		for _, danger := range []string{"live.participant.global_ban", "wallet.adjust", "users.restrict", "room.participant.kick"} {
			bad := celebrity
			bad.Previous = celebrity.Version
			bad.Powers = []string{danger}
			_, e = repo.Execute(ctx, oc, packages.Command{Kind: "save", ID: celebrity.ID, Definition: &bad, Key: access.ID(), Reason: "Synthetic forbidden power"})
			expect(t, e, apperror.Forbidden)
		}
	})
	t.Run("capability expiry recovery and fenced provider leases", func(t *testing.T) {
		if _, _, e = lr.RecoverCapabilities(ctx, "", "", nil); e != nil {
			t.Fatal(e)
		}
		q := livesql.New(pool)
		member, e := q.Member(ctx, livesql.MemberParams{RoomID: room2, AccountID: user})
		if e != nil || member.Seat.Valid || member.SelfSeated {
			t.Fatal("expired capability retained seat", e)
		}
		in := command("room.join", room1, "")
		in.Hidden = true
		execute(t, mc2, in)
		if e = as.Assign(ctx, oc, mod, []string{}, "Synthetic hidden privilege revoke", access.ID()); e != nil {
			t.Fatal(e)
		}
		if _, _, e = lr.RecoverCapabilities(ctx, "", "", nil); e != nil {
			t.Fatal(e)
		}
		member, e = q.Member(ctx, livesql.MemberParams{RoomID: room1, AccountID: mod})
		if e != nil || member.State != "left" || member.Hidden {
			t.Fatal("revoked hidden member remained", e)
		}
		if e = as.Assign(ctx, oc, mod, []string{staffRole}, "Synthetic staff restore", access.ID()); e != nil {
			t.Fatal(e)
		}
		old, e := q.Claim(ctx, livesql.ClaimParams{RoomID: room1, LeaseToken: access.ID()})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_operations SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1 AND room_id=$2", old.ID, room1); e != nil {
			t.Fatal(e)
		}
		current, e := q.Claim(ctx, livesql.ClaimParams{RoomID: room1, LeaseToken: access.ID()})
		if e != nil || current.ID != old.ID {
			t.Fatal("expired lease not reclaimed", e)
		}
		if _, e = q.Complete(ctx, livesql.CompleteParams{ID: old.ID, LeaseToken: old.LeaseToken}); !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal("stale lease completed current work", e)
		}
		if e = q.Retry(ctx, livesql.RetryParams{ID: old.ID, LeaseToken: old.LeaseToken}); e != nil {
			t.Fatal(e)
		}
		var fenced bool
		if e = operator.QueryRow(ctx, "SELECT state='running' AND lease_token=$2 FROM app.live_operations WHERE id=$1", current.ID, current.LeaseToken).Scan(&fenced); e != nil || !fenced {
			t.Fatal("stale retry stole lease", e)
		}
		if _, e = q.Complete(ctx, livesql.CompleteParams{ID: current.ID, LeaseToken: current.LeaseToken}); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("Wallet purchase atomic idempotency profit and concurrent spend", func(t *testing.T) {
		_, e = wr.Execute(ctx, oc, wallet.Command{Kind: "adjust", Target: user, Amount: 150, Direction: "credit", Key: access.ID(), Reason: "Synthetic package funds", Correlation: access.ID()})
		if e != nil {
			t.Fatal(e)
		}
		denyPurchase := access.OverrideChange{Action: "packages.purchase", Allowed: false, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic purchase denial"}
		if e = ar.Override(ctx, oc, user, denyPurchase); e != nil {
			t.Fatal(e)
		}
		_, e = repo.Purchase(ctx, uc, celebrity.ID, access.ID())
		expect(t, e, apperror.Forbidden)
		denyPurchase.Remove = true
		if e = ar.Override(ctx, oc, user, denyPurchase); e != nil {
			t.Fatal(e)
		}
		key := access.ID()
		var wg sync.WaitGroup
		answers := make(chan packages.Purchase, 2)
		errs := make(chan error, 2)
		for range 2 {
			wg.Go(func() { v, e := repo.Purchase(ctx, uc, celebrity.ID, key); answers <- v; errs <- e })
		}
		wg.Wait()
		close(answers)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		op := ""
		for v := range answers {
			if op != "" && op != v.OperationID {
				t.Fatal("duplicate purchase")
			}
			op = v.OperationID
			if v.Price != 100 || v.FinancialStatus != "committed" {
				t.Fatal("purchase snapshot")
			}
		}
		_, e = repo.Purchase(ctx, uc, king.ID, key)
		expect(t, e, apperror.Conflict)
		_, e = repo.Purchase(ctx, uc, king.ID, access.ID())
		expect(t, e, apperror.InsufficientDiamonds)
		if _, e = wr.Execute(ctx, oc, wallet.Command{Kind: "adjust", Target: user, Amount: 100, Direction: "credit", Key: access.ID(), Reason: "Synthetic renewal funds", Correlation: access.ID()}); e != nil {
			t.Fatal(e)
		}
		original, e := repo.Purchase(ctx, uc, celebrity.ID, key)
		if e != nil {
			t.Fatal(e)
		}
		renewalKey := access.ID()
		renewal, e := repo.Purchase(ctx, uc, celebrity.ID, renewalKey)
		if e != nil {
			t.Fatal(e, failureClass(e))
		}
		if !renewal.StartsAt.Equal(original.ExpiresAt) || renewal.ExpiresAt.Sub(renewal.StartsAt) != time.Hour || !renewal.StartsAt.After(renewal.CreatedAt) {
			t.Fatal("renewal did not append an immutable paid period")
		}
		retry, e := repo.Purchase(ctx, uc, celebrity.ID, renewalKey)
		if e != nil || retry.OperationID != renewal.OperationID || retry.EntitlementID != renewal.EntitlementID {
			t.Fatal("renewal duplicate debit", e)
		}
		var n int
		var profit int64
		if e = operator.QueryRow(ctx, "SELECT count(*),COALESCE(sum(diamonds),0)::bigint FROM app.package_profit WHERE operation_id=$1", op).Scan(&n, &profit); e != nil || n != 1 || profit != 100 {
			t.Fatal("profit journal", e)
		}
		var balance int64
		if e = operator.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id='user:'||$1", user).Scan(&balance); e != nil || balance != 50 {
			t.Fatal("purchase balance", e)
		}
		disabled := celebrity
		disabled.Previous = celebrity.Version
		disabled.Active = false
		celebrity = save(t, disabled)
		prior, e := repo.Purchase(ctx, uc, celebrity.ID, key)
		if e != nil || prior.OperationID != op {
			t.Fatal("committed retry after deactivation", e)
		}
		_, e = repo.Purchase(ctx, uc, celebrity.ID, access.ID())
		expect(t, e, apperror.FinancialConfigurationRequired)
		restored := celebrity
		restored.Previous = celebrity.Version
		restored.Active = true
		celebrity = save(t, restored)
		var canBypass bool
		if e = operator.QueryRow(ctx, "SELECT has_function_privilege('biho_live_test_app','app.wallet_post_existing(text,text[],bigint[])','EXECUTE')").Scan(&canBypass); e != nil || canBypass {
			t.Fatal("runtime can bypass Wallet dispatcher", e)
		}
		var diff int
		if e = operator.QueryRow(ctx, "SELECT count(*) FROM(SELECT a.id FROM app.wallet_accounts a LEFT JOIN app.wallet_entries e ON e.account_id=a.id GROUP BY a.id HAVING a.balance::numeric<>COALESCE(sum(e.delta::numeric),0)) differences").Scan(&diff); e != nil || diff != 0 {
			t.Fatal("ledger reconciliation", e)
		}
	})
	t.Run("scoped audit failure rolls back package version", func(t *testing.T) {
		fn := pgx.Identifier{"app", "test_package_audit_" + owner[:16]}.Sanitize()
		trigger := pgx.Identifier{"test_package_audit_" + owner[:16]}.Sanitize()
		if _, e = operator.Exec(ctx, "CREATE FUNCTION "+fn+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit rejection'; END $$"); e != nil {
			t.Fatal(e)
		}
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
			defer done()
			if _, e := operator.Exec(cleanup, "DROP TRIGGER IF EXISTS "+trigger+" ON app.access_audit"); e != nil {
				t.Error("audit trigger cleanup")
			}
			if _, e := operator.Exec(cleanup, "DROP FUNCTION "+fn+"()"); e != nil {
				t.Error("audit function cleanup")
			}
		}()
		if _, e = operator.Exec(ctx, "CREATE TRIGGER "+trigger+" BEFORE INSERT ON app.access_audit FOR EACH ROW WHEN(NEW.actor='"+owner+"' AND NEW.reason='Synthetic audit failure') EXECUTE FUNCTION "+fn+"()"); e != nil {
			t.Fatal(e)
		}
		changed := queen
		changed.Previous = queen.Version
		changed.Priority = 999
		_, e = repo.Execute(ctx, oc, packages.Command{Kind: "save", ID: queen.ID, Definition: &changed, Key: access.ID(), Reason: "Synthetic audit failure"})
		expect(t, e, apperror.ServiceUnavailable)
		var version string
		if e = operator.QueryRow(ctx, "SELECT current_version FROM app.package_catalog WHERE id=$1", queen.ID).Scan(&version); e != nil || version != queen.Version {
			t.Fatal("audit failure committed catalog version", e)
		}
	})
	t.Run("host package discovery priority filters snapshots and stable pages", func(t *testing.T) {
		hostBoost := grantPackage(t, celebrity.ID, host2)
		baseBoost := grantPackage(t, queen.ID, host1)
		grantPackage(t, king.ID, second)
		p, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		items := p.Data.([]live.RankedRoom)
		if len(items) != 1 || items[0].ID != room2 || items[0].Priority != 90 || !p.More {
			t.Fatal("host priority", items)
		}
		grantPackage(t, queen.ID, second) // An audience subscription must not invalidate host ordering.
		p2, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Cursor: p.Next, Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		items = p2.Data.([]live.RankedRoom)
		if len(items) != 1 || items[0].ID != room1 || items[0].Priority != 50 || p2.More {
			t.Fatal("stable page", items)
		}
		extra := grantPackage(t, king.ID, host1)
		boosted, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		boostedItems := boosted.Data.([]live.RankedRoom)
		if len(boostedItems) != 2 || boostedItems[0].ID != room1 || boostedItems[0].Priority != 1000 {
			t.Fatal("highest package priority not selected")
		}
		if _, e = repo.Execute(ctx, oc, packages.Command{Kind: "revoke", ID: extra.ID, Key: access.ID(), Reason: "Synthetic ranking boost revoke"}); e != nil {
			t.Fatal(e)
		}
		update := queen
		update.Previous = queen.Version
		update.Priority = 90
		queen = save(t, update)
		_, e = lr.Read(ctx, uc, live.Query{Kind: "rooms", Cursor: p.Next, Limit: 1})
		expect(t, e, apperror.LiveListRestartRequired)
		p, e = lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		items = p.Data.([]live.RankedRoom)
		if len(items) != 2 || items[0].ID > items[1].ID {
			t.Fatal("equal priority stable IDs")
		}
		if _, e = operator.Exec(ctx, "UPDATE app.package_entitlements SET starts_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", hostBoost.ID); e != nil {
			t.Fatal(e)
		}
		expiredList, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range expiredList.Data.([]live.RankedRoom) {
			if v.ID == room2 && v.Priority != 0 {
				t.Fatal("expired host package boosted room")
			}
		}
		if _, e = repo.Execute(ctx, oc, packages.Command{Kind: "revoke", ID: baseBoost.ID, Key: access.ID(), Reason: "Synthetic base boost revoke"}); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET visibility='private' WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		p, e = lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil || len(p.Data.([]live.RankedRoom)) != 1 {
			t.Fatal("private ranking leak", e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET visibility='hidden' WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		hiddenList, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil || len(hiddenList.Data.([]live.RankedRoom)) != 1 {
			t.Fatal("hidden room listing leak", e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='restricted' WHERE id=$1", host1); e != nil {
			t.Fatal(e)
		}
		p, e = lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil || len(p.Data.([]live.RankedRoom)) != 0 {
			t.Fatal("restricted host ranking leak", e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='active' WHERE id=$1", host1); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET visibility='public',state='closed' WHERE id=$1", room2); e != nil {
			t.Fatal(e)
		}
		closedList, e := lr.Read(ctx, uc, live.Query{Kind: "rooms", Limit: 50})
		if e != nil || len(closedList.Data.([]live.RankedRoom)) != 1 {
			t.Fatal("closed room listing leak", e)
		}

	})
	t.Log("Synthetic global moderation, current capabilities, purchases/profit, seat races and ranking exercised; real providers/client behavior NOT TESTED")
}

func failureClass(e error) string {
	if errors.Is(e, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	if errors.Is(e, context.Canceled) {
		return "CANCELED"
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) {
		return "SQLSTATE_" + pg.Code
	}
	return "UNCLASSIFIED"
}
