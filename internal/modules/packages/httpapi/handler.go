// Package httpapi exposes bounded package and effective-capability resources.
package httpapi

import (
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/packages"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Service interface {
	Execute(context.Context, access.Credentials, packages.Command) (any, error)
	Read(context.Context, access.Credentials, packages.Query) (packages.Page, error)
	Purchase(context.Context, access.Credentials, string, string) (packages.Purchase, error)
}
type Powers interface {
	Powers(context.Context, access.Credentials, string) (access.Effective, error)
	CapabilityPolicy(context.Context, access.Credentials, *access.PolicyChange) (any, error)
	Override(context.Context, access.Credentials, string, access.OverrideChange) error
	CreateModeratorRole(context.Context, access.Credentials, string, string, string) (access.Role, error)
}
type Handler struct {
	s Service
	p Powers
	w *response.Writer
	d *validation.Decoder
}

func New(s Service, p Powers, l *slog.Logger) (*Handler, error) {
	if s == nil || p == nil {
		return nil, errors.New("packages: dependencies")
	}
	w, e := response.New(l)
	if e != nil {
		return nil, e
	}
	d, e := validation.NewNonNullSnakeCaseDecoder(16 << 10)
	if e != nil {
		return nil, e
	}
	return &Handler{s, p, w, d}, nil
}
func credentials(r *http.Request) access.Credentials {
	a, i := r.Header.Values("Authorization"), r.Header.Values("X-Installation-ID")
	if len(a) != 1 || len(i) != 1 || !strings.HasPrefix(a[0], "Bearer ") {
		return access.Credentials{}
	}
	return access.Credentials{Access: strings.TrimPrefix(a[0], "Bearer "), Installation: i[0]}
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, v any, e error, o response.Options) {
	w.Header().Set("Cache-Control", "no-store")
	if e != nil {
		if apperror.Public(e).Code() == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		}
		if apperror.Public(e).Code() == apperror.ServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		e = h.w.Error(w, r, e)
	} else {
		e = h.w.Success(w, r, 200, "CAPABILITY_RESOURCE_COMPLETED", v, o)
	}
	if e != nil {
		panic(http.ErrAbortHandler)
	}
}
func invalid() error { return access.Failure(apperror.ValidationFailed) }
func query(r *http.Request, allowed ...string) (url.Values, error) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return nil, invalid()
	}
	for k, v := range q {
		ok := false
		for _, a := range allowed {
			if k == a {
				ok = true
			}
		}
		if !ok || len(v) != 1 || v[0] == "" {
			return nil, invalid()
		}
	}
	return q, nil
}
func (h *Handler) Mount(r chi.Router) {
	r.Get("/identity/me/powers", h.powers)
	r.Get("/admin/capabilities", h.catalog)
	r.Post("/admin/global-moderator-roles", h.moderatorRole)
	r.Get("/admin/capabilities/room-policy", h.policy)
	r.Put("/admin/capabilities/room-policy", h.policy)
	r.Put("/admin/users/{account_id}/capabilities", h.override)
	for path, kind := range map[string]string{"/packages": "packages", "/packages/{id}": "package", "/identity/me/packages": "entitlements", "/identity/me/package-purchases": "my.purchases", "/admin/packages": "admin.packages", "/admin/packages/{id}/history": "history", "/admin/packages/{id}/subscribers": "subscribers", "/admin/package-purchases": "purchases"} {
		r.Get(path, h.read(kind))
	}
	r.Post("/admin/packages", h.command("save"))
	r.Put("/admin/packages/{id}", h.command("save"))
	r.Post("/admin/packages/{id}/entitlements", h.command("grant"))
	r.Post("/admin/package-entitlements/{id}/revocations", h.command("revoke"))
	r.Post("/packages/{id}/purchases", h.purchase)
}
func (h *Handler) read(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, e := query(r, "cursor", "limit")
		limit := 50
		if e == nil && q.Get("limit") != "" {
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil {
				e = invalid()
			}
		}
		var p packages.Page
		if e == nil {
			p, e = h.s.Read(r.Context(), credentials(r), packages.Query{Kind: kind, ID: chi.URLParam(r, "id"), Cursor: q.Get("cursor"), Limit: limit})
		}
		o := response.Options{}
		if kind != "package" && e == nil {
			o = pagination(p)
		}
		h.write(w, r, p.Items, e, o)
	}
}
func (h *Handler) command(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Definition *definitionRequest `json:"definition"`
			AccountID  string             `json:"account_id"`
			Reason     string             `json:"reason"`
		}
		_, e := query(r)
		key, ke := commandKey(r)
		if e == nil {
			e = ke
		}
		if e == nil {
			e = h.d.Decode(r, &body)
		}
		var definition *packages.Definition
		if body.Definition != nil {
			d := body.Definition
			if d.Active == nil || d.Priority == nil {
				e = invalid()
			} else {
				definition = &packages.Definition{Code: d.Code, Previous: d.Previous, Name: d.Name, Description: d.Description, Price: d.Price, Duration: d.Duration, Powers: d.Powers, Badge: d.Badge, Entrance: d.Entrance, Priority: *d.Priority, Active: *d.Active}
			}
		}
		if kind == "save" && body.AccountID != "" || kind != "save" && body.Definition != nil || kind == "revoke" && body.AccountID != "" {
			e = invalid()
		}
		var out any
		if e == nil {
			out, e = h.s.Execute(r.Context(), credentials(r), packages.Command{Kind: kind, ID: chi.URLParam(r, "id"), Target: body.AccountID, Reason: body.Reason, Key: key, Definition: definition})
		}
		h.write(w, r, out, e, response.Options{})
	}
}
func (h *Handler) purchase(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	_, e := query(r)
	key, ke := commandKey(r)
	if e == nil {
		e = ke
	}
	if e == nil {
		e = h.d.Decode(r, &body)
	}
	var v packages.Purchase
	if e == nil {
		v, e = h.s.Purchase(r.Context(), credentials(r), chi.URLParam(r, "id"), key)
	}
	h.write(w, r, v, e, response.Options{})
}
func (h *Handler) powers(w http.ResponseWriter, r *http.Request) {
	q, e := query(r, "room_id")
	var v access.Effective
	if e == nil {
		v, e = h.p.Powers(r.Context(), credentials(r), q.Get("room_id"))
	}
	h.write(w, r, v, e, response.Options{})
}
func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	_, e := query(r)
	if e == nil {
		_, e = h.p.CapabilityPolicy(r.Context(), credentials(r), nil)
	}
	h.write(w, r, map[string]any{"capabilities": access.Capabilities(), "global_moderator_default_capabilities": access.ModeratorDefaults()}, e, response.Options{})
}
func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
	_, e := query(r)
	var in *access.PolicyChange
	if r.Method == http.MethodPut {
		in = &access.PolicyChange{}
		if e == nil {
			e = h.d.Decode(r, in)
		}
	}
	var out any
	if e == nil {
		out, e = h.p.CapabilityPolicy(r.Context(), credentials(r), in)
	}
	h.write(w, r, out, e, response.Options{})
}
func (h *Handler) override(w http.ResponseWriter, r *http.Request) {
	_, e := query(r)
	var body struct {
		Action    string    `json:"action"`
		RoomID    string    `json:"room_id"`
		Allowed   *bool     `json:"allowed"`
		ExpiresAt time.Time `json:"expires_at"`
		Remove    bool      `json:"remove"`
		Reason    string    `json:"reason"`
	}
	if e == nil {
		e = h.d.Decode(r, &body)
	}
	if e == nil && !body.Remove && body.Allowed == nil {
		e = invalid()
	}
	if e == nil {
		allowed := false
		if body.Allowed != nil {
			allowed = *body.Allowed
		}
		e = h.p.Override(r.Context(), credentials(r), chi.URLParam(r, "account_id"), access.OverrideChange{Action: body.Action, RoomID: body.RoomID, Allowed: allowed, ExpiresAt: body.ExpiresAt, Remove: body.Remove, Reason: body.Reason})
	}
	h.write(w, r, struct{}{}, e, response.Options{})
}

