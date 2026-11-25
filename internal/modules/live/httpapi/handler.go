// Package httpapi defines safe mobile/admin projections and signed webhook ingress.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
)

type Service interface {
	Execute(context.Context, access.Credentials, live.Command) (live.Result, error)
	Read(context.Context, access.Credentials, live.Query) (live.Page, error)
	Token(context.Context, access.Credentials, string) (any, error)
	Ingest(context.Context, live.Event) error
	Reconcile(context.Context, access.Credentials, string) error
}
type Verifier func(*http.Request) (live.Event, error)
type Handler struct {
	service   Service
	verify    Verifier
	responses *response.Writer
	decoder   *validation.Decoder
}

func New(s Service, v Verifier, l *slog.Logger) (*Handler, error) {
	if s == nil || v == nil {
		return nil, errors.New("live: dependencies required")
	}
	w, e := response.New(l)
	if e != nil {
		return nil, e
	}
	d, e := validation.NewDecoder(4096)
	if e != nil {
		return nil, e
	}
	return &Handler{s, v, w, d}, nil
}
func credentials(r *http.Request) access.Credentials {
	a, i := r.Header.Values("Authorization"), r.Header.Values("X-Installation-ID")
	if len(a) != 1 || len(i) != 1 || !strings.HasPrefix(a[0], "Bearer ") {
		return access.Credentials{}
	}
	token := strings.TrimPrefix(a[0], "Bearer ")
	if strings.ContainsAny(token, " \t\r\n") {
		return access.Credentials{}
	}
	return access.Credentials{Access: token, Installation: i[0]}
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, code string, data any, e error, o response.Options) {
	w.Header().Set("Cache-Control", "no-store")
	var failure error
	if e != nil {
		if apperror.Public(e).Code() == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		} else if apperror.Public(e).Code() == apperror.ServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		failure = h.responses.Error(w, r, e)
	} else {
		failure = h.responses.Success(w, r, status, code, data, o)
	}
	if failure != nil {
		panic(http.ErrAbortHandler)
	}
}

type empty struct{}
type agencyCreate struct {
	Name string `json:"name"`
}
type agencyUpdate struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type hostApply struct {
	Classification string `json:"classification"`
}
type hostDecision struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}
type hostClassify struct {
	Classification string `json:"classification"`
	Reason         string `json:"reason"`
}
type roomCreate struct {
	Title    string `json:"title"`
	Type     string `json:"room_type"`
	Capacity int32  `json:"capacity"`
	Seats    *int32 `json:"seats"`
}
type seatInvite struct {
	Seat int32 `json:"seat"`
}
type memberRole struct {
	Role   string `json:"role"`
	Reason string `json:"reason"`
}
type memberMute struct {
	Muted  *bool  `json:"muted"`
	Reason string `json:"reason"`
}
type compatibilityDTO struct {
	AudioHostAudio *bool `json:"audio_host_audio"`
	AudioHostVideo *bool `json:"audio_host_video"`
	VideoHostAudio *bool `json:"video_host_audio"`
	VideoHostVideo *bool `json:"video_host_video"`
}

func (c *compatibilityDTO) valid() bool {
	return c != nil && c.AudioHostAudio != nil && c.AudioHostVideo != nil && c.VideoHostAudio != nil && c.VideoHostVideo != nil
}
func (c *compatibilityDTO) policy() live.Compatibility {
	return live.Compatibility{AudioHostAudio: *c.AudioHostAudio, AudioHostVideo: *c.AudioHostVideo, VideoHostAudio: *c.VideoHostAudio, VideoHostVideo: *c.VideoHostVideo}
}

