package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/accounts"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres/walletsql"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) Execute(ctx context.Context, c access.Credentials, in wallet.Command) (out any, err error) {
	permission := ""
	owner := false
	target := in.Target
	switch in.Kind {
	case "policy":
		permission = wallet.PolicyWrite
		owner = true
	case "adjust":
		permission = wallet.AdjustPermission
		owner = true
	case "association":
		permission = wallet.AssociationPermission
		owner = true
		target = in.Association.AccountID
	case "transition":
		if in.Action != "cancel" {
			permission = wallet.ReviewPermission
		}
	}
	var snapshot *activitySnapshot
	if in.Kind == "withdrawal" {
		snapshot, err = r.activitySnapshot(ctx, c)
		if err != nil {
			return nil, safe(err)
		}
	}
	fingerprint, e := wallet.Fingerprint(wallet.CanonicalCommand(in))
	if e != nil {
		return nil, safe(e)
	}
	err = r.guard.Financial(ctx, c, permission, owner, target, func(ctx context.Context, tx pgx.Tx, u access.Unit) error {
		q := walletsql.New(tx)
		actor := u.Actor()
		prior, e := q.ExistingCommand(ctx, walletsql.ExistingCommandParams{ActorID: actor, Kind: in.Kind, IdempotencyKey: in.Key})
		if e == nil {
			if prior.Fingerprint != fingerprint {
				return wallet.Error(apperror.Conflict)
			}
			out = json.RawMessage(prior.Result)
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		now, e := q.Clock(ctx)
		if e != nil {
			return e
		}
		if e := accounts.Bind(tx).Initialize(ctx, actor); e != nil {
			return e
		}
		var policyID string
		var p wallet.Policy
		var total, fee int64
		var operationID string
		var approvedEligibility wallet.Eligibility
		audit := false
		fields := []string{}
		action := in.Kind
		switch in.Kind {
		case "policy":
			p = *in.Policy
			previous, e := q.LatestPolicy(ctx, walletsql.LatestPolicyParams{Kind: p.Kind, Scope: p.Scope})
			if e == nil {
				if p.PreviousID != previous.ID {
					return wallet.Error(apperror.Conflict)
				}
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return e
			} else if p.PreviousID != "" {
				return wallet.Error(apperror.Conflict)
			}
			raw, e := json.Marshal(p)
			if e != nil {
				return e
			}
			if len(raw) > 16384 {
				return wallet.Error(apperror.ValidationFailed)
			}
			if e := q.InsertPolicy(ctx, walletsql.InsertPolicyParams{ID: p.ID, Kind: p.Kind, Scope: p.Scope, PreviousID: p.PreviousID, EffectiveAt: timestamp(p.EffectiveAt), ActorID: actor, Config: string(raw)}); e != nil {
				return e
			}
			out = p
			audit = true
			target = p.ID
			fields = []string{"policy", p.Kind, p.Scope, "previous:" + p.PreviousID, "new:" + p.ID}
		case "association":
			a := *in.Association
			if a.EffectiveFrom.After(now.Time) || a.EffectiveUntil != nil && a.EffectiveUntil.After(now.Time) {
				return wallet.Error(wallet.ConfigurationRequired)
			}
			if e := active(ctx, tx, a.AgencyID); e != nil {
				return e
			}
			current, e := q.CurrentAssociation(ctx, a.AccountID)
			if e == nil {
				closeAt := a.EffectiveFrom
				if a.EffectiveUntil != nil {
					closeAt = *a.EffectiveUntil
				}
				if !closeAt.After(current.EffectiveFrom.Time) {
					return wallet.Error(apperror.Conflict)
				}
				if a.EffectiveUntil != nil && (current.AgencyID != a.AgencyID || current.Classification != a.Classification || !current.EffectiveFrom.Time.Equal(a.EffectiveFrom)) {
					return wallet.Error(apperror.Conflict)
				}
				if e := q.CloseAssociation(ctx, walletsql.CloseAssociationParams{AccountID: a.AccountID, EffectiveUntil: timestamp(closeAt)}); e != nil {
					return e
				}
				if a.EffectiveUntil != nil {
					a.ID = current.ID
					out = a
					break
				}
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			a.ID = access.ID()
			var until pgtype.Timestamptz
			if a.EffectiveUntil != nil {
				until = timestamp(*a.EffectiveUntil)
			}
			if e := q.InsertAssociation(ctx, walletsql.InsertAssociationParams{ID: a.ID, AccountID: a.AccountID, AgencyID: a.AgencyID, Classification: a.Classification, EffectiveFrom: timestamp(a.EffectiveFrom), EffectiveUntil: until}); e != nil {
				return e
			}
			out = a
		case "transition":
			v, e := q.Withdrawal(ctx, in.ID)
			if e != nil {
				return e
			}
			if in.Action == "cancel" && v.AccountID != actor {
				return wallet.Error(apperror.NotFound)
			}
			if in.Action == "approve" {
				if e := active(ctx, tx, v.AccountID); e != nil {
					return e
				}
			}
			desired := map[string]string{"approve": "approved", "reject": "rejected", "cancel": "cancelled"}[in.Action]
			if v.State == "rejected" || v.State == "cancelled" || in.Action == "approve" && v.State != "pending" || in.Action == "cancel" && v.State != "pending" {
				return wallet.Error(apperror.Conflict)
			}
			if in.Action != "approve" {
				operationID = access.ID()
				command := walletsql.InsertOperationParams{ID: operationID, Kind: "withdrawal_release", ActorID: actor, TargetID: v.AccountID, IdempotencyKey: in.Key, Fingerprint: fingerprint, Amount: v.Reserved, PolicyID: v.PolicyID, PolicySnapshot: `{}`, Reference: v.ID, CreatedAt: now}
				if e := post(ctx, q, command, map[string]int64{"held:" + v.AccountID: -v.Reserved, "user:" + v.AccountID: v.Reserved}); e != nil {
					return e
				}
			}
			if e := q.Transition(ctx, walletsql.TransitionParams{ID: v.ID, State: desired}); e != nil {
				return e
			}
			if e := q.InsertWithdrawalEvent(ctx, walletsql.InsertWithdrawalEventParams{ID: access.ID(), WithdrawalID: v.ID, ActorID: actor, Action: in.Action, ResultingState: desired, IdempotencyKey: in.Key, Fingerprint: fingerprint}); e != nil {
				return e
			}
			v.State = desired
			out = withdrawal(v)
			target = v.AccountID
			audit = true
			fields = []string{"withdrawal", in.ID, in.Action, "settlement_not_configured"}
		case "transfer", "gift", "adjust", "withdrawal":
			operationID = access.ID()
			kind := in.Kind
			target = in.Target
			deltas := map[string]int64{}
			switch in.Kind {
			case "transfer":
				if target == actor {
					return wallet.Error(apperror.ValidationFailed)
				}
				if e := active(ctx, tx, target); e != nil {
					return e
				}
				p, e = policy(ctx, q, "transfer", "all")
				if e != nil {
					if errors.Is(e, pgx.ErrNoRows) {
						return wallet.Error(wallet.ConfigurationRequired)
					}
					return e
				}
				if !p.Enabled {
					return wallet.Error(wallet.ConfigurationRequired)
				}
				if in.Amount < p.Min || in.Amount > p.Max {
					return wallet.Error(apperror.ValidationFailed)
				}
				if e := periodLimits(ctx, q, actor, "transfer", in.Amount, p, now.Time); e != nil {
					return e
				}
				fee, e = p.Fee.Calculate(in.Amount)
				if e != nil {
					return e
				}
				total, e = wallet.Add(in.Amount, fee)
				if e != nil {
					return e
				}
				deltas["user:"+actor] = -total
				deltas["user:"+target] = in.Amount
				deltas["platform:fees"] = fee
			case "gift":
				if target == actor {
					return wallet.Error(apperror.ValidationFailed)
				}
				if e := active(ctx, tx, target); e != nil {
					return e
				}
				association, e := q.CurrentAssociation(ctx, target)
				scope := "agency"
				agencyID := ""
				if errors.Is(e, pgx.ErrNoRows) {
					scope = "non_agency"
				} else if e != nil {
					return e
				} else {
					agencyID = association.AgencyID
					if agencyID == actor {
						return wallet.Error(apperror.ValidationFailed)
					}
					if e := active(ctx, tx, agencyID); e != nil {
						return e
					}
				}
				p, e = policy(ctx, q, "gift", scope)
				if e != nil {
					if errors.Is(e, pgx.ErrNoRows) {
						return wallet.Error(wallet.ConfigurationRequired)
					}
					return e
				}
				if !p.Enabled {
					return wallet.Error(wallet.ConfigurationRequired)
				}
				price, ok := p.Catalog[in.GiftID]
				if !ok {
					return wallet.Error(apperror.NotFound)
				}
				total, e = wallet.Multiply(price, in.Quantity)
				if e != nil {
					return e
				}
				allocation, e := p.Allocate(total)
				if e != nil {
					return e
				}
				deltas["user:"+actor] = -total
				for i, id := range []string{"user:" + target, "user:" + agencyID, "platform:gifts", "platform:tax"} {
					if allocation[i] > 0 {
						sum, e := wallet.Add(deltas[id], allocation[i])
						if e != nil {
							return e
						}
						deltas[id] = sum
					}
				}
			case "adjust":
				if e := active(ctx, tx, target); e != nil {
					return e
				}
				kind = "adjustment"
				total = in.Amount
				sign := int64(1)
				if in.Direction == "debit" {
					sign = -1
				}
				deltas["user:"+target] = sign * total
				deltas["platform:issuance"] = -sign * total
				audit = true
				fields = []string{"diamonds", in.Direction, "explicit_supply_change"}
			case "withdrawal":
				target = actor
				kind = "withdrawal_reserve"
				eligibilityValue, withdrawalPolicy, e := eligibility(ctx, q, actor, snapshot)
				if e != nil {
					return e
				}
				if !eligibilityValue.Eligible {
					for _, reason := range eligibilityValue.Reasons {
						if reason == agency.ReasonUnavailable {
							return wallet.Error(wallet.ActivityUnavailable)
						}
						if reason == "CONFIGURATION_REQUIRED" {
							return wallet.Error(wallet.ConfigurationRequired)
						}
					}
					return wallet.Error(wallet.NotEligible)
				}
				p = withdrawalPolicy
				approvedEligibility = eligibilityValue
				if in.Amount < p.Min || in.Amount > p.Max {
					return wallet.Error(apperror.ValidationFailed)
				}
				if e := periodLimits(ctx, q, actor, kind, in.Amount, p, now.Time); e != nil {
					return e
				}
				fee, e = p.Fee.Calculate(in.Amount)
				if e != nil {
					return e
				}
				total, e = wallet.Add(in.Amount, fee)
				if e != nil {
					return e
				}
				deltas["user:"+actor] = -total
				deltas["held:"+actor] = total
			}
			if in.Kind != "adjust" || in.Direction == "debit" {
				b, e := q.AccountBalance(ctx, "user:"+func() string {
					if in.Kind == "adjust" {
						return target
					}
					return actor
				}())
				if e != nil {
					return e
				}
				if b < total {
					return wallet.Error(wallet.InsufficientFunds)
				}
			}
			var raw []byte
			policyID = p.ID
			if policyID != "" {
				raw, e = json.Marshal(p)
				if e != nil {
					return e
				}
			} else {
				raw = []byte(`{}`)
			}
			amount := in.Amount
			if in.Kind == "gift" {
				amount = total
			}
			operation := walletsql.InsertOperationParams{ID: operationID, Kind: kind, ActorID: actor, TargetID: target, IdempotencyKey: in.Key, Fingerprint: fingerprint, Amount: amount, Fee: fee, PolicyID: policyID, PolicySnapshot: string(raw), Reference: in.Reference, CreatedAt: now}
			if in.Kind == "gift" {
				operation.Reference = in.GiftID
			}
			if e := post(ctx, q, operation, deltas); e != nil {
				return e
			}
			if in.Kind == "withdrawal" {
				snapshotRaw, e := json.Marshal(approvedEligibility)
				if e != nil {
					return e
				}
				id := access.ID()
				if e := q.InsertWithdrawal(ctx, walletsql.InsertWithdrawalParams{ID: id, AccountID: actor, ReserveOperationID: operationID, Amount: in.Amount, Fee: fee, Reserved: total, PolicyID: p.ID, EligibilitySnapshot: string(snapshotRaw)}); e != nil {
					return e
				}
				stored, e := q.Withdrawal(ctx, id)
				if e != nil {
					return e
				}
				out = withdrawal(stored)
			} else {
				value, e := detail(ctx, q, operationID, actor, in.Kind == "adjust")
				if e != nil {
					return e
				}
				out = value
			}
		default:
			return wallet.Error(apperror.ValidationFailed)
		}
		if in.Kind == "association" {
			audit = true
			fields = []string{"agency_association", "classification", "effective_dates"}
			target = in.Association.AccountID
		}
		if audit {
			if e := u.Audit(ctx, access.Audit{Actor: actor, Target: target, Action: "financial." + action, Fields: append(fields, "operation:"+operationID), Reason: in.Reason, Correlation: in.Correlation}); e != nil {
				return e
			}
		}
		encoded, e := json.Marshal(out)
		if e != nil {
			return e
		}
		return q.SaveCommand(ctx, walletsql.SaveCommandParams{ActorID: actor, Kind: in.Kind, IdempotencyKey: in.Key, Fingerprint: fingerprint, Result: string(encoded)})
	})
	return out, safe(err)
}
