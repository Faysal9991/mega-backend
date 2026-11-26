package access

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCapabilitySourcesAndProtectedCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Capabilities() {
		if seen[c.Action] || !slices.Contains(Catalog(), c.Action) {
			t.Fatal("duplicate or unsupported capability")
		}
		seen[c.Action] = true
		if c.PackageSafe && (strings.Contains(c.Action, "ban") || strings.Contains(c.Action, "kick") || strings.Contains(c.Action, "adjust") || strings.Contains(c.Action, "assign") || c.Action == "room.join.hidden" || c.Action == "room.enter.private") {
			t.Fatal("protected purchasable power")
		}
	}
	for _, a := range ModeratorDefaults() {
		if !seen[a] {
			t.Fatal("unsupported moderator default")
		}
	}
	for _, a := range RoomPolicyActions() {
		if !strings.HasPrefix(a, "room.") {
			t.Fatal("global action in room policy")
		}
	}
}

func TestExecutableAndDatabaseCapabilityCatalogAgree(t *testing.T) {
	raw, e := os.ReadFile("../../../database/migrations/00010_dynamic_capabilities.sql")
	if e != nil {
		t.Fatal(e)
	}
	matches := regexp.MustCompile(`\('([a-z._]+)',(true|false)\)`).FindAllStringSubmatch(string(raw), -1)
	stored := map[string]bool{}
	for _, m := range matches {
		stored[m[1]] = m[2] == "true"
	}
	if len(stored) != len(Capabilities()) {
		t.Fatal("capability migration/catalog count mismatch")
	}
	for _, c := range Capabilities() {
		safe, ok := stored[c.Action]
		if !ok || safe != c.PackageSafe {
			t.Fatal("capability migration/catalog mismatch", c.Action)
		}
	}
}

func TestGlobalModeratorDefaultBoundary(t *testing.T) {
	for _, action := range []string{"room.close", "room.admin.assign", "room.moderator.assign", "room.participant.ban", "live.rooms.moderate"} {
		if slices.Contains(ModeratorDefaults(), action) {
			t.Fatalf("unapproved global moderator default %s", action)
		}
	}
	for _, action := range []string{"room.participant.kick", "room.participant.mute", "room.close", "room.admin.assign", "room.participant.ban", "live.rooms.moderate", "live.participant.global_ban"} {
		if !LiveRestrictedAction(action) {
			t.Fatalf("restriction bypass %s", action)
		}
	}
	for _, action := range []string{"wallet.adjust", "packages.purchase", "capabilities.manage", "agencies.manage"} {
		if LiveRestrictedAction(action) {
			t.Fatalf("unrelated authority removed %s", action)
		}
	}
}

func TestRoomAdminDefaultBoundary(t *testing.T) {
	want := []string{"room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.seat.self_leave"}
	if !slices.Equal(RoomDefaults()["admin"], want) {
		t.Fatal("room admin defaults exceed approved set")
	}
	if !slices.Contains(RoomDefaults()["host"], "room.close") || !slices.Contains(RoomDefaults()["host"], "room.admin.assign") {
		t.Fatal("host authority changed")
	}
}
