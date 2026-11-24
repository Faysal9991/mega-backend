package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func TestHTTPFoundationSettings(t *testing.T) {
	for _, tt := range []struct {
		key, value string
		valid      bool
	}{
		{"HTTP_MAX_HEADER_BYTES", "1024", true}, {"HTTP_MAX_HEADER_BYTES", "1048576", true},
		{"HTTP_MAX_HEADER_BYTES", "", false}, {"HTTP_MAX_HEADER_BYTES", "1023", false}, {"HTTP_MAX_HEADER_BYTES", "1048577", false},
		{"HTTP_MAX_HEADER_BYTES", "-1", false}, {"HTTP_MAX_HEADER_BYTES", "PRIVATE_SENTINEL", false},
		{"HTTP_MAX_CONCURRENT_REQUESTS", "1", true}, {"HTTP_MAX_CONCURRENT_REQUESTS", "4096", true},
		{"HTTP_MAX_CONCURRENT_REQUESTS", "", false}, {"HTTP_MAX_CONCURRENT_REQUESTS", "0", false}, {"HTTP_MAX_CONCURRENT_REQUESTS", "4097", false},
		{"HTTP_MAX_CONCURRENT_REQUESTS", "-1", false}, {"HTTP_MAX_CONCURRENT_REQUESTS", "PRIVATE_SENTINEL", false},
		{"HTTP_REQUEST_TIMEOUT", "1ms", true}, {"HTTP_REQUEST_TIMEOUT", "5m", true},
		{"HTTP_REQUEST_TIMEOUT", "", false}, {"HTTP_REQUEST_TIMEOUT", "0s", false}, {"HTTP_REQUEST_TIMEOUT", "-1s", false},
		{"HTTP_REQUEST_TIMEOUT", "5m1ns", false}, {"HTTP_REQUEST_TIMEOUT", "PRIVATE_SENTINEL", false},
	} {
		t.Run(tt.key+"/"+tt.value, func(t *testing.T) {
			got, err := config.Load(lookup(map[string]string{tt.key: tt.value}))
			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.key) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") || got != (config.Config{}) {
				t.Fatalf("invalid validation result: %v", err)
			}
		})
	}
	cfg, err := config.Load(lookup(map[string]string{"HTTP_REQUEST_TIMEOUT": "2s", "HTTP_MAX_CONCURRENT_REQUESTS": "32", "HTTP_MAX_HEADER_BYTES": "8192"}))
	if err != nil || cfg.HTTP.RequestTimeout != 2*time.Second || cfg.HTTP.MaxConcurrentRequests != 32 || cfg.HTTP.MaxHeaderBytes != 8192 {
		t.Fatal("HTTP overrides were not applied")
	}
}
