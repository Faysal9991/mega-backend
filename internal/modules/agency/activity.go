// Package agency defines the agency-host relationship and verified activity
// boundary used by financial eligibility. It does not collect live activity.
package agency

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	AudioHost               = "audio_host"
	VideoHost               = "video_host"
	AgencyScope             = "agency"
	CurrentCalendarMonth    = "current_calendar_month"
	InitialQualifyingDays   = 15
	InitialDailyLiveMinutes = 120
	// MaxSessions bounds an authoritative monthly snapshot before sorting it.
	// An adapter must also bound its query and decoding allocation at the source.
	MaxSessions = 10000

	ReasonQualified        = "ACTIVITY_REQUIREMENT_MET"
	ReasonInsufficientDays = "INSUFFICIENT_QUALIFYING_DAYS"
	ReasonUnavailable      = "VERIFIED_ACTIVITY_UNAVAILABLE"
	ReasonInvalidActivity  = "INVALID_VERIFIED_ACTIVITY"
	ReasonPolicyRequired   = "ACTIVITY_POLICY_REQUIRED"
)

var (
	ErrInvalidPolicy   = errors.New("agency: invalid or unsupported activity policy")
	ErrInvalidActivity = errors.New("agency: invalid authoritative activity snapshot")
)

// Association remains separate from identity status, staff and premium roles.
// EffectiveUntil is exclusive; changing classification closes an existing row
// and adds a new row rather than rewriting historical classification.
type Association struct {
	AgencyID       string     `json:"agency_id"`
	Classification string     `json:"classification"`
	EffectiveFrom  time.Time  `json:"effective_from"`
	EffectiveUntil *time.Time `json:"effective_until"`
}

func ValidClassification(value string) bool { return value == AudioHost || value == VideoHost }

func (a Association) Validate() error {
	if !identifier(a.AgencyID) || !ValidClassification(a.Classification) || a.EffectiveFrom.IsZero() || a.EffectiveUntil != nil && !a.EffectiveUntil.After(a.EffectiveFrom) {
		return ErrInvalidPolicy
	}
	return nil
}

func (a Association) ActiveAt(at time.Time) bool {
	return a.Validate() == nil && !at.IsZero() && !at.Before(a.EffectiveFrom) && (a.EffectiveUntil == nil || at.Before(*a.EffectiveUntil))
}

// Policy is an immutable snapshot selected by the caller. No zero/default
// timezone or period is financially binding. Version and EffectiveAt identify
// the historical policy used to produce an eligibility snapshot.
type Policy struct {
	Version                 string    `json:"version"`
	Scope                   string    `json:"scope"`
	Timezone                string    `json:"timezone"`
	Period                  string    `json:"period"`
	MinimumQualifyingDays   int       `json:"minimum_qualifying_days"`
	MinimumDailyLiveMinutes int       `json:"minimum_daily_live_minutes"`
	EffectiveAt             time.Time `json:"effective_at"`
}

func (p Policy) Validate() error {
	if !identifier(p.Version) || p.Scope != AgencyScope && !ValidClassification(p.Scope) || p.Period != CurrentCalendarMonth || p.MinimumQualifyingDays < 1 || p.MinimumQualifyingDays > 31 || p.MinimumDailyLiveMinutes < 1 || p.MinimumDailyLiveMinutes > 1440 || p.EffectiveAt.IsZero() {
		return ErrInvalidPolicy
	}
	_, err := p.location()
	return err
}

func (p Policy) AppliesTo(classification string) bool {
	return ValidClassification(classification) && (p.Scope == AgencyScope || p.Scope == classification)
}

func (p Policy) location() (*time.Location, error) {
	// "Local" is host-dependent and cannot define a fixed business timezone.
	if p.Timezone == "" || p.Timezone == "Local" || strings.TrimSpace(p.Timezone) != p.Timezone {
		return nil, ErrInvalidPolicy
	}
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return nil, ErrInvalidPolicy
	}
	return location, nil
}

