//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesshttp "github.com/Faysal9991/mega-backend/internal/modules/access/httpapi"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	livehttp "github.com/Faysal9991/mega-backend/internal/modules/live/httpapi"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live/provider"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Two independently composed module HTTP servers, repositories and pools share
// only PostgreSQL. No provider network, process-global authority cache or mock guard.
func reviewHTTP(t *testing.T, ctx context.Context, first *pgxpool.Pool, cfg config.DatabaseConfig, limits live.Limits) ([2]*httptest.Server, [2]*pgxpool.Pool) {
	t.Helper()
	second, e := platform.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(second.Close)
	pools := [2]*pgxpool.Pool{first, second}
	var servers [2]*httptest.Server
	for i, p := range pools {
		ar, e := accesspg.New(p)
		if e != nil {
			t.Fatal(e)
		}
		as, e := access.New(ar)
		if e != nil {
			t.Fatal(e)
		}
		ah, e := accesshttp.New(as, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if e != nil {
			t.Fatal(e)
		}
		lr, e := livepg.New(p, limits)
		if e != nil {
			t.Fatal(e)
		}
		ls, e := live.New(lr, provider.Unavailable{}, limits, "")
		if e != nil {
			t.Fatal(e)
		}
		lh, e := livehttp.New(ls, provider.Unavailable{}.Verify, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if e != nil {
			t.Fatal(e)
		}
		base, e := config.Load(func(key string) (string, bool) {
			if key == "APP_ENV" {
				return "test", true
			}
			if key == "HTTP_REQUEST_TIMEOUT" {
				return "30s", true
			}
			return "", false
		})
		if e != nil {
			t.Fatal(e)
		}
		r, e := httpserver.NewRouter(base.HTTP, slog.New(slog.NewTextHandler(io.Discard, nil)), httpserver.NewLifecycle(nil), func(r chi.Router) { ah.Mount(r); lh.Mount(r) })
		if e != nil {
			t.Fatal(e)
		}
		servers[i] = httptest.NewServer(r)
		t.Cleanup(servers[i].Close)
	}
	return servers, pools
}
func reviewRequest(ctx context.Context, client *http.Client, base, method, path, body, key string, c access.Credentials) (int, error) {
	req, e := http.NewRequestWithContext(ctx, method, base+"/api/v1"+path, strings.NewReader(body))
	if e != nil {
		return 0, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Access)
	req.Header.Set("X-Installation-ID", c.Installation)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, e := client.Do(req)
	if e != nil {
		return 0, fmt.Errorf("HTTP transport unavailable")
	}
	defer res.Body.Close()
	var envelope struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope); e != nil {
		return res.StatusCode, fmt.Errorf("invalid response envelope")
	}
	if envelope.Success != (res.StatusCode < 400) || envelope.Code == "" {
		return res.StatusCode, fmt.Errorf("inconsistent response envelope")
	}
	return res.StatusCode, nil
}
func testReviewLiveRestriction(t *testing.T, ctx context.Context, operator *pgx.Conn, pool *pgxpool.Pool, cfg config.DatabaseConfig, limits live.Limits, creds map[string]access.Credentials, rooms []string, owner, mod, target string) {
	servers, _ := reviewHTTP(t, ctx, pool, cfg, limits)
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if _, e := operator.Exec(cleanup, "UPDATE app.live_global_bans SET active=false WHERE account_id=$1", mod); e != nil {
			t.Error("review moderator ban restoration")
		}
		if _, e := operator.Exec(cleanup, "UPDATE app.live_members SET state='active',role='audience',seat=NULL,invited_seat=NULL,hidden=false,self_seated=false,private_entry=false WHERE room_id=ANY($1::text[]) AND account_id=$2", rooms, mod); e != nil {
			t.Error("review moderator membership restoration")
		}
	}()
	client := &http.Client{Timeout: 35 * time.Second}
	defer client.CloseIdleConnections()
	request := func(i int, actor, method, path, body, key string, want int) {
		t.Helper()
		status, e := reviewRequest(ctx, client, servers[i].URL, method, path, body, key, creds[actor])
		if e != nil || status != want {
			t.Fatalf("%s %s status=%d want=%d err=%v", method, path, status, want, e)
		}
	}
	ar, e := accesspg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	// A separately assigned legacy global moderation capability must also be
	// removed by restriction, not expanded back into host powers afterward.
	legacy := access.OverrideChange{Action: live.RoomModerate, Allowed: true, ExpiresAt: time.Now().Add(time.Hour), Reason: "Synthetic review legacy authority"}
	if e = ar.Override(ctx, creds[owner], mod, legacy); e != nil {
		t.Fatal(e)
	}
	defer func() {
		legacy.Remove = true
		if e := ar.Override(ctx, creds[owner], mod, legacy); e != nil {
			t.Error(e)
		}
	}()
	mute := func(room string) string { return "/rooms/" + room + "/members/" + target + "/mute" }
	body := `{"muted":true,"reason":"Synthetic source review"}`
	prior := access.ID()
	request(0, mod, "PATCH", mute(rooms[0]), body, prior, 202)
	request(1, mod, "PATCH", mute(rooms[1]), body, access.ID(), 202)
	banPath := "/live-participants/" + mod + "/ban"
	request(0, owner, "PUT", banPath, `{"banned":true,"reason":"Synthetic source review"}`, access.ID(), 200)
	for i := range servers {
		for _, room := range rooms {
			request(i, mod, "PATCH", mute(room), body, access.ID(), 403)
		}
		request(i, mod, "PATCH", mute(rooms[0]), body, prior, 403) // committed command replay cannot restore authority
		request(i, mod, "PUT", "/live-participants/"+target+"/ban", `{"banned":true,"reason":"Synthetic source review"}`, access.ID(), 403)
		request(i, mod, "POST", "/admin/rooms/"+rooms[0]+"/close", `{}`, access.ID(), 403)
	}
	for _, scope := range []string{"", rooms[0]} {
		powers, e := ar.Powers(ctx, creds[mod], scope)
		if e != nil {
			t.Fatal(e)
		}
		for _, action := range powers.Permissions {
			if access.LiveRestrictedAction(action) {
				t.Fatalf("banned actor retained %s", action)
			}
		}
	}
	request(1, owner, "PUT", banPath, `{"banned":false,"reason":"Synthetic source review lift"}`, access.ID(), 200)
	for i, room := range rooms {
		request(i, mod, "PATCH", mute(room), body, access.ID(), 202)
		// Reinstate membership explicitly: removing the restriction cannot resurrect it.
		request(i, mod, "POST", "/rooms/"+room+"/join", `{}`, access.ID(), 200)
	}
	request(0, mod, "PUT", "/live-participants/"+target+"/ban", `{"banned":false,"reason":"Synthetic source review reinstated"}`, access.ID(), 200)
	request(1, mod, "PUT", "/live-participants/"+owner+"/ban", `{"banned":true,"reason":"Synthetic protected owner"}`, access.ID(), 403)
	// Normal APIs cannot ban protected owners. Inject only this synthetic owner's
	// impossible persisted state to prove fail-closed behavior without an exemption.
	if _, e = operator.Exec(ctx, "INSERT INTO app.live_global_bans(account_id,active) VALUES($1,true) ON CONFLICT(account_id) DO UPDATE SET active=true", owner); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if _, e := operator.Exec(cleanup, "UPDATE app.live_global_bans SET active=false WHERE account_id=$1", owner); e != nil {
			t.Error("synthetic owner ban cleanup")
		}
	}()
	powers, e := ar.Powers(ctx, creds[owner], rooms[0])
	if e != nil || !powers.Owner || powers.Allows("room.participant.mute") || !powers.Allows("wallet.adjust") {
		t.Fatal("owner live restriction/configuration separation", e)
	}
	request(0, owner, "PATCH", mute(rooms[0]), body, access.ID(), 403)
	request(1, owner, "PUT", banPath, `{"banned":false,"reason":"Synthetic restricted owner"}`, access.ID(), 403)
}

