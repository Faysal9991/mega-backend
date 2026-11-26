package access

import (
	"slices"
	"strings"
	"time"
)

// Capability is executable backend policy, never an administrator-defined verb.
type Capability struct {
	Action      string `json:"action"`
	PackageSafe bool   `json:"package_safe"`
}

func Capabilities() []Capability {
	out := []Capability{}
	for _, p := range Catalog() {
		out = append(out, Capability{Action: p, PackageSafe: slices.Contains([]string{"room.enter", "room.seat.self_join", "room.seat.self_leave", "benefit.badge", "benefit.entrance"}, p)})
	}
	return out
}
func RoomDefaults() map[string][]string {
	full := []string{"room.close", "room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.lock", "room.seat.unlock", "room.seat.self_join", "room.seat.self_leave", "room.title.update", "room.announcement.update", "room.admin.assign", "room.moderator.assign", "room.settings.update", "room.moderation_history.read"}
	moderate := []string{"room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.participant.ban", "room.seat.self_leave"}
	return map[string][]string{"host": full, "admin": slices.Clone(full), "co_host": slices.Clone(moderate), "moderator": slices.Clone(moderate), "audience": {"room.seat.self_leave"}}
}
func ModeratorDefaults() []string {
	return []string{"room.enter", "room.enter.private", "room.join.hidden", "room.participant.kick", "room.participant.mute", "room.participant.seat_remove", "room.seat.self_join", "room.seat.self_leave", "live.participant.global_ban"}
}
func PackagePower(action string) bool {
	for _, c := range Capabilities() {
		if c.Action == action {
			return c.PackageSafe
		}
	}
	return false
}
func (e Effective) Allows(action string) bool {
	return slices.Contains(e.Permissions, action)
}

type PolicyChange struct {
	Previous string              `json:"previous_policy_id"`
	Roles    map[string][]string `json:"roles"`
	Reason   string              `json:"reason"`
}
type OverrideChange struct {
	Action    string    `json:"action"`
	RoomID    string    `json:"room_id"`
	Allowed   bool      `json:"allowed"`
	ExpiresAt time.Time `json:"expires_at"`
	Remove    bool      `json:"remove"`
	Reason    string    `json:"reason"`
}

// RoomPolicyActions excludes every global staff and financial action, while
// allowing trusted policy administrators to configure supported room powers.
func RoomPolicyActions() []string {
	out := []string{}
	for _, p := range Catalog() {
		if strings.HasPrefix(p, "room.") {
			out = append(out, p)
		}
	}
	return out
}
