package response

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/requestmeta"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func setup(t *testing.T) (*Writer, *http.Request, *httptest.ResponseRecorder, chimiddleware.WrapResponseWriter, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	writer, err := New(slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil).WithContext(requestmeta.WithID(context.Background(), "request-123"))
	recorder := httptest.NewRecorder()
	tracked := chimiddleware.NewWrapResponseWriter(recorder, 1)
	return writer, r, recorder, tracked, &logs
}
func envelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("X-Content-Type-Options") != "nosniff" || got["request_id"] != w.Header().Get("X-Request-ID") {
		t.Fatal("response headers/correlation")
	}
	if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatal("length mismatch")
	}
	return got
}
func TestSuccessContracts(t *testing.T) {
	for _, status := range []int{200, 201, 202} {
		wr, r, rec, w, _ := setup(t)
		if err := wr.Success(w, r, status, "TEST_FETCHED", List[string](nil), Options{}); err != nil {
			t.Fatal(err)
		}
		got := envelope(t, rec)
		if rec.Code != status || got["success"] != true || got["code"] != "TEST_FETCHED" || len(got) != 4 || len(got["data"].([]any)) != 0 {
			t.Fatal("success contract")
		}
	}
	wr, r, rec, w, _ := setup(t)
	cursor := "next"
	if err := wr.Success(w, r, 200, "TEST_FETCHED", struct{}{}, Options{Message: "Fetched", Meta: &Meta{Pagination: Pagination{Limit: 10, NextCursor: &cursor, HasMore: true}}}); err != nil {
		t.Fatal(err)
	}
	got := envelope(t, rec)
	if len(got) != 6 || got["meta"].(map[string]any)["pagination"].(map[string]any)["next_cursor"] != "next" {
		t.Fatal("optional metadata")
	}
	for _, method := range []string{"GET", "HEAD"} {
		for _, direct := range []bool{false, true} {
			wr, r, rec, w, _ := setup(t)
			r.Method = method
			w.Header().Set("Content-Type", "bad")
			w.Header().Set("Content-Length", "123")
			var err error
			if direct {
				err = wr.NoContent(w, r)
			} else {
				err = wr.Success(w, r, 204, "", nil, Options{})
			}
			if err != nil || rec.Code != 204 || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" || rec.Header().Get("Content-Length") != "" || rec.Header().Get("X-Request-ID") != "request-123" {
				t.Fatal("204 contract", err)
			}
		}
	}
	wr, r, rec, w, _ = setup(t)
	r.Method = "HEAD"
	if err := wr.Success(w, r, 200, "TEST_FETCHED", List[int]{1, 2}, Options{}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "0" {
		t.Fatal("HEAD semantics")
	}
	raw, err := json.Marshal(List[int]{1, 2})
	if err != nil || string(raw) != "[1,2]" {
		t.Fatal("nonempty collection")
	}
}
func TestErrorContractsAndMapping(t *testing.T) {
	statuses := map[apperror.Code]int{apperror.InvalidRequest: 400, apperror.InvalidJSON: 400, apperror.UnsupportedMediaType: 415, apperror.ValidationFailed: 422, apperror.Unauthenticated: 401, apperror.Forbidden: 403, apperror.NotFound: 404, apperror.MethodNotAllowed: 405, apperror.Conflict: 409, apperror.PayloadTooLarge: 413, apperror.RateLimited: 429, apperror.ServiceUnavailable: 503, apperror.RequestTimeout: 504, apperror.Internal: 500}
	for _, code := range apperror.Codes() {
		wr, r, rec, w, logs := setup(t)
		err := wr.Error(w, r, apperror.New(code, errors.New("PRIVATE_SENTINEL")))
		if err != nil {
			t.Fatal(err)
		}
		got := envelope(t, rec)
		if rec.Code != statuses[code] || got["success"] != false || got["code"] != string(code) || got["message"] != code.Message() || len(got) != 4 || strings.Contains(rec.Body.String()+logs.String(), "PRIVATE_SENTINEL") {
			t.Fatal("error contract", code)
		}
	}
	if Status("unknown") != 500 {
		t.Fatal("unknown mapping")
	}
	wr, r, rec, w, logs := setup(t)
	if err := wr.Error(w, r, errors.New("PRIVATE_SENTINEL")); err != nil {
		t.Fatal(err)
	}
	if envelope(t, rec)["code"] != "INTERNAL_ERROR" || strings.Contains(logs.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unknown error disclosure")
	}
	wr, r, rec, w, _ = setup(t)
	if err := wr.Error(w, r, apperror.Validation([]apperror.Field{{Field: "amount", Code: "INVALID_AMOUNT", Message: "Must be positive"}})); err != nil {
		t.Fatal(err)
	}
	got := envelope(t, rec)
	if rec.Code != 422 || got["error"].(map[string]any)["fields"].([]any)[0].(map[string]any)["field"] != "amount" {
		t.Fatal("field contract")
	}
	wr, r, rec, w, _ = setup(t)
	r.Method = "HEAD"
	if err := wr.Error(w, r, apperror.New(apperror.NotFound, nil)); err != nil || rec.Body.Len() != 0 || rec.Code != 404 {
		t.Fatal("HEAD error")
	}
}
func TestPaginationConsistency(t *testing.T) {
	empty := ""
	next := "opaque"
	space := " bad"
	long := strings.Repeat("x", 1025)
	control := "a\x00b"
	for _, tt := range []struct {
		p     Pagination
		valid bool
	}{
		{Pagination{Limit: 1}, true}, {Pagination{Limit: 1000}, true}, {Pagination{Limit: 10, NextCursor: &next, HasMore: true}, true},
		{Pagination{}, false}, {Pagination{Limit: 1001}, false}, {Pagination{Limit: 1, HasMore: true}, false}, {Pagination{Limit: 1, NextCursor: &next}, false}, {Pagination{Limit: 1, HasMore: true, NextCursor: &empty}, false}, {Pagination{Limit: 1, HasMore: true, NextCursor: &space}, false}, {Pagination{Limit: 1, HasMore: true, NextCursor: &long}, false}, {Pagination{Limit: 1, HasMore: true, NextCursor: &control}, false},
	} {
		if tt.p.Valid() != tt.valid {
			t.Fatalf("pagination: %+v", tt.p)
		}
	}
}
func TestEncodingFailureBeforeCommit(t *testing.T) {
	for _, data := range []any{make(chan int), math.NaN(), strings.Repeat("x", MaxJSONBytes)} {
		wr, r, rec, w, logs := setup(t)
		if err := wr.Success(w, r, 201, "TEST_CREATED", data, Options{}); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 500 || envelope(t, rec)["code"] != "INTERNAL_ERROR" || !strings.Contains(logs.String(), "SERIALIZATION_FAILED") {
			t.Fatal("serialization committed invalid response")
		}
	}
	for _, tt := range []struct {
		status  int
		code    string
		options Options
	}{
		{418, "TEST", Options{}}, {200, "bad", Options{}}, {200, "NOT_FOUND", Options{}}, {200, "TEST", Options{Message: "bad\n"}}, {200, "TEST", Options{Meta: &Meta{}}},
	} {
		wr, r, rec, w, _ := setup(t)
		if err := wr.Success(w, r, tt.status, tt.code, nil, tt.options); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 500 || envelope(t, rec)["code"] != "INTERNAL_ERROR" {
			t.Fatal("invalid metadata")
		}
	}
}

