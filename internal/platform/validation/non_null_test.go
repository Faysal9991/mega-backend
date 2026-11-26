package validation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNonNullCommandContractIsOptIn(t *testing.T) {
	strict, e := NewNonNullSnakeCaseDecoder(1024)
	if e != nil {
		t.Fatal(e)
	}
	legacy, e := NewSnakeCaseDecoder(1024)
	if e != nil {
		t.Fatal(e)
	}
	for _, body := range []string{`{"value":null}`, `{"nested":{"value":null}}`, `{"values":[null]}`} {
		var out struct {
			Value  string            `json:"value"`
			Nested map[string]string `json:"nested"`
			Values []string          `json:"values"`
		}
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if strict.Decode(r, &out) == nil {
			t.Fatal("null accepted")
		}
		r = httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if e = legacy.Decode(r, &out); e != nil {
			t.Fatal("existing nullable contract changed", e)
		}
	}
}
