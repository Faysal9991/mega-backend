package config

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type LiveConfig struct {
	Enabled          bool
	URL, Key, Secret string
	Limits           LiveLimits
}

func LoadLive(lookup LookupEnv) (LiveConfig, error) {
	out := LiveConfig{Limits: LiveLimits{Capacity: 100, Seats: 12, DatabaseTimeout: 8 * time.Second, TokenTTL: time.Minute, ProviderTimeout: 3 * time.Second, EventAge: 24 * time.Hour}}
	if lookup == nil {
		return out, invalid("LIVEKIT_ENABLED", "lookup required")
	}
	flag, _ := lookup("LIVEKIT_ENABLED")
	if flag != "" && flag != "false" && flag != "true" {
		return out, invalid("LIVEKIT_ENABLED", "must be true or false")
	}
	out.Enabled = flag == "true"
	keys := []struct {
		name string
		dst  *string
	}{{"LIVEKIT_URL", &out.URL}, {"LIVEKIT_API_KEY", &out.Key}, {"LIVEKIT_API_SECRET", &out.Secret}}
	for _, key := range keys {
		*key.dst, _ = lookup(key.name)
	}
	if out.Enabled {
		u, e := url.Parse(out.URL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
			return out, invalid("LIVEKIT_URL", "must be an HTTPS origin")
		}
		if !liveConfigText(out.Key, 128) || !liveConfigText(out.Secret, 256) || len(out.Secret) < 32 {
			return out, invalid("LIVEKIT_API_SECRET", "valid server credentials required")
		}
	}

	for _, v := range []struct {
		name string
		dst  *int
	}{{"LIVE_ROOM_CAPACITY", &out.Limits.Capacity}, {"LIVE_AUDIO_SEATS", &out.Limits.Seats}} {
		if raw, ok := lookup(v.name); ok {
			n, e := strconv.Atoi(raw)
			if e != nil {
				return out, invalid(v.name, "must be integer")
			}
			*v.dst = n
		}
	}
	for _, v := range []struct {
		name string
		dst  *bool
	}{{"LIVE_AUDIO_HOST_AUDIO", &out.Limits.Compatibility.AudioHostAudio}, {"LIVE_AUDIO_HOST_VIDEO", &out.Limits.Compatibility.AudioHostVideo}, {"LIVE_VIDEO_HOST_AUDIO", &out.Limits.Compatibility.VideoHostAudio}, {"LIVE_VIDEO_HOST_VIDEO", &out.Limits.Compatibility.VideoHostVideo}} {
		raw, _ := lookup(v.name)
		if raw != "" && raw != "true" && raw != "false" {
			return out, invalid(v.name, "must be true or false")
		}
		*v.dst = raw == "true"
	}
	if raw, ok := lookup("LIVE_DATABASE_TIMEOUT"); ok {
		d, e := time.ParseDuration(raw)
		if e != nil || d < 100*time.Millisecond || d > 30*time.Second {
			return out, invalid("LIVE_DATABASE_TIMEOUT", "must be between 100ms and 30s")
		}
		out.Limits.DatabaseTimeout = d
	}
	if out.Limits.Capacity < 2 || out.Limits.Capacity > 500 || out.Limits.Seats < 1 || out.Limits.Seats > 32 {
		return out, invalid("LIVE_ROOM_CAPACITY", "invalid live limits")
	}
	return out, nil
}

type LiveLimits struct {
	Capacity, Seats                     int
	TokenTTL, ProviderTimeout, EventAge time.Duration
	DatabaseTimeout                     time.Duration
	Compatibility                       LiveCompatibility
}
type LiveCompatibility struct{ AudioHostAudio, AudioHostVideo, VideoHostAudio, VideoHostVideo bool }

func liveConfigText(s string, max int) bool {
	if len(s) < 1 || len(s) > max || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
