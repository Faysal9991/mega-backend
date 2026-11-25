// Package httpapi maps strict financial DTOs to the single Wallet Engine.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/Faysal9991/mega-backend/internal/platform/validation"
	"github.com/go-chi/chi/v5"
)

// Service is a transport boundary; authority and settlement live in Wallet.
type Service interface {
	Read(context.Context, access.Credentials, wallet.Query) (wallet.ReadResult, error)
	Execute(context.Context, access.Credentials, wallet.Command) (any, error)
}
type Handler struct {
	service   Service
	responses *response.Writer
	decoder   *validation.Decoder
}

func New(service Service, logger *slog.Logger) (*Handler, error) {
	if service == nil {
		return nil, errors.New("wallet: service required")
	}
	responses, err := response.New(logger)
	if err != nil {
		return nil, err
	}
	decoder, err := validation.NewDecoder(32 << 10)
	if err != nil {
		return nil, err
	}
	return &Handler{service: service, responses: responses, decoder: decoder}, nil
}
func credentials(r *http.Request) access.Credentials {
	auths, installations := r.Header.Values("Authorization"), r.Header.Values("X-Installation-ID")
	if len(auths) != 1 || len(installations) != 1 || !strings.HasPrefix(auths[0], "Bearer ") {
		return access.Credentials{}
	}
	token := strings.TrimPrefix(auths[0], "Bearer ")
	if strings.ContainsAny(token, " \t\r\n") {
		return access.Credentials{}
	}
	return access.Credentials{Access: token, Installation: installations[0]}
}
func (h *Handler) write(w http.ResponseWriter, r *http.Request, status int, code string, data any, err error, options response.Options) {
	w.Header().Set("Cache-Control", "no-store")
	var failure error
	if err != nil {
		if apperror.Public(err).Code() == apperror.RateLimited {
			w.Header().Set("Retry-After", "60")
		}
		failure = h.responses.Error(w, r, err)
	} else {
		failure = h.responses.Success(w, r, status, code, data, options)
	}
	if failure != nil {
		panic(http.ErrAbortHandler)
	}
}
func (h *Handler) Mount(r chi.Router) {
	r.Get("/wallet", h.read("balance", "WALLET_FETCHED", "", false))
	r.Get("/wallet/transactions", h.read("history", "WALLET_TRANSACTIONS_FETCHED", "", true))
	r.Get("/wallet/transactions/{operation_id}", h.read("operation", "WALLET_TRANSACTION_FETCHED", "operation_id", false))
	r.Get("/wallet/transfers", h.read("transfers", "TRANSFERS_FETCHED", "", true))
	r.Post("/wallet/transfers", h.transfer)
	r.Get("/wallet/policies", h.read("summary", "WALLET_POLICIES_FETCHED", "", false))
	r.Get("/wallet/withdrawal-eligibility", h.read("eligibility", "WITHDRAWAL_ELIGIBILITY_FETCHED", "", false))
	r.Get("/wallet/withdrawals", h.read("withdrawals", "WITHDRAWALS_FETCHED", "", true))
	r.Post("/wallet/withdrawals", h.withdraw)
	r.Get("/wallet/withdrawals/{withdrawal_id}", h.read("withdrawal", "WITHDRAWAL_FETCHED", "withdrawal_id", false))
	r.Post("/wallet/withdrawals/{withdrawal_id}/cancel", h.cancel)
	r.Post("/gifts", h.gift)
	r.Get("/admin/economy/policies", h.read("policies", "ECONOMY_POLICIES_FETCHED", "", true))
	r.Post("/admin/economy/policies", h.policy)
	r.Get("/admin/financial-operations/{operation_id}", h.read("inspect", "FINANCIAL_OPERATION_FETCHED", "operation_id", false))
	r.Post("/admin/wallet-adjustments", h.adjust)
	r.Get("/admin/withdrawals", h.read("reviews", "ADMIN_WITHDRAWALS_FETCHED", "", true))
	r.Post("/admin/withdrawals/{withdrawal_id}/review", h.review)
	r.Get("/admin/agency-hosts/{account_id}", h.read("associations", "AGENCY_HOST_HISTORY_FETCHED", "account_id", true))
	r.Put("/admin/agency-hosts/{account_id}", h.association)
}
func query(r *http.Request, list bool) (wallet.Query, error) {
	q := wallet.Query{Limit: 20}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, wallet.Error(apperror.ValidationFailed)
	}
	for key, v := range values {
		if !list || len(v) != 1 || v[0] == "" {
			return q, wallet.Error(apperror.ValidationFailed)
		}
		switch key {
		case "limit":
			q.Limit, err = strconv.Atoi(v[0])
			if err != nil || q.Limit < 1 || q.Limit > 50 {
				return q, wallet.Error(apperror.ValidationFailed)
			}
		case "cursor":
			if !access.ValidID(v[0]) {
				return q, wallet.Error(apperror.ValidationFailed)
			}
			q.Cursor = v[0]
		default:
			return q, wallet.Error(apperror.ValidationFailed)
		}
	}
	return q, nil
}
func (h *Handler) read(kind, code, idParameter string, list bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q, err := query(r, list)
		q.Kind = kind
		if idParameter != "" {
			q.ID = chi.URLParam(r, idParameter)
			if !access.ValidID(q.ID) {
				err = wallet.Error(apperror.ValidationFailed)
			}
		}
		var out wallet.ReadResult
		if err == nil {
			out, err = h.service.Read(r.Context(), credentials(r), q)
		}
		options := response.Options{}
		if list && err == nil {
			var next *string
			if out.HasMore {
				next = &out.Next
			}
			options.Meta = &response.Meta{Pagination: response.Pagination{Limit: q.Limit, NextCursor: next, HasMore: out.HasMore}}
			if out.Data == nil {
				out.Data = []any{}
			}
		}
		h.write(w, r, 200, code, out.Data, err, options)
	}
}
func command(r *http.Request, kind string) (wallet.Command, error) {
	in := wallet.Command{Kind: kind, Correlation: requestmeta.ID(r.Context())}
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || !wallet.ValidKey(values[0]) {
		return in, wallet.Error(apperror.ValidationFailed)
	}
	in.Key = values[0]
	return in, nil
}
func (h *Handler) execute(w http.ResponseWriter, r *http.Request, status int, code string, in wallet.Command, err error) {
	var data any
	if err == nil {
		data, err = h.service.Execute(r.Context(), credentials(r), in)
	}
	h.write(w, r, status, code, data, err, response.Options{})
}

