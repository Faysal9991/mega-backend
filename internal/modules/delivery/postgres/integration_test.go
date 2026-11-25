//go:build integration

package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/deliverysql"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres/intents"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	walletpg "github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
)

type memoryObject struct {
	data []byte
	mime string
}
type memoryStore struct {
	mu       sync.Mutex
	objects  map[string]memoryObject
	fail     bool
	reads    int
	onDelete func(string)
}

func (s *memoryStore) Presign(_ context.Context, key, mime string, size int64, sum string, ttl time.Duration) (string, http.Header, error) {
	return "https://upload.example.invalid/" + key, http.Header{"Content-Type": {mime}}, nil
}
func (s *memoryStore) Head(_ context.Context, key string) (delivery.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return delivery.Object{}, errors.New("synthetic outage")
	}
	v, ok := s.objects[key]
	if !ok {
		return delivery.Object{}, errors.New("missing synthetic object")
	}
	return delivery.Object{Size: int64(len(v.data)), ContentType: v.mime, ETag: delivery.Hash(string(v.data))}, nil
}
func (s *memoryStore) Read(_ context.Context, key, etag string, max int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	v := s.objects[key]
	if delivery.Hash(string(v.data)) != etag || int64(len(v.data)) > max {
		return nil, errors.New("object changed or exceeded bound")
	}
	return bytes.Clone(v.data), nil
}
func (s *memoryStore) Put(_ context.Context, key string, b []byte, mime string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("synthetic outage")
	}
	s.objects[key] = memoryObject{bytes.Clone(b), mime}
	return nil
}
func (s *memoryStore) Delete(_ context.Context, key string) error {
	if s.onDelete != nil {
		s.onDelete(key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

type roomSink struct {
	mu   sync.Mutex
	fail bool
	ids  []string
}

func (s *roomSink) SendGift(_ context.Context, _ string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var event struct {
		ID string `json:"event_id"`
	}
	if e := json.Unmarshal(b, &event); e != nil {
		return e
	}
	s.ids = append(s.ids, event.ID)
	if s.fail {
		return errors.New("synthetic provider failure")
	}
	return nil
}

type pushSink struct {
	calls   atomic.Int32
	invalid bool
	err     error
}

func (s *pushSink) Send(_ context.Context, p delivery.Push) error {
	s.calls.Add(1)
	if s.err != nil {
		return s.err
	}
	if s.invalid {
		return delivery.ErrInvalidToken
	}
	return nil
}
func settings(t *testing.T, prefix, role string) config.DatabaseConfig {
	t.Helper()
	if os.Getenv("BIHO_INTEGRATION_ACK") != "biho_live_test" {
		t.Fatal("isolated acknowledgment required")
	}
	c, e := config.LoadDatabase(func(k string) (string, bool) { return os.LookupEnv(prefix + k) }, config.Test)
	if e != nil || c.Name != "biho_live_test" || c.User != role {
		t.Fatal("isolated role/name guard")
	}
	c.MaxConns = 4
	return c
}
func decode[T any](t *testing.T, v any) T {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var result T
	if e = json.Unmarshal(b, &result); e != nil {
		t.Fatal(e)
	}
	return result
}
func expect(t *testing.T, e error, code apperror.Code) {
	t.Helper()
	if e == nil || apperror.Public(e).Code() != code {
		t.Fatalf("expected %s, got %v", code, e)
	}
}
func TestIntegrationDeliveryBatch(t *testing.T) {
	app, mc := settings(t, "TEST_", "biho_live_test_app"), settings(t, "TEST_MIGRATION_", "biho_live_test_migrator")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cc, e := platform.ConnectionConfig(mc)
	if e != nil {
		t.Fatal(e)
	}
	operator, e := pgx.ConnectConfig(ctx, cc)
	if e != nil {
		t.Fatal("isolated operator connection")
	}
	defer operator.Close(context.Background())
	var db, role string
	if e = operator.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != "biho_live_test" || role != "biho_live_test_migrator" {
		t.Fatal("authenticated target guard")
	}
	var owners int
	if e = operator.QueryRow(ctx, "SELECT count(*) FROM app.access_owners").Scan(&owners); e != nil || owners != 0 {
		t.Fatal("never replace existing protected authority")
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
	nonce := access.ID()
	ids := []string{}
	installs := []string{}
	giftIDs := []string{}
	recipientPolicyID := ""
	announcementIDs := []string{}
	roomID, sessionID, assocID := access.ID(), access.ID(), access.ID()
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 90*time.Second)
		defer done()
		tx, e := operator.Begin(cleanup)
		if e != nil {
			t.Error("fixture cleanup begin")
			return
		}
		defer tx.Rollback(cleanup)
		var foreign int
		if e = tx.QueryRow(cleanup, "SELECT count(*) FROM app.wallet_operations WHERE actor_id=ANY($1::text[]) AND NOT(target_id=ANY($1::text[])) OR target_id=ANY($1::text[]) AND NOT(actor_id=ANY($1::text[]))", ids).Scan(&foreign); e != nil || foreign != 0 {
			t.Error("refuse cleanup with foreign financial counterparties")
			return
		}
		statements := []string{
			"DELETE FROM app.delivery_outbox WHERE topic='gift' AND id IN (SELECT operation_id FROM app.room_gifts WHERE sender_id=ANY($1::text[])) OR topic='push' AND payload->>'notification_id' IN (SELECT id FROM app.notifications WHERE account_id=ANY($1::text[])) OR topic='media_gc' AND payload->>'asset_id' IN (SELECT id FROM app.delivery_assets WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.room_gifts WHERE sender_id=ANY($1::text[])",
			"UPDATE app.wallet_accounts a SET balance=(a.balance::numeric-d.delta)::bigint FROM (SELECT e.account_id,sum(e.delta::numeric) delta FROM app.wallet_entries e JOIN app.wallet_operations o ON o.id=e.operation_id WHERE o.actor_id=ANY($1::text[]) GROUP BY e.account_id) d WHERE a.id=d.account_id",
			"DELETE FROM app.wallet_entries WHERE operation_id IN (SELECT id FROM app.wallet_operations WHERE actor_id=ANY($1::text[]))",
			"DELETE FROM app.wallet_operations WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.economy_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.delivery_commands WHERE actor_id=ANY($1::text[])",
			"DELETE FROM app.delivery_asset_links WHERE target_id=ANY($1::text[])",
			"DELETE FROM app.push_devices WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.notifications WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.notification_preferences WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_events WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.live_presence WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_operations WHERE room_id IN (SELECT id FROM app.live_rooms WHERE owner_id=ANY($1::text[]))",
			"DELETE FROM app.live_members WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_rooms WHERE owner_id=ANY($1::text[])",
			"DELETE FROM app.agency_activity_contexts WHERE account_id=ANY($1::text[]) OR agency_id=ANY($1::text[])",
			"DELETE FROM app.agency_host_associations WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.agencies WHERE id=ANY($1::text[])",
			"DELETE FROM app.access_audit WHERE actor=ANY($1::text[])",
			"DELETE FROM app.access_assignments WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.access_owners WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_refresh_tokens WHERE session_id IN(SELECT id FROM app.identity_sessions WHERE account_id=ANY($1::text[]))",
			"DELETE FROM app.identity_sessions WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.identity_google WHERE account_id=ANY($1::text[])",
			"DELETE FROM app.live_account_restrictions WHERE account_id=ANY($1::text[])",
		}
		for _, sql := range statements {
			if _, e = tx.Exec(cleanup, sql, ids); e != nil {
				t.Error("tracked fixture cleanup", e)
				return
			}
		}
		for _, id := range giftIDs {
			if _, e = tx.Exec(cleanup, "UPDATE app.gift_catalog SET current_version=NULL WHERE id=$1", id); e != nil {
				t.Error(e)
				return
			}
			if _, e = tx.Exec(cleanup, "DELETE FROM app.gift_versions WHERE gift_id=$1", id); e != nil {
				t.Error(e)
				return
			}
			if _, e = tx.Exec(cleanup, "DELETE FROM app.gift_catalog WHERE id=$1", id); e != nil {
				t.Error(e)
				return
			}
		}
		for _, sql := range []string{"DELETE FROM app.delivery_assets WHERE owner_id=ANY($1::text[])", "DELETE FROM app.economy_policies WHERE actor_id=ANY($1::text[])", "DELETE FROM app.wallet_accounts WHERE owner_id=ANY($1::text[])", "DELETE FROM app.profiles WHERE account_id=ANY($1::text[])", "DELETE FROM app.identity_accounts WHERE id=ANY($1::text[])"} {
			if _, e = tx.Exec(cleanup, sql, ids); e != nil {
				t.Error("tracked final fixture cleanup", e)
				return
			}
		}
		for _, id := range installs {
			if _, e = tx.Exec(cleanup, "DELETE FROM app.identity_installations WHERE id=$1", id); e != nil {
				t.Error(e)
				return
			}
		}
		for _, id := range ids {
			if _, e = tx.Exec(cleanup, "DELETE FROM app.identity_rate_limits WHERE scope_hash=$1", identity.Digest("profiles-access:"+id)); e != nil {
				t.Error(e)
				return
			}
		}
		// Only this schema-isolated test creates recipient policy versions/announcements.
		if _, e = tx.Exec(cleanup, "DELETE FROM app.gift_recipient_policies WHERE id=$1", recipientPolicyID); e != nil {
			t.Error(e)
			return
		}
		for _, id := range announcementIDs {
			if _, e = tx.Exec(cleanup, "DELETE FROM app.delivery_announcements WHERE id=$1", id); e != nil {
				t.Error(e)
				return
			}
		}
		if e = tx.Commit(cleanup); e != nil {
			t.Error("fixture cleanup commit", e)
		}
	}()
	newAccount := func() (string, access.Credentials) {
		t.Helper()
		id := access.ID()
		ids = append(ids, id)
		installs = append(installs, id)
		cred := access.Credentials{Access: strings.Repeat("x", 11) + id, Installation: id}
		for _, sql := range []string{"INSERT INTO app.identity_accounts(id) VALUES($1)", "INSERT INTO app.profiles(account_id,display_name,avatar_url) VALUES($1,'Synthetic delivery fixture','')", "INSERT INTO app.identity_google(account_id,subject) VALUES($1,$1)", "INSERT INTO app.identity_installations(id) VALUES($1)"} {
			if _, e = operator.Exec(ctx, sql, id); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$1,$1,$2,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", id, identity.Digest(cred.Access)); e != nil {
			t.Fatal(e)
		}
		return id, cred
	}
	owner, oc := newAccount()
	sender, sc := newAccount()
	recipient, rc := newAccount()
	outsider, xc := newAccount()
	_ = xc
	bootstrap, e := operator.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = accesspg.Bootstrap(ctx, bootstrap, owner, "Synthetic delivery fixture", "delivery-test"); e != nil {
		_ = bootstrap.Rollback(ctx)
		t.Fatal(e)
	}
	if e = bootstrap.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	limits := live.Limits{DatabaseTimeout: 30 * time.Second, Capacity: 20, Seats: 5, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: 24 * time.Hour, Compatibility: live.Compatibility{AudioHostAudio: true}}
	liveRepo, e := livepg.New(pool, limits)
	if e != nil {
		t.Fatal(e)
	}
	store := &memoryStore{objects: map[string]memoryObject{}}
	cfg := config.DeliveryConfig{MediaEnabled: true, Prefix: "test-" + nonce, PublicURL: "https://media.example.invalid", Timeout: time.Second, DatabaseTimeout: 30 * time.Second, TokenKey: config.Secret(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))}
	repo, e := New(pool, liveRepo, store, cfg)
	if e != nil {
		t.Fatal(e)
	}
	repo2, e := New(other, liveRepo, store, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = repo.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	wp, e := walletpg.New(pool, nil)
	if e != nil {
		t.Fatal(e)
	}
	wp.WithRoomGifts(repo)
	wp2, e := walletpg.New(other, nil)
	if e != nil {
		t.Fatal(e)
	}
	wp2.WithRoomGifts(repo2)
	engine, second := wallet.New(wp), wallet.New(wp2)
	q := deliverysql.New(pool)
	exec := func(tb *testing.T, c access.Credentials, cmd wallet.Command) any {
		tb.Helper()
		out, e := engine.Execute(ctx, c, cmd)
		if e != nil {
			tb.Fatalf("wallet %s: %v", cmd.Kind, e)
		}
		return out
	}
	cmd := func(kind string) wallet.Command {
		return wallet.Command{Kind: kind, Key: "delivery:" + access.ID(), Correlation: "delivery-fixture", Reason: "Synthetic delivery fixture", Reference: "synthetic-issuance"}
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.agencies(id,name,status) VALUES($1,'Synthetic delivery agency','active')", owner); e != nil {
		t.Fatal(e)
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.agency_host_associations(id,account_id,agency_id,classification,effective_from) VALUES($1,$2,$3,'audio_host',clock_timestamp()-interval '1 hour')", assocID, recipient, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.live_rooms(id,session_id,owner_id,kind,title,state,capacity,seats,provider_sid) VALUES($1,$2,$3,'audio','Synthetic delivery room','open',10,3,'RM_delivery')", roomID, sessionID, recipient); e != nil {
		t.Fatal(e)
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.live_members(room_id,account_id,role,state) VALUES($1,$2,'host','active'),($1,$3,'audience','active'),($1,$4,'audience','active')", roomID, recipient, sender, outsider); e != nil {
		t.Fatal(e)
	}
	if _, e = operator.Exec(ctx, "INSERT INTO app.live_presence(participant_sid,room_id,account_id,association_id,classification,started_at,qualifying) VALUES($1,$3,$4,$6,'audio_host',clock_timestamp(),true),($2,$3,$5,NULL,'',clock_timestamp(),false)", "PA_delivery_"+recipient, "PA_delivery_"+sender, roomID, recipient, sender, assocID); e != nil {
		t.Fatal(e)
	}
	var catalog delivery.Gift
	if !t.Run("catalog authority policy gates and encrypted binding", func(t *testing.T) {
		change := GiftChange{Gift: delivery.Gift{Code: "rose_" + nonce[:12], Name: "Rose", Description: "Synthetic gift", Price: 100, Category: "standard", Active: true}, Reason: "Synthetic gift catalog"}
		_, e := repo.PutGift(ctx, sc, "", "catalog-forbidden", change)
		expect(t, e, apperror.Forbidden)
		out, e := repo.PutGift(ctx, oc, "", "catalog-"+nonce, change)
		if e != nil {
			t.Fatal(e)
		}
		catalog = decode[delivery.Gift](t, out)
		giftIDs = append(giftIDs, catalog.ID)
		failing := *repo
		failing.guard = auditFailureGuard{repo.guard}
		changed := change
		changed.Gift = catalog
		changed.Gift.Name = "must rollback"
		changed.PreviousID = catalog.VersionID
		_, e = failing.PutGift(ctx, oc, catalog.ID, access.ID(), changed)
		expect(t, e, apperror.ServiceUnavailable)
		stored, e := q.Gift(ctx, catalog.ID)
		if e != nil || stored.VersionID != catalog.VersionID {
			t.Fatal("audit failure committed catalog mutation", e)
		}

		if _, e = repo.Preferences(ctx, rc, &delivery.Preferences{GiftReceived: true, AgencyDecision: true}); e != nil {
			t.Fatal(e)
		}
		token := "synthetic-fcm-token-" + nonce
		if e = repo.Register(ctx, rc, "android", token); e != nil {
			t.Fatal(e)
		}
		if e = repo.Register(ctx, rc, "android", token); e != nil {
			t.Fatal(e)
		}
		var encrypted []byte
		if e = operator.QueryRow(ctx, "SELECT token_cipher FROM app.push_devices WHERE installation_id=$1", rc.Installation).Scan(&encrypted); e != nil || bytes.Contains(encrypted, []byte(token)) {
			t.Fatal("unencrypted push token", e)
		}
		gift := cmd("gift")
		gift.Target = recipient
		gift.GiftID = catalog.ID
		gift.Quantity = 1
		gift.RoomID = roomID
		gift.SessionID = sessionID
		_, e = engine.Execute(ctx, sc, gift)
		expect(t, e, wallet.ConfigurationRequired)
		policy := cmd("policy")
		policy.Policy = &wallet.Policy{Kind: "gift", Scope: "agency", EffectiveAt: time.Now().Add(-time.Hour), Enabled: true, TaxJurisdiction: "synthetic", VAT: "inclusive_diamonds", Rounding: "largest_remainder", HostBPS: 6000, AgencyBPS: 2000, PlatformBPS: 1000, TaxBPS: 1000, Catalog: map[string]int64{"legacy_only": 1}}
		exec(t, oc, policy)
		_, e = engine.Execute(ctx, sc, gift)
		expect(t, e, wallet.ConfigurationRequired)
		out, e = repo.RecipientPolicy(ctx, oc, "recipient-"+nonce, "", "Synthetic recipient policy")
		if e != nil {
			t.Fatal(e)
		}
		recipientPolicyID = decode[map[string]string](t, out)["policy_id"]
		fund := cmd("adjust")
		fund.Target = sender
		fund.Amount = 10000
		fund.Direction = "credit"
		exec(t, oc, fund)
	}) {
		return
	}
	sink := &roomSink{fail: true}
	worker := &Dispatcher{Repo: repo, Rooms: sink, Push: &pushSink{}, Workers: 2}
	var first wallet.Detail
	var firstCmd wallet.Command
	t.Run("atomic committed gift retry and provider failure", func(t *testing.T) {
		firstCmd = cmd("gift")
		firstCmd.Target = recipient
		firstCmd.GiftID = catalog.ID
		firstCmd.Quantity = 2
		firstCmd.RoomID = roomID
		firstCmd.SessionID = sessionID
		first = decode[wallet.Detail](t, exec(t, sc, firstCmd))
		if first.Operation.Amount != 200 {
			t.Fatal("catalog price ignored")
		}
		before, e := walletsql.New(pool).Reconcile(ctx)
		if e != nil || len(before) != 0 {
			t.Fatal("ledger reconciliation", e)
		}
		job, e := q.Delivery(ctx, first.Operation.ID)
		if e != nil || job.State != "pending" {
			t.Fatal("gift outbox not atomic", e)
		}
		worked, e := worker.Step(ctx, []string{"gift"})
		if e != nil || !worked {
			t.Fatal("provider failure dispatch", e)
		}
		job, e = q.Delivery(ctx, first.Operation.ID)
		if e != nil || job.State != "pending" || job.Attempts != 1 {
			t.Fatal("transient retry state", e)
		}
		prior := decode[wallet.Detail](t, exec(t, sc, firstCmd))
		if prior.Operation.ID != first.Operation.ID {
			t.Fatal("duplicate debit")
		}
		changed := firstCmd
		changed.Quantity = 3
		_, e = engine.Execute(ctx, sc, changed)
		expect(t, e, apperror.Conflict)
		if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET available_at=clock_timestamp() WHERE id=$1", first.Operation.ID); e != nil {
			t.Fatal(e)
		}
		sink.fail = false
		if _, e = worker.Step(ctx, []string{"gift"}); e != nil {
			t.Fatal(e)
		}
		job, e = q.Delivery(ctx, first.Operation.ID)
		if e != nil || job.State != "delivered" {
			t.Fatal("provider recovery", e)
		}
		var balance int64
		if e = operator.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id=$1", "user:"+sender).Scan(&balance); e != nil || balance != 9800 {
			t.Fatal("provider replay changed money", e)
		}
	})
	t.Run("recipient room restriction and overflow denial", func(t *testing.T) {
		in := firstCmd
		in.Key = access.ID()
		in.Target = outsider
		_, e := engine.Execute(ctx, sc, in)
		expect(t, e, apperror.Forbidden)
		in.Target = recipient
		in.Quantity = 1 << 62
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, apperror.ValidationFailed)
		in.Quantity = 1000
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, wallet.InsufficientFunds)
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET state='closed' WHERE id=$1", roomID); e != nil {
			t.Fatal(e)
		}
		in.Quantity = 1
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, apperror.Conflict)
		retry := decode[wallet.Detail](t, exec(t, sc, firstCmd))
		if retry.Operation.ID != first.Operation.ID {
			t.Fatal("committed retry lost after closure")
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET state='open' WHERE id=$1", roomID); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='restricted' WHERE id=$1", recipient); e != nil {
			t.Fatal(e)
		}
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, apperror.Forbidden)
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='active' WHERE id=$1", recipient); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='restricted' WHERE id=$1", sender); e != nil {
			t.Fatal(e)
		}
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, apperror.Unauthenticated)
		if _, e = operator.Exec(ctx, "UPDATE app.identity_accounts SET status='active' WHERE id=$1", sender); e != nil {
			t.Fatal(e)
		}

	})
	t.Run("concurrent duplicate gifts and fenced recovery", func(t *testing.T) {
		in := firstCmd
		in.Key = access.ID()
		in.Quantity = 1
		results := make(chan any, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, service := range []*wallet.Service{engine, second} {
			wg.Go(func() { v, e := service.Execute(ctx, sc, in); results <- v; errs <- e })
		}
		wg.Wait()
		for range 2 {
			if e := <-errs; e != nil {
				t.Fatal(e)
			}
		}
		a, b := decode[wallet.Detail](t, <-results), decode[wallet.Detail](t, <-results)
		if a.Operation.ID != b.Operation.ID {
			t.Fatal("concurrent duplicate debit")
		}
		old, e := q.Claim(ctx, deliverysql.ClaimParams{LeaseID: access.ID(), Column2: []string{"gift"}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", old.ID); e != nil {
			t.Fatal(e)
		}
		fresh, e := deliverysql.New(other).Claim(ctx, deliverysql.ClaimParams{LeaseID: access.ID(), Column2: []string{"gift"}})
		if e != nil || fresh.ID != old.ID {
			t.Fatal("expired lease recovery", e)
		}
		n, e := q.Ack(ctx, deliverysql.AckParams{ID: old.ID, LeaseID: old.LeaseID, State: "delivered"})
		if e != nil || n != 0 {
			t.Fatal("stale lease acknowledgment accepted", e)
		}
		state, code, e := worker.deliver(ctx, fresh)
		if e != nil {
			t.Fatal(e)
		}
		n, e = q.Ack(ctx, deliverysql.AckParams{ID: fresh.ID, LeaseID: fresh.LeaseID, State: state, ErrorCode: code})
		if e != nil || n != 1 {
			t.Fatal("fresh lease acknowledgment", e)
		}
		for range 2 {
			unique := in
			unique.Key = access.ID()
			exec(t, sc, unique)
		}
		for _, r := range []*Repository{repo, repo2} {
			wg.Go(func() {
				d := &Dispatcher{Repo: r, Rooms: sink, Workers: 1}
				_, e := d.Step(ctx, []string{"gift"})
				errs <- e
			})
		}
		wg.Wait()
		for range 2 {
			if e := <-errs; e != nil {
				t.Fatal(e)
			}
		}
	})
	t.Run("closed-room delivery and audited explicit replay", func(t *testing.T) {
		in := firstCmd
		in.Key = access.ID()
		in.Quantity = 1
		value := decode[wallet.Detail](t, exec(t, sc, in))
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET state='closed' WHERE id=$1", roomID); e != nil {
			t.Fatal(e)
		}
		if _, e = worker.Step(ctx, []string{"gift"}); e != nil {
			t.Fatal(e)
		}
		job, e := q.Delivery(ctx, value.Operation.ID)
		if e != nil || job.State != "skipped" || job.ErrorCode != "ROOM_CLOSED" {
			t.Fatal("closed room delivery status", e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_rooms SET state='open' WHERE id=$1", roomID); e != nil {
			t.Fatal(e)
		}
		pending := firstCmd
		pending.Key = access.ID()
		pending.Quantity = 1
		queued := decode[wallet.Detail](t, exec(t, sc, pending))
		providerOp := access.ID()
		if _, e = operator.Exec(ctx, "INSERT INTO app.live_operations(id,room_id,kind,target) VALUES($1,$2,'participant',$3)", providerOp, roomID, outsider); e != nil {
			t.Fatal(e)
		}
		calls := len(sink.ids)
		if _, e = worker.Step(ctx, []string{"gift"}); e != nil {
			t.Fatal(e)
		}
		blocked, e := q.Delivery(ctx, queued.Operation.ID)
		if e != nil || blocked.State != "pending" || blocked.ErrorCode != "ROOM_SYNCHRONIZING" || len(sink.ids) != calls {
			t.Fatal("known pending room corrections did not gate broadcast", e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.live_operations SET state='done' WHERE id=$1", providerOp); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET available_at=clock_timestamp() WHERE id=$1", queued.Operation.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = worker.Step(ctx, []string{"gift"}); e != nil {
			t.Fatal(e)
		}
		applied, e := q.Delivery(ctx, queued.Operation.ID)
		if e != nil || applied.State != "delivered" {
			t.Fatal("synchronized room delivery did not recover", e)
		}
		var before, after int64
		if e = operator.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id=$1", "user:"+sender).Scan(&before); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET state='failed',attempts=10,error_code='RETRY_EXHAUSTED' WHERE id=$1", first.Operation.ID); e != nil {
			t.Fatal(e)
		}
		_, e = repo.Retry(ctx, sc, first.Operation.ID, access.ID(), "Synthetic replay")
		expect(t, e, apperror.Forbidden)
		if _, e = repo.Retry(ctx, oc, first.Operation.ID, access.ID(), "Synthetic audited replay"); e != nil {
			t.Fatal(e)
		}
		if _, e = worker.Step(ctx, []string{"gift"}); e != nil {
			t.Fatal(e)
		}
		if e = operator.QueryRow(ctx, "SELECT balance FROM app.wallet_accounts WHERE id=$1", "user:"+sender).Scan(&after); e != nil || after != before {
			t.Fatal("replay modified financial posting", e)
		}
	})
	t.Run("immutable catalog versions and deactivation", func(t *testing.T) {
		updated := catalog
		updated.Active = false
		value, e := repo.PutGift(ctx, oc, catalog.ID, access.ID(), GiftChange{Gift: updated, PreviousID: catalog.VersionID, Reason: "Synthetic deactivation"})
		if e != nil {
			t.Fatal(e)
		}
		version := decode[delivery.Gift](t, value)
		if version.VersionID == catalog.VersionID || !version.CreatedAt.Equal(catalog.CreatedAt) {
			t.Fatal("version/creation history changed")
		}
		prior := decode[wallet.Detail](t, exec(t, sc, firstCmd))
		if prior.Operation.ID != first.Operation.ID {
			t.Fatal("catalog deactivation lost committed retry")
		}
		in := firstCmd
		in.Key = access.ID()
		_, e = engine.Execute(ctx, sc, in)
		expect(t, e, apperror.NotFound)
		if _, e = pool.Exec(ctx, "UPDATE app.gift_versions SET price=1 WHERE id=$1", catalog.VersionID); e == nil {
			t.Fatal("runtime rewrote immutable catalog price")
		}
	})
	t.Run("validated uploads duplicate completion and attachment race", func(t *testing.T) {
		var data bytes.Buffer
		if e = png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 640, 480))); e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data.Bytes())
		upload := delivery.Upload{Purpose: "avatar", ContentType: "image/png", Size: int64(data.Len()), SHA256: base64.StdEncoding.EncodeToString(sum[:])}
		prepare := func() (delivery.Asset, error) {
			signed, e := repo.Upload(ctx, sc, access.ID(), upload)
			if e != nil {
				return delivery.Asset{}, e
			}
			v, e := q.Asset(ctx, signed.Asset.ID)
			if e != nil {
				return delivery.Asset{}, e
			}
			if e = store.Put(ctx, v.ObjectKey, data.Bytes(), "image/png"); e != nil {
				return delivery.Asset{}, e
			}
			return repo.Complete(ctx, sc, v.ID)
		}
		logo := upload
		logo.Purpose = "agency_logo"
		logo.Target = owner
		_, e := repo.Upload(ctx, sc, access.ID(), logo)
		expect(t, e, apperror.Forbidden)
		firstAsset, e := prepare()
		if e != nil {
			t.Fatal(e)
		}
		reads := store.reads
		again, e := repo.Complete(ctx, sc, firstAsset.ID)
		if e != nil || again.ID != firstAsset.ID || store.reads != reads {
			t.Fatal("duplicate completion repeated processing", e)
		}
		_, e = repo.Complete(ctx, rc, firstAsset.ID)
		expect(t, e, apperror.Forbidden)
		secondAsset, e := prepare()
		if e != nil {
			t.Fatal(e)
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i, a := range []delivery.Asset{firstAsset, secondAsset} {
			r := repo
			if i == 1 {
				r = repo2
			}
			wg.Go(func() { _, e := r.Attach(ctx, sc, a.ID, "", access.ID()); results <- e })
		}
		wg.Wait()
		success := 0
		for range 2 {
			e := <-results
			if e == nil {
				success++
			} else {
				expect(t, e, apperror.Conflict)
			}
		}
		if success != 1 {
			t.Fatal("attachment compare-and-swap failed")
		}
		current, e := q.AssetLink(ctx, deliverysql.AssetLinkParams{TargetID: sender, Purpose: "avatar"})
		if e != nil {
			t.Fatal(e)
		}
		winner, loser := firstAsset.ID, secondAsset.ID
		if current == secondAsset.ID {
			winner, loser = secondAsset.ID, firstAsset.ID
		}
		v, e := q.Asset(ctx, winner)
		if e != nil {
			t.Fatal(e)
		}
		var avatar string
		if e = operator.QueryRow(ctx, "SELECT avatar_url FROM app.profiles WHERE account_id=$1", sender).Scan(&avatar); e != nil || avatar != v.PublicUrl {
			t.Fatal("Profile-owned avatar mismatch", e)
		}
		// Migrator-only clock fixture makes only this tracked asset eligible for GC.
		for _, id := range []string{loser, winner} {
			if _, e = operator.Exec(ctx, "UPDATE app.delivery_assets SET expires_at=clock_timestamp()-interval '2 hours' WHERE id=$1", id); e != nil {
				t.Fatal(e)
			}
			if _, e = operator.Exec(ctx, "UPDATE app.delivery_outbox SET available_at=clock_timestamp() WHERE topic='media_gc' AND payload->>'asset_id'=$1", id); e != nil {
				t.Fatal(e)
			}
		}
		store.onDelete = func(key string) {
			if strings.Contains(key, loser) {
				_, e := repo.Attach(ctx, sc, loser, winner, access.ID())
				if e == nil || apperror.Public(e).Code() != apperror.Conflict {
					t.Error("retiring asset attached during deletion", e)
				}
			}
		}
		for range 2 {
			if _, e = worker.Step(ctx, []string{"media_gc"}); e != nil {
				t.Fatal(e)
			}
		}
		store.onDelete = nil
		dead, e := q.Asset(ctx, loser)
		if e != nil || dead.State != "deleted" {
			t.Fatal("unreferenced cleanup", e)
		}
		liveAsset, e := q.Asset(ctx, winner)
		if e != nil || liveAsset.State != "ready" {
			t.Fatal("referenced asset deleted", e)
		}
		store.mu.Lock()
		_, retained := store.objects[liveAsset.ApprovedKey]
		store.mu.Unlock()
		if !retained {
			t.Fatal("approved referenced object removed")
		}
		for _, purpose := range []string{"agency_logo", "gift_thumbnail"} {
			adminUpload := upload
			adminUpload.Purpose = purpose
			adminUpload.Target = owner
			signed, e := repo.Upload(ctx, oc, access.ID(), adminUpload)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := q.Asset(ctx, signed.Asset.ID)
			if e != nil {
				t.Fatal(e)
			}
			if e = store.Put(ctx, raw.ObjectKey, data.Bytes(), "image/png"); e != nil {
				t.Fatal(e)
			}
			approved, e := repo.Complete(ctx, oc, raw.ID)
			if e != nil {
				t.Fatal(e)
			}
			if purpose == "agency_logo" {
				if _, e = repo.Attach(ctx, oc, approved.ID, "", access.ID()); e != nil {
					t.Fatal(e)
				}
				if _, e = repo.Attachment(ctx, sc, "agency_logo", owner); e != nil {
					t.Fatal(e)
				}
			} else {
				current, e := q.Gift(ctx, catalog.ID)
				if e != nil {
					t.Fatal(e)
				}
				change := gift(current)
				change.ThumbnailAssetID = approved.ID
				if _, e = repo.PutGift(ctx, oc, catalog.ID, access.ID(), GiftChange{Gift: change, PreviousID: current.VersionID, Reason: "Synthetic gift image"}); e != nil {
					t.Fatal(e)
				}
			}
		}
		// Provider outage preserves pending; invalid bytes are never approved.
		signed, e := repo.Upload(ctx, sc, access.ID(), upload)
		if e != nil {
			t.Fatal(e)
		}
		raw, e := q.Asset(ctx, signed.Asset.ID)
		if e != nil {
			t.Fatal(e)
		}
		store.fail = true
		_, e = repo.Complete(ctx, sc, raw.ID)
		expect(t, e, apperror.ServiceUnavailable)
		store.fail = false
		if e = store.Put(ctx, raw.ObjectKey, data.Bytes(), "image/png"); e != nil {
			t.Fatal(e)
		}
		if _, e = repo.Complete(ctx, sc, raw.ID); e != nil {
			t.Fatal(e)
		}
		bad := []byte("not an image")
		digest := sha256.Sum256(bad)
		upload.Size = int64(len(bad))
		upload.SHA256 = base64.StdEncoding.EncodeToString(digest[:])
		signed, e = repo.Upload(ctx, sc, access.ID(), upload)
		if e != nil {
			t.Fatal(e)
		}
		raw, e = q.Asset(ctx, signed.Asset.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = store.Put(ctx, raw.ObjectKey, bad, "image/png"); e != nil {
			t.Fatal(e)
		}
		_, e = repo.Complete(ctx, sc, raw.ID)
		expect(t, e, apperror.ValidationFailed)
		invalid, e := q.Asset(ctx, raw.ID)
		if e != nil || invalid.State != "failed" {
			t.Fatal("invalid image approved", e)
		}
	})
	t.Run("push preferences invalid token and session unbinding", func(t *testing.T) {
		sink := &pushSink{invalid: true}
		d := &Dispatcher{Repo: repo, Push: sink, Workers: 1}
		worked, e := d.Step(ctx, []string{"push"})
		if e != nil || !worked || sink.calls.Load() != 1 {
			t.Fatal("invalid-token dispatch", e)
		}
		if _, e = q.Device(ctx, rc.Installation); !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal("invalid registration retained", e)
		}
		for range 6 {
			if _, e = d.Step(ctx, []string{"push"}); e != nil {
				t.Fatal(e)
			}
		}
		if sink.calls.Load() != 1 {
			t.Fatal("stale binding delivered")
		}
		if _, e = repo.Preferences(ctx, rc, &delivery.Preferences{}); e != nil {
			t.Fatal(e)
		}
		if e = repo.Register(ctx, rc, "ios", "synthetic-rotated-"+nonce); e != nil {
			t.Fatal(e)
		}
		tx, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if e = intents.Notify(ctx, tx, recipient, "room_invitation", access.ID()); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		if worked, e = d.Step(ctx, []string{"push"}); e != nil || worked {
			t.Fatal("opt-out preference ignored", e)
		}
		inst := access.ID()
		installs = append(installs, inst)
		multi := access.Credentials{Access: strings.Repeat("m", 11) + inst, Installation: inst}
		if _, e = operator.Exec(ctx, "INSERT INTO app.identity_installations(id) VALUES($1)", inst); e != nil {
			t.Fatal(e)
		}
		if _, e = operator.Exec(ctx, "INSERT INTO app.identity_sessions(id,account_id,installation_id,access_hash,access_expires_at,expires_at) VALUES($1,$2,$1,$3,clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day')", inst, recipient, identity.Digest(multi.Access)); e != nil {
			t.Fatal(e)
		}
		if e = repo.Register(ctx, multi, "android", "synthetic-multi-"+nonce); e != nil {
			t.Fatal(e)
		}
		if _, e = repo.Preferences(ctx, rc, &delivery.Preferences{GiftReceived: true, RoomInvitation: true, AdminAnnouncement: true}); e != nil {
			t.Fatal(e)
		}
		notify := func(kind string) {
			t.Helper()
			tx, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if e = intents.Notify(ctx, tx, recipient, kind, access.ID()); e != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
		}
		notify("room_invitation")
		if e = repo.Register(ctx, rc, "ios", "synthetic-new-rotation-"+nonce); e != nil {
			t.Fatal(e)
		}
		accepted := &pushSink{}
		d.Push = accepted
		for range 2 {
			if _, e = d.Step(ctx, []string{"push"}); e != nil {
				t.Fatal(e)
			}
		}
		if accepted.calls.Load() != 1 {
			t.Fatal("rotated old token delivered or other installation suppressed")
		}
		ann := Announcement{Accounts: []string{recipient, sender}, Template: "maintenance", Reason: "Synthetic bounded announcement"}
		_, e = repo.Announcement(ctx, sc, access.ID(), ann)
		expect(t, e, apperror.Forbidden)
		value, e := repo.Announcement(ctx, oc, access.ID(), ann)
		if e != nil {
			t.Fatal(e)
		}
		announcementIDs = append(announcementIDs, decode[map[string]any](t, value)["announcement_id"].(string))
		var workers sync.WaitGroup
		failures := make(chan error, 2)
		for _, r := range []*Repository{repo, repo2} {
			workers.Go(func() {
				dispatch := &Dispatcher{Repo: r, Push: accepted, Workers: 1}
				_, e := dispatch.Step(ctx, []string{"push"})
				failures <- e
			})
		}
		workers.Wait()
		for range 2 {
			if e := <-failures; e != nil {
				t.Fatal(e)
			}
		}
		if accepted.calls.Load() != 3 {
			t.Fatal("multi-device announcement dispatch duplicated or lost")
		}
		ir, e := identitypg.New(pool)
		if e != nil {
			t.Fatal(e)
		}
		if e = ir.Within(ctx, func(s identity.Store) error { return s.RevokeSession(ctx, recipient) }); e != nil {
			t.Fatal(e)
		}
		if _, e = q.Device(ctx, rc.Installation); !errors.Is(e, pgx.ErrNoRows) {
			t.Fatal("logout left push bound", e)
		}
		if _, e = q.Device(ctx, multi.Installation); e != nil {
			t.Fatal("logout removed another installation", e)
		}
		switched := access.Credentials{Access: strings.Repeat("z", 11) + access.ID(), Installation: multi.Installation}
		switchSession := access.ID()
		if e = ir.Within(ctx, func(s identity.Store) error {
			return s.CreateSession(ctx, identity.Session{ID: switchSession, Account: sender, Installation: multi.Installation}, identity.Digest(switched.Access))
		}); e != nil {
			t.Fatal(e)
		}
		priorBinding, e := q.Device(ctx, multi.Installation)
		if e != nil || priorBinding.AccountID != recipient {
			t.Fatal("caller installation ID cleared another account's active binding", e)
		}
		e = repo.Register(ctx, switched, "android", "synthetic-switched-"+nonce)
		expect(t, e, apperror.Conflict)
		e = repo.RegisterWithPrevious(ctx, switched, "android", "synthetic-switched-"+nonce, sc.Access)
		expect(t, e, apperror.Forbidden)
		failing := *repo
		failing.guard = auditFailureGuard{repo.guard}
		e = failing.RegisterWithPrevious(ctx, switched, "android", "synthetic-switched-"+nonce, multi.Access)
		expect(t, e, apperror.ServiceUnavailable)
		old, e := q.Device(ctx, multi.Installation)
		if e != nil || old.AccountID != recipient {
			t.Fatal("binding switch audit rollback failed", e)
		}
		if e = repo.RegisterWithPrevious(ctx, switched, "android", "synthetic-switched-"+nonce, multi.Access); e != nil {
			t.Fatal(e)
		}

		binding, e := q.Device(ctx, multi.Installation)
		if e != nil || binding.AccountID != sender {
			t.Fatal("cross-account binding", e)
		}
		if e = ir.RestrictInstallation(ctx, multi.Installation); e != nil {
			t.Fatal(e)
		}
		e = repo.Register(ctx, switched, "android", "synthetic-restricted-"+nonce)
		expect(t, e, apperror.Unauthenticated)

		e = repo.Register(ctx, rc, "ios", "synthetic-revoked-"+nonce)
		expect(t, e, apperror.Unauthenticated)
		stopped, cancel := context.WithCancel(context.Background())
		cancel()
		if e = (&Dispatcher{Repo: repo, Workers: 2}).Run(stopped); e != nil {
			t.Fatal("worker shutdown", e)
		}
	})
	final, e := walletsql.New(pool).Reconcile(ctx)
	if e != nil || len(final) != 0 {
		t.Fatal("unexplained final ledger difference", e)
	}
	t.Log("synthetic delivery fixtures: committed gifts conserved; no provider replay debited again; real providers NOT TESTED")
}

type auditFailureUnit struct{ access.Unit }

func (auditFailureUnit) Audit(context.Context, access.Audit) error {
	return errors.New("synthetic audit rejection")
}

type auditFailureGuard struct{ Guard }

func (g auditFailureGuard) Authorized(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(context.Context, pgx.Tx, access.Unit) error) error {
	return g.Guard.Authorized(ctx, c, p, owner, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error { return fn(ctx, tx, auditFailureUnit{u}) })
}
