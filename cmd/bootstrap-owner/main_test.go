package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBootstrapExplicitSelectionAndCredentialGuards(t *testing.T) {
	for _, args := range [][]string{nil, {"-account-id", "invalid"}, {"-account-id", strings.Repeat("a", 32), "-reason", "fixture"}, {"-account-id", strings.Repeat("a", 32), "-reason", "fixture", "-operation-id", "bad correlation"}, {"-unknown"}} {
		var out, err bytes.Buffer
		reads := 0
		if code := run(args, func(string) (string, bool) { reads++; return "", false }, &out, &err); code != 1 || reads != 0 || out.Len() != 0 {
			t.Fatal("invalid invocation reached credentials or published success")
		}
	}
	var out, err bytes.Buffer
	// A valid explicit selection with no dedicated migration configuration fails before network.
	if code := run([]string{"-account-id", strings.Repeat("a", 32), "-reason", "fixture", "-operation-id", "fixture-operation"}, func(string) (string, bool) { return "", false }, &out, &err); code != 1 || out.Len() != 0 {
		t.Fatal("missing migration credentials accepted")
	}
}
