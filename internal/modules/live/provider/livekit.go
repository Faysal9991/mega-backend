// Package provider adapts official LiveKit server APIs; it owns no media transport.
package provider

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"
)

type Client struct {
	rooms       *lksdk.RoomServiceClient
	key, secret string
	slots       chan struct{}
}

func New(url, key, secret string) *Client {
	return &Client{rooms: lksdk.NewRoomServiceClient(url, key, secret), key: key, secret: secret, slots: make(chan struct{}, 8)}
}
func (c *Client) acquire(ctx context.Context) (func(), error) {
	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func missing(e error) bool { var t twirp.Error; return errors.As(e, &t) && t.Code() == twirp.NotFound }
func (c *Client) Create(ctx context.Context, r live.Room) error {
	done, e := c.acquire(ctx)
	if e != nil {
		return e
	}
	defer done()
	_, e = c.rooms.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: r.SessionID, MaxParticipants: uint32(r.Capacity), EmptyTimeout: 300, DepartureTimeout: 30, Metadata: `{"room_id":"` + r.ID + `","room_type":"` + r.Kind + `"}`})
	return e
}
func (c *Client) Close(ctx context.Context, r live.Room) error {
	done, e := c.acquire(ctx)
	if e != nil {
		return e
	}
	defer done()
	_, e = c.rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: r.SessionID})
	if missing(e) {
		return nil
	}
	return e
}
func sources(r live.Room) []livekit.TrackSource {
	v := []livekit.TrackSource{livekit.TrackSource_MICROPHONE}
	if r.Kind == "video" {
		v = append(v, livekit.TrackSource_CAMERA)
	}
	return v
}
func (c *Client) Participant(ctx context.Context, r live.Room, m live.Member) error {
	done, e := c.acquire(ctx)
	if e != nil {
		return e
	}
	defer done()
	if m.State != "active" {
		_, e = c.rooms.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: r.SessionID, Identity: m.AccountID})
		if missing(e) {
			return nil
		}
		return e
	}
	publish := live.CanPublish(r, m) && !m.Hidden
	permission := &livekit.ParticipantPermission{CanSubscribe: true, CanPublish: publish, CanPublishData: false, CanUpdateMetadata: false, Hidden: m.Hidden}
	if publish {
		permission.CanPublishSources = sources(r)
	}
	_, e = c.rooms.UpdateParticipant(ctx, &livekit.UpdateParticipantRequest{Room: r.SessionID, Identity: m.AccountID, Permission: permission})
	if missing(e) {
		return nil
	}
	return e
}
func (c *Client) Snapshot(ctx context.Context, r live.Room) (live.Snapshot, error) {
	done, e := c.acquire(ctx)
	if e != nil {
		return live.Snapshot{}, e
	}
	defer done()
	v, e := c.rooms.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: r.SessionID})
	if missing(e) {
		return live.Snapshot{Exists: false, Participants: []live.Participant{}}, nil
	}
	if e != nil {
		return live.Snapshot{}, e
	}
	if len(v.Participants) > 500 {
		return live.Snapshot{}, errors.New("livekit: snapshot limit exceeded")
	}
	out := make([]live.Participant, 0, len(v.Participants))
	for _, p := range v.Participants {
		out = append(out, live.Participant{Identity: p.Identity, SID: p.Sid})
	}
	return live.Snapshot{Exists: true, Participants: out}, nil
}
func (c *Client) Token(g live.Grant, ttl time.Duration) (string, error) {
	publish, yes, no := live.CanPublish(g.Room, g.Member) && !g.Member.Hidden, true, false
	grant := &auth.VideoGrant{RoomJoin: true, Room: g.Room.SessionID, CanPublish: &publish, CanSubscribe: &yes, CanPublishData: &no, CanUpdateOwnMetadata: &no, Hidden: g.Member.Hidden}
	if publish {
		grant.SetCanPublishSources(sources(g.Room))
	}
	return auth.NewAccessToken(c.key, c.secret).SetIdentity(g.Identity).SetValidFor(ttl).SetVideoGrant(grant).ToJWT()
}

// Verify uses the official raw-body checksum and JWT verifier. The transport
// must apply MaxBytesReader before this function, which closes the body.
func (c *Client) Verify(r *http.Request) (live.Event, error) {
	if len(r.Header.Values("Authorization")) != 1 {
		return live.Event{}, errors.New("livekit: invalid authorization")
	}
	e, err := webhook.ReceiveWebhookEvent(r, auth.NewSimpleKeyProvider(c.key, c.secret))
	if err != nil {
		return live.Event{}, err
	}
	return live.Event{ID: e.Id, Type: e.Event, SessionID: e.GetRoom().GetName(), RoomSID: e.GetRoom().GetSid(), AccountID: e.GetParticipant().GetIdentity(), ParticipantSID: e.GetParticipant().GetSid(), At: time.Unix(e.CreatedAt, 0).UTC()}, nil
}
