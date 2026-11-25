package config

import (
	"strings"
	"testing"
)

func TestLiveConfigFailClosed(t *testing.T) {
	for _, tc := range []struct {
		values map[string]string
		ok     bool
	}{{nil, true}, {map[string]string{"LIVEKIT_ENABLED": "yes"}, false}, {map[string]string{"LIVEKIT_ENABLED": "true", "LIVEKIT_URL": "https://provider.invalid", "LIVEKIT_API_KEY": "test", "LIVEKIT_API_SECRET": strings.Repeat("x", 32)}, true}, {map[string]string{"LIVEKIT_ENABLED": "true", "LIVEKIT_URL": "http://provider.invalid", "LIVEKIT_API_KEY": "test", "LIVEKIT_API_SECRET": strings.Repeat("x", 32)}, false}, {map[string]string{"LIVEKIT_ENABLED": "true", "LIVEKIT_URL": "https://user:secret@provider.invalid", "LIVEKIT_API_KEY": "test", "LIVEKIT_API_SECRET": strings.Repeat("x", 32)}, false}} {
		c, e := LoadLive(func(k string) (string, bool) { v, ok := tc.values[k]; return v, ok })
		if (e == nil) != tc.ok {
			t.Fatal("configuration validation mismatch")
		}
		if e == nil && c.Limits.Compatibility.AudioHostAudio {
			t.Fatal("implicit policy")
		}
		if e != nil && strings.Contains(e.Error(), strings.Repeat("x", 32)) {
			t.Fatal("credential leak")
		}
	}
}
