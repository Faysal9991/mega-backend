// Package wallet owns whole-diamond policy, arithmetic and financial commands.
package wallet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

const (
	ConfigurationRequired apperror.Code = "FINANCIAL_CONFIGURATION_REQUIRED"
	InsufficientFunds     apperror.Code = "INSUFFICIENT_DIAMONDS"
	ActivityUnavailable   apperror.Code = "VERIFIED_ACTIVITY_UNAVAILABLE"
	NotEligible           apperror.Code = "WITHDRAWAL_NOT_ELIGIBLE"
	PolicyRead                          = "economy.policies.read"
	PolicyWrite                         = "economy.policies.write"
	OperationsRead                      = "wallet.operations.read"
	AdjustPermission                    = "wallet.adjust"
	ReviewPermission                    = "withdrawals.review"
	AssociationPermission               = "agency.hosts.manage"
)

type Fee struct {
	Fixed    int64  `json:"fixed"`
	BPS      int64  `json:"bps"`
	Rounding string `json:"rounding"`
}
type Policy struct {
	ID              string           `json:"policy_id"`
	Kind            string           `json:"kind"`
	Scope           string           `json:"scope"`
	PreviousID      string           `json:"previous_policy_id"`
	EffectiveAt     time.Time        `json:"effective_at"`
	Enabled         bool             `json:"enabled"`
	ReviewOnly      bool             `json:"review_only"`
	TaxJurisdiction string           `json:"tax_jurisdiction"`
	Min             int64            `json:"min_diamonds"`
	Max             int64            `json:"max_diamonds"`
	DailyLimit      int64            `json:"daily_limit"`
	MonthlyLimit    int64            `json:"monthly_limit"`
	Fee             Fee              `json:"fee"`
	VAT             string           `json:"vat"`
	Rounding        string           `json:"rounding"`
	HostBPS         int64            `json:"host_bps"`
	AgencyBPS       int64            `json:"agency_bps"`
	PlatformBPS     int64            `json:"platform_bps"`
	TaxBPS          int64            `json:"tax_bps"`
	Catalog         map[string]int64 `json:"catalog"`
	Activity        *agency.Policy   `json:"activity"`
}
type Balance struct {
	AccountID string `json:"account_id"`
	Available int64  `json:"available_diamonds"`
	Held      int64  `json:"held_diamonds"`
	Unit      string `json:"unit"`
}
type Operation struct {
	ID        string    `json:"operation_id"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor_id"`
	Target    string    `json:"counterparty_id"`
	Amount    int64     `json:"diamonds"`
	Fee       int64     `json:"fee_diamonds"`
	PolicyID  string    `json:"policy_id"`
	Reference string    `json:"reference"`
	CreatedAt time.Time `json:"created_at"`
}
type Posting struct {
	Account string `json:"ledger_account"`
	Delta   int64  `json:"delta_diamonds"`
}
type Detail struct {
	Operation Operation       `json:"operation"`
	Postings  []Posting       `json:"postings"`
	Policy    json.RawMessage `json:"policy_snapshot"`
}
type Withdrawal struct {
	ID         string    `json:"withdrawal_id"`
	AccountID  string    `json:"account_id"`
	Amount     int64     `json:"diamonds"`
	Fee        int64     `json:"fee_diamonds"`
	Reserved   int64     `json:"reserved_diamonds"`
	PolicyID   string    `json:"policy_id"`
	State      string    `json:"state"`
	Settlement string    `json:"settlement_status"`
	CreatedAt  time.Time `json:"created_at"`
}
type Eligibility struct {
	Eligible       bool           `json:"eligible"`
	Reasons        []string       `json:"reasons"`
	PolicyID       string         `json:"policy_id"`
	Classification string         `json:"classification"`
	Activity       *agency.Result `json:"activity,omitempty"`
	Available      int64          `json:"available_diamonds"`
	Settlement     string         `json:"settlement_status"`
}
type Association struct {
	ID             string     `json:"association_id"`
	AccountID      string     `json:"account_id"`
	AgencyID       string     `json:"agency_id"`
	Classification string     `json:"classification"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until"`
}
type PolicySummary struct {
	Transfer   *Policy  `json:"transfer"`
	Withdrawal *Policy  `json:"withdrawal"`
	Gifts      []Policy `json:"gifts"`
	Settlement string   `json:"settlement_status"`
}

// Query is a closed set of bounded wallet projections; no raw SQL or caller actor IDs.
type Query struct {
	Kind, ID, Cursor, Filter string
	Limit                    int
}
type ReadResult struct {
	Data    any
	Next    string
	HasMore bool
	Limit   int
}

