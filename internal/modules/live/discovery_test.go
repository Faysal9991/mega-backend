package live

import (
	"strings"
	"testing"
)

func TestListCursorCanonicalAndBounded(t *testing.T) {
	v := ListCursor{Snapshot: strings.Repeat("a", 32), Room: strings.Repeat("b", 32), Priority: 123}
	got, ok := ParseListCursor(v.String())
	if !ok || got != v {
		t.Fatal("roundtrip")
	}
	for _, bad := range []string{"", strings.Repeat("a", 32), v.Snapshot + ".-1." + v.Room, v.Snapshot + ".01." + v.Room, v.Snapshot + ".1000001." + v.Room, v.String() + ".extra"} {
		if _, ok := ParseListCursor(bad); ok {
			t.Fatal("invalid cursor", bad)
		}
	}
}

func TestVideoCallRequiresTrustedStageAllocation(t *testing.T) {
	m := Member{State: "active", Role: "audience", Seat: 1}
	r := Room{Kind: "video"}
	if CanPublish(r, m) {
		t.Fatal("seat number alone authorized video publishing")
	}
	m.SelfSeated = true
	if !CanPublish(r, m) {
		t.Fatal("authorized video call cannot publish")
	}
	m.Hidden = true
	if CanPublish(r, m) {
		t.Fatal("hidden call can publish")
	}
	m.Hidden = false
	m.Muted = true
	if CanPublish(r, m) {
		t.Fatal("muted call can publish")
	}
}

func TestPreConsolidatedCommandFingerprintsRemainStable(t *testing.T) {
	// The committed e94b765 command field order is part of persisted retry keys.
	type prior struct {
		Admin                                                             bool
		Policy                                                            *Policy
		Kind, ID, Target, Key, Reason, Name, Classification, Status, Role string
		Seat, Capacity, Seats                                             int32
		Muted                                                             bool
	}
	old := prior{Kind: "room.join", ID: strings.Repeat("a", 32), Key: "prior-command", Reason: "Participant request"}
	current := Command{Kind: old.Kind, ID: old.ID, Key: old.Key, Reason: old.Reason}
	a, e := Fingerprint(old)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Fingerprint(current)
	if e != nil || a != b {
		t.Fatal("existing command retry fingerprint changed", e)
	}
}