func pagination(p packages.Page) response.Options {
	var next *string
	if p.More {
		next = &p.Next
	}
	return response.Options{Meta: &response.Meta{Pagination: response.Pagination{Limit: p.Limit, NextCursor: next, HasMore: p.More}}}
}

type definitionRequest struct {
	Code        string   `json:"code"`
	Previous    string   `json:"previous_version_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       int64    `json:"diamond_price"`
	Duration    int64    `json:"duration_seconds"`
	Powers      []string `json:"powers"`
	Badge       string   `json:"badge_asset_id"`
	Entrance    string   `json:"entrance_asset_id"`
	Priority    *int32   `json:"live_listing_priority"`
	Active      *bool    `json:"active"`
}

func (h *Handler) moderatorRole(w http.ResponseWriter, r *http.Request) {
	_, e := query(r)
	var body struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if e == nil {
		e = h.d.Decode(r, &body)
	}
	var out access.Role
	if e == nil {
		out, e = h.p.CreateModeratorRole(r.Context(), credentials(r), body.Name, body.Reason, requestmeta.ID(r.Context()))
	}
	h.write(w, r, out, e, response.Options{})
}

func commandKey(r *http.Request) (string, error) {
	v := r.Header.Values("Idempotency-Key")
	if len(v) != 1 || !access.ValidCorrelation(v[0]) {
		return "", invalid()
	}
	return v[0], nil
}