type policyPublish struct {
	PreviousID    *string           `json:"previous_policy_id"`
	Capacity      int               `json:"capacity"`
	Seats         int               `json:"seats"`
	Compatibility *compatibilityDTO `json:"compatibility"`
	Reason        string            `json:"reason"`
}
type reason struct {
	Reason string `json:"reason"`
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/admin/live-policies", h.read("policy", false))
	r.Post("/admin/live-policies", h.command("policy.publish"))
	r.Get("/agencies/{id}", h.read("agency", false))
	r.Post("/agencies", h.command("agency.create"))
	r.Get("/admin/agencies", h.read("admin.agencies", true))
	r.Patch("/admin/agencies/{id}", h.command("agency.update"))
	r.Put("/agencies/{id}/managers/{target}", h.command("manager.add"))
	r.Delete("/agencies/{id}/managers/{target}", h.command("manager.remove"))
	r.Post("/agencies/{id}/host-applications", h.command("host.apply"))
	r.Get("/agencies/{id}/host-applications", h.read("applications", true))
	r.Post("/host-applications/{id}/decision", h.command("host.decide"))
	r.Post("/agency-membership/leave", h.command("host.leave"))
	r.Get("/agency-membership/history", h.read("my.history", true))
	r.Get("/admin/hosts/{id}/history", h.read("history", true))
	r.Patch("/admin/hosts/{target}/classification", h.command("host.classify"))
	r.Get("/rooms", h.read("rooms", true))
	r.Post("/rooms", h.command("room.create"))
	r.Get("/rooms/{id}", h.read("room", false))
	r.Post("/rooms/{id}/close", h.command("room.close"))
	r.Post("/rooms/{id}/join", h.command("room.join"))
	r.Post("/rooms/{id}/leave", h.command("room.leave"))
	r.Post("/rooms/{id}/token", h.token)
	r.Post("/rooms/{id}/seats/invitations/{target}", h.command("seat.invite"))
	r.Post("/rooms/{id}/seats/accept", h.command("seat.accept"))
	r.Post("/rooms/{id}/seats/release", h.command("seat.release"))
	r.Delete("/rooms/{id}/seats/{target}", h.command("seat.remove"))
	r.Patch("/rooms/{id}/members/{target}/role", h.command("member.role"))
	r.Patch("/rooms/{id}/members/{target}/mute", h.command("member.mute"))
	r.Post("/rooms/{id}/members/{target}/kick", h.command("member.kick"))
	r.Post("/rooms/{id}/members/{target}/ban", h.command("member.ban"))
	r.Delete("/rooms/{id}/members/{target}/ban", h.command("member.unban"))
	r.Get("/admin/rooms", h.read("admin.rooms", true))
	r.Get("/admin/rooms/{id}", h.read("admin.room", false))
	r.Post("/admin/rooms/{id}/close", h.command("room.close", true))
	r.Post("/admin/rooms/{id}/reconcile", h.reconcile)
	r.Get("/admin/live-activity/{id}", h.read("activity", true))
	r.Post("/providers/livekit/webhook", h.webhook)
}
func (h *Handler) command(kind string, admin ...bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in := live.Command{Kind: kind, ID: chi.URLParam(r, "id"), Target: chi.URLParam(r, "target"), Reason: kind, Admin: len(admin) > 0 && admin[0]}
		var e error
		if keys := r.Header.Values("Idempotency-Key"); len(keys) == 1 {
			in.Key = keys[0]
		}
		if r.URL.RawQuery != "" {
			e = live.Fail(apperror.ValidationFailed)
		} else {
			switch kind {
			case "policy.publish":
				var b policyPublish
				e = h.decoder.Decode(r, &b)
				in.Reason = b.Reason
				if (!b.Compatibility.valid() || b.PreviousID == nil) && e == nil {
					e = live.Fail(apperror.ValidationFailed)
				}
				if b.Compatibility.valid() && b.PreviousID != nil {
					in.Policy = &live.Policy{PreviousID: *b.PreviousID, Capacity: b.Capacity, Seats: b.Seats, Compatibility: b.Compatibility.policy()}
				}
			case "agency.create":
				var b agencyCreate
				e = h.decoder.Decode(r, &b)
				in.Name = b.Name
			case "agency.update":
				var b agencyUpdate
				e = h.decoder.Decode(r, &b)
				in.Name = b.Name
				in.Status = b.Status
				in.Reason = b.Reason
			case "host.apply":
				var b hostApply
				e = h.decoder.Decode(r, &b)
				in.Classification = b.Classification
			case "host.decide":
				var b hostDecision
				e = h.decoder.Decode(r, &b)
				in.Status = b.State
				in.Reason = b.Reason
			case "host.classify":
				var b hostClassify
				e = h.decoder.Decode(r, &b)
				in.Classification = b.Classification
				in.Reason = b.Reason
			case "room.create":
				var b roomCreate
				e = h.decoder.Decode(r, &b)
				in.Name = b.Title
				in.Status = b.Type
				in.Capacity = b.Capacity
				if b.Seats == nil {
					if e == nil {
						e = live.Fail(apperror.ValidationFailed)
					}
				} else {
					in.Seats = *b.Seats
				}
			case "seat.invite":
				var b seatInvite
				e = h.decoder.Decode(r, &b)
				in.Seat = b.Seat
			case "member.role":
				var b memberRole
				e = h.decoder.Decode(r, &b)
				in.Role = b.Role
				in.Reason = b.Reason
			case "member.mute":
				var b memberMute
				e = h.decoder.Decode(r, &b)
				in.Reason = b.Reason
				if e == nil && b.Muted == nil {
					e = live.Fail(apperror.ValidationFailed)
				}
				if b.Muted != nil {
					in.Muted = *b.Muted
				}
			case "member.kick", "member.ban", "member.unban", "seat.remove", "manager.add", "manager.remove":
				var b reason
				e = h.decoder.Decode(r, &b)
				in.Reason = b.Reason
			default:
				var b empty
				e = h.decoder.Decode(r, &b)
			}
		}
		var out live.Result
		if e == nil {
			out, e = h.service.Execute(r.Context(), credentials(r), in)
		}
		status := http.StatusOK
		if out.OperationID != "" {
			status = http.StatusAccepted
		}
		h.write(w, r, status, "LIVE_COMMAND_ACCEPTED", out, e, response.Options{})
	}
}
func query(r *http.Request, list bool) (live.Query, error) {
	q := live.Query{Limit: 20}
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return q, live.Fail(apperror.ValidationFailed)
	}
	for k, a := range v {
		if !list || len(a) != 1 || a[0] == "" {
			return q, live.Fail(apperror.ValidationFailed)
		}
		switch k {
		case "limit":
			q.Limit, e = strconv.Atoi(a[0])
			if e != nil || q.Limit < 1 || q.Limit > 50 {
				return q, live.Fail(apperror.ValidationFailed)
			}
		case "cursor":
			q.Cursor = a[0]
			if !live.Text(q.Cursor, 128) {
				return q, live.Fail(apperror.ValidationFailed)
			}
		default:
			return q, live.Fail(apperror.ValidationFailed)
		}
	}
	return q, nil
}
func (h *Handler) read(kind string, list bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r, list)
		q.Kind = kind
		q.ID = chi.URLParam(r, "id")
		var out live.Page
		if e == nil {
			out, e = h.service.Read(r.Context(), credentials(r), q)
		}
		o := response.Options{}
		if list && e == nil {
			var next *string
			if out.More {
				next = &out.Next
			}
			o.Meta = &response.Meta{Pagination: response.Pagination{Limit: q.Limit, NextCursor: next, HasMore: out.More}}
		}
		h.write(w, r, 200, "LIVE_RESOURCE_FETCHED", out.Data, e, o)
	}
}
func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	var b empty
	e := h.decoder.Decode(r, &b)
	if r.URL.RawQuery != "" {
		e = live.Fail(apperror.ValidationFailed)
	}
	var out any
	if e == nil {
		out, e = h.service.Token(r.Context(), credentials(r), chi.URLParam(r, "id"))
	}
	h.write(w, r, 200, "LIVE_TOKEN_ISSUED", out, e, response.Options{})
}
func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	var b empty
	e := h.decoder.Decode(r, &b)
	if r.URL.RawQuery != "" {
		e = live.Fail(apperror.ValidationFailed)
	}
	if e == nil {
		e = h.service.Reconcile(r.Context(), credentials(r), chi.URLParam(r, "id"))
	}
	h.write(w, r, 202, "LIVE_RECONCILIATION_ACCEPTED", struct{}{}, e, response.Options{})
}
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	controller := http.NewResponseController(w)
	if deadline, ok := ctx.Deadline(); ok {
		if e := controller.SetReadDeadline(deadline); e != nil && !errors.Is(e, http.ErrNotSupported) {
			h.write(w, r, 0, "", nil, live.Fail(apperror.ServiceUnavailable), response.Options{})
			return
		}
	}
	defer func() {
		if e := controller.SetReadDeadline(time.Time{}); e != nil && !errors.Is(e, http.ErrNotSupported) {
			cancel()
		}
	}()
	r = r.WithContext(ctx)
	if r.Body == nil || r.ContentLength > 64<<10 {
		h.write(w, r, 0, "", nil, live.Fail(apperror.PayloadTooLarge), response.Options{})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	event, e := h.verify(r)
	if e != nil {
		var large *http.MaxBytesError
		var timed net.Error
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(e, &timed) && timed.Timeout()) {
			e = live.Fail(apperror.RequestTimeout)
		} else if errors.As(e, &large) {
			e = live.Fail(apperror.PayloadTooLarge)
		} else {
			e = live.Fail(apperror.Unauthenticated)
		}
	} else {
		e = h.service.Ingest(ctx, event)
	}
	h.write(w, r, 200, "LIVE_EVENT_ACCEPTED", struct{}{}, e, response.Options{})
}
