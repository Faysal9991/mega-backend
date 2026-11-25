package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
)

func TestCheckedDiamondArithmetic(t *testing.T) {
	for _, c := range []struct {
		a, b, want int64
		ok         bool
	}{
		{0, 0, 0, true}, {math.MaxInt64, 0, math.MaxInt64, true}, {math.MinInt64, 0, math.MinInt64, true},
		{math.MaxInt64, 1, 0, false}, {math.MinInt64, -1, 0, false}, {math.MaxInt64, math.MinInt64, -1, true},
		{-1, math.MinInt64, 0, false}, {1, math.MaxInt64, 0, false}, {-12, 13, 1, true}, {math.MinInt64, math.MaxInt64, -1, true},
	} {
		got, e := Add(c.a, c.b)
		if (e == nil) != c.ok || c.ok && got != c.want {
			t.Fatalf("Add(%d,%d)=%d,%v", c.a, c.b, got, e)
		}
	}
	for _, c := range []struct {
		a, b, want int64
		ok         bool
	}{
		{0, math.MaxInt64, 0, true}, {math.MaxInt64, 0, 0, true}, {math.MaxInt64, 1, math.MaxInt64, true},
		{math.MaxInt64, 2, 0, false}, {3037000499, 3037000499, 9223372030926249001, true}, {3037000500, 3037000500, 0, false},
		{-1, 0, 0, false}, {0, -1, 0, false}, {math.MinInt64, 1, 0, false}, {2, 3, 6, true},
	} {
		got, e := Multiply(c.a, c.b)
		if (e == nil) != c.ok || c.ok && got != c.want {
			t.Fatalf("Multiply(%d,%d)=%d,%v", c.a, c.b, got, e)
		}
	}
}

func TestFeeCeilingBigIntegerReference(t *testing.T) {
	values := []struct {
		amount int64
		fee    Fee
	}{
		{0, Fee{Fixed: 0, BPS: 0, Rounding: "ceil"}}, {1, Fee{Fixed: 0, BPS: 1, Rounding: "ceil"}},
		{9999, Fee{Fixed: 2, BPS: 1, Rounding: "ceil"}}, {10000, Fee{Fixed: 2, BPS: 1, Rounding: "ceil"}},
		{10001, Fee{Fixed: 2, BPS: 1, Rounding: "ceil"}}, {math.MaxInt64, Fee{Fixed: 0, BPS: 10000, Rounding: "ceil"}},
		{math.MaxInt64, Fee{Fixed: 1, BPS: 10000, Rounding: "ceil"}}, {math.MaxInt64, Fee{Fixed: math.MaxInt64, BPS: 1, Rounding: "ceil"}},
		{math.MaxInt64, Fee{Fixed: 0, BPS: 9999, Rounding: "ceil"}}, {math.MaxInt64, Fee{Fixed: math.MaxInt64, BPS: 0, Rounding: "ceil"}},
	}
	rng := rand.New(rand.NewSource(73))
	for range 256 {
		values = append(values, struct {
			amount int64
			fee    Fee
		}{rng.Int63(), Fee{Fixed: rng.Int63n(1000000), BPS: rng.Int63n(10001), Rounding: "ceil"}})
	}
	for _, v := range values {
		expected := new(big.Int).Mul(big.NewInt(v.amount), big.NewInt(v.fee.BPS))
		expected.Add(expected, big.NewInt(9999))
		expected.Div(expected, big.NewInt(10000))
		expected.Add(expected, big.NewInt(v.fee.Fixed))
		got, e := v.fee.Calculate(v.amount)
		if !expected.IsInt64() {
			if e == nil {
				t.Fatal("fee overflow accepted")
			}
			continue
		}
		if e != nil || got != expected.Int64() {
			t.Fatalf("fee %+v amount=%d got=%d expected=%s err=%v", v.fee, v.amount, got, expected, e)
		}
	}
	for _, c := range []struct {
		amount int64
		fee    Fee
	}{{-1, Fee{Rounding: "ceil"}}, {1, Fee{Fixed: -1, Rounding: "ceil"}}, {1, Fee{BPS: -1, Rounding: "ceil"}}, {1, Fee{BPS: 10001, Rounding: "ceil"}}, {1, Fee{Rounding: "floor"}}, {1, Fee{}}} {
		if _, e := c.fee.Calculate(c.amount); e == nil {
			t.Fatalf("unconfigured/invalid fee %+v accepted", c)
		}
	}
}