// Command is server-constructed by strict transport DTOs, never decoded directly.
type Command struct {
	Kind, Target, ID, Key, Reason, Correlation, Reference, Action, GiftID, Direction string
	Amount, Quantity                                                                 int64
	Policy                                                                           *Policy
	Association                                                                      *Association
}
type Repository interface {
	Read(context.Context, access.Credentials, Query) (ReadResult, error)
	Execute(context.Context, access.Credentials, Command) (any, error)
}
type Service struct{ repo Repository }

func New(repo Repository) *Service   { return &Service{repo: repo} }
func Error(code apperror.Code) error { return apperror.New(code, nil) }
func (s *Service) Read(ctx context.Context, c access.Credentials, q Query) (ReadResult, error) {
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Filter != "" {
		return ReadResult{}, Error(apperror.ValidationFailed)
	}
	if q.Limit < 1 || q.Limit > 50 || q.Cursor != "" && !access.ValidID(q.Cursor) || q.ID != "" && !access.ValidID(q.ID) {
		return ReadResult{}, Error(apperror.ValidationFailed)
	}
	if !slices.Contains([]string{"balance", "history", "transfers", "operation", "eligibility", "withdrawals", "withdrawal", "summary", "policies", "inspect", "reviews", "associations"}, q.Kind) {
		return ReadResult{}, Error(apperror.ValidationFailed)
	}
	if slices.Contains([]string{"operation", "withdrawal", "inspect", "associations"}, q.Kind) && !access.ValidID(q.ID) {
		return ReadResult{}, Error(apperror.ValidationFailed)
	}
	return s.repo.Read(ctx, c, q)
}
func (s *Service) Execute(ctx context.Context, c access.Credentials, in Command) (any, error) {
	if !ValidKey(in.Key) || !access.ValidCorrelation(in.Correlation) || in.Target != "" && !access.ValidID(in.Target) || in.ID != "" && !access.ValidID(in.ID) {
		return nil, Error(apperror.ValidationFailed)
	}
	switch in.Kind {
	case "transfer":
		if !access.ValidID(in.Target) || in.Amount < 1 {
			return nil, Error(apperror.ValidationFailed)
		}
	case "withdrawal":
		if in.Amount < 1 {
			return nil, Error(apperror.ValidationFailed)
		}
	case "gift":
		if in.Quantity < 1 || !ValidCatalogID(in.GiftID) || !access.ValidID(in.Target) {
			return nil, Error(apperror.ValidationFailed)
		}
	case "adjust":
		if in.Amount < 1 || !access.ValidID(in.Target) || !slices.Contains([]string{"credit", "debit"}, in.Direction) || !access.ValidReason(in.Reason) || !ValidKey(in.Reference) {
			return nil, Error(apperror.ValidationFailed)
		}
	case "transition":
		if !access.ValidID(in.ID) || !slices.Contains([]string{"approve", "reject", "cancel"}, in.Action) || !access.ValidReason(in.Reason) {
			return nil, Error(apperror.ValidationFailed)
		}
	case "policy":
		if in.Policy == nil || !access.ValidReason(in.Reason) {
			return nil, Error(apperror.ValidationFailed)
		}
		p := *in.Policy
		p.ID = access.ID()
		p.EffectiveAt = p.EffectiveAt.UTC().Truncate(time.Microsecond)
		if p.Activity != nil {
			a := *p.Activity
			a.Version = p.ID
			a.EffectiveAt = p.EffectiveAt
			p.Activity = &a
		}
		if e := p.Validate(); e != nil {
			return nil, e
		}
		in.Policy = &p
	case "association":
		if in.Association == nil || !access.ValidReason(in.Reason) {
			return nil, Error(apperror.ValidationFailed)
		}
		a := *in.Association
		a.EffectiveFrom = a.EffectiveFrom.UTC().Truncate(time.Microsecond)
		if a.EffectiveUntil != nil {
			until := a.EffectiveUntil.UTC().Truncate(time.Microsecond)
			a.EffectiveUntil = &until
		}
		if !access.ValidID(a.AccountID) || !access.ValidID(a.AgencyID) || a.AccountID == a.AgencyID || !slices.Contains([]string{"audio_host", "video_host"}, a.Classification) || a.EffectiveFrom.IsZero() || a.EffectiveUntil != nil && !a.EffectiveUntil.After(a.EffectiveFrom) {
			return nil, Error(apperror.ValidationFailed)
		}
		in.Association = &a
	default:
		return nil, Error(apperror.ValidationFailed)
	}
	return s.repo.Execute(ctx, c, in)
}
func ValidKey(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}
func ValidCatalogID(s string) bool { return ValidKey(s) && len(s) <= 32 }
func Fingerprint(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func Add(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, Error(apperror.ValidationFailed)
	}
	return a + b, nil
}
func Multiply(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a != 0 && b > math.MaxInt64/a {
		return 0, Error(apperror.ValidationFailed)
	}
	return a * b, nil
}
func (f Fee) Calculate(amount int64) (int64, error) {
	if amount < 0 || f.Fixed < 0 || f.BPS < 0 || f.BPS > 10000 || f.Rounding != "ceil" {
		return 0, Error(ConfigurationRequired)
	}
	whole := amount / 10000 * f.BPS
	remainder := amount % 10000 * f.BPS
	variable, e := Add(whole, (remainder+9999)/10000)
	if e != nil {
		return 0, e
	}
	return Add(variable, f.Fixed)
}
func (p Policy) Validate() error {
	if !slices.Contains([]string{"transfer", "withdrawal", "gift", "activity"}, p.Kind) || p.EffectiveAt.IsZero() || p.PreviousID != "" && !access.ValidID(p.PreviousID) {
		return Error(apperror.ValidationFailed)
	}
	if p.Kind == "withdrawal" && !p.ReviewOnly {
		return Error(ConfigurationRequired)
	}
	if p.Kind == "activity" {
		if p.Activity == nil || p.Scope != p.Activity.Scope {
			return Error(apperror.ValidationFailed)
		}
		if p.Activity.Validate() != nil {
			return Error(apperror.ValidationFailed)
		}
		return nil
	}
	if p.Kind == "gift" {
		if !slices.Contains([]string{"agency", "non_agency"}, p.Scope) || !ValidKey(p.TaxJurisdiction) || p.VAT != "inclusive_diamonds" || p.Rounding != "largest_remainder" || len(p.Catalog) < 1 || len(p.Catalog) > 100 {
			return Error(ConfigurationRequired)
		}
		sum := int64(0)
		for _, n := range []int64{p.HostBPS, p.AgencyBPS, p.PlatformBPS, p.TaxBPS} {
			if n < 0 || n > 10000 {
				return Error(apperror.ValidationFailed)
			}
			sum += n
		}
		if sum != 10000 || p.Scope == "non_agency" && p.AgencyBPS != 0 {
			return Error(apperror.ValidationFailed)
		}
		for id, price := range p.Catalog {
			if !ValidCatalogID(id) || price < 1 {
				return Error(apperror.ValidationFailed)
			}
		}
		return nil
	}
	if p.Scope != "all" || p.Min < 1 || p.Max < p.Min || p.DailyLimit < 0 || p.MonthlyLimit < 0 || p.DailyLimit > 0 && p.DailyLimit < p.Min || p.MonthlyLimit > 0 && p.MonthlyLimit < p.Min {
		return Error(apperror.ValidationFailed)
	}
	fee, e := p.Fee.Calculate(p.Max)
	if e != nil {
		return e
	}
	_, e = Add(p.Max, fee)
	return e
}