type transferRequest struct {
	RecipientID string `json:"recipient_id"`
	Diamonds    *int64 `json:"diamonds"`
}

func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	var body transferRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "transfer")
	if err == nil {
		err = keyErr
	}
	if err == nil {
		if body.Diamonds == nil || *body.Diamonds < 1 || !access.ValidID(body.RecipientID) {
			err = wallet.Error(apperror.ValidationFailed)
		} else {
			in.Target = body.RecipientID
			in.Amount = *body.Diamonds
		}
	}
	h.execute(w, r, 200, "TRANSFER_SETTLED", in, err)
}

type withdrawalRequest struct {
	Diamonds *int64 `json:"diamonds"`
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	var body withdrawalRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "withdrawal")
	if err == nil {
		err = keyErr
	}
	if err == nil {
		if body.Diamonds == nil || *body.Diamonds < 1 {
			err = wallet.Error(apperror.ValidationFailed)
		} else {
			in.Amount = *body.Diamonds
		}
	}
	h.execute(w, r, 202, "WITHDRAWAL_REQUESTED", in, err)
}

type giftRequest struct {
	HostID   string `json:"host_id"`
	GiftID   string `json:"gift_id"`
	Quantity *int64 `json:"quantity"`
}

func (h *Handler) gift(w http.ResponseWriter, r *http.Request) {
	var body giftRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "gift")
	if err == nil {
		err = keyErr
	}
	if err == nil {
		if !access.ValidID(body.HostID) || !wallet.ValidCatalogID(body.GiftID) || body.Quantity == nil || *body.Quantity < 1 {
			err = wallet.Error(apperror.ValidationFailed)
		} else {
			in.Target = body.HostID
			in.GiftID = body.GiftID
			in.Quantity = *body.Quantity
		}
	}
	h.execute(w, r, 200, "GIFT_SETTLED", in, err)
}

type adjustmentRequest struct {
	AccountID string `json:"account_id"`
	Diamonds  *int64 `json:"diamonds"`
	Direction string `json:"direction"`
	Reference string `json:"reference"`
	Reason    string `json:"reason"`
}

