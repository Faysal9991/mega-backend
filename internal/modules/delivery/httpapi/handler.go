// Package httpapi mounts strict public/admin delivery projections.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
)

type Service interface {
	Attachment(context.Context, access.Credentials, string, string) (any, error)
	Retry(context.Context, access.Credentials, string, string, string) (any, error)
	Gifts(context.Context, access.Credentials, string, string, int, bool) (delivery.Page, error)
	PutGift(context.Context, access.Credentials, string, string, delivery.GiftChange) (any, error)
	RecipientPolicy(context.Context, access.Credentials, string, string, string) (any, error)
	GiftHistory(context.Context, access.Credentials, string, int, bool) (delivery.Page, error)
	Upload(context.Context, access.Credentials, string, delivery.Upload) (delivery.SignedUpload, error)
	Asset(context.Context, access.Credentials, string) (delivery.Asset, error)
	Complete(context.Context, access.Credentials, string) (delivery.Asset, error)
	Attach(context.Context, access.Credentials, string, string, string) (any, error)
	RegisterWithPrevious(context.Context, access.Credentials, string, string, string) error
	RemoveDevice(context.Context, access.Credentials) error
	Preferences(context.Context, access.Credentials, *delivery.Preferences) (delivery.Preferences, error)
	NotificationHistory(context.Context, access.Credentials, string, int) (delivery.Page, error)
	Announcement(context.Context, access.Credentials, string, delivery.Announcement) (any, error)
	Deliveries(context.Context, access.Credentials, string, int) (delivery.Page, error)
}
type Wallet interface {
	Execute(context.Context, access.Credentials, wallet.Command) (any, error)
}
type Handler struct {
	s       Service
	wallet  Wallet
	w       *response.Writer
	decoder *validation.Decoder
}

func New(s Service, w Wallet, l *slog.Logger) (*Handler, error) {
	if s == nil || w == nil {
		return nil, errors.New("delivery: dependencies required")
	}
	writer, e := response.New(l)
	if e != nil {
		return nil, e
	}
	decoder, e := validation.NewSnakeCaseDecoder(32 << 10)
	if e != nil {
		return nil, e
	}
	return &Handler{s, w, writer, decoder}, nil
}
func Credentials(r *http.Request) access.Credentials {
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
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, data any, e error, o response.Options) {
	w.Header().Set("Cache-Control", "no-store")
	var failure error
	if e != nil {
		code := apperror.Public(e).Code()
		if code == apperror.ServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		if code == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		}
		failure = h.w.Error(w, r, e)
	} else {
		failure = h.w.Success(w, r, status, "DELIVERY_REQUEST_COMPLETED", data, o)
	}
	if failure != nil {
		panic(http.ErrAbortHandler)
	}
}
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, v any) error {
	if e := h.decoder.Decode(r, v); e != nil {
		return e
	}
	return nil
}
func (h *Handler) Mount(r chi.Router) {
	r.Get("/gifts", h.list("gifts", false))
	r.Get("/gifts/{id}", h.list("gift", false))
	r.Post("/rooms/{id}/gifts", h.mutate("send"))
	r.Get("/gift-history", h.list("gift_history", false))
	r.Get("/admin/gifts", h.list("gifts", true))
	r.Post("/admin/gifts", h.mutate("gift.create"))
	r.Put("/admin/gifts/{id}", h.mutate("gift.update"))
	r.Post("/admin/gift-recipient-policies", h.mutate("recipient_policy"))
	r.Get("/admin/gift-history", h.list("gift_history", true))
	r.Get("/media/attachments/{purpose}/{target}", h.mutate("attachment.get"))
	r.Post("/media/upload-intents", h.mutate("upload"))
	r.Get("/media/assets/{id}", h.mutate("asset"))
	r.Post("/media/assets/{id}/complete", h.mutate("complete"))
	r.Put("/media/assets/{id}/attachment", h.mutate("attach"))
	r.Put("/push/device", h.mutate("register"))
	r.Delete("/push/device", h.mutate("unregister"))
	r.Get("/notification-preferences", h.mutate("prefs.get"))
	r.Put("/notification-preferences", h.mutate("prefs.put"))
	r.Get("/notifications", h.list("notifications", false))
	r.Post("/admin/announcements", h.mutate("announcement"))
	r.Post("/admin/deliveries/{id}/retry", h.mutate("retry"))
	r.Get("/admin/deliveries", h.list("deliveries", true))
}
func (h *Handler) list(kind string, admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, e := url.ParseQuery(r.URL.RawQuery)
		limit := 20
		for k, v := range query {
			if k != "limit" && k != "cursor" || len(v) != 1 || v[0] == "" {
				e = delivery.Fail(apperror.ValidationFailed)
			}
		}
		if v := query.Get("limit"); v != "" {
			var parseErr error
			limit, parseErr = strconv.Atoi(v)
			if parseErr != nil {
				e = parseErr
			}
		}
		cursor := query.Get("cursor")
		if e != nil || !delivery.ValidPage(cursor, limit) {
			h.write(w, r, 0, nil, delivery.Fail(apperror.ValidationFailed), response.Options{})
			return
		}
		var page delivery.Page
		c := Credentials(r)
		switch kind {
		case "gifts", "gift":
			page, e = h.s.Gifts(r.Context(), c, chi.URLParam(r, "id"), cursor, limit, admin)
		case "gift_history":
			page, e = h.s.GiftHistory(r.Context(), c, cursor, limit, admin)
		case "notifications":
			page, e = h.s.NotificationHistory(r.Context(), c, cursor, limit)
		case "deliveries":
			page, e = h.s.Deliveries(r.Context(), c, cursor, limit)
		}
		options := response.Options{}
		if kind != "gift" {
			var next *string
			if page.More {
				next = &page.Next
			}
			options.Meta = &response.Meta{Pagination: response.Pagination{Limit: limit, NextCursor: next, HasMore: page.More}}
		}
		h.write(w, r, 200, page.Data, e, options)
	}
}

