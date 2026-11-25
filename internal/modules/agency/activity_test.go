package agency

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

const testAccount = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func activityPolicy(scope string) Policy {
	return Policy{Version: "fixture-policy-1", Scope: scope, Timezone: "Asia/Dhaka", Period: CurrentCalendarMonth, MinimumQualifyingDays: InitialQualifyingDays, MinimumDailyLiveMinutes: InitialDailyLiveMinutes, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func clock() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }
func location(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, e := time.LoadLocation(name)
	if e != nil {
		t.Fatal(e)
	}
	return loc
}
func sessionsForDays(t *testing.T, days, minutes int) []Session {
	t.Helper()
	loc := location(t, "Asia/Dhaka")
	sessions := make([]Session, days)
	for i := range days {
		start := time.Date(2026, 11, i+1, 10, 0, 0, 0, loc)
		sessions[i] = Session{ID: fmt.Sprintf("fixture-session-%d", i), AccountID: testAccount, Start: start, End: start.Add(time.Duration(minutes) * time.Minute)}
	}
	return sessions
}

func TestActivityApprovedDayAndMinuteThresholds(t *testing.T) {
	for _, scope := range []string{AudioHost, VideoHost, AgencyScope} {
		for _, c := range []struct {
			days, minutes int
			want          bool
			qualifies     int
		}{{14, 120, false, 14}, {14, 480, false, 14}, {15, 119, false, 0}, {15, 120, true, 15}, {16, 120, true, 16}} {
			t.Run(fmt.Sprintf("%s-%dd-%dm", scope, c.days, c.minutes), func(t *testing.T) {
				got, e := Calculate(testAccount, activityPolicy(scope), clock(), sessionsForDays(t, c.days, c.minutes))
				if e != nil || got.Eligible != c.want || got.QualifyingDays != c.qualifies {
					t.Fatalf("eligibility=%+v err=%v", got, e)
				}
				reason := ReasonInsufficientDays
				if c.want {
					reason = ReasonQualified
				}
				if got.Reason != reason {
					t.Fatal("reason drift")
				}
			})
		}
	}
}

func TestActivityUnionDedupAndFractionalDuration(t *testing.T) {
	p := activityPolicy(AudioHost)
	p.MinimumQualifyingDays = 1
	sessions := sessionsForDays(t, 1, 60)
	start := sessions[0].Start
	sessions = append(sessions, sessions[0], Session{ID: "overlap", AccountID: testAccount, Start: start.Add(30 * time.Minute), End: start.Add(119*time.Minute + 59*time.Second + 999*time.Millisecond)})
	original := append([]Session{}, sessions...)
	got, e := Calculate(testAccount, p, clock(), sessions)
	if e != nil || got.Eligible || got.QualifyingDays != 0 || len(got.Days) != 1 || got.Days[0].VerifiedSeconds != 7199 {
		t.Fatalf("overlap/rounding %+v %v", got, e)
	}
	if !reflect.DeepEqual(sessions, original) {
		t.Fatal("calculator mutated trusted source snapshot")
	}
	sessions = append(sessions, Session{ID: "last-millisecond", AccountID: testAccount, Start: start.Add(119*time.Minute + 59*time.Second + 999*time.Millisecond), End: start.Add(120 * time.Minute)})
	got, e = Calculate(testAccount, p, clock(), sessions)
	if e != nil || !got.Eligible || got.Days[0].VerifiedSeconds != 7200 {
		t.Fatalf("exact elapsed threshold %+v %v", got, e)
	}
	// Disjoint intervals may collectively satisfy the daily threshold.
	split := []Session{{ID: "one", AccountID: testAccount, Start: start, End: start.Add(time.Hour)}, {ID: "two", AccountID: testAccount, Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}}
	got, e = Calculate(testAccount, p, clock(), split)
	if e != nil || !got.Eligible {
		t.Fatal("same-day verified intervals not accumulated")
	}
}

func TestActivityClipsMonthAndSplitsBusinessDays(t *testing.T) {
	p := activityPolicy(AgencyScope)
	p.MinimumQualifyingDays = 2
	loc := location(t, p.Timezone)
	first := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	last := clock()
	sessions := []Session{
		{ID: "before-month", AccountID: testAccount, Start: first.Add(-2 * time.Hour), End: first.Add(time.Hour)},
		{ID: "midnight", AccountID: testAccount, Start: time.Date(2026, 11, 15, 23, 0, 0, 0, loc), End: time.Date(2026, 11, 16, 1, 0, 0, 0, loc)},
		{ID: "at-check", AccountID: testAccount, Start: last.Add(-2 * time.Hour), End: last},
		{ID: "outside", AccountID: testAccount, Start: first.Add(-4 * time.Hour), End: first.Add(-time.Hour)},
	}
	got, e := Calculate(testAccount, p, clock(), sessions)
	if e != nil || got.Eligible || got.QualifyingDays != 1 || !got.PeriodStart.Equal(first) || !got.PeriodEnd.Equal(last) {
		t.Fatalf("month clipping %+v %v", got, e)
	}
	want := []Day{{Date: "2026-11-01", VerifiedSeconds: 3600}, {Date: "2026-11-15", VerifiedSeconds: 3600}, {Date: "2026-11-16", VerifiedSeconds: 3600}, {Date: "2026-11-25", VerifiedSeconds: 7200, Qualifies: true}}
	if !reflect.DeepEqual(got.Days, want) {
		t.Fatalf("day boundaries %+v", got.Days)
	}
	// Caller clock location cannot change the configured business calendar.
	other, e := Calculate(testAccount, p, clock().In(location(t, "Pacific/Honolulu")), sessions)
	if e != nil || !reflect.DeepEqual(got, other) {
		t.Fatal("host clock timezone changed eligibility")
	}
}

func TestCurrentMonthQualificationAndReset(t *testing.T) {
	p := activityPolicy(AudioHost)
	loc := location(t, p.Timezone)
	sessions := sessionsForDays(t, 15, 120)
	// Qualification is immediate on day 15, including a segment completed
	// exactly at the check cutoff; month-end is not a prerequisite.
	at := sessions[14].End
	qualified, err := Calculate(testAccount, p, at, sessions)
	if err != nil || !qualified.Eligible || qualified.QualifyingDays != 15 || !qualified.PeriodEnd.Equal(at) {
		t.Fatalf("mid-month qualification %+v %v", qualified, err)
	}
	history := append([]Session(nil), sessions...)
	nextMonth := time.Date(2026, 12, 1, 0, 0, 0, 0, loc)
	for _, at := range []time.Time{nextMonth, nextMonth.Add(time.Hour)} {
		reset, err := Calculate(testAccount, p, at, sessions)
		if err != nil || reset.Eligible || reset.QualifyingDays != 0 || len(reset.Days) != 0 || !reset.PeriodStart.Equal(nextMonth) || !reset.PeriodEnd.Equal(at) {
			t.Fatalf("new month reset %+v %v", reset, err)
		}
	}
	if !reflect.DeepEqual(sessions, history) || !qualified.Eligible || qualified.QualifyingDays != 15 {
		t.Fatal("monthly window changed historical records/results")
	}
	// The business month has reset even while the UTC calendar is still November.
	if nextMonth.UTC().Month() != time.November {
		t.Fatal("fixture does not cross timezone month boundary")
	}
	// Completed activity after the cutoff must never contribute early.
	if got, err := Calculate(testAccount, p, at.Add(-time.Nanosecond), sessions); !errors.Is(err, ErrInvalidActivity) || got.Eligible {
		t.Fatal("unfinished interval accepted")
	}
}

func TestActivityDSTUsesElapsedTime(t *testing.T) {
	for _, c := range []struct {
		month time.Month
		day   int
		hours int
	}{{time.March, 8, 23}, {time.November, 1, 25}} {
		p := activityPolicy(VideoHost)
		p.Timezone = "America/New_York"
		p.MinimumQualifyingDays = 1
		loc := location(t, p.Timezone)
		start := time.Date(2026, c.month, c.day, 0, 0, 0, 0, loc)
		end := time.Date(2026, c.month, c.day+1, 0, 0, 0, 0, loc)
		now := time.Date(2026, c.month, 15, 0, 0, 0, 0, loc)
		got, e := Calculate(testAccount, p, now, []Session{{ID: "dst-day", AccountID: testAccount, Start: start, End: end}})
		if e != nil || !got.Eligible || len(got.Days) != 1 || got.Days[0].VerifiedSeconds != int64(c.hours*3600) {
			t.Fatalf("DST %+v %v", got, e)
		}
	}
}

type sourceFixture struct {
	sessions   []Session
	err        error
	account    string
	start, end time.Time
	calls      int
	cancel     context.CancelFunc
}

func (s *sourceFixture) Sessions(_ context.Context, account string, start, end time.Time) ([]Session, error) {
	s.calls++
	s.account = account
	s.start = start
	s.end = end
	if s.cancel != nil {
		s.cancel()
	}
	return s.sessions, s.err
}
func TestTrustedSourceUnavailableAndCancellation(t *testing.T) {
	p := activityPolicy(AudioHost)
	for _, source := range []ActivitySource{nil, UnavailableSource{}, &sourceFixture{err: errors.New("unavailable")}} {
		got, e := Eligibility(context.Background(), source, testAccount, p, clock())
		if e != nil || got.Eligible || got.Reason != ReasonUnavailable || got.Days == nil {
			t.Fatalf("unavailable %+v %v", got, e)
		}
	}
	source := &sourceFixture{sessions: sessionsForDays(t, 15, 120)}
	got, e := Eligibility(context.Background(), source, testAccount, p, clock())
	if e != nil || !got.Eligible || source.account != testAccount || !source.start.Equal(got.PeriodStart) || !source.end.Equal(got.PeriodEnd) {
		t.Fatal("source scope or complete snapshot incorrect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := source.calls
	got, e = Eligibility(ctx, source, testAccount, p, clock())
	if !errors.Is(e, context.Canceled) || got.Eligible || source.calls != before {
		t.Fatal("cancellation reached trusted source or opened eligibility")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	source.cancel = cancel
	got, e = Eligibility(ctx, source, testAccount, p, clock())
	if !errors.Is(e, context.Canceled) || got.Eligible {
		t.Fatal("late cancellation published eligibility")
	}
	empty, e := Calculate(testAccount, p, clock(), nil)
	if e != nil || empty.Eligible || empty.Reason != ReasonInsufficientDays || empty.Days == nil {
		t.Fatal("verified empty snapshot semantics")
	}
}

func TestMalformedAuthoritativeSessionsFailClosed(t *testing.T) {
	p := activityPolicy(AudioHost)
	p.MinimumQualifyingDays = 1
	valid := sessionsForDays(t, 1, 120)[0]
	wrongAccount := valid
	wrongAccount.AccountID = "another-account"
	missingID := valid
	missingID.ID = ""
	empty := valid
	empty.End = empty.Start
	reversed := valid
	reversed.End = reversed.Start.Add(-time.Second)
	future := valid
	future.End = clock().Add(time.Second)
	duplicateConflict := valid
	duplicateConflict.End = duplicateConflict.End.Add(time.Second)
	zero := valid
	zero.Start = time.Time{}
	for _, sessions := range [][]Session{{valid, wrongAccount}, {missingID}, {empty}, {reversed}, {future}, {zero}, {valid, duplicateConflict}, make([]Session, MaxSessions+1)} {
		got, e := Calculate(testAccount, p, clock(), sessions)
		if !errors.Is(e, ErrInvalidActivity) || got.Eligible || got.Reason != ReasonInvalidActivity {
			t.Fatalf("malformed source %+v %v", got, e)
		}
	}
}

func TestActivityPolicyExplicitConfigurationAndSnapshots(t *testing.T) {
	valid := activityPolicy(AudioHost)
	for _, mutate := range []func(*Policy){func(p *Policy) { p.Version = "" }, func(p *Policy) { p.Version = "v\n" }, func(p *Policy) { p.Scope = "owner" }, func(p *Policy) { p.Timezone = "" }, func(p *Policy) { p.Timezone = "Local" }, func(p *Policy) { p.Timezone = "Invalid/Zone" }, func(p *Policy) { p.Period = "previous_completed_calendar_month" }, func(p *Policy) { p.MinimumQualifyingDays = 0 }, func(p *Policy) { p.MinimumQualifyingDays = 32 }, func(p *Policy) { p.MinimumDailyLiveMinutes = 0 }, func(p *Policy) { p.MinimumDailyLiveMinutes = 1441 }, func(p *Policy) { p.EffectiveAt = time.Time{} }} {
		p := valid
		mutate(&p)
		if p.Validate() == nil {
			t.Fatalf("invalid policy accepted %+v", p)
		}
		got, e := Calculate(testAccount, p, clock(), nil)
		if !errors.Is(e, ErrInvalidPolicy) || got.Eligible {
			t.Fatal("unconfigured policy opened eligibility")
		}
	}
	future := valid
	future.EffectiveAt = clock().Add(time.Second)
	if got, e := Calculate(testAccount, future, clock(), nil); !errors.Is(e, ErrInvalidPolicy) || got.Eligible {
		t.Fatal("future policy applied")
	}
	sessions := sessionsForDays(t, 15, 120)
	old, e := Calculate(testAccount, valid, clock(), sessions)
	if e != nil || !old.Eligible {
		t.Fatal(e)
	}
	updated := valid
	updated.Version = "fixture-policy-2"
	updated.MinimumQualifyingDays = 16
	next, e := Calculate(testAccount, updated, clock(), sessions)
	if e != nil || next.Eligible || old.PolicyVersion == next.PolicyVersion || !old.Eligible || old.QualifyingDays != 15 {
		t.Fatal("historical policy snapshot mutated")
	}
	// Independent callers share no mutable calculator state.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			got, e := Calculate(testAccount, valid, clock(), sessions)
			if e != nil {
				errs <- e
				return
			}
			if !reflect.DeepEqual(got, old) {
				errs <- errors.New("nondeterministic eligibility")
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}

func TestAssociationHistoryAndClassifications(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	for _, class := range []string{AudioHost, VideoHost} {
		a := Association{AgencyID: "fixture-agency", Classification: class, EffectiveFrom: start, EffectiveUntil: &end}
		if a.Validate() != nil || !a.ActiveAt(start) || a.ActiveAt(start.Add(-time.Nanosecond)) || a.ActiveAt(end) {
			t.Fatal("association effective interval")
		}
		if !activityPolicy(class).AppliesTo(class) || !activityPolicy(AgencyScope).AppliesTo(class) {
			t.Fatal("host policy scope")
		}
	}
	if activityPolicy(AudioHost).AppliesTo(VideoHost) || activityPolicy(VideoHost).AppliesTo(AudioHost) || activityPolicy(AgencyScope).AppliesTo("owner") {
		t.Fatal("classification conflated with authority")
	}
	for _, a := range []Association{{AgencyID: "x", Classification: "owner", EffectiveFrom: start}, {AgencyID: "", Classification: AudioHost, EffectiveFrom: start}, {AgencyID: "x", Classification: AudioHost}, {AgencyID: "x", Classification: AudioHost, EffectiveFrom: start, EffectiveUntil: &start}} {
		if a.Validate() == nil || a.ActiveAt(start) {
			t.Fatal("invalid association accepted")
		}
	}
}

func TestActivityMidnightDSTGap(t *testing.T) {
	p := activityPolicy(AudioHost)
	p.Timezone = "America/Santiago"
	p.MinimumQualifyingDays = 2
	loc := location(t, p.Timezone)
	start := time.Date(2026, 9, 5, 0, 0, 0, 0, loc)
	end := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	got, e := Calculate(testAccount, p, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), []Session{{ID: "midnight-gap", AccountID: testAccount, Start: start, End: end}})
	if e != nil || !got.Eligible || len(got.Days) != 2 || got.Days[0].VerifiedSeconds != 24*3600 || got.Days[1].VerifiedSeconds != 23*3600 {
		t.Fatalf("midnight DST gap %+v %v", got, e)
	}
}
func TestCivilBoundaryHandlesSkippedAndRepeatedMidnight(t *testing.T) {
	for _, c := range []struct {
		zone  string
		year  int
		month time.Month
		day   int
		date  string
		hour  int
	}{
		{"America/Santiago", 2026, time.September, 6, "2026-09-06", 1},
		{"America/Havana", 2026, time.November, 1, "2026-11-01", 0},
		{"Pacific/Apia", 2011, time.December, 30, "2011-12-31", 0},
	} {
		loc := location(t, c.zone)
		boundary := civilBoundary(c.year, c.month, c.day, loc)
		if boundary.IsZero() || boundary.In(loc).Format("2006-01-02") != c.date || boundary.In(loc).Hour() != c.hour {
			t.Fatalf("civil boundary %s %s", c.zone, boundary.In(loc))
		}
		if previous := boundary.Add(-time.Nanosecond).In(loc); previous.Format("2006-01-02") >= c.date {
			t.Fatalf("boundary was not first valid instant: %s", previous)
		}
	}
}
