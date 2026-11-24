package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	for _, tt := range []struct {
		name, port string
		want       int
	}{
		{"valid defaults", "", 0},
		{"invalid config", "PRIVATE_SENTINEL", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			calls := map[string]int{}
			code := run(func(key string) (string, bool) {
				calls[key]++
				return tt.port, key == "HTTP_PORT" && tt.port != ""
			}, &stdout, &stderr)
			if code != tt.want {
				t.Fatalf("exit code %d, want %d", code, tt.want)
			}
			for key, count := range calls {
				if count != 1 {
					t.Fatalf("setting %s loaded %d times", key, count)
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE_SENTINEL") {
				t.Fatal("startup leaked invalid setting value")
			}
			if tt.want == 1 && (stdout.Len() != 0 || !strings.Contains(stderr.String(), "HTTP_PORT")) {
				t.Fatal("invalid configuration must fail before successful startup output")
			}
			if tt.want == 0 && (stderr.Len() != 0 || stdout.String() != "Configuration validated; HTTP server is not implemented.\n") {
				t.Fatal("unexpected startup output")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestRunOutputFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(func(string) (string, bool) { return "", false }, failingWriter{}, &stderr); code != 1 {
		t.Fatalf("output failure exit code = %d, want 1", code)
	}
}
