package validation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
)

type input struct {
	Name   string      `json:"name"`
	Number json.Number `json:"number"`
	Extra  any         `json:"extra"`
}

func TestDecoderPolicy(t *testing.T) {
	decoder, err := NewDecoder(1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, body, media string
		code              apperror.Code
	}{
		{"valid", `{"name":"ok","number":9007199254740993,"extra":9007199254740993}`, "application/json", ""},
		{"case matching", `{"NAME":"ok"}`, "Application/JSON; charset=UTF-8", ""},
		{"empty", "", "application/json", apperror.InvalidJSON},
		{"malformed", `{"name":`, "application/json", apperror.InvalidJSON},
		{"unknown", `{"secret":"PRIVATE_SENTINEL"}`, "application/json", apperror.InvalidJSON},
		{"null", "null", "application/json", apperror.InvalidRequest},
		{"array", "[]", "application/json", apperror.InvalidRequest},
		{"scalar", "1", "application/json", apperror.InvalidRequest},
		{"multiple", "{} {}", "application/json", apperror.InvalidJSON},
		{"garbage", "{} PRIVATE_SENTINEL", "application/json", apperror.InvalidJSON},
		{"duplicate", `{"name":"a","name":"b"}`, "application/json", apperror.InvalidJSON},
		{"folded duplicate", `{"name":"a","NAME":"b"}`, "application/json", apperror.InvalidJSON},
		{"escaped duplicate", `{"name":"a","na\u006de":"b"}`, "application/json", apperror.InvalidJSON},
		{"nested duplicate", `{"extra":{"a":1,"A":2}}`, "application/json", apperror.InvalidJSON},
		{"unicode fold duplicate", `{"extra":{"S":1,"ſ":2}}`, "application/json", apperror.InvalidJSON},
		{"repeated separate objects", `{"extra":[{"a":1},{"a":2}]}`, "application/json", ""},
		{"nested array", `{"extra":[[1],null,true,"x"]}`, "application/json", ""},
		{"wrong type", `{"name":123}`, "application/json", apperror.InvalidJSON},
		{"depth", `{"extra":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`, "application/json", apperror.InvalidJSON},
		{"invalid utf8", "{\"name\":\"" + string([]byte{255}) + "\"}", "application/json", apperror.InvalidJSON},
		{"no media", "{}", "", apperror.UnsupportedMediaType},
		{"wrong media", "{}", "text/plain", apperror.UnsupportedMediaType},
		{"suffix media", "{}", "application/problem+json", apperror.UnsupportedMediaType},
		{"bad media", "{}", "application/json; charset", apperror.UnsupportedMediaType},
		{"bad charset", "{}", "application/json; charset=latin1", apperror.UnsupportedMediaType},
		{"unknown parameter", "{}", "application/json; foo=bar", apperror.UnsupportedMediaType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			if tt.media != "" {
				r.Header.Set("Content-Type", tt.media)
			}
			var dst input
			err := decoder.Decode(r, &dst)
			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				if tt.name == "valid" && (dst.Number.String() != "9007199254740993" || dst.Extra.(json.Number).String() != "9007199254740993") {
					t.Fatal("numeric precision lost")
				}
				return
			}
			if err == nil || apperror.Public(err).Code() != tt.code || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatalf("got %v; want %s", err, tt.code)
			}
		})
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("PRIVATE_SENTINEL") }

type customDTO struct{}

func (*customDTO) UnmarshalJSON([]byte) error { return nil }
func TestDecoderBoundsAndMisuse(t *testing.T) {
	for _, size := range []int64{0, -1, 16<<20 + 1} {
		if _, err := NewDecoder(size); err == nil {
			t.Fatal("invalid budget")
		}
	}
	decoder, _ := NewDecoder(32)
	for _, chunked := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"`+strings.Repeat("x", 40)+`"}`))
		r.Header.Set("Content-Type", "application/json")
		if chunked {
			r.ContentLength = -1
			r.Body = http.MaxBytesReader(nil, r.Body, 16)
		}
		if apperror.Public(decoder.Decode(r, &input{})).Code() != apperror.PayloadTooLarge {
			t.Fatal("oversized body")
		}
	}
	for _, dst := range []any{nil, input{}, (*input)(nil), new([]string), &customDTO{}} {
		r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		if apperror.Public(decoder.Decode(r, dst)).Code() != apperror.Internal {
			t.Fatal("invalid destination")
		}
	}
	for _, kind := range []string{"nil body", "broken read", "encoding", "duplicate media"} {
		r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		want := apperror.InvalidJSON
		switch kind {
		case "nil body":
			r.Body = nil
		case "broken read":
			r.Body = io.NopCloser(brokenReader{})
			want = apperror.InvalidRequest
		case "encoding":
			r.Header.Set("Content-Encoding", "gzip")
			want = apperror.UnsupportedMediaType
		case "duplicate media":
			r.Header.Add("Content-Type", "application/json")
			want = apperror.UnsupportedMediaType
		}
		if apperror.Public(decoder.Decode(r, &input{})).Code() != want {
			t.Fatal(kind)
		}
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"}`))
			r.Header.Set("Content-Type", "application/json")
			if err := decoder.Decode(r, &input{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestValidationResult(t *testing.T) {
	var forward, reverse Result
	if forward.Err() != nil {
		t.Fatal("empty validation")
	}
	for i := 0; i < 30; i++ {
		forward.Add(string(rune('a'+i%26)), "REQUIRED", "Required")
	}
	for i := 29; i >= 0; i-- {
		reverse.Add(string(rune('a'+i%26)), "REQUIRED", "Required")
	}
	a := apperror.Public(forward.Err()).Fields()
	b := apperror.Public(reverse.Err()).Fields()
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if len(a) != 20 || string(aj) != string(bj) {
		t.Fatal("unbounded/nondeterministic fields")
	}
	var invalid Result
	invalid.Add("bad field", "REQUIRED", "Required")
	if apperror.Public(invalid.Err()).Code() != apperror.Internal {
		t.Fatal("unsafe diagnostic accepted")
	}
	var same Result
	same.Add("a", "ZZZ", "Z")
	same.Add("a", "AAA", "B")
	same.Add("a", "AAA", "A")
	same.Add("a", "AAA", "A")
	fields := apperror.Public(same.Err()).Fields()
	if len(fields) != 3 || fields[0].Message != "A" {
		t.Fatal("diagnostic ordering/deduplication")
	}
}

func TestExactBodyBudgetAndMalformedNestedJSON(t *testing.T) {
	decoder, _ := NewDecoder(32)
	for _, chunked := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/", strings.NewReader("{}"+strings.Repeat(" ", 30)))
		r.Header.Set("Content-Type", "application/json")
		if chunked {
			r.ContentLength = -1
		}
		if err := decoder.Decode(r, &input{}); err != nil {
			t.Fatal("exact budget rejected", err)
		}
	}
	for _, body := range []string{`{"extra":[}`, `{"extra":{"a":}}`, `{"extra":{"a":1,}}`, `{"extra":[1,]}`, `{"extra":{"a":1}`, `{"extra":[1}`, `{"extra":[1`, `{"extra":{"a":1}x}`, "   "} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if apperror.Public(decoder.Decode(r, &input{})).Code() != apperror.InvalidJSON {
			t.Fatal("malformed JSON accepted")
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("Content-Encoding", "identity")
	r.Header.Add("Content-Encoding", "gzip")
	if apperror.Public(decoder.Decode(r, &input{})).Code() != apperror.UnsupportedMediaType {
		t.Fatal("ambiguous encoding accepted")
	}
}
