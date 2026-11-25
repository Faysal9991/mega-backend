// Package httpapi translates strict HTTP DTOs into identity application calls.
package httpapi

import (
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

type Handler struct {
	s         *identity.Service
	responses *response.Writer
	decoder   *validation.Decoder
}

func New(s *identity.Service, logger *slog.Logger) (*Handler, error) {
	w, err := response.New(logger)
	if err != nil {
		return nil, err
	}
	d, err := validation.NewDecoder(24 << 10)
	if err != nil {
		return nil, err
	}
	return &Handler{s, w, d}, nil
}
func (h *Handler) Mount(r chi.Router) {
	r.Post("/auth/google/challenges", h.challenge)
	r.Post("/auth/google", h.google)
	r.Post("/auth/password/login", h.passwordLogin)
	r.Post("/auth/refresh", h.refresh)
	r.Post("/auth/logout", func(w http.ResponseWriter, r *http.Request) { h.logout(w, r, false) })
	r.Post("/auth/logout-all", func(w http.ResponseWriter, r *http.Request) { h.logout(w, r, true) })
	r.Put("/identity/identifiers", h.identifiers)
	r.Put("/identity/password", h.password)
	r.Get("/identity/me", h.current)
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, code string, data any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		if apperror.Public(err).Code() == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		}
		_ = h.responses.Error(w, r, err)
		return
	}
	_ = h.responses.Success(w, r, 200, code, data, response.Options{})
}
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := h.decoder.Decode(r, dst); err != nil {
		h.write(w, r, "", nil, err)
		return false
	}
	return true
}
func (h *Handler) abuse(r *http.Request, installation, identifier string, expensive bool) error {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = "unknown"
	}
	return h.s.Abuse(r.Context(), ip, installation, identifier, expensive)
}
func bearer(r *http.Request) (string, string, error) {
	values := r.Header.Values("Authorization")
	inst := r.Header.Values("X-Installation-ID")
	if len(values) != 1 || len(inst) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", "", apperror.New(apperror.Unauthenticated, nil)
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if strings.ContainsAny(token, " \t\r\n") {
		return "", "", apperror.New(apperror.Unauthenticated, nil)
	}
	return token, inst[0], nil
}
func (h *Handler) challenge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Installation string `json:"installation_id"`
		Purpose      string `json:"purpose"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	if err := h.abuse(r, in.Installation, "", false); err != nil {
		h.write(w, r, "", nil, err)
		return
	}
	out, err := h.s.Challenge(r.Context(), in.Installation, in.Purpose)
	h.write(w, r, "GOOGLE_CHALLENGE_CREATED", out, err)
}
func (h *Handler) google(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Installation string `json:"installation_id"`
		Challenge    string `json:"challenge_token"`
		Token        string `json:"id_token"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	if err := h.abuse(r, in.Installation, "", true); err != nil {
		h.write(w, r, "", nil, err)
		return
	}
	out, err := h.s.Google(r.Context(), in.Installation, in.Challenge, in.Token)
	if out.Tokens != nil {
		h.write(w, r, "AUTHENTICATED", out.Tokens, err)
	} else {
		h.write(w, r, "GOOGLE_PROOF_CREATED", struct {
			Proof   string `json:"proof_token"`
			Expires int    `json:"expires_in"`
		}{out.Proof, 300}, err)
	}
}
func (h *Handler) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Installation string `json:"installation_id"`
		Identifier   string `json:"identifier"`
		Password     string `json:"password"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	kind, value, e := identity.NormalizeIdentifier(in.Identifier)
	if e != nil {
		kind = "invalid"
		value = "invalid"
	}
	if err := h.abuse(r, in.Installation, kind+":"+value, true); err != nil {
		h.write(w, r, "", nil, err)
		return
	}
	out, err := h.s.PasswordLogin(r.Context(), in.Installation, in.Identifier, in.Password)
	h.write(w, r, "AUTHENTICATED", out, err)
}
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Installation string `json:"installation_id"`
		Token        string `json:"refresh_token"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	if err := h.abuse(r, in.Installation, "", false); err != nil {
		h.write(w, r, "", nil, err)
		return
	}
	out, err := h.s.Refresh(r.Context(), in.Installation, in.Token)
	h.write(w, r, "SESSION_REFRESHED", out, err)
}
func (h *Handler) identifiers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Phone    string `json:"phone"`
		Proof    string `json:"proof_token"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	token, inst, err := bearer(r)
	if err == nil {
		err = h.abuse(r, inst, "", false)
	}
	if err == nil {
		err = h.s.Identifiers(r.Context(), token, inst, in.Username, in.Phone, in.Proof)
	}
	h.write(w, r, "IDENTIFIERS_UPDATED", struct {
		Updated bool `json:"updated"`
	}{true}, err)
}
func (h *Handler) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Installation string `json:"installation_id"`
		Password     string `json:"password"`
		Proof        string `json:"proof_token"`
		Access       string `json:"access_token"`
		Current      string `json:"current_password"`
	}
	if !h.decode(w, r, &in) {
		return
	}
	err := h.abuse(r, in.Installation, "", true)
	if err == nil {
		err = h.s.SetPassword(r.Context(), identity.PasswordChange{Installation: in.Installation, Password: in.Password, ProofToken: in.Proof, AccessToken: in.Access, CurrentPassword: in.Current})
	}
	h.write(w, r, "PASSWORD_UPDATED", struct {
		Reauth bool `json:"reauthentication_required"`
	}{true}, err)
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request, all bool) {
	var in struct{}
	if !h.decode(w, r, &in) {
		return
	}
	token, inst, err := bearer(r)
	if err == nil {
		err = h.abuse(r, inst, "", false)
	}
	if err == nil {
		err = h.s.Logout(r.Context(), token, inst, all)
	}
	h.write(w, r, "LOGGED_OUT", struct {
		Done bool `json:"logged_out"`
	}{true}, err)
}
func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	token, inst, err := bearer(r)
	var out identity.Me
	if err == nil {
		var p identity.Principal
		p, err = h.s.Authenticate(r.Context(), token, inst)
		if err == nil {
			out, err = h.s.Current(r.Context(), p)
		}
	}
	h.write(w, r, "IDENTITY_FETCHED", out, err)
}
