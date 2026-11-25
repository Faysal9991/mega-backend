package live

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
)

func TestCompatibilityAndPublishing(t *testing.T) {
	p := Compatibility{AudioHostAudio: true, VideoHostVideo: true}
	for _, v := range []struct {
		host, room string
		want       bool
	}{{"audio_host", "audio", true}, {"audio_host", "video", false}, {"video_host", "audio", false}, {"video_host", "video", true}, {"admin", "audio", false}} {
		if p.Allows(v.host, v.room) != v.want {
			t.Fatal(v)
		}
	}
	for _, v := range []struct {
		r    Room
		m    Member
		want bool
	}{{Room{Kind: "audio"}, Member{State: "active", Role: "audience", Seat: 1}, true}, {Room{Kind: "video"}, Member{State: "active", Role: "audience", Seat: 1}, false}, {Room{Kind: "video"}, Member{State: "active", Role: "co_host"}, true}, {Room{Kind: "audio"}, Member{State: "active", Role: "host", Muted: true}, false}, {Room{Kind: "audio"}, Member{State: "banned", Role: "host"}, false}, {Room{Kind: "audio"}, Member{State: "active", Role: "moderator"}, false}} {
		if CanPublish(v.r, v.m) != v.want {
			t.Fatal(v)
		}
	}
}
func testLimits() Limits {
	return Limits{Capacity: 100, Seats: 12, TokenTTL: time.Minute, ProviderTimeout: time.Second, EventAge: 24 * time.Hour}
}
func TestCommandBoundary(t *testing.T) {
	l := testLimits()
	if e := l.Validate(); e != nil {
		t.Fatal(e)
	}
	valid := Command{Kind: "room.create", Key: "request", Reason: "create", Name: "Room", Status: "audio", Capacity: 10, Seats: 5}
	if e := valid.Validate(l); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Command){func(c *Command) { c.Capacity = 501 }, func(c *Command) { c.Seats = 33 }, func(c *Command) { c.Status = "video" }, func(c *Command) { c.Key = "" }, func(c *Command) { c.Name = "\n" }, func(c *Command) { c.Target = "actor-from-client" }, func(c *Command) { c.Kind = "wallet.credit" }} {
		v := valid
		change(&v)
		if v.Validate(l) == nil {
			t.Fatal("unsafe command accepted")
		}
	}
}

type storeStub struct {
	Store
	flushError  error
	flushCalled bool
}

func (s *storeStub) Execute(context.Context, access.Credentials, Command) (Result, error) {
	return Result{RoomID: access.ID(), OperationID: access.ID()}, nil
}
func (s *storeStub) Flush(context.Context, string, Provider) error {
	s.flushCalled = true
	return s.flushError
}

type providerStub struct{ Provider }

func TestAcceptedProviderFailureRemainsPending(t *testing.T) {
	s := &storeStub{flushError: errors.New("timeout")}
	svc, e := New(s, providerStub{}, testLimits(), "https://provider.invalid")
	if e != nil {
		t.Fatal(e)
	}
	out, e := svc.Execute(context.Background(), access.Credentials{}, Command{Kind: "room.close", ID: access.ID(), Key: "retry", Reason: "close"})
	if e != nil || out.ProviderState != "pending" || !s.flushCalled {
		t.Fatal(out, e)
	}
}
