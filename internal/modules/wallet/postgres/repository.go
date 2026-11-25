// Package postgres implements the single Wallet Engine and composes the existing
// authoritative Access/Identity transaction without changing its lock protocol.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/accounts"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const notConfigured = "not_configured"

type Repository struct {
	pool   *pgxpool.Pool
	guard  FinancialGuard
	source agency.ActivitySource
	gifts  RoomGifts
}

func New(pool *pgxpool.Pool, source agency.ActivitySource) (*Repository, error) {
	g, e := accesspg.New(pool)
	if e != nil {
		return nil, e
	}
	if source == nil {
		source = agency.UnavailableSource{}
	}
	return &Repository{pool: pool, guard: g, source: source}, nil
}
func timestamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t.UTC(), Valid: true} }
func text(s string) pgtype.Text                { return pgtype.Text{String: s, Valid: true} }
func safe(e error) error {
	if e == nil {
		return nil
	}
	var a *apperror.Error
	if errors.As(e, &a) {
		return a
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return wallet.Error(apperror.NotFound)
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "23505" || p.Code == "23503") {
		return wallet.Error(apperror.Conflict)
	}
	return wallet.Error(apperror.ServiceUnavailable)
}
func (r *Repository) Ready(ctx context.Context) error {
	v, e := walletsql.New(r.pool).SchemaReady(ctx)
	if e != nil || !v.Valid || !v.Bool {
		return wallet.Error(apperror.ServiceUnavailable)
	}
	return nil
}
func op(id, kind, actor, target string, amount, fee int64, policy, ref string, created pgtype.Timestamptz) wallet.Operation {
	return wallet.Operation{ID: id, Kind: kind, Actor: actor, Target: target, Amount: amount, Fee: fee, PolicyID: policy, Reference: ref, CreatedAt: created.Time.UTC()}
}
func withdrawal(v walletsql.WithdrawalRow) wallet.Withdrawal {
	return wallet.Withdrawal{ID: v.ID, AccountID: v.AccountID, Amount: v.Amount, Fee: v.Fee, Reserved: v.Reserved, PolicyID: v.PolicyID, State: v.State, CreatedAt: v.CreatedAt.Time.UTC(), Settlement: notConfigured}
}
func policy(ctx context.Context, q *walletsql.Queries, kind, scope string) (wallet.Policy, error) {
	v, e := q.ActivePolicy(ctx, walletsql.ActivePolicyParams{Kind: kind, Scope: scope})
	if e != nil {
		return wallet.Policy{}, e
	}
	var p wallet.Policy
	if e = json.Unmarshal(v.Config, &p); e != nil {
		return p, e
	}
	if p.ID != v.ID || p.Kind != kind || p.Scope != scope {
		return p, wallet.Error(apperror.ServiceUnavailable)
	}
	if e := p.Validate(); e != nil {
		return p, wallet.Error(wallet.ConfigurationRequired)
	}
	return p, nil
}
func balance(ctx context.Context, q *walletsql.Queries, actor string) (wallet.Balance, error) {
	v, e := q.Wallet(ctx, text(actor))
	return wallet.Balance{AccountID: actor, Available: v.Available, Held: v.Held, Unit: "diamonds"}, e
}
func active(ctx context.Context, tx pgx.Tx, id string) error {
	a, e := identitypg.BindSecurity(tx).Account(ctx, id)
	if e != nil {
		return e
	}
	if a.Status != "active" {
		return wallet.Error(apperror.Forbidden)
	}
	return accounts.Bind(tx).Initialize(ctx, id)
}
func detail(ctx context.Context, q *walletsql.Queries, id, actor string, admin bool) (wallet.Detail, error) {
	v, e := q.Operation(ctx, id)
	if e != nil {
		return wallet.Detail{}, e
	}
	if !admin {
		ok, e := q.Participant(ctx, walletsql.ParticipantParams{OperationID: id, OwnerID: text(actor)})
		if e != nil {
			return wallet.Detail{}, e
		}
		if !ok {
			return wallet.Detail{}, wallet.Error(apperror.NotFound)
		}
	}
	entries, e := q.Entries(ctx, id)
	if e != nil {
		return wallet.Detail{}, e
	}
	postings := []wallet.Posting{}
	for _, p := range entries {
		if admin || p.AccountID == "user:"+actor || p.AccountID == "held:"+actor {
			postings = append(postings, wallet.Posting{Account: p.AccountID, Delta: p.Delta})
		}
	}
	return wallet.Detail{Operation: op(v.ID, v.Kind, v.ActorID, v.TargetID, v.Amount, v.Fee, v.PolicyID, v.Reference, v.CreatedAt), Postings: postings, Policy: v.PolicySnapshot}, nil
}
func (r *Repository) Read(ctx context.Context, c access.Credentials, in wallet.Query) (out wallet.ReadResult, err error) {
	permission := ""
	owner := false
	target := ""
	var snapshot *activitySnapshot
	if in.Kind == "eligibility" {
		snapshot, err = r.activitySnapshot(ctx, c)
		if err != nil {
			return out, safe(err)
		}
	}
	switch in.Kind {
	case "policies":
		permission = wallet.PolicyRead
	case "inspect":
		permission = wallet.OperationsRead
	case "reviews":
		permission = wallet.ReviewPermission
	case "associations":
		permission = wallet.AssociationPermission
		owner = true
		target = in.ID
	}
	err = r.guard.Financial(ctx, c, permission, owner, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := walletsql.New(tx)
		actor := u.Actor()
		out.Limit = in.Limit
		switch in.Kind {
		case "balance":
			v, e := balance(ctx, q, actor)
			out.Data = v
			return e
		case "summary":
			v := wallet.PolicySummary{Gifts: []wallet.Policy{}, Settlement: notConfigured}
			for _, kind := range []string{"transfer", "withdrawal"} {
				p, e := policy(ctx, q, kind, "all")
				if e != nil && !errors.Is(e, pgx.ErrNoRows) {
					return e
				}
				if e == nil {
					if kind == "transfer" {
						v.Transfer = &p
					} else {
						v.Withdrawal = &p
					}
				}
			}
			for _, scope := range []string{"agency", "non_agency"} {
				p, e := policy(ctx, q, "gift", scope)
				if e != nil && !errors.Is(e, pgx.ErrNoRows) {
					return e
				}
				if e == nil {
					v.Gifts = append(v.Gifts, p)
				}
			}
			out.Data = v
			return nil
		case "history", "transfers":
			filter := ""
			if in.Kind == "transfers" {
				filter = "transfer"
			}
			rows, e := q.History(ctx, walletsql.HistoryParams{AccountID: text(actor), Cursor: in.Cursor, Kind: filter, PageLimit: int32(in.Limit + 1)})
			if e != nil {
				return e
			}
			items := []wallet.Operation{}
			for _, v := range rows {
				items = append(items, op(v.ID, v.Kind, v.ActorID, v.TargetID, v.Amount, v.Fee, v.PolicyID, v.Reference, v.CreatedAt))
			}
			if len(items) > in.Limit {
				out.HasMore = true
				items = items[:in.Limit]
				out.Next = items[len(items)-1].ID
			}
			out.Data = items
			return nil
		case "operation", "inspect":
			v, e := detail(ctx, q, in.ID, actor, in.Kind == "inspect")
			out.Data = v
			return e
		case "withdrawal":
			v, e := q.Withdrawal(ctx, in.ID)
			if e != nil {
				return e
			}
			if v.AccountID != actor {
				return wallet.Error(apperror.NotFound)
			}
			out.Data = withdrawal(v)
			return nil
		case "withdrawals", "reviews":
			filter := actor
			if in.Kind == "reviews" {
				filter = ""
			}
			rows, e := q.Withdrawals(ctx, walletsql.WithdrawalsParams{AccountID: filter, Cursor: in.Cursor, PageLimit: int32(in.Limit + 1)})
			if e != nil {
				return e
			}
			items := []wallet.Withdrawal{}
			for _, v := range rows {
				items = append(items, withdrawal(walletsql.WithdrawalRow(v)))
			}
			if len(items) > in.Limit {
				out.HasMore = true
				items = items[:in.Limit]
				out.Next = items[len(items)-1].ID
			}
			out.Data = items
			return nil
		case "eligibility":
			v, _, e := eligibility(ctx, q, actor, snapshot)
			out.Data = v
			return e
		case "policies":
			rows, e := q.Policies(ctx, walletsql.PoliciesParams{ID: in.Cursor, Limit: int32(in.Limit + 1)})
			if e != nil {
				return e
			}
			items := []wallet.Policy{}
			for _, raw := range rows {
				var p wallet.Policy
				if e := json.Unmarshal(raw, &p); e != nil {
					return e
				}
				items = append(items, p)
			}
			if len(items) > in.Limit {
				out.HasMore = true
				items = items[:in.Limit]
				out.Next = items[len(items)-1].ID
			}
			out.Data = items
			return nil
		case "associations":
			rows, e := q.AssociationHistory(ctx, walletsql.AssociationHistoryParams{AccountID: in.ID, ID: in.Cursor, Limit: int32(in.Limit + 1)})
			if e != nil {
				return e
			}
			items := []wallet.Association{}
			for _, v := range rows {
				items = append(items, association(v))
			}
			if len(items) > in.Limit {
				out.HasMore = true
				items = items[:in.Limit]
				out.Next = items[len(items)-1].ID
			}
			out.Data = items
			return nil
		default:
			return wallet.Error(apperror.ValidationFailed)
		}
	})
	return out, safe(err)
}
func association(v walletsql.AppAgencyHostAssociation) wallet.Association {
	a := wallet.Association{ID: v.ID, AccountID: v.AccountID, AgencyID: v.AgencyID, Classification: v.Classification, EffectiveFrom: v.EffectiveFrom.Time.UTC()}
	if v.EffectiveUntil.Valid {
		t := v.EffectiveUntil.Time.UTC()
		a.EffectiveUntil = &t
	}
	return a
}
func periodLimits(ctx context.Context, q *walletsql.Queries, actor, kind string, amount int64, p wallet.Policy, now time.Time) error {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, v := range []struct {
		start, end time.Time
		max        int64
	}{{day, day.AddDate(0, 0, 1), p.DailyLimit}, {month, month.AddDate(0, 1, 0), p.MonthlyLimit}} {
		if v.max == 0 {
			continue
		}
		raw, e := q.PeriodTotal(ctx, walletsql.PeriodTotalParams{ActorID: actor, Kind: kind, CreatedAt: timestamp(v.start), CreatedAt_2: timestamp(v.end)})
		if e != nil {
			return e
		}
		sum, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return wallet.Error(apperror.ServiceUnavailable)
		}
		sum.Add(sum, big.NewInt(amount))
		if sum.Cmp(big.NewInt(v.max)) > 0 {
			if kind == "transfer" {
				return wallet.Error(apperror.ValidationFailed)
			}
			return wallet.Error(wallet.NotEligible)
		}
	}
	return nil
}
func post(ctx context.Context, q *walletsql.Queries, operation walletsql.InsertOperationParams, deltas map[string]int64) error {
	ids := []string{}
	for id, amount := range deltas {
		if amount != 0 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	amounts := make([]int64, 0, len(ids))
	for _, id := range ids {
		current, e := q.AccountBalance(ctx, id)
		if e != nil {
			return e
		}
		next, e := wallet.Add(current, deltas[id])
		if e != nil {
			return e
		}
		if id == "platform:issuance" && next == -1<<63 {
			return wallet.Error(apperror.ValidationFailed)
		}
		if id != "platform:issuance" && next < 0 {
			return wallet.Error(wallet.InsufficientFunds)
		}
		if id == "platform:issuance" && next > 0 {
			return wallet.Error(apperror.Conflict)
		}
		amounts = append(amounts, deltas[id])
	}
	if e := q.InsertOperation(ctx, operation); e != nil {
		return e
	}
	return q.Post(ctx, walletsql.PostParams{Column1: operation.ID, Column2: ids, Column3: amounts})
}

type FinancialGuard interface {
	Financial(context.Context, access.Credentials, string, bool, string, func(context.Context, pgx.Tx, access.Unit) error) error
}

// RoomGifts resolves nonfinancial live/catalog authority and records delivery
// intent inside the Wallet-owned transaction. Implementations cannot post money.
type GiftCatalogSnapshot struct {
	Price    int64
	Snapshot json.RawMessage
}
type RoomGifts interface {
	Resolve(context.Context, pgx.Tx, string, wallet.Command) (GiftCatalogSnapshot, error)
	Record(context.Context, pgx.Tx, string, string, wallet.Command, int64, GiftCatalogSnapshot) error
}

func (r *Repository) WithRoomGifts(g RoomGifts) *Repository { r.gifts = g; return r }