// Allocate conserves total with largest-remainder rounding; ties use the fixed
// host, agency, platform, tax order selected explicitly by the stored policy.
func (p Policy) Allocate(total int64) ([4]int64, error) {
	var out [4]int64
	if total < 1 || p.Kind != "gift" {
		return out, Error(apperror.ValidationFailed)
	}
	if e := p.Validate(); e != nil {
		return out, e
	}
	rates := [4]int64{p.HostBPS, p.AgencyBPS, p.PlatformBPS, p.TaxBPS}
	var rem [4]int64
	left := total
	for i, r := range rates {
		out[i] = total/10000*r + (total%10000*r)/10000
		rem[i] = (total % 10000 * r) % 10000
		left -= out[i]
	}
	order := []int{0, 1, 2, 3}
	slices.SortStableFunc(order, func(a, b int) int {
		if rem[a] > rem[b] {
			return -1
		}
		if rem[a] < rem[b] {
			return 1
		}
		return 0
	})
	for i := int64(0); i < left; i++ {
		out[order[i]]++
	}
	return out, nil
}
func CanonicalCommand(c Command) Command {
	c.Key = ""
	c.Correlation = ""
	if c.Policy != nil {
		p := *c.Policy
		p.ID = ""
		if p.Activity != nil {
			a := *p.Activity
			a.Version = ""
			p.Activity = &a
		}
		c.Policy = &p
	}
	return c
}
func SafeReference(s string) bool { return ValidKey(strings.TrimSpace(s)) && strings.TrimSpace(s) == s }
