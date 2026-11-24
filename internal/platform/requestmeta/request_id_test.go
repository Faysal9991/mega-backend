package requestmeta

import (
	"context"
	"testing"
)

func TestValidatedContext(t *testing.T) {
	base := context.Background()
	valid := WithID(base, "caller_123-abc")
	if ID(base) != "" || ID(valid) != "caller_123-abc" {
		t.Fatal("context mutation/correlation")
	}
	for _, id := range []string{"", "bad id", "bad\n", "bad,one", "বাংলা"} {
		if ID(WithID(valid, id)) != "" {
			t.Fatal("invalid correlation retained")
		}
	}
	if ID(valid) != "caller_123-abc" {
		t.Fatal("parent context mutated")
	}
}