// Session is a closed, verified interval from an authoritative server source.
// ID is an immutable live-session/interval identity, never just a room ID.
// Intervals use [Start, End); a boundary instant belongs to one day only.
type Session struct {
	ID        string
	AccountID string
	Start     time.Time
	End       time.Time
}

// ActivitySource must return a complete, bounded, authoritative snapshot of
// verified sessions overlapping [start,end) for this account. It must include
// crossing sessions, not only sessions whose start lies inside the period.
// No client-reported duration, Redis presence or unverified record qualifies.
type ActivitySource interface {
	Sessions(context.Context, string, time.Time, time.Time) ([]Session, error)
}

// UnavailableSource is the production boundary until an authoritative collector
// exists. It deliberately provides no fabricated successful empty snapshot.
type UnavailableSource struct{}

func (UnavailableSource) Sessions(context.Context, string, time.Time, time.Time) ([]Session, error) {
	return nil, errors.New("agency: authoritative activity source unavailable")
}

type Day struct {
	Date            string `json:"date"`
	VerifiedSeconds int64  `json:"verified_seconds"`
	Qualifies       bool   `json:"qualifies"`
}

type Result struct {
	Eligible       bool      `json:"eligible"`
	Reason         string    `json:"reason"`
	PolicyVersion  string    `json:"policy_version"`
	QualifyingDays int       `json:"qualifying_days"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"` // Authoritative check cutoff, not month-end.
	Days           []Day     `json:"days"`
}

// Eligibility requests verified data before any financial row locks are held.
// Source failure is represented as ineligible, never as zero verified activity
// that can be mistaken for a successful authority check. Cancellation propagates.
func Eligibility(ctx context.Context, source ActivitySource, accountID string, policy Policy, now time.Time) (Result, error) {
	result, _, err := period(policy, now)
	if err != nil {
		return result, err
	}
	if !identifier(accountID) {
		return invalid(result), ErrInvalidActivity
	}
	if err := ctx.Err(); err != nil {
		return unavailable(result), err
	}
	if source == nil {
		return unavailable(result), nil
	}
	sessions, err := source.Sessions(ctx, accountID, result.PeriodStart, result.PeriodEnd)
	if err != nil {
		if ctx.Err() != nil {
			return unavailable(result), ctx.Err()
		}
		return unavailable(result), nil
	}
	if err := ctx.Err(); err != nil {
		return unavailable(result), err
	}
	return Calculate(accountID, policy, now, sessions)
}

