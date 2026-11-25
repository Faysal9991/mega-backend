package config_test

import (
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"strings"
	"testing"
)

func TestIdentityConfiguration(t *testing.T) {
	c, err := config.LoadIdentity(lookup(nil))
	if err != nil || c.GoogleClientID != "" {
		t.Fatal("missing Google config did not disable provider")
	}
	values := map[string]string{"GOOGLE_CLIENT_ID": "web-123.apps.googleusercontent.com", "GOOGLE_AUTHORIZED_PARTY_IDS": "android-123.apps.googleusercontent.com"}
	c, err = config.LoadIdentity(lookup(values))
	if err != nil || len(c.GoogleAuthorizedParties) != 2 {
		t.Fatal("Google audience/party config")
	}
	for _, bad := range []map[string]string{{"GOOGLE_CLIENT_ID": "PRIVATE_SENTINEL"}, {"GOOGLE_AUTHORIZED_PARTY_IDS": "PRIVATE_SENTINEL"}, {"GOOGLE_CLIENT_ID": "web.apps.googleusercontent.com", "GOOGLE_AUTHORIZED_PARTY_IDS": "PRIVATE_SENTINEL"}} {
		_, err := config.LoadIdentity(lookup(bad))
		if err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid config accepted or leaked")
		}
	}
	if _, err := config.LoadIdentity(nil); err == nil {
		t.Fatal("nil lookup")
	}
}
