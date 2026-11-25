// Package httpapi exposes strict profile and scoped administration contracts.
package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
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
)

type Handler struct {
	s         *access.Service
	responses *response.Writer
	decoder   *validation.Decoder
}

func New(s *access.Service, logger *slog.Logger) (*Handler, error) {
	if s == nil {
		return nil, errors.New("access: service required")
	}
	w, e := response.New(logger)
	if e != nil {
		return nil, e
	}
	d, e := validation.NewDecoder(16 << 10)
	if e != nil {
		return nil, e
	}
	return &Handler{s, w, d}, nil
}
func credentials(r *http.Request) access.Credentials {
	a := r.Header.Get("Authorization")
	if !strings.HasPrefix(a, "Bearer ") {
		return access.Credentials{}
	}
	return access.Credentials{Access: strings.TrimPrefix(a, "Bearer "), Installation: r.Header.Get("X-Installation-ID")}
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, code string, data any, err error, options response.Options) {
	w.Header().Set("Cache-Control", "no-store")
	var e error
	if err != nil {
		if apperror.Public(err).Code() == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		}
		e = h.responses.Error(w, r, err)
	} else {
		e = h.responses.Success(w, r, status, code, data, options)
	}
	if e != nil {
		panic(http.ErrAbortHandler)
	}
}
func (h *Handler) result(w http.ResponseWriter, r *http.Request, code string, data any, err error) {
	h.write(w, r, 200, code, data, err, response.Options{})
}
func (h *Handler) Mount(r chi.Router) {
	r.Get("/profiles/me", h.own)
	r.Patch("/profiles/me", h.patchOwn)
	r.Get("/profiles/{account_id}", h.public)
	r.Get("/identity/me/permissions", h.permissions)
	r.Get("/admin/users", h.directory)
	r.Get("/admin/users/{account_id}", h.user)
	r.Get("/admin/users/{account_id}/private", h.private)
	r.Patch("/admin/users/{account_id}/profile", h.patchAdmin)
	r.Put("/admin/users/{account_id}/restriction", h.restrict)
	r.Get("/admin/users/{account_id}/roles", h.assignments)
	r.Put("/admin/users/{account_id}/roles", h.assign)
	r.Get("/admin/roles", h.roles)
	r.Post("/admin/roles", func(w http.ResponseWriter, r *http.Request) { h.putRole(w, r, true) })
	r.Put("/admin/roles/{role_id}", func(w http.ResponseWriter, r *http.Request) { h.putRole(w, r, false) })
	r.Delete("/admin/roles/{role_id}", h.deleteRole)
}
func (h *Handler) own(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Profile(r.Context(), credentials(r), "")
	h.result(w, r, "PROFILE_FETCHED", v, e)
}
func (h *Handler) public(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Profile(r.Context(), credentials(r), chi.URLParam(r, "account_id"))
	h.result(w, r, "PROFILE_FETCHED", v, e)
}
func (h *Handler) permissions(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Permissions(r.Context(), credentials(r))
	h.result(w, r, "PERMISSIONS_FETCHED", v, e)
}
func (h *Handler) user(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.User(r.Context(), credentials(r), chi.URLParam(r, "account_id"))
	h.result(w, r, "USER_FETCHED", v, e)
}
func (h *Handler) private(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Private(r.Context(), credentials(r), chi.URLParam(r, "account_id"))
	h.result(w, r, "USER_PRIVATE_FETCHED", v, e)
}
func (h *Handler) directory(w http.ResponseWriter, r *http.Request) {
	q, e := requestQuery(r)
	limit := 20
	if e == nil && q["limit"] != "" {
		limit, e = strconv.Atoi(q["limit"])
	}
	if e != nil {
		h.result(w, r, "USERS_FETCHED", nil, access.Failure(apperror.ValidationFailed))
		return
	}
	p, e := h.s.Directory(r.Context(), credentials(r), q["cursor"], limit)
	if e == nil {
		p, e = fitDirectoryPage(r, p)
	}
	h.write(w, r, 200, "USERS_FETCHED", p.Users, e, directoryOptions(p))
}
func requestQuery(r *http.Request) (map[string]string, error) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	out := map[string]string{}
	for k, v := range q {
		if (k != "limit" && k != "cursor") || len(v) != 1 || v[0] == "" {
			e = errors.New("invalid pagination")
		}
		out[k] = v[0]
	}
	return out, e
}