// Calculate evaluates a complete trusted fixture/snapshot without I/O. It sorts
// a private interval copy and unions overlaps across all sessions before daily
// splitting, so neither duplicates nor overlapping sessions multiply duration.
// Fractional seconds are retained until daily qualification; no per-session or
// per-minute rounding can turn 119m59.999s into 120 minutes.
func Calculate(accountID string, policy Policy, now time.Time, sessions []Session) (Result, error) {
	result, location, err := period(policy, now)
	if err != nil {
		return result, err
	}
	if !identifier(accountID) || len(sessions) > MaxSessions {
		return invalid(result), ErrInvalidActivity
	}
	type interval struct{ start, end time.Time }
	intervals := make([]interval, 0, len(sessions))
	seen := make(map[string]Session, len(sessions))
	for _, session := range sessions {
		if !identifier(session.ID) || session.AccountID != accountID || session.Start.IsZero() || session.End.IsZero() || !session.End.After(session.Start) || session.End.After(now) {
			return invalid(result), ErrInvalidActivity
		}
		if previous, exists := seen[session.ID]; exists {
			if !previous.Start.Equal(session.Start) || !previous.End.Equal(session.End) || previous.AccountID != session.AccountID {
				return invalid(result), ErrInvalidActivity
			}
			continue
		}
		seen[session.ID] = session
		start, end := session.Start.UTC(), session.End.UTC()
		if start.Before(result.PeriodStart) {
			start = result.PeriodStart
		}
		if end.After(result.PeriodEnd) {
			end = result.PeriodEnd
		}
		if start.Before(end) {
			intervals = append(intervals, interval{start, end})
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start.Equal(intervals[j].start) {
			return intervals[i].end.Before(intervals[j].end)
		}
		return intervals[i].start.Before(intervals[j].start)
	})
	merged := make([]interval, 0, len(intervals))
	for _, current := range intervals {
		if len(merged) == 0 || current.start.After(merged[len(merged)-1].end) {
			merged = append(merged, current)
			continue
		}
		if current.end.After(merged[len(merged)-1].end) {
			merged[len(merged)-1].end = current.end
		}
	}
	durations := map[string]time.Duration{}
	for _, current := range merged {
		for start := current.start; start.Before(current.end); {
			local := start.In(location)
			// Within a zone interval the UTC offset is constant. Split at the
			// earlier of its end or the next local midnight; this also handles
			// midnight DST gaps/folds and skipped/repeated civil dates.
			_, offset := local.Zone()
			next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(offset) * time.Second)
			_, zoneEnd := local.ZoneBounds()
			if !zoneEnd.IsZero() && zoneEnd.Before(next) {
				next = zoneEnd
			}
			if !next.After(start) {
				return invalid(result), ErrInvalidActivity
			}
			end := current.end
			if next.Before(end) {
				end = next
			}
			key := local.Format("2006-01-02")
			durations[key] += end.Sub(start)
			start = end
		}
	}
	keys := make([]string, 0, len(durations))
	for key := range durations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	threshold := time.Duration(policy.MinimumDailyLiveMinutes) * time.Minute
	for _, key := range keys {
		duration := durations[key]
		qualifies := duration >= threshold
		result.Days = append(result.Days, Day{Date: key, VerifiedSeconds: int64(duration / time.Second), Qualifies: qualifies})
		if qualifies {
			result.QualifyingDays++
		}
	}
	result.Eligible = result.QualifyingDays >= policy.MinimumQualifyingDays
	result.Reason = ReasonInsufficientDays
	if result.Eligible {
		result.Reason = ReasonQualified
	}
	return result, nil
}

func period(policy Policy, now time.Time) (Result, *time.Location, error) {
	result := Result{Reason: ReasonPolicyRequired, PolicyVersion: policy.Version, Days: []Day{}}
	if policy.Validate() != nil || now.IsZero() || now.Before(policy.EffectiveAt) {
		return result, nil, ErrInvalidPolicy
	}
	location, err := policy.location()
	if err != nil {
		return result, nil, err
	}
	local := now.In(location)
	start := civilBoundary(local.Year(), local.Month(), 1, location)
	end := now.UTC()
	// The exact month boundary is a valid empty window: no previous-month
	// activity carries over, and no completed segment is invented.
	if start.IsZero() || end.Before(start) {
		return result, nil, ErrInvalidPolicy
	}
	result.PeriodStart, result.PeriodEnd = start.UTC(), end.UTC()
	return result, location, nil
}
func unavailable(result Result) Result {
	result.Eligible = false
	result.Reason = ReasonUnavailable
	return result
}
func invalid(result Result) Result {
	result.Eligible = false
	result.Reason = ReasonInvalidActivity
	return result
}
func identifier(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// civilBoundary finds the first actual instant of a requested calendar date,
// including a skipped midnight. A fixed four-day search covers IANA offsets and
// civil-date jumps; unsupported transition data fails closed with a zero result.
func civilBoundary(year int, month time.Month, day int, location *time.Location) time.Time {
	nominal := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	cursor, limit := nominal.Add(-48*time.Hour), nominal.Add(48*time.Hour)
	for transitions := 0; cursor.Before(limit) && transitions < 64; transitions++ {
		local := cursor.In(location)
		_, offset := local.Zone()
		_, end := local.ZoneBounds()
		if end.IsZero() || end.After(limit) {
			end = limit
		}
		candidate := nominal.Add(-time.Duration(offset) * time.Second)
		if candidate.Before(cursor) {
			candidate = cursor
		}
		date := candidate.In(location)
		localDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
		if candidate.Before(end) && !localDate.Before(nominal) {
			return candidate.UTC()
		}
		if !end.After(cursor) {
			return time.Time{}
		}
		cursor = end
	}
	return time.Time{}
}