func fixtureGiftPolicy() Policy {
	return Policy{Kind: "gift", Scope: "agency", EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Enabled: true, VAT: "inclusive_diamonds", TaxJurisdiction: "synthetic-jurisdiction", Rounding: "largest_remainder", HostBPS: 2500, AgencyBPS: 2500, PlatformBPS: 2500, TaxBPS: 2500, Catalog: map[string]int64{"fixture-rose": 1}}
}
func fixtureTransferPolicy() Policy {
	return Policy{Kind: "transfer", Scope: "all", EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Enabled: true, Min: 1, Max: 10000, Fee: Fee{Rounding: "ceil"}}
}

func TestGiftPolicyRequiresCompleteConfiguredEconomy(t *testing.T) {
	for name, mutate := range map[string]func(*Policy){
		"missing_kind": func(p *Policy) { p.Kind = "" }, "no_effective_time": func(p *Policy) { p.EffectiveAt = time.Time{} },
		"invalid_previous": func(p *Policy) { p.PreviousID = "owner" }, "unknown_scope": func(p *Policy) { p.Scope = "all" },
		"missing_vat": func(p *Policy) { p.VAT = "" }, "fiat_vat": func(p *Policy) { p.VAT = "fiat" }, "exclusive_vat": func(p *Policy) { p.VAT = "exclusive" },
		"missing_jurisdiction": func(p *Policy) { p.TaxJurisdiction = "" }, "unsafe_jurisdiction": func(p *Policy) { p.TaxJurisdiction = "jurisdiction\n" },
		"missing_rounding": func(p *Policy) { p.Rounding = "" }, "implicit_rounding": func(p *Policy) { p.Rounding = "floor" },
		"negative_rate": func(p *Policy) { p.HostBPS = -1 }, "excess_rate": func(p *Policy) { p.HostBPS = 10001 },
		"underallocated": func(p *Policy) { p.HostBPS = 2499 }, "overallocated": func(p *Policy) { p.HostBPS = 2501 },
		"missing_catalog": func(p *Policy) { p.Catalog = nil }, "zero_price": func(p *Policy) { p.Catalog["fixture-rose"] = 0 },
		"negative_price": func(p *Policy) { p.Catalog["fixture-rose"] = -1 }, "invalid_catalog_id": func(p *Policy) { p.Catalog = map[string]int64{"rose\n": 1} },
		"long_catalog_id":      func(p *Policy) { p.Catalog = map[string]int64{strings.Repeat("x", 33): 1} },
		"nonagency_agency_cut": func(p *Policy) { p.Scope = "non_agency" },
	} {
		t.Run(name, func(t *testing.T) {
			p := fixtureGiftPolicy()
			mutate(&p)
			if p.Validate() == nil {
				t.Fatal("invalid gift policy accepted")
			}
			if _, e := p.Allocate(100); e == nil {
				t.Fatal("invalid policy allowed distribution")
			}
		})
	}
	p := fixtureGiftPolicy()
	if p.Validate() != nil {
		t.Fatal("explicit complete gift policy denied")
	}
	p.Scope = "non_agency"
	p.AgencyBPS = 0
	p.HostBPS = 5000
	if p.Validate() != nil {
		t.Fatal("explicit non-agency allocation denied")
	}
	p.Catalog["fixture-rose"] = math.MaxInt64
	if p.Validate() != nil {
		t.Fatal("valid int64 catalog price denied")
	}
}