func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) {
	var body adjustmentRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "adjust")
	if err == nil {
		err = keyErr
	}
	if err == nil {
		if !access.ValidID(body.AccountID) || body.Diamonds == nil || *body.Diamonds < 1 || (body.Direction != "credit" && body.Direction != "debit") || !wallet.ValidKey(body.Reference) || !access.ValidReason(body.Reason) {
			err = wallet.Error(apperror.ValidationFailed)
		} else {
			in.Target = body.AccountID
			in.Amount = *body.Diamonds
			in.Direction = body.Direction
			in.Reference = body.Reference
			in.Reason = body.Reason
		}
	}
	h.execute(w, r, 200, "WALLET_ADJUSTMENT_SETTLED", in, err)
}

type reasonRequest struct {
	Reason string `json:"reason"`
}
type reviewRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	var body reasonRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "transition")
	if err == nil {
		err = keyErr
	}
	in.ID = chi.URLParam(r, "withdrawal_id")
	in.Action = "cancel"
	in.Reason = body.Reason
	if err == nil && (!access.ValidID(in.ID) || !access.ValidReason(in.Reason)) {
		err = wallet.Error(apperror.ValidationFailed)
	}
	h.execute(w, r, 200, "WITHDRAWAL_CANCELLED", in, err)
}
func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	var body reviewRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "transition")
	if err == nil {
		err = keyErr
	}
	in.ID = chi.URLParam(r, "withdrawal_id")
	in.Action = body.Action
	in.Reason = body.Reason
	if err == nil && (!access.ValidID(in.ID) || !access.ValidReason(in.Reason) || (in.Action != "approve" && in.Action != "reject")) {
		err = wallet.Error(apperror.ValidationFailed)
	}
	h.execute(w, r, 200, "WITHDRAWAL_REVIEWED", in, err)
}

// Policy DTOs deliberately omit policy/version IDs and all caller actor IDs.
// Pointer fields distinguish omitted mandatory commercial choices from zero.
type feeRequest struct {
	Fixed    *int64 `json:"fixed"`
	BPS      *int64 `json:"bps"`
	Rounding string `json:"rounding"`
}
type activityRequest struct {
	Timezone                string `json:"timezone"`
	Period                  string `json:"period"`
	MinimumQualifyingDays   *int   `json:"minimum_qualifying_days"`
	MinimumDailyLiveMinutes *int   `json:"minimum_daily_live_minutes"`
}
type policyRequest struct {
	Kind            string           `json:"kind"`
	Scope           string           `json:"scope"`
	PreviousID      string           `json:"previous_policy_id"`
	EffectiveAt     time.Time        `json:"effective_at"`
	Enabled         *bool            `json:"enabled"`
	Reason          string           `json:"reason"`
	Min             *int64           `json:"min_diamonds"`
	Max             *int64           `json:"max_diamonds"`
	DailyLimit      *int64           `json:"daily_limit"`
	MonthlyLimit    *int64           `json:"monthly_limit"`
	Fee             *feeRequest      `json:"fee"`
	ReviewOnly      *bool            `json:"review_only"`
	VAT             *string          `json:"vat"`
	Rounding        *string          `json:"rounding"`
	TaxJurisdiction *string          `json:"tax_jurisdiction"`
	HostBPS         *int64           `json:"host_bps"`
	AgencyBPS       *int64           `json:"agency_bps"`
	PlatformBPS     *int64           `json:"platform_bps"`
	TaxBPS          *int64           `json:"tax_bps"`
	Catalog         map[string]int64 `json:"catalog"`
	Activity        *activityRequest `json:"activity"`
}

