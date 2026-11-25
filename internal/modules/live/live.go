// Package live owns room policy and provider-independent orchestration.
package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

const (
	AgencyManage = "agencies.manage"
	HostManage   = "agency.hosts.manage"
	RoomModerate = "live.rooms.moderate"
	ActivityRead = "live.activity.read"
	PolicyManage = "live.policies.manage"
)

const MaxParticipatingRooms = 50

func Fail(c apperror.Code) error { return apperror.New(c, nil) }

// Compatibility has no implicit cross-classification inheritance. Empty means deny.
type Compatibility struct {
	AudioHostAudio bool `json:"audio_host_audio"`
	AudioHostVideo bool `json:"audio_host_video"`
	VideoHostAudio bool `json:"video_host_audio"`
	VideoHostVideo bool `json:"video_host_video"`
}

func (p Compatibility) Allows(host, room string) bool {
	switch host + ":" + room {
	case "audio_host:audio":
		return p.AudioHostAudio
	case "audio_host:video":
		return p.AudioHostVideo
	case "video_host:audio":
		return p.VideoHostAudio
	case "video_host:video":
		return p.VideoHostVideo
	}
	return false
}

type Limits struct {
	Capacity, Seats                     int
	TokenTTL, ProviderTimeout, EventAge time.Duration
	DatabaseTimeout                     time.Duration
	Compatibility                       Compatibility
}

func (p Limits) Validate() error {
	if p.DatabaseTimeout != 0 && (p.DatabaseTimeout < 100*time.Millisecond || p.DatabaseTimeout > 30*time.Second) {
		return errors.New("live: invalid database timeout")
	}
	if p.Capacity < 2 || p.Capacity > 500 || p.Seats < 1 || p.Seats > 32 || p.TokenTTL < 30*time.Second || p.TokenTTL > 2*time.Minute || p.ProviderTimeout < 100*time.Millisecond || p.ProviderTimeout > 5*time.Second || p.EventAge < time.Minute || p.EventAge > 24*time.Hour {
		return errors.New("live: invalid limits")
	}
	return nil
}

type Room struct {
	ID        string     `json:"room_id"`
	SessionID string     `json:"session_id"`
	OwnerID   string     `json:"owner_id"`
	Kind      string     `json:"room_type"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Capacity  int32      `json:"capacity"`
	Seats     int32      `json:"seats"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}
type Member struct {
	AccountID   string `json:"account_id"`
	Role        string `json:"role"`
	State       string `json:"state"`
	Muted       bool   `json:"muted"`
	Seat        int32  `json:"seat"`
	InvitedSeat int32  `json:"invited_seat"`
}
type Agency struct {
	ID        string    `json:"agency_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Application struct {
	ID             string     `json:"application_id"`
	AgencyID       string     `json:"agency_id"`
	AccountID      string     `json:"account_id"`
	Classification string     `json:"classification"`
	State          string     `json:"state"`
	CreatedAt      time.Time  `json:"created_at"`
	DecidedAt      *time.Time `json:"decided_at"`
}
type Association struct {
	ID             string     `json:"association_id"`
	AccountID      string     `json:"account_id"`
	AgencyID       string     `json:"agency_id"`
	Classification string     `json:"classification"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until"`
}
type Policy struct {
	ID            string        `json:"policy_id"`
	PreviousID    string        `json:"previous_policy_id"`
	Capacity      int           `json:"capacity"`
	Seats         int           `json:"seats"`
	Compatibility Compatibility `json:"compatibility"`
	CreatedAt     time.Time     `json:"created_at"`
}

type Command struct {
	Admin          bool
	Policy         *Policy
	Kind           string
	ID             string
	Target         string
	Key            string
	Reason         string
	Name           string
	Classification string
	Status         string
	Role           string
	Seat           int32
	Capacity       int32
	Seats          int32
	Muted          bool
}