func TestAllocationConservationAndStableLargestRemainder(t *testing.T) {
	p := fixtureGiftPolicy()
	for total, want := range map[int64][4]int64{1: {1, 0, 0, 0}, 2: {1, 1, 0, 0}, 3: {1, 1, 1, 0}, 4: {1, 1, 1, 1}, 5: {2, 1, 1, 1}} {
		got, e := p.Allocate(total)
		if e != nil || got != want {
			t.Fatalf("tie total=%d got=%v want=%v err=%v", total, got, want, e)
		}
	}
	rng := rand.New(rand.NewSource(991))
	totals := []int64{1, 2, 3, 4, 9999, 10000, 10001, math.MaxInt64 - 1, math.MaxInt64}
	for range 256 {
		totals = append(totals, rng.Int63n(math.MaxInt64-1)+1)
	}
	for i, total := range totals {
		rates := [4]int64{2500, 2500, 2500, 2500}
		if i%2 == 1 {
			remaining := int64(10000)
			for j := 0; j < 3; j++ {
				rates[j] = rng.Int63n(remaining + 1)
				remaining -= rates[j]
			}
			rates[3] = remaining
		}
		p := fixtureGiftPolicy()
		p.HostBPS, p.AgencyBPS, p.PlatformBPS, p.TaxBPS = rates[0], rates[1], rates[2], rates[3]
		got, e := p.Allocate(total)
		if e != nil {
			t.Fatal(e)
		}
		want := referenceAllocation(total, rates)
		if got != want {
			t.Fatalf("largest remainder total=%d rates=%v got=%v want=%v", total, rates, got, want)
		}
		sum := new(big.Int)
		for j, n := range got {
			if n < 0 || n > total || rates[j] == 0 && n != 0 {
				t.Fatal("invalid allocation component")
			}
			sum.Add(sum, big.NewInt(n))
		}
		if sum.Cmp(big.NewInt(total)) != 0 {
			t.Fatal("allocation issued or burned supply")
		}
		again, e := p.Allocate(total)
		if e != nil || again != got {
			t.Fatal("allocation nondeterministic")
		}
	}
	for _, total := range []int64{0, -1, math.MinInt64} {
		if _, e := p.Allocate(total); e == nil {
			t.Fatal("invalid gift total allowed")
		}
	}
	notGift := fixtureTransferPolicy()
	if _, e := notGift.Allocate(1); e == nil {
		t.Fatal("transfer policy distributed a gift")
	}
}
func referenceAllocation(total int64, rates [4]int64) (out [4]int64) {
	var remainders [4]*big.Int
	remaining := new(big.Int).SetInt64(total)
	for i, rate := range rates {
		product := new(big.Int).Mul(big.NewInt(total), big.NewInt(rate))
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(product, big.NewInt(10000), r)
		out[i] = q.Int64()
		remainders[i] = r
		remaining.Sub(remaining, q)
	}
	for range int(remaining.Int64()) {
		best := 0
		for i := 1; i < 4; i++ {
			if remainders[i].Cmp(remainders[best]) > 0 {
				best = i
			}
		}
		out[best]++
		remainders[best] = big.NewInt(-1)
	}
	return out
}

func TestTransferAndActivityPolicyValidation(t *testing.T) {
	for _, mutate := range []func(*Policy){func(p *Policy) { p.Scope = "owner" }, func(p *Policy) { p.Min = 0 }, func(p *Policy) { p.Max = 0 }, func(p *Policy) { p.DailyLimit = -1 }, func(p *Policy) { p.MonthlyLimit = -1 }, func(p *Policy) { p.Min = 10; p.DailyLimit = 9 }, func(p *Policy) { p.Min = 10; p.MonthlyLimit = 9 }, func(p *Policy) { p.Fee.Rounding = "" }, func(p *Policy) { p.Fee.BPS = 10001 }} {
		p := fixtureTransferPolicy()
		mutate(&p)
		if p.Validate() == nil {
			t.Fatal("invalid transfer configuration accepted")
		}
	}
	p := fixtureTransferPolicy()
	p.DailyLimit = 10
	p.MonthlyLimit = 20
	if p.Validate() != nil {
		t.Fatal("bounded configured transfer policy denied")
	}
	a := agency.Policy{Version: "fixture-activity", Scope: agency.AudioHost, Timezone: "Asia/Dhaka", Period: agency.CurrentCalendarMonth, MinimumQualifyingDays: 15, MinimumDailyLiveMinutes: 120, EffectiveAt: p.EffectiveAt}
	activity := Policy{Kind: "activity", Scope: agency.AudioHost, EffectiveAt: p.EffectiveAt, Activity: &a}
	if activity.Validate() != nil {
		t.Fatal("valid explicit activity policy denied")
	}
	activity.Scope = agency.VideoHost
	if activity.Validate() == nil {
		t.Fatal("activity scope injection allowed")
	}
	activity.Scope = agency.AudioHost
	a.Timezone = ""
	if activity.Validate() == nil {
		t.Fatal("unconfigured business timezone allowed")
	}
}

type fixtureWalletRepository struct {
	reads, executes int
	query           Query
	command         Command
	credentials     access.Credentials
	readResult      ReadResult
	result          any
	err             error
}

