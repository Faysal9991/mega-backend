package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/go-chi/chi/v5"
)

const testActor = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testRecipient = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testResource = "cccccccccccccccccccccccccccccccc"

type fixtureRepository struct {
	owner       bool
	failure     error
	query       wallet.Query
	command     wallet.Command
	credentials access.Credentials
	calls       int
}

func (f *fixtureRepository) authorize(c access.Credentials, admin bool) error {
	if f.failure != nil {
		return f.failure
	}
	if c.Access != strings.Repeat("s", 43) || c.Installation != strings.Repeat("a", 32) {
		return wallet.Error(apperror.Unauthenticated)
	}
	if admin && !f.owner {
		return wallet.Error(apperror.Forbidden)
	}
	return nil
}
func (f *fixtureRepository) Read(_ context.Context, c access.Credentials, q wallet.Query) (wallet.ReadResult, error) {
	f.calls++
	f.query = q
	f.credentials = c
	if err := f.authorize(c, q.Kind == "policies" || q.Kind == "inspect" || q.Kind == "reviews" || q.Kind == "associations"); err != nil {
		return wallet.ReadResult{}, err
	}
	switch q.Kind {
	case "balance":
		return wallet.ReadResult{Data: wallet.Balance{AccountID: testActor, Available: 100, Held: 0, Unit: "diamonds"}}, nil
	case "operation", "inspect":
		return wallet.ReadResult{Data: wallet.Detail{Operation: wallet.Operation{ID: q.ID, Kind: "transfer", Actor: testActor, Target: testRecipient, Amount: 10, PolicyID: testResource, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Postings: []wallet.Posting{}, Policy: json.RawMessage(`{}`)}}, nil
	case "withdrawal":
		return wallet.ReadResult{Data: pendingWithdrawal()}, nil
	case "eligibility":
		return wallet.ReadResult{Data: wallet.Eligibility{Eligible: false, Reasons: []string{"FINANCIAL_CONFIGURATION_REQUIRED"}, Available: 100, Settlement: "not_configured"}}, nil
	case "summary":
		return wallet.ReadResult{Data: wallet.PolicySummary{Gifts: []wallet.Policy{}, Settlement: "not_configured"}}, nil
	default:
		return wallet.ReadResult{Data: []any{}, Limit: q.Limit}, nil
	}
}
func pendingWithdrawal() wallet.Withdrawal {
	return wallet.Withdrawal{ID: testResource, AccountID: testActor, Amount: 10, Reserved: 10, PolicyID: testResource, State: "pending", Settlement: "not_configured", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func (f *fixtureRepository) Execute(_ context.Context, c access.Credentials, in wallet.Command) (any, error) {
	f.calls++
	f.command = in
	f.credentials = c
	admin := in.Kind == "policy" || in.Kind == "adjust" || in.Kind == "association" || in.Kind == "transition" && in.Action != "cancel"
	if err := f.authorize(c, admin); err != nil {
		return nil, err
	}
	switch in.Kind {
	case "withdrawal", "transition":
		out := pendingWithdrawal()
		if in.Action == "approve" {
			out.State = "approved"
		}
		if in.Action == "reject" {
			out.State = "rejected"
		}
		if in.Action == "cancel" {
			out.State = "cancelled"
		}
		return out, nil
	case "policy":
		return in.Policy, nil
	case "association":
		return in.Association, nil
	default:
		kind := in.Kind
		if kind == "adjust" {
			kind = "adjustment"
		}
		return wallet.Operation{ID: testResource, Kind: kind, Actor: testActor, Target: in.Target, Amount: in.Amount, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	}
}
func setup(t *testing.T, f *fixtureRepository) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h, err := New(wallet.New(f), logger)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	router, err := httpserver.NewRouter(cfg.HTTP, logger, httpserver.NewLifecycle(nil), func(r chi.Router) { h.Mount(r) })
	if err != nil {
		t.Fatal(err)
	}
	return router
}
func request(router http.Handler, method, path, body, key string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Request-ID", "wallet-fixture")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if auth {
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 43))
		r.Header.Set("X-Installation-ID", strings.Repeat("a", 32))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}
func TestFinancialHTTPStrictInputsAndOwnership(t *testing.T) {
	f := &fixtureRepository{}
	router := setup(t, f)
	for _, tc := range []struct {
		name, method, path, body, key string
		status                        int
	}{
		{"transfer", "POST", "/wallet/transfers", `{"recipient_id":"` + testRecipient + `","diamonds":10}`, "transfer-1", 200},
		{"actor_injection", "POST", "/wallet/transfers", `{"recipient_id":"` + testRecipient + `","diamonds":10,"actor_id":"` + testRecipient + `"}`, "transfer-2", 400},
		{"large_quantity", "POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":1001}`, "gift-large", 200},
		{"max_int64_quantity", "POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":9223372036854775807}`, "gift-max", 200},
		{"overflow_quantity", "POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":9223372036854775808}`, "gift-overflow", 400},
		{"price_injection", "POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":1,"price":1}`, "gift-1", 400},
		{"wallet_injection", "POST", "/wallet/withdrawals", `{"diamonds":10,"account_id":"` + testRecipient + `"}`, "withdraw-1", 400},
		{"settlement_injection", "POST", "/wallet/withdrawals", `{"diamonds":10,"state":"settled"}`, "withdraw-1", 400},
		{"zero", "POST", "/wallet/withdrawals", `{"diamonds":0}`, "withdraw-1", 422},
		{"negative", "POST", "/wallet/withdrawals", `{"diamonds":-1}`, "withdraw-1", 422},
		{"null", "POST", "/wallet/withdrawals", `{"diamonds":null}`, "withdraw-1", 422},
		{"fraction", "POST", "/wallet/withdrawals", `{"diamonds":1.5}`, "withdraw-1", 400},
		{"integer_as_float", "POST", "/wallet/withdrawals", `{"diamonds":1.0}`, "withdraw-1", 400},
		{"overflow", "POST", "/wallet/withdrawals", `{"diamonds":9223372036854775808}`, "withdraw-1", 400},
		{"string_amount", "POST", "/wallet/withdrawals", `{"diamonds":"10"}`, "withdraw-1", 400},
		{"missing_key", "POST", "/wallet/withdrawals", `{"diamonds":10}`, "", 422},
		{"invalid_key", "POST", "/wallet/withdrawals", `{"diamonds":10}`, "invalid key", 422},
		{"invalid_limit", "GET", "/wallet/transactions?limit=51", "", "", 422},
		{"duplicate_limit", "GET", "/wallet/transactions?limit=1&limit=2", "", "", 422},
		{"invalid_cursor", "GET", "/wallet/transactions?cursor=" + testResource + "%0A", "", "", 422},
		{"actor_query", "GET", "/wallet/transactions?account_id=" + testRecipient, "", "", 422},
		{"ordinary_admin_inspection", "GET", "/admin/financial-operations/" + testResource, "", "", 403},
		{"ordinary_admin_adjust", "POST", "/admin/wallet-adjustments", `{"account_id":"` + testRecipient + `","diamonds":1,"direction":"credit","reference":"fixture:1","reason":"fixture"}`, "adjust-1", 403},
		{"review_cannot_settle", "POST", "/admin/withdrawals/" + testResource + "/review", `{"action":"settle","reason":"fixture"}`, "review-1", 422},
		{"review_cannot_cancel", "POST", "/admin/withdrawals/" + testResource + "/review", `{"action":"cancel","reason":"fixture"}`, "review-1", 422},
		{"cancel_cannot_approve", "POST", "/wallet/withdrawals/" + testResource + "/cancel", `{"action":"approve","reason":"fixture"}`, "cancel-1", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(router, tc.method, tc.path, tc.body, tc.key, true)
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") != "wallet-fixture" {
				t.Fatal("response protection/correlation drift")
			}
		})
	}
	w := request(router, "POST", "/wallet/transfers", `{"recipient_id":"`+testRecipient+`","diamonds":10}`, "scope-key", true)
	if w.Code != 200 || f.command.Target != testRecipient || f.command.Amount != 10 || f.command.Correlation != "wallet-fixture" || f.command.Key != "scope-key" || f.credentials.Installation != strings.Repeat("a", 32) {
		t.Fatal("server command/credentials projection drift")
	}
	w = request(router, "GET", "/wallet/transactions/"+testResource, "", "", true)
	if w.Code != 200 || f.query.Kind != "operation" || f.query.ID != testResource {
		t.Fatal("resource query ownership projection drift")
	}
	w = request(router, "GET", "/wallet", "", "", false)
	if w.Code != 401 {
		t.Fatal("unauthenticated wallet read")
	}
	f.failure = wallet.Error(apperror.ServiceUnavailable)
	w = request(router, "GET", "/wallet", "", "", true)
	if w.Code != 503 {
		t.Fatal("security storage failure opened access")
	}
	f.failure = wallet.Error(apperror.Conflict)
	w = request(router, "POST", "/wallet/transfers", `{"recipient_id":"`+testRecipient+`","diamonds":10}`, "scope-key", true)
	if w.Code != 409 {
		t.Fatal("idempotency conflict envelope drift")
	}
}
func TestFinancialHTTPErrorCodes(t *testing.T) {
	f := &fixtureRepository{}
	router := setup(t, f)
	for _, tc := range []struct {
		code   apperror.Code
		status int
	}{
		{wallet.ConfigurationRequired, 409}, {wallet.InsufficientFunds, 409}, {wallet.NotEligible, 403}, {wallet.ActivityUnavailable, 503}, {apperror.RateLimited, 429},
	} {
		f.failure = wallet.Error(tc.code)
		w := request(router, "POST", "/wallet/withdrawals", `{"diamonds":10}`, "failure-fixture", true)
		if w.Code != tc.status {
			t.Fatalf("%s status %d: %s", tc.code, w.Code, w.Body.String())
		}
		var out struct {
			Success bool          `json:"success"`
			Code    apperror.Code `json:"code"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Success || out.Code != tc.code {
			t.Fatal("financial error code drift")
		}
		if tc.code == apperror.RateLimited && w.Header().Get("Retry-After") != "60" {
			t.Fatal("missing retry budget")
		}
	}
}

func TestFinancialHTTPRoutesAndPendingSettlement(t *testing.T) {
	f := &fixtureRepository{owner: true}
	router := setup(t, f)
	for _, tc := range []struct {
		path, kind string
		list       bool
	}{
		{"/wallet", "balance", false}, {"/wallet/transactions", "history", true}, {"/wallet/transfers", "transfers", true}, {"/wallet/policies", "summary", false}, {"/wallet/withdrawal-eligibility", "eligibility", false}, {"/wallet/withdrawals", "withdrawals", true}, {"/wallet/withdrawals/" + testResource, "withdrawal", false}, {"/admin/economy/policies", "policies", true}, {"/admin/financial-operations/" + testResource, "inspect", false}, {"/admin/withdrawals", "reviews", true}, {"/admin/agency-hosts/" + testRecipient, "associations", true},
	} {
		w := request(router, "GET", tc.path, "", "", true)
		if w.Code != 200 || f.query.Kind != tc.kind {
			t.Fatalf("route %s: %s", tc.path, w.Body.String())
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("invalid envelope")
		}
		if _, ok := out["meta"]; ok != tc.list {
			t.Fatal("list metadata mismatch")
		}
		if tc.list && string(out["data"]) != "[]" {
			t.Fatal("empty collection is not []")
		}
	}
	for _, tc := range []struct {
		method, path, body, code string
		status                   int
	}{
		{"POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":1}`, "GIFT_SETTLED", 200},
		{"POST", "/admin/wallet-adjustments", `{"account_id":"` + testRecipient + `","diamonds":1,"direction":"credit","reference":"synthetic:credit","reason":"explicit synthetic issuance"}`, "WALLET_ADJUSTMENT_SETTLED", 200},
		{"POST", "/wallet/withdrawals", `{"diamonds":10}`, "WITHDRAWAL_REQUESTED", 202},
		{"POST", "/admin/withdrawals/" + testResource + "/review", `{"action":"approve","reason":"fixture"}`, "WITHDRAWAL_REVIEWED", 200},
		{"POST", "/wallet/withdrawals/" + testResource + "/cancel", `{"reason":"fixture"}`, "WITHDRAWAL_CANCELLED", 200},
		{"POST", "/gifts", `{"host_id":"` + testRecipient + `","gift_id":"rose","quantity":1}`, "GIFT_SETTLED", 200},
		{"POST", "/admin/economy/policies", `{"kind":"transfer","scope":"all","effective_at":"2026-01-01T00:00:00Z","enabled":false,"min_diamonds":1,"max_diamonds":100,"daily_limit":0,"monthly_limit":0,"fee":{"fixed":0,"bps":0,"rounding":"ceil"},"reason":"explicit policy fixture"}`, "ECONOMY_POLICY_CREATED", 201},
		{"PUT", "/admin/agency-hosts/" + testRecipient, `{"agency_id":"` + testResource + `","classification":"audio_host","effective_from":"2026-01-01T00:00:00Z","reason":"fixture"}`, "AGENCY_HOST_UPDATED", 201},
	} {
		w := request(router, tc.method, tc.path, tc.body, "fixture-key", true)
		if w.Code != tc.status {
			t.Fatalf("route %s status %d: %s", tc.path, w.Code, w.Body.String())
		}
		var out struct {
			Code string         `json:"code"`
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != tc.code {
			t.Fatal("success code drift")
		}
		if strings.Contains(tc.path, "withdrawal") && out.Data["settlement_status"] != "not_configured" {
			t.Fatal("pending/review response falsely reports settled")
		}
	}
	if f.command.Association == nil || f.command.Association.AccountID != testRecipient {
		t.Fatal("association account must come from URL")
	}
}
func TestFinancialPolicyExplicitChoices(t *testing.T) {
	f := &fixtureRepository{owner: true}
	router := setup(t, f)
	policy := map[string]any{"kind": "transfer", "scope": "all", "enabled": false, "effective_at": "2026-01-01T00:00:00Z", "reason": "explicit synthetic configuration", "min_diamonds": 1, "max_diamonds": 100, "daily_limit": 0, "monthly_limit": 0, "fee": map[string]any{"fixed": 0, "bps": 0, "rounding": "ceil"}}
	marshal := func(v any) string {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	if w := request(router, "POST", "/admin/economy/policies", marshal(policy), "explicit-policy", true); w.Code != 201 {
		t.Fatalf("valid explicit policy: %s", w.Body.String())
	}
	for _, field := range []string{"enabled", "min_diamonds", "max_diamonds", "daily_limit", "monthly_limit", "fee"} {
		copy := map[string]any{}
		for k, v := range policy {
			copy[k] = v
		}
		delete(copy, field)
		if w := request(router, "POST", "/admin/economy/policies", marshal(copy), "invalid-policy", true); w.Code != 422 {
			t.Fatalf("missing %s accepted: %s", field, w.Body.String())
		}
	}
	for _, tc := range []struct {
		field  string
		value  any
		status int
	}{
		{"actor_id", testRecipient, 400}, {"policy_id", testResource, 400}, {"host_bps", 5000, 422}, {"reason", "trailing reason\n", 422}, {"previous_policy_id", testResource + "\n", 422},
	} {
		copy := map[string]any{}
		for k, v := range policy {
			copy[k] = v
		}
		copy[tc.field] = tc.value
		if w := request(router, "POST", "/admin/economy/policies", marshal(copy), "invalid-policy", true); w.Code != tc.status {
			t.Fatalf("field %s: status %d: %s", tc.field, w.Code, w.Body.String())
		}
	}
	f.owner = false
	if w := request(router, "POST", "/admin/economy/policies", marshal(policy), "ordinary-policy", true); w.Code != 403 {
		t.Fatal("ordinary profile scope authorized financial policy")
	}
}

func TestWalletSharedResponseFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/openapi/fixtures/wallet.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Method, Path, Request, Key string
		Status                     int
		Owner                      bool
		Body                       map[string]any
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Path, func(t *testing.T) {
			f := &fixtureRepository{owner: tc.Owner}
			router := setup(t, f)
			w := request(router, tc.Method, tc.Path, tc.Request, tc.Key, true)
			if w.Code != tc.Status {
				t.Fatalf("status %d", w.Code)
			}
			var actual map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tc.Body) {
				t.Fatalf("shared fixture drift: %s", w.Body.String())
			}
		})
	}
}