func (p policyRequest) policy() (wallet.Policy, error) {
	out := wallet.Policy{Kind: p.Kind, Scope: p.Scope, PreviousID: p.PreviousID, EffectiveAt: p.EffectiveAt.UTC()}
	invalid := func() (wallet.Policy, error) { return out, wallet.Error(apperror.ValidationFailed) }
	if p.Enabled == nil || p.EffectiveAt.IsZero() || !access.ValidReason(p.Reason) || p.PreviousID != "" && !access.ValidID(p.PreviousID) {
		return invalid()
	}
	out.Enabled = *p.Enabled
	hasAmounts := p.Min != nil || p.Max != nil || p.DailyLimit != nil || p.MonthlyLimit != nil || p.Fee != nil
	hasGift := p.VAT != nil || p.Rounding != nil || p.TaxJurisdiction != nil || p.HostBPS != nil || p.AgencyBPS != nil || p.PlatformBPS != nil || p.TaxBPS != nil || p.Catalog != nil
	switch p.Kind {
	case "transfer", "withdrawal":
		if hasGift || p.Activity != nil || p.Kind == "transfer" && p.ReviewOnly != nil || p.Min == nil || p.Max == nil || p.DailyLimit == nil || p.MonthlyLimit == nil || p.Fee == nil || p.Fee.Fixed == nil || p.Fee.BPS == nil || p.Kind == "withdrawal" && p.ReviewOnly == nil {
			return invalid()
		}
		out.Min = *p.Min
		out.Max = *p.Max
		out.DailyLimit = *p.DailyLimit
		out.MonthlyLimit = *p.MonthlyLimit
		out.Fee = wallet.Fee{Fixed: *p.Fee.Fixed, BPS: *p.Fee.BPS, Rounding: p.Fee.Rounding}
		if p.ReviewOnly != nil {
			out.ReviewOnly = *p.ReviewOnly
		}
	case "gift":
		if hasAmounts || p.Activity != nil || p.ReviewOnly != nil || p.VAT == nil || p.Rounding == nil || p.TaxJurisdiction == nil || p.HostBPS == nil || p.AgencyBPS == nil || p.PlatformBPS == nil || p.TaxBPS == nil {
			return invalid()
		}
		out.VAT = *p.VAT
		out.Rounding = *p.Rounding
		out.TaxJurisdiction = *p.TaxJurisdiction
		out.HostBPS = *p.HostBPS
		out.AgencyBPS = *p.AgencyBPS
		out.PlatformBPS = *p.PlatformBPS
		out.TaxBPS = *p.TaxBPS
		out.Catalog = p.Catalog
	case "activity":
		if hasAmounts || hasGift || p.ReviewOnly != nil || p.Activity == nil || p.Activity.MinimumQualifyingDays == nil || p.Activity.MinimumDailyLiveMinutes == nil {
			return invalid()
		}
		out.Activity = &agency.Policy{Version: access.ID(), Scope: p.Scope, Timezone: p.Activity.Timezone, Period: p.Activity.Period, MinimumQualifyingDays: *p.Activity.MinimumQualifyingDays, MinimumDailyLiveMinutes: *p.Activity.MinimumDailyLiveMinutes, EffectiveAt: out.EffectiveAt}
	default:
		return invalid()
	}
	return out, nil
}
func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
	var body policyRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "policy")
	if err == nil {
		err = keyErr
	}
	if err == nil {
		var p wallet.Policy
		p, err = body.policy()
		in.Policy = &p
		in.Reason = body.Reason
	}
	h.execute(w, r, 201, "ECONOMY_POLICY_CREATED", in, err)
}

type associationRequest struct {
	AgencyID       string     `json:"agency_id"`
	Classification string     `json:"classification"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until"`
	Reason         string     `json:"reason"`
}

func (h *Handler) association(w http.ResponseWriter, r *http.Request) {
	var body associationRequest
	err := h.decoder.Decode(r, &body)
	in, keyErr := command(r, "association")
	if err == nil {
		err = keyErr
	}
	in.Target = chi.URLParam(r, "account_id")
	if err == nil {
		if !access.ValidID(in.Target) || !access.ValidID(body.AgencyID) || (body.Classification != agency.AudioHost && body.Classification != agency.VideoHost) || body.EffectiveFrom.IsZero() || !access.ValidReason(body.Reason) {
			err = wallet.Error(apperror.ValidationFailed)
		} else {
			a := wallet.Association{AccountID: in.Target, AgencyID: body.AgencyID, Classification: body.Classification, EffectiveFrom: body.EffectiveFrom.UTC(), EffectiveUntil: body.EffectiveUntil}
			if a.EffectiveUntil != nil {
				at := a.EffectiveUntil.UTC()
				a.EffectiveUntil = &at
			}
			in.Association = &a
			in.Reason = body.Reason
		}
	}
	h.execute(w, r, 201, "AGENCY_HOST_UPDATED", in, err)
}