func (r *fixtureWalletRepository) Read(_ context.Context, c access.Credentials, q Query) (ReadResult, error) {
	r.reads++
	r.credentials = c
	r.query = q
	return r.readResult, r.err
}
func (r *fixtureWalletRepository) Execute(_ context.Context, c access.Credentials, command Command) (any, error) {
	r.executes++
	r.credentials = c
	r.command = command
	return r.result, r.err
}
func validCommand() Command {
	return Command{Kind: "transfer", Target: strings.Repeat("b", 32), Amount: 1, Key: "fixture-idempotency", Correlation: "fixture-operation"}
}

func TestServiceRejectsInvalidFinancialCommandsBeforeRepository(t *testing.T) {
	for name, mutate := range map[string]func(*Command){
		"unknown_kind": func(c *Command) { c.Kind = "settled" }, "empty_key": func(c *Command) { c.Key = "" }, "long_key": func(c *Command) { c.Key = strings.Repeat("x", 65) }, "unsafe_key": func(c *Command) { c.Key = "key\n" },
		"zero_amount": func(c *Command) { c.Amount = 0 }, "negative_amount": func(c *Command) { c.Amount = -1 }, "missing_recipient": func(c *Command) { c.Target = "" }, "invalid_recipient": func(c *Command) { c.Target = "owner" },
		"gift_zero_quantity": func(c *Command) { c.Kind = "gift"; c.Quantity = 0; c.GiftID = "fixture-rose" }, "gift_negative_quantity": func(c *Command) { c.Kind = "gift"; c.Quantity = -1; c.GiftID = "fixture-rose" },
		"gift_missing_id": func(c *Command) { c.Kind = "gift"; c.Quantity = 1 }, "gift_id_injection": func(c *Command) { c.Kind = "gift"; c.Quantity = 1; c.GiftID = "rose\n" },
		"review_invalid_id": func(c *Command) { c.Kind = "transition"; c.ID = "invalid"; c.Action = "approve"; c.Reason = "fixture" },
		"review_settled_action": func(c *Command) {
			c.Kind = "transition"
			c.ID = strings.Repeat("a", 32)
			c.Action = "settle"
			c.Reason = "fixture"
		},
		"adjustment_no_reason":         func(c *Command) { c.Kind = "adjust"; c.Direction = "credit" },
		"adjustment_unknown_direction": func(c *Command) { c.Kind = "adjust"; c.Direction = "settle"; c.Reason = "fixture" },
		"policy_nil":                   func(c *Command) { c.Kind = "policy"; c.Reason = "fixture" },
		"association_nil":              func(c *Command) { c.Kind = "association"; c.Reason = "fixture" },
	} {
		t.Run(name, func(t *testing.T) {
			r := &fixtureWalletRepository{}
			c := validCommand()
			mutate(&c)
			_, e := New(r).Execute(context.Background(), access.Credentials{}, c)
			if e == nil || r.executes != 0 {
				t.Fatalf("invalid financial command reached repository: %+v err=%v calls=%d", c, e, r.executes)
			}
		})
	}
}

func TestServiceQueriesOwnershipBoundaryAndErrorPropagation(t *testing.T) {
	credentials := access.Credentials{Access: strings.Repeat("x", 43), Installation: strings.Repeat("a", 32)}
	sentinel := errors.New("fixture storage unavailable")
	for _, q := range []Query{{Kind: "history", Limit: 51}, {Kind: "history", Limit: -1}, {Kind: "history", Cursor: "other"}, {Kind: "operation", ID: "other"}, {Kind: "unknown"}} {
		r := &fixtureWalletRepository{}
		if _, e := New(r).Read(context.Background(), credentials, q); e == nil || r.reads != 0 {
			t.Fatal("invalid query reached repository")
		}
	}
	r := &fixtureWalletRepository{readResult: ReadResult{Data: Balance{AccountID: "server-owned", Unit: "DIAMOND"}}, err: sentinel}
	_, e := New(r).Read(context.Background(), credentials, Query{Kind: "balance"})
	if !errors.Is(e, sentinel) || r.query.Limit != 20 || r.credentials != credentials {
		t.Fatal("credentials/default bound/storage error changed")
	}
	r = &fixtureWalletRepository{err: sentinel}
	c := validCommand()
	_, e = New(r).Execute(context.Background(), credentials, c)
	if !errors.Is(e, sentinel) || r.credentials != credentials || r.command != c {
		t.Fatal("service changed money command or hid ambiguous/error outcome")
	}
}

