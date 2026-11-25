package provider

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/live"
)

// Unavailable permits independent agency/workflow administration without media
// credentials. It never issues a token or invents provider success/presence.
type Unavailable struct{}

func (Unavailable) Create(context.Context, live.Room) error { return errors.New("livekit: disabled") }
func (Unavailable) Close(context.Context, live.Room) error  { return errors.New("livekit: disabled") }
func (Unavailable) Participant(context.Context, live.Room, live.Member) error {
	return errors.New("livekit: disabled")
}
func (Unavailable) Snapshot(context.Context, live.Room) (live.Snapshot, error) {
	return live.Snapshot{}, errors.New("livekit: disabled")
}
func (Unavailable) Token(live.Grant, time.Duration) (string, error) {
	return "", errors.New("livekit: disabled")
}
func (Unavailable) Verify(*http.Request) (live.Event, error) {
	return live.Event{}, errors.New("livekit: no trusted provider configured")
}