type giftDTO struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Price       *int64  `json:"diamond_price"`
	Category    string  `json:"category"`
	Thumbnail   string  `json:"thumbnail_asset_id"`
	Animation   string  `json:"animation_url"`
	Active      *bool   `json:"active"`
	Order       *int32  `json:"display_order"`
	Previous    *string `json:"previous_version_id"`
	Reason      string  `json:"reason"`
}

func (h *Handler) mutate(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var out any
		var e error
		if len(r.Header.Values("Idempotency-Key")) > 1 || r.URL.RawQuery != "" {
			h.write(w, r, 0, nil, delivery.Fail(apperror.ValidationFailed), response.Options{})
			return
		}
		c := Credentials(r)
		id := chi.URLParam(r, "id")
		key := r.Header.Get("Idempotency-Key")
		status := 200
		switch kind {
		case "gift.create", "gift.update":
			var in giftDTO
			e = h.decode(w, r, &in)
			if e == nil {
				if in.Price == nil || in.Active == nil || in.Order == nil || in.Previous == nil {
					e = delivery.Fail(apperror.ValidationFailed)
				} else {
					out, e = h.s.PutGift(r.Context(), c, id, key, delivery.GiftChange{Gift: delivery.Gift{Code: in.Code, Name: in.Name, Description: in.Description, Price: *in.Price, Category: in.Category, ThumbnailAssetID: in.Thumbnail, AnimationURL: in.Animation, Active: *in.Active, DisplayOrder: *in.Order}, PreviousID: *in.Previous, Reason: in.Reason})
					if kind == "gift.create" {
						status = 201
					}
				}
			}
		case "recipient_policy":
			var in struct {
				Previous *string `json:"previous_policy_id"`
				Mode     string  `json:"mode"`
				Reason   string  `json:"reason"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				if in.Previous == nil || in.Mode != "active_publishers" {
					e = delivery.Fail(apperror.ValidationFailed)
				} else {
					out, e = h.s.RecipientPolicy(r.Context(), c, key, *in.Previous, in.Reason)
				}
			}
		case "send":
			var in struct {
				Session   string `json:"session_id"`
				Recipient string `json:"recipient_id"`
				Gift      string `json:"gift_id"`
				Quantity  int64  `json:"quantity"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				if !access.ValidID(id) || !access.ValidID(in.Session) {
					e = delivery.Fail(apperror.ValidationFailed)
				} else {
					out, e = h.wallet.Execute(r.Context(), c, wallet.Command{Kind: "gift", RoomID: id, SessionID: in.Session, Target: in.Recipient, GiftID: in.Gift, Quantity: in.Quantity, Key: key, Correlation: requestmeta.ID(r.Context())})
				}
			}
		case "upload":
			var in delivery.Upload
			e = h.decode(w, r, &in)
			if e == nil {
				out, e = h.s.Upload(r.Context(), c, key, in)
				status = 201
			}
		case "attachment.get":
			out, e = h.s.Attachment(r.Context(), c, chi.URLParam(r, "purpose"), chi.URLParam(r, "target"))
		case "asset":
			out, e = h.s.Asset(r.Context(), c, id)
		case "complete":
			var in struct{}
			e = h.decode(w, r, &in)
			if e == nil {
				out, e = h.s.Complete(r.Context(), c, id)
			}
		case "attach":
			var in struct {
				Expected *string `json:"expected_asset_id"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				if in.Expected == nil {
					e = delivery.Fail(apperror.ValidationFailed)
				} else {
					out, e = h.s.Attach(r.Context(), c, id, *in.Expected, key)
				}
			}
		case "register":
			var in struct {
				Platform string `json:"platform"`
				Token    string `json:"token"`
				Previous string `json:"previous_access_token"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				e = h.s.RegisterWithPrevious(r.Context(), c, in.Platform, in.Token, in.Previous)
			}
			out = map[string]bool{"registered": e == nil}
		case "unregister":
			e = h.s.RemoveDevice(r.Context(), c)
			out = map[string]bool{"removed": e == nil}
		case "prefs.get":
			out, e = h.s.Preferences(r.Context(), c, nil)
		case "prefs.put":
			var in struct {
				Gift    *bool `json:"gift_received"`
				Agency  *bool `json:"agency_decision"`
				Account *bool `json:"account_event"`
				Room    *bool `json:"room_invitation"`
				Admin   *bool `json:"admin_announcement"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				if in.Gift == nil || in.Agency == nil || in.Account == nil || in.Room == nil || in.Admin == nil {
					e = delivery.Fail(apperror.ValidationFailed)
				} else {
					p := delivery.Preferences{GiftReceived: *in.Gift, AgencyDecision: *in.Agency, AccountEvent: *in.Account, RoomInvitation: *in.Room, AdminAnnouncement: *in.Admin}
					out, e = h.s.Preferences(r.Context(), c, &p)
				}
			}
		case "retry":
			var in struct {
				Reason string `json:"reason"`
			}
			e = h.decode(w, r, &in)
			if e == nil {
				out, e = h.s.Retry(r.Context(), c, id, key, in.Reason)
			}
		case "announcement":
			var in delivery.Announcement
			e = h.decode(w, r, &in)
			if e == nil {
				out, e = h.s.Announcement(r.Context(), c, key, in)
				status = 202
			}
		}
		if kind == "send" && e == nil {
			out = map[string]any{"financial_operation": out, "financial_status": "committed", "delivery_status": "not_confirmed"}
		}
		h.write(w, r, status, out, e, response.Options{})
	}
}