func TestPolicyCommandBindsServerVersionWithoutMutatingInput(t *testing.T) {
	p := fixtureTransferPolicy()
	p.ID = strings.Repeat("f", 32)
	r := &fixtureWalletRepository{}
	_, e := New(r).Execute(context.Background(), access.Credentials{}, Command{Kind: "policy", Key: "policy-key", Correlation: "operation", Reason: "configured synthetic test policy", Policy: &p})
	if e != nil {
		t.Fatal(e)
	}
	if !access.ValidID(r.command.Policy.ID) || r.command.Policy.ID == p.ID || p.ID != strings.Repeat("f", 32) {
		t.Fatal("policy authority accepted client version or mutated input")
	}
	a := agency.Policy{Version: "client-version", Scope: agency.AudioHost, Timezone: "Asia/Dhaka", Period: agency.CurrentCalendarMonth, MinimumQualifyingDays: 15, MinimumDailyLiveMinutes: 120, EffectiveAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	activity := Policy{Kind: "activity", Scope: agency.AudioHost, EffectiveAt: p.EffectiveAt, Activity: &a}
	_, e = New(r).Execute(context.Background(), access.Credentials{}, Command{Kind: "policy", Key: "activity-key", Correlation: "operation", Reason: "configured synthetic activity", Policy: &activity})
	if e != nil {
		t.Fatal(e)
	}
	if r.command.Policy.Activity.Version != r.command.Policy.ID || !r.command.Policy.Activity.EffectiveAt.Equal(activity.EffectiveAt) || a.Version != "client-version" || a.EffectiveAt.Equal(activity.EffectiveAt) {
		t.Fatal("activity snapshot did not bind server policy identity/effective timestamp")
	}
}

func TestIdempotencyFingerprintCanonicalization(t *testing.T) {
	first := validCommand()
	p := fixtureGiftPolicy()
	p.ID = strings.Repeat("a", 32)
	first.Policy = &p
	second := first
	second.Key = "other-key"
	second.Correlation = "other-operation"
	p2 := p
	p2.ID = strings.Repeat("b", 32)
	second.Policy = &p2
	a, e := Fingerprint(CanonicalCommand(first))
	if e != nil {
		t.Fatal(e)
	}
	b, e := Fingerprint(CanonicalCommand(second))
	if e != nil || a != b {
		t.Fatal("transport retry metadata changed payload fingerprint")
	}
	second.Amount++
	b, e = Fingerprint(CanonicalCommand(second))
	if e != nil || a == b {
		t.Fatal("conflicting amount reused financial fingerprint")
	}
	if first.Key != "fixture-idempotency" || first.Policy.ID != strings.Repeat("a", 32) {
		t.Fatal("canonicalization mutated original")
	}
	left, e := Fingerprint(map[string]int64{"b": 2, "a": 1})
	if e != nil {
		t.Fatal(e)
	}
	right, e := Fingerprint(map[string]int64{"a": 1, "b": 2})
	if e != nil || left != right {
		t.Fatal("map ordering changed canonical fingerprint")
	}
	if _, e := Fingerprint(make(chan int)); e == nil {
		t.Fatal("unsupported payload fingerprint succeeded")
	}
}

func TestWithdrawalReviewDoesNotInventSettlement(t *testing.T) {
	for _, action := range []string{"approve", "reject", "cancel"} {
		r := &fixtureWalletRepository{result: Withdrawal{State: action, Settlement: "not_configured"}}
		c := Command{Kind: "transition", ID: strings.Repeat("a", 32), Action: action, Reason: "synthetic test review", Key: "review-key", Correlation: "operation"}
		out, e := New(r).Execute(context.Background(), access.Credentials{}, c)
		if e != nil {
			t.Fatal(e)
		}
		value, ok := out.(Withdrawal)
		if !ok || value.Settlement != "not_configured" || r.executes != 1 {
			t.Fatal("review/cancel invented settlement or retried")
		}
	}
	raw, e := json.Marshal(Eligibility{Eligible: false, Reasons: []string{string(ConfigurationRequired)}, Settlement: "not_configured"})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), `"eligible":true`) {
		t.Fatal("unconfigured settlement declared eligible")
	}
	for _, v := range []string{"", " a", "a ", "a\n", "a/../b", strings.Repeat("x", 65)} {
		if ValidKey(v) || SafeReference(v) {
			t.Fatalf("unsafe financial key/reference %q accepted", v)
		}
	}
	if !ValidKey("fixture:credit-1.0") || !SafeReference("fixture:credit-1.0") {
		t.Fatal("bounded safe source reference denied")
	}
}

