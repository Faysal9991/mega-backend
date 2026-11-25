package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
)

type activitySnapshot struct {
	AssociationID, PolicyID string
	Result                  agency.Result
	At                      time.Time
}

// Counts cannot decrease within a business month for an authoritative completed
// snapshot. Revalidate its month and cutoff under the financial lock; do not
// compare the advancing check cutoff as though it were a fixed month-end.
func (s *activitySnapshot) validAt(actor string, policy agency.Policy, at time.Time) bool {
	probe, err := agency.Calculate(actor, policy, at, nil)
	return err == nil && !at.Before(s.At) && s.Result.PeriodEnd.Equal(s.At) && probe.PeriodStart.Equal(s.Result.PeriodStart)
}

func activityPolicy(ctx context.Context, q *walletsql.Queries, classification string) (wallet.Policy, error) {
	p, e := policy(ctx, q, "activity", classification)
	if errors.Is(e, pgx.ErrNoRows) {
		return policy(ctx, q, "activity", "agency")
	}
	return p, e
}

// Trusted collection is fetched before the security/funds transaction. A changed
// association/policy between collection and mutation fails closed instead of
// authorizing with a stale eligibility result or calling providers under locks.
func (r *Repository) activitySnapshot(ctx context.Context, c access.Credentials) (*activitySnapshot, error) {
	if len(c.Access) != 43 || !identity.ValidInstallation(c.Installation) {
		return nil, wallet.Error(apperror.Unauthenticated)
	}
	auth, e := identitypg.New(r.pool)
	if e != nil {
		return nil, e
	}
	p, e := auth.Access(ctx, identity.Digest(c.Access), c.Installation)
	if e != nil {
		if errors.Is(e, identity.ErrMissing) {
			return nil, wallet.Error(apperror.Unauthenticated)
		}
		return nil, e
	}
	q := walletsql.New(r.pool)
	association, e := q.CurrentAssociation(ctx, p.Account.ID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	selected, e := activityPolicy(ctx, q, association.Classification)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !selected.Enabled || selected.Activity == nil {
		return nil, nil
	}
	clock, clockErr := q.Clock(ctx)
	if clockErr != nil {
		return nil, clockErr
	}
	at := clock.Time.UTC()
	result, e := agency.Eligibility(ctx, r.source, p.Account.ID, *selected.Activity, at)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result.Eligible = false
		result.Reason = agency.ReasonUnavailable
	}
	return &activitySnapshot{AssociationID: association.ID, PolicyID: selected.ID, Result: result, At: at}, nil
}
func eligibility(ctx context.Context, q *walletsql.Queries, actor string, snapshot *activitySnapshot) (out wallet.Eligibility, p wallet.Policy, err error) {
	out = wallet.Eligibility{Reasons: []string{}, Settlement: notConfigured}
	b, e := balance(ctx, q, actor)
	if e != nil {
		return out, p, e
	}
	out.Available = b.Available
	p, e = policy(ctx, q, "withdrawal", "all")
	if errors.Is(e, pgx.ErrNoRows) {
		out.Reasons = append(out.Reasons, "CONFIGURATION_REQUIRED")
		return out, p, nil
	}
	if e != nil {
		return out, p, e
	}
	out.PolicyID = p.ID
	if !p.Enabled || !p.ReviewOnly {
		out.Reasons = append(out.Reasons, "CONFIGURATION_REQUIRED")
	}
	fee, e := p.Fee.Calculate(p.Min)
	if e != nil {
		return out, p, e
	}
	minimum, e := wallet.Add(p.Min, fee)
	if e != nil {
		return out, p, e
	}
	if b.Available < minimum {
		out.Reasons = append(out.Reasons, "INSUFFICIENT_DIAMONDS")
	}
	a, e := q.CurrentAssociation(ctx, actor)
	if e == nil {
		out.Classification = a.Classification
		selected, e := activityPolicy(ctx, q, a.Classification)
		if errors.Is(e, pgx.ErrNoRows) {
			out.Reasons = append(out.Reasons, "CONFIGURATION_REQUIRED")
		} else if e != nil {
			return out, p, e
		} else if !selected.Enabled || selected.Activity == nil {
			out.Reasons = append(out.Reasons, "CONFIGURATION_REQUIRED")
		} else {
			if snapshot == nil || snapshot.AssociationID != a.ID || snapshot.PolicyID != selected.ID {
				out.Reasons = append(out.Reasons, agency.ReasonUnavailable)
			} else {
				// A month change during a long collection must not authorize the old month.
				clock, clockErr := q.Clock(ctx)
				if clockErr != nil {
					return out, p, clockErr
				}
				if !snapshot.validAt(actor, *selected.Activity, clock.Time.UTC()) {
					return out, p, wallet.Error(apperror.Conflict)
				}
				result := snapshot.Result
				out.Activity = &result
				if !result.Eligible {
					out.Reasons = append(out.Reasons, result.Reason)
				}
			}
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, p, e
	}
	out.Eligible = len(out.Reasons) == 0
	return out, p, nil
}
