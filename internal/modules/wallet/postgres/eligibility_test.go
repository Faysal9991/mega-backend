package postgres

import (
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/agency"
)

func TestActivitySnapshotRevalidatedAtAcceptance(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		t.Fatal(err)
	}
	policy := agency.Policy{Version: "immutable-v2", Scope: agency.AudioHost, Timezone: loc.String(), Period: agency.CurrentCalendarMonth, MinimumQualifyingDays: 15, MinimumDailyLiveMinutes: 120, EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	at := time.Date(2026, 11, 30, 23, 59, 59, 0, loc)
	result, err := agency.Calculate("host", policy, at, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := activitySnapshot{Result: result, At: at}
	for _, c := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"same cutoff", at, true},
		{"later cutoff same month", at.Add(time.Millisecond), true},
		{"new business month while still November UTC", at.Add(time.Second), false},
		{"future snapshot", at.Add(-time.Nanosecond), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := snapshot.validAt("host", policy, c.at); got != c.want {
				t.Fatalf("validAt=%v want %v", got, c.want)
			}
		})
	}
	snapshot.Result.PeriodEnd = at.Add(time.Nanosecond)
	if snapshot.validAt("host", policy, at) {
		t.Fatal("inconsistent cutoff accepted")
	}
}
