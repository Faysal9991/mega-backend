package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/proto"
)

const testSecret = "synthetic-provider-secret-for-tests-only-000000"

func TestLeastPrivilegeTokenClaims(t *testing.T) {
	c := New("https://provider.invalid", "fixture-key", testSecret)
	for _, v := range []struct {
		kind, role  string
		seat        int32
		muted, want bool
	}{{"audio", "host", 0, false, true}, {"audio", "audience", 1, false, true}, {"audio", "audience", 0, false, false}, {"video", "co_host", 0, false, true}, {"video", "moderator", 0, false, false}, {"video", "host", 0, true, false}} {
		token, e := c.Token(live.Grant{Room: live.Room{SessionID: "immutable-session", Kind: v.kind}, Member: live.Member{State: "active", Role: v.role, Seat: v.seat, Muted: v.muted}, Identity: "server-account"}, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		verifier, e := auth.ParseAPIToken(token)
		if e != nil {
			t.Fatal(e)
		}
		_, claims, e := verifier.Verify(testSecret)
		if e != nil {
			t.Fatal(e)
		}
		g := claims.Video
		if claims.Identity != "server-account" || g.Room != "immutable-session" || !g.RoomJoin || g.RoomAdmin || g.RoomCreate || g.GetCanPublish() != v.want || g.GetCanPublishData() || g.CanUpdateOwnMetadata == nil || *g.CanUpdateOwnMetadata {
			t.Fatal("unsafe token grant")
		}
		if v.want && len(g.CanPublishSources) == 0 {
			t.Fatal("unrestricted source")
		}
		if v.kind == "audio" && v.want && (len(g.CanPublishSources) != 1 || g.CanPublishSources[0] != "microphone") {
			t.Fatal("audio camera grant")
		}
	}
}
func signed(t *testing.T, body []byte) *http.Request {
	t.Helper()
	sum := sha256.Sum256(body)
	token, e := auth.NewAccessToken("fixture-key", testSecret).SetSha256(base64.StdEncoding.EncodeToString(sum[:])).SetValidFor(time.Minute).ToJWT()
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	r.Header.Set("Authorization", token)
	return r
}
func TestOfficialWebhookSignatureAndBodyBinding(t *testing.T) {
	c := New("https://provider.invalid", "fixture-key", testSecret)
	body := []byte(`{"event":"participant_joined","id":"event-1","createdAt":"1795560000","room":{"name":"session","sid":"RM_1"},"participant":{"identity":"account","sid":"PA_1"}}`)
	r := signed(t, body)
	v, e := c.Verify(r)
	if e != nil || v.ID != "event-1" || v.ParticipantSID != "PA_1" {
		t.Fatal(v, e)
	}
	r = signed(t, body)
	r.Body = http.NoBody
	if _, e := c.Verify(r); e == nil {
		t.Fatal("unsigned body mutation accepted")
	}
	r = signed(t, body)
	r.Header.Set("Authorization", "invalid")
	if _, e := c.Verify(r); e == nil {
		t.Fatal("invalid signature")
	}
	r = signed(t, body)
	r.Header.Add("Authorization", "duplicate")
	if _, e := c.Verify(r); e == nil {
		t.Fatal("duplicate authorization")
	}
}
func TestProviderTimeoutAndRemovalIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "RemoveParticipant") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"code":"not_found","msg":"fixture absent"}`))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(150 * time.Millisecond):
		}
	}))
	defer server.Close()
	c := New(server.URL, "fixture-key", testSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if e := c.Create(ctx, live.Room{SessionID: "s", Capacity: 5}); e == nil {
		t.Fatal("timeout ignored")
	}
	if e := c.Participant(context.Background(), live.Room{SessionID: "s"}, live.Member{AccountID: "a", State: "banned"}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < cap(c.slots); i++ {
		c.slots <- struct{}{}
	}
	ctx2, stop := context.WithCancel(context.Background())
	stop()
	if _, e := c.Snapshot(ctx2, live.Room{}); e == nil {
		t.Fatal("admission ignored cancellation")
	}
	for i := 0; i < cap(c.slots); i++ {
		<-c.slots
	}
}

func TestSnapshotDistinguishesMissingAndEmptyRoom(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(map[bool]string{true: "empty", false: "missing"}[exists], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !exists {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"code":"not_found","msg":"fixture absent"}`))
					return
				}
				w.Header().Set("Content-Type", "application/protobuf")
				w.WriteHeader(200)
			}))
			defer server.Close()
			c := New(server.URL, "fixture-key", testSecret)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			snapshot, e := c.Snapshot(ctx, live.Room{SessionID: "fixture"})
			if e != nil || snapshot.Exists != exists || len(snapshot.Participants) != 0 {
				t.Fatal("provider absence conflated with empty room", e)
			}
		})
	}
}

func TestHiddenTokenCannotPublishOrAdminister(t *testing.T) {
	c := New("https://example.invalid", "fixture-key", testSecret)
	token, e := c.Token(live.Grant{Room: live.Room{SessionID: "hidden-session", Kind: "audio"}, Member: live.Member{State: "active", Role: "audience", Seat: 1, Hidden: true}, Identity: "auditable-identity"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	v, e := auth.ParseAPIToken(token)
	if e != nil {
		t.Fatal(e)
	}
	_, claims, e := v.Verify(testSecret)
	if e != nil {
		t.Fatal(e)
	}
	g := claims.Video
	if g == nil || !g.Hidden || g.RoomAdmin || g.RoomCreate || g.RoomList || g.CanPublish == nil || *g.CanPublish || g.CanPublishData == nil || *g.CanPublishData || g.Room != "hidden-session" || claims.Identity != "auditable-identity" {
		t.Fatal("unsafe hidden grants")
	}
}

func TestHiddenParticipantUpdateUsesOfficialPermission(t *testing.T) {
	seen := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var body livekit.UpdateParticipantRequest
		raw, e := io.ReadAll(io.LimitReader(r.Body, 8192))
		if e == nil {
			e = proto.Unmarshal(raw, &body)
		}
		seen <- e == nil && body.Room == "controlled-session" && body.Identity == "auditable" && body.Permission != nil && body.Permission.Hidden && !body.Permission.CanPublish && !body.Permission.CanPublishData
		response, e := proto.Marshal(&livekit.ParticipantInfo{Identity: "auditable"})
		if e != nil {
			t.Error("controlled proto response")
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/protobuf")
		_, _ = w.Write(response)

	}))
	defer server.Close()
	c := New(server.URL, "fixture-key", testSecret)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := c.Participant(ctx, live.Room{SessionID: "controlled-session", Kind: "audio"}, live.Member{AccountID: "auditable", State: "active", Role: "audience", Seat: 1, Hidden: true}); e != nil {
		t.Fatal(e)
	}
	select {
	case valid := <-seen:
		if !valid {
			t.Fatal("hidden permission not transmitted safely")
		}
	case <-ctx.Done():
		t.Fatal("provider request absent")
	}
}