func Text(s string, max int) bool {
	if len(s) < 1 || len(s) > max || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (c Command) Validate(l Limits) error {
	if !Text(c.Key, 128) || !access.ValidReason(c.Reason) || c.ID != "" && !access.ValidID(c.ID) || c.Target != "" && !access.ValidID(c.Target) {
		return Fail(apperror.ValidationFailed)
	}
	switch c.Kind {
	case "agency.create", "room.create", "host.leave", "policy.publish":
	default:
		if c.Kind != "host.classify" && !access.ValidID(c.ID) {
			return Fail(apperror.ValidationFailed)
		}
	}
	switch c.Kind {
	case "manager.add", "manager.remove", "host.classify", "seat.invite", "seat.remove", "member.role", "member.mute", "member.kick", "member.ban", "member.unban":
		if !access.ValidID(c.Target) {
			return Fail(apperror.ValidationFailed)
		}
	}
	switch c.Kind {
	case "policy.publish":
		if c.Policy == nil || c.Policy.Capacity < 2 || c.Policy.Capacity > l.Capacity || c.Policy.Seats < 1 || c.Policy.Seats > l.Seats || c.Policy.PreviousID != "" && !access.ValidID(c.Policy.PreviousID) {
			return Fail(apperror.ValidationFailed)
		}
	case "agency.create":
		if !Text(c.Name, 128) {
			return Fail(apperror.ValidationFailed)
		}
	case "agency.update":
		if !Text(c.Name, 128) || c.Status != "active" && c.Status != "pending" && c.Status != "restricted" && c.Status != "rejected" {
			return Fail(apperror.ValidationFailed)
		}
	case "host.apply", "host.classify":
		if !agency.ValidClassification(c.Classification) {
			return Fail(apperror.ValidationFailed)
		}
	case "host.decide":
		if c.Status != "approved" && c.Status != "rejected" {
			return Fail(apperror.ValidationFailed)
		}
	case "room.create":
		if c.Status != "audio" && c.Status != "video" || !Text(c.Name, 128) || c.Capacity < 2 || c.Capacity > int32(l.Capacity) || c.Seats < 0 || c.Seats > int32(l.Seats) || c.Status == "video" && c.Seats != 0 {
			return Fail(apperror.ValidationFailed)
		}
	case "seat.invite":
		if c.Seat < 1 || c.Seat > int32(l.Seats) {
			return Fail(apperror.ValidationFailed)
		}
	case "member.role":
		if c.Role != "co_host" && c.Role != "moderator" && c.Role != "audience" {
			return Fail(apperror.ValidationFailed)
		}
	case "manager.add", "manager.remove", "host.leave", "room.close", "room.join", "room.leave", "seat.accept", "seat.release", "seat.remove", "member.mute", "member.kick", "member.ban", "member.unban":
	default:
		return Fail(apperror.ValidationFailed)
	}
	return nil
}
func Fingerprint(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}

type Result struct {
	ProviderState string `json:"provider_state,omitempty"`
	Data          any    `json:"data"`
	RoomID        string `json:"room_id,omitempty"`
	OperationID   string `json:"operation_id,omitempty"`
}
type Query struct {
	Kind, ID, Cursor string
	Limit            int
}
type Page struct {
	Data any
	Next string
	More bool
}
type Grant struct {
	Room     Room
	Member   Member
	Identity string
}

func CanPublish(r Room, m Member) bool {
	return m.State == "active" && !m.Muted && (m.Role == "host" || m.Role == "co_host" || r.Kind == "audio" && m.Seat > 0)
}

type Event struct {
	ID, Type, SessionID, RoomSID, AccountID, ParticipantSID string
	At                                                      time.Time
}
type Participant struct{ Identity, SID string }
type Snapshot struct {
	Exists       bool
	Participants []Participant
}

type Provider interface {
	Create(context.Context, Room) error
	Close(context.Context, Room) error
	Participant(context.Context, Room, Member) error
	Snapshot(context.Context, Room) (Snapshot, error)
	Token(Grant, time.Duration) (string, error)
}
type Store interface {
	Execute(context.Context, access.Credentials, Command) (Result, error)
	Read(context.Context, access.Credentials, Query) (Page, error)
	Grant(context.Context, access.Credentials, string) (Grant, error)
	Flush(context.Context, string, Provider) error
	FlushSession(context.Context, string, Provider) error
	Ingest(context.Context, Event) error
	Reconcile(context.Context, access.Credentials, string, Provider) error
}
type Service struct {
	store    Store
	provider Provider
	limits   Limits
	endpoint string
}

func New(s Store, p Provider, l Limits, endpoint string) (*Service, error) {
	if s == nil || p == nil {
		return nil, errors.New("live: dependencies required")
	}
	if e := l.Validate(); e != nil {
		return nil, e
	}
	return &Service{s, p, l, strings.Replace(endpoint, "https://", "wss://", 1)}, nil
}
func (s *Service) Execute(ctx context.Context, c access.Credentials, in Command) (Result, error) {
	if e := in.Validate(s.limits); e != nil {
		return Result{}, e
	}
	out, e := s.store.Execute(ctx, c, in)
	if e == nil && out.RoomID != "" { // A durable pending operation is an accepted outcome even when provider is unavailable.
		if flushErr := s.store.Flush(ctx, out.RoomID, s.provider); flushErr != nil {
			out.ProviderState = "pending"
		} else {
			out.ProviderState = "synchronized"
		}
	}
	return out, e
}
func (s *Service) Read(ctx context.Context, c access.Credentials, q Query) (Page, error) {
	if q.Limit < 1 || q.Limit > 50 || q.Cursor != "" && (q.Kind != "activity" && !access.ValidID(q.Cursor) || q.Kind == "activity" && !Text(q.Cursor, 128)) || q.ID != "" && !access.ValidID(q.ID) {
		return Page{}, Fail(apperror.ValidationFailed)
	}
	return s.store.Read(ctx, c, q)
}
func (s *Service) Token(ctx context.Context, c access.Credentials, id string) (any, error) {
	if !access.ValidID(id) {
		return nil, Fail(apperror.ValidationFailed)
	}
	g, e := s.store.Grant(ctx, c, id)
	if e != nil {
		return nil, e
	}
	token, e := s.provider.Token(g, s.limits.TokenTTL)
	if e != nil {
		return nil, Fail(apperror.ServiceUnavailable)
	}
	return struct {
		Token     string `json:"token"`
		URL       string `json:"url"`
		ExpiresIn int    `json:"expires_in_seconds"`
		SessionID string `json:"session_id"`
	}{token, s.endpoint, int(s.limits.TokenTTL.Seconds()), g.Room.SessionID}, nil
}
func (s *Service) Ingest(ctx context.Context, e Event) error {
	if err := s.store.Ingest(ctx, e); err != nil {
		return err
	}
	switch e.Type {
	case "participant_joined", "participant_left", "room_finished", "room_started":
		return s.store.FlushSession(ctx, e.SessionID, s.provider)
	}
	return nil
}
func (s *Service) Reconcile(ctx context.Context, c access.Credentials, id string) error {
	if !access.ValidID(id) {
		return Fail(apperror.ValidationFailed)
	}
	return s.store.Reconcile(ctx, c, id, s.provider)
}