type failingWriter struct {
	header         http.Header
	status, writes int
	short          bool
}

func (w *failingWriter) Header() http.Header    { return w.header }
func (w *failingWriter) WriteHeader(status int) { w.status = status }
func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.short {
		return len(p) - 1, nil
	}
	return 0, errors.New("PRIVATE_SENTINEL")
}
func TestWriteFailuresAndCommitment(t *testing.T) {
	for _, short := range []bool{false, true} {
		for _, internal := range []bool{false, true} {
			wr, r, _, _, logs := setup(t)
			original := &failingWriter{header: make(http.Header), short: short}
			w := chimiddleware.NewWrapResponseWriter(original, 1)
			var err error
			if internal {
				err = wr.Error(w, r, errors.New("PRIVATE_SENTINEL"))
			} else {
				err = wr.Success(w, r, 200, "TEST_FETCHED", nil, Options{})
			}
			if err == nil || original.writes != 1 || strings.Contains(err.Error()+logs.String(), "PRIVATE_SENTINEL") || !strings.Contains(logs.String(), "RESPONSE_WRITE_FAILED") {
				t.Fatal("unsafe write failure")
			}
		}
	}
	for _, commit := range []string{"status", "flush", "write"} {
		wr, r, rec, w, _ := setup(t)
		switch commit {
		case "status":
			w.WriteHeader(202)
		case "flush":
			w.(http.Flusher).Flush()
		case "write":
			_, _ = w.Write([]byte("partial"))
		}
		before := rec.Body.String()
		if wr.Error(w, r, errors.New("private")) == nil || wr.NoContent(w, r) == nil || wr.Success(w, r, 200, "TEST", nil, Options{}) == nil || rec.Body.String() != before {
			t.Fatal("second response after commitment")
		}
	}
	wr, r, rec, w, _ := setup(t)
	if wr.Success(rec, r, 200, "TEST", nil, Options{}) == nil {
		t.Fatal("untracked writer accepted")
	}
	r = r.WithContext(context.Background())
	if wr.NoContent(w, r) == nil {
		t.Fatal("missing correlation accepted")
	}
	if _, err := New(nil); err == nil {
		t.Fatal("nil logger")
	}
}
func TestSharedWriterConcurrent(t *testing.T) {
	wr, err := New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			w := chimiddleware.NewWrapResponseWriter(rec, 1)
			r := httptest.NewRequest("GET", "/", nil).WithContext(requestmeta.WithID(context.Background(), "concurrent"))
			if err := wr.Success(w, r, 200, "TEST", List[int](nil), Options{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestStagingBoundary(t *testing.T) {
	var buffer cappedBuffer
	if n, err := buffer.Write(bytes.Repeat([]byte{'x'}, MaxJSONBytes)); err != nil || n != MaxJSONBytes {
		t.Fatal("budget boundary")
	}
	if n, err := buffer.Write([]byte{'x'}); err == nil || n != 0 || buffer.Len() != MaxJSONBytes {
		t.Fatal("overflow appended")
	}
	bad := string([]byte{255})
	if (Pagination{Limit: 1, HasMore: true, NextCursor: &bad}).Valid() {
		t.Fatal("invalid UTF-8 cursor")
	}
}
