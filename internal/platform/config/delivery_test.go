package config

import (
	"strings"
	"testing"
)

func TestDeliveryOptionalAndSecrets(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	c, e := LoadDelivery(lookup, Test)
	if e != nil || c.MediaEnabled || c.PushEnabled || c.Workers != 2 {
		t.Fatal("optional providers", e)
	}
	for k, v := range map[string]string{"MEDIA_ENABLED": "yes", "MEDIA_PREFIX": "../prod", "DELIVERY_WORKERS": "100", "PUSH_ENABLED": "true", "PUSH_TOKEN_ENCRYPTION_KEY": "wrong", "DELIVERY_DATABASE_TIMEOUT": "1h"} {
		_, e := LoadDelivery(func(key string) (string, bool) {
			if key == k {
				return v, true
			}
			return "", false
		}, Test)
		if e == nil || strings.Contains(e.Error(), "wrong") {
			t.Fatal(k, e)
		}
	}
}