func testReviewLockBenchmark(t *testing.T, ctx context.Context, operator *pgx.Conn, pool *pgxpool.Pool, cfg config.DatabaseConfig, limits live.Limits, creds map[string]access.Credentials, actors []string) {
	servers, pools := reviewHTTP(t, ctx, pool, cfg, limits)
	client := &http.Client{Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	for _, concurrency := range []int{1, 4, 8} {
		const n = 8
		// Warm both HTTP/DB paths before the measured fixed workload.
		for i := range servers {
			if status, e := reviewRequest(ctx, client, servers[i].URL, "GET", "/profiles/me", "", access.ID(), creds[actors[i]]); e != nil || status != 200 {
				t.Fatal("benchmark warmup", status, e)
			}
		}
		before := [2]*pgxpool.Stat{pools[0].Stat(), pools[1].Stat()}
		sampleCtx, cancel := context.WithCancel(ctx)
		type observation struct {
			samples, peak int
			maxWait       float64
			err           error
		}
		observed := make(chan observation, 1)
		go func() {
			var o observation
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-sampleCtx.Done():
					observed <- o
					return
				case <-ticker.C:
					var waiters int
					var age float64
					e := operator.QueryRow(ctx, `SELECT count(*)::integer,COALESCE(max(EXTRACT(EPOCH FROM(clock_timestamp()-waitstart))),0)::float8 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=772341909 AND objsubid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&waiters, &age)
					if e != nil {
						if sampleCtx.Err() == nil {
							o.err = fmt.Errorf("lock sampler unavailable")
						}
						observed <- o
						return
					}
					o.samples++
					o.peak = max(o.peak, waiters)
					o.maxWait = max(o.maxWait, age)
				}
			}
		}()
		jobs := make(chan int)
		results := make(chan struct {
			elapsed time.Duration
			err     error
		}, n)
		var wg sync.WaitGroup
		started := time.Now()
		for range concurrency {
			wg.Go(func() {
				for j := range jobs {
					start := time.Now()
					status, e := reviewRequest(ctx, client, servers[j%2].URL, "GET", "/profiles/me", "", access.ID(), creds[actors[j%len(actors)]])
					if e == nil && status != 200 {
						e = fmt.Errorf("HTTP status %d", status)
					}
					results <- struct {
						elapsed time.Duration
						err     error
					}{time.Since(start), e}
				}
			})
		}
		for j := range n {
			jobs <- j
		}
		close(jobs)
		wg.Wait()
		close(results)
		elapsed := time.Since(started)
		cancel()
		o := <-observed
		durations := []time.Duration{}
		failures := 0
		for result := range results {
			durations = append(durations, result.elapsed)
			if result.err != nil {
				failures++
			}
		}
		slices.Sort(durations)
		var poolWait time.Duration
		var empty, canceled int64
		for i, p := range pools {
			after := p.Stat()
			poolWait += after.EmptyAcquireWaitTime() - before[i].EmptyAcquireWaitTime()
			empty += after.EmptyAcquireCount() - before[i].EmptyAcquireCount()
			canceled += after.CanceledAcquireCount() - before[i].CanceledAcquireCount()
		}
		p95 := durations[int(math.Ceil(.95*float64(n)))-1]
		t.Logf("SYNTHETIC_LOCK_HTTP n=%d concurrency=%d instances=2 pool_max_each=%d p50_ms=%.2f p95_ms=%.2f p99_ms=%.2f throughput_rps=%.3f errors=%d lock_samples=%d peak_lock_waiters=%d max_observed_lock_wait_ms=%.2f pool_empty_acquires=%d pool_wait_total_ms=%.2f pool_canceled=%d", n, concurrency, cfg.MaxConns, float64(durations[(n-1)/2])/float64(time.Millisecond), float64(p95)/float64(time.Millisecond), float64(durations[n-1])/float64(time.Millisecond), float64(n)/elapsed.Seconds(), failures, o.samples, o.peak, o.maxWait*1000, empty, float64(poolWait)/float64(time.Millisecond), canceled)
		// Overload errors are measurements, not an invented latency/SLO assertion.
		// Warm-up verifies functional access; report failures in every load case.
		if o.err != nil {
			t.Fatal("benchmark sampler failure", o.err)
		}
	}
}

func testReviewRoomPolicy(t *testing.T, ctx context.Context, operator *pgx.Conn, pool *pgxpool.Pool, cfg config.DatabaseConfig, limits live.Limits, creds map[string]access.Credentials, room, owner, actor, target, mod, original string, tracked *[]string) {
	var versions []string
	servers, _ := reviewHTTP(t, ctx, pool, cfg, limits)
	client := &http.Client{Timeout: 35 * time.Second}
	defer client.CloseIdleConnections()
	request := func(i int, account, method, path, body string, want int) {
		t.Helper()
		status, e := reviewRequest(ctx, client, servers[i].URL, method, path, body, access.ID(), creds[account])
		if e != nil || status != want {
			t.Fatalf("role policy HTTP %s status=%d want=%d err=%v", path, status, want, e)
		}
	}
	// Restore only this fixture's changed policy head/members. Keep the returned
	// immutable version IDs for the enclosing fixture's exact-ID cleanup.
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if _, e := operator.Exec(cleanup, "UPDATE app.access_policy_head SET policy_id=$1,revision=revision+1 WHERE singleton AND policy_id=ANY($2::text[])", original, versions); e != nil {
			t.Error("review policy restoration")
		}
		if _, e := operator.Exec(cleanup, "UPDATE app.live_members SET role='audience',state='active' WHERE room_id=$1 AND account_id=ANY($2::text[])", room, []string{actor, target, mod}); e != nil {
			t.Error("review member restoration")
		}
	}()
	// Each scenario establishes its own active target/actor memberships.
	if _, e := operator.Exec(ctx, "UPDATE app.live_members SET role='audience',state='active' WHERE room_id=$1 AND account_id=ANY($2::text[])", room, []string{actor, target, mod}); e != nil {
		t.Fatal(e)
	}
	if _, e := operator.Exec(ctx, "UPDATE app.live_members SET role='admin' WHERE room_id=$1 AND account_id=$2", room, actor); e != nil {
		t.Fatal(e)
	}
	ar, e := accesspg.New(pool)
	if e != nil {
		t.Fatal(e)
	}
	powers, e := ar.Powers(ctx, creds[actor], room)
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"room.close", "room.admin.assign", "room.participant.ban"} {
		if powers.Allows(action) {
			t.Fatalf("unapproved seed admin power %s", action)
		}
	}
	mute := "/rooms/" + room + "/members/" + target + "/mute"
	body := `{"muted":true,"reason":"Synthetic dynamic room policy"}`
	request(0, actor, "PATCH", mute, body, 202)
	request(1, actor, "POST", "/rooms/"+room+"/close", `{}`, 403)
	request(1, actor, "POST", "/rooms/"+room+"/members/"+target+"/ban", `{"reason":"Synthetic restricted default"}`, 403)
	request(0, actor, "PATCH", "/rooms/"+room+"/members/"+target+"/role", `{"role":"admin","reason":"Synthetic restricted default"}`, 403)
	previous := original
	publish := func(roles map[string][]string) {
		t.Helper()
		out, e := ar.CapabilityPolicy(ctx, creds[owner], &access.PolicyChange{Previous: previous, Roles: roles, Reason: "Synthetic immediate policy review"})
		if e != nil {
			t.Fatal(e)
		}
		previous = out.(map[string]any)["policy_id"].(string)
		versions = append(versions, previous)
		*tracked = append(*tracked, previous)
	}
	roles := access.RoomDefaults()
	roles["admin"] = []string{"room.seat.self_leave"}
	publish(roles)
	for i := range servers {
		request(i, actor, "PATCH", mute, body, 403)
	}
	roles = access.RoomDefaults()
	roles["admin"] = append(roles["admin"], "room.close", "room.admin.assign", "room.participant.ban")
	publish(roles)
	for i := range servers {
		request(i, actor, "PATCH", mute, body, 202)
	}
	request(1, actor, "PATCH", "/rooms/"+room+"/members/"+target+"/role", `{"role":"admin","reason":"Synthetic explicit configured authority"}`, 202)
	request(0, actor, "POST", "/rooms/"+room+"/members/"+mod+"/ban", `{"reason":"Synthetic explicit configured authority"}`, 202)
	powers, e = ar.Powers(ctx, creds[actor], room)
	if e != nil || !powers.Allows("room.close") {
		t.Fatal("explicit dynamic close grant lost", e)
	}
	// Separate global staff authority survives a room-policy change, but never
	// a global live restriction (covered in the neighboring regression).
	powers, e = ar.Powers(ctx, creds[mod], "")
	if e != nil || !powers.Allows("live.participant.global_ban") {
		t.Fatal("global staff authority lost", e)
	}
	return
}