func TestServiceValidCommandDispatchAndNoSilentRetry(t *testing.T) {
	commands := []Command{
		validCommand(),
		{Kind: "withdrawal", Amount: 10},
		{Kind: "gift", Target: strings.Repeat("b", 32), Quantity: 1000, GiftID: "fixture-rose"},
		{Kind: "adjust", Target: strings.Repeat("b", 32), Amount: 1, Direction: "credit", Reason: "synthetic credit", Reference: "fixture:source"},
		{Kind: "adjust", Target: strings.Repeat("b", 32), Amount: 1, Direction: "debit", Reason: "synthetic compensation", Reference: "fixture:compensation"},
		{Kind: "transition", ID: strings.Repeat("b", 32), Action: "cancel", Reason: "synthetic cancellation"},
	}
	for _, c := range commands {
		c.Key = "fixture-key"
		c.Correlation = "fixture-operation"
		ambiguous := errors.New("ambiguous commit fixture")
		r := &fixtureWalletRepository{err: ambiguous}
		_, e := New(r).Execute(context.Background(), access.Credentials{}, c)
		if !errors.Is(e, ambiguous) || r.executes != 1 {
			t.Fatalf("command %s rejected or silently retried: %v", c.Kind, e)
		}
	}
	for _, action := range []string{"settle", "paid", "capture", "refund", "approve\n"} {
		c := Command{Kind: "transition", ID: strings.Repeat("b", 32), Action: action, Reason: "synthetic test", Key: "fixture-key", Correlation: "fixture-operation"}
		r := &fixtureWalletRepository{}
		if _, e := New(r).Execute(context.Background(), access.Credentials{}, c); e == nil || r.executes != 0 {
			t.Fatalf("unsupported settlement action %q accepted", action)
		}
	}
}

func TestFinancialEffectiveTimestampPrecision(t *testing.T) {
	at := time.Date(2026, time.October, 31, 23, 59, 59, 123456789, time.FixedZone("fixture", 6*3600))
	p := fixtureTransferPolicy()
	p.EffectiveAt = at
	r := &fixtureWalletRepository{}
	c := Command{Kind: "policy", Key: "policy-time", Correlation: "operation", Reason: "explicit precision fixture", Policy: &p}
	if _, e := New(r).Execute(context.Background(), access.Credentials{}, c); e != nil {
		t.Fatal(e)
	}
	if !r.command.Policy.EffectiveAt.Equal(at.UTC().Truncate(time.Microsecond)) || !p.EffectiveAt.Equal(at) {
		t.Fatal("policy timestamp storage precision or input mutation")
	}
	end := at.Add(time.Hour)
	a := Association{AccountID: strings.Repeat("a", 32), AgencyID: strings.Repeat("b", 32), Classification: agency.AudioHost, EffectiveFrom: at, EffectiveUntil: &end}
	if _, e := New(r).Execute(context.Background(), access.Credentials{}, Command{Kind: "association", Key: "association-time", Correlation: "operation", Reason: "explicit precision fixture", Association: &a}); e != nil {
		t.Fatal(e)
	}
	if !r.command.Association.EffectiveFrom.Equal(at.UTC().Truncate(time.Microsecond)) || !r.command.Association.EffectiveUntil.Equal(end.UTC().Truncate(time.Microsecond)) || !a.EffectiveFrom.Equal(at) || !a.EffectiveUntil.Equal(end) {
		t.Fatal("association timestamp precision or caller mutation")
	}
}

// Golden from c749cec's Command projection: adding live-room fields must not
// invalidate previously committed non-room idempotency fingerprints.
func TestPreDeliveryCommandFingerprintPreserved(t *testing.T) {
	c := Command{Kind: "gift", Target: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GiftID: "rose", Quantity: 2}
	sum, e := Fingerprint(CanonicalCommand(c))
	if e != nil || sum != "18dd3ac96f68bdbbf6d73b8429cd43ddbaacd9ca8a117dcb18c9bacd71ad5d6a" {
		t.Fatal("baseline idempotency fingerprint changed", e)
	}
	c.RoomID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	c.SessionID = "cccccccccccccccccccccccccccccccc"
	live, e := Fingerprint(CanonicalCommand(c))
	if e != nil || live == sum {
		t.Fatal("live gift fingerprint lacks room binding")
	}
}