// RawMessage preserves presence/null. Decoder still rejects unknown/duplicate keys.
type patchRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
	Bio         json.RawMessage `json:"bio"`
}
type adminPatchRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
	Bio         json.RawMessage `json:"bio"`
	Reason      string          `json:"reason"`
}

func patch(name, bio json.RawMessage) (profiles.Patch, error) {
	var p profiles.Patch
	for _, v := range []struct {
		raw json.RawMessage
		dst **string
	}{{name, &p.DisplayName}, {bio, &p.Bio}} {
		if v.raw == nil {
			continue
		}
		if string(v.raw) == "null" {
			return p, access.Failure(apperror.ValidationFailed)
		}
		var s string
		if json.Unmarshal(v.raw, &s) != nil {
			return p, access.Failure(apperror.ValidationFailed)
		}
		*v.dst = &s
	}
	return p, p.Validate()
}
func (h *Handler) patchOwn(w http.ResponseWriter, r *http.Request) {
	var in patchRequest
	e := h.decoder.Decode(r, &in)
	var v profiles.Profile
	if e == nil {
		var p profiles.Patch
		p, e = patch(in.DisplayName, in.Bio)
		if e == nil {
			v, e = h.s.UpdateProfile(r.Context(), credentials(r), "", p, "", "")
		}
	}
	h.result(w, r, "PROFILE_UPDATED", v, e)
}
func (h *Handler) patchAdmin(w http.ResponseWriter, r *http.Request) {
	var in adminPatchRequest
	e := h.decoder.Decode(r, &in)
	var v profiles.Profile
	if e == nil {
		var p profiles.Patch
		p, e = patch(in.DisplayName, in.Bio)
		if e == nil {
			v, e = h.s.UpdateProfile(r.Context(), credentials(r), chi.URLParam(r, "account_id"), p, in.Reason, requestmeta.ID(r.Context()))
		}
	}
	h.result(w, r, "PROFILE_UPDATED", v, e)
}
func (h *Handler) restrict(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Restricted *bool  `json:"restricted"`
		Reason     string `json:"reason"`
	}
	e := h.decoder.Decode(r, &in)
	if e == nil {
		if in.Restricted == nil {
			e = access.Failure(apperror.ValidationFailed)
		} else {
			e = h.s.SetRestriction(r.Context(), credentials(r), chi.URLParam(r, "account_id"), *in.Restricted, in.Reason, requestmeta.ID(r.Context()))
		}
	}
	h.result(w, r, "RESTRICTION_UPDATED", struct {
		Updated bool `json:"updated"`
	}{true}, e)
}
func (h *Handler) roles(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Roles(r.Context(), credentials(r))
	h.result(w, r, "ROLES_FETCHED", v, e)
}
func (h *Handler) putRole(w http.ResponseWriter, r *http.Request, create bool) {
	var in struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
		Reason      string   `json:"reason"`
	}
	e := h.decoder.Decode(r, &in)
	var v access.Role
	if e == nil {
		v, e = h.s.PutRole(r.Context(), credentials(r), chi.URLParam(r, "role_id"), in.Name, in.Permissions, create, in.Reason, requestmeta.ID(r.Context()))
	}
	status := 200
	if create {
		status = 201
	}
	h.write(w, r, status, "ROLE_SAVED", v, e, response.Options{})
}
func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	e := h.decoder.Decode(r, &in)
	if e == nil {
		e = h.s.DeleteRole(r.Context(), credentials(r), chi.URLParam(r, "role_id"), in.Reason, requestmeta.ID(r.Context()))
	}
	h.result(w, r, "ROLE_DELETED", struct {
		Deleted bool `json:"deleted"`
	}{true}, e)
}
func (h *Handler) assignments(w http.ResponseWriter, r *http.Request) {
	v, e := h.s.Assignments(r.Context(), credentials(r), chi.URLParam(r, "account_id"))
	h.result(w, r, "ASSIGNMENTS_FETCHED", struct {
		RoleIDs []string `json:"role_ids"`
	}{v}, e)
}
func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RoleIDs []string `json:"role_ids"`
		Reason  string   `json:"reason"`
	}
	e := h.decoder.Decode(r, &in)
	if e == nil {
		e = h.s.Assign(r.Context(), credentials(r), chi.URLParam(r, "account_id"), in.RoleIDs, in.Reason, requestmeta.ID(r.Context()))
	}
	h.result(w, r, "ASSIGNMENTS_UPDATED", struct {
		Updated bool `json:"updated"`
	}{true}, e)
}
