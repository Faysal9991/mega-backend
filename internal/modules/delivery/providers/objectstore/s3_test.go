package objectstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
)

func TestS3PresignScopeAndBoundedReads(t *testing.T) {
	var match string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "3")
			w.Header().Set("ETag", `"synthetic-etag"`)
			return
		}
		if r.Method == "GET" {
			match = r.Header.Get("If-Match")
			w.Write([]byte("123"))
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	store := NewS3(server.URL, "test-region", "private-assets", "synthetic-key", "synthetic-secret-for-test-only", true)
	defer store.Close()
	ctx := context.Background()
	signed, headers, e := store.Presign(ctx, "test/pending/server-id", "image/png", 3, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(signed)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(u.Path, "/private-assets/test/pending/server-id") || u.Query().Get("X-Amz-Expires") != "60" {
		t.Fatal("presign scope/expiry")
	}
	checksum := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if u.Query().Get("x-amz-checksum-sha256") != checksum && headers.Get("X-Amz-Checksum-Sha256") != checksum {
		t.Fatal("checksum not bound to upload")
	}
	combined := u.Query().Get("X-Amz-SignedHeaders")
	if !strings.Contains(combined, "content-length") || !strings.Contains(combined, "content-type") {
		t.Fatal("required size/type not signed", combined, headers)
	}
	if u.Query().Get("x-amz-acl") != "private" && headers.Get("X-Amz-Acl") != "private" {
		t.Fatal("private ACL not bound")
	}
	obj, e := store.Head(ctx, "test/pending/server-id")
	if e != nil || obj.Size != 3 || obj.ContentType != "image/png" {
		t.Fatal("metadata", e)
	}
	if _, e := store.Read(ctx, "test/pending/server-id", obj.ETag, 2); e == nil {
		t.Fatal("oversized body accepted")
	}
	if match != `"synthetic-etag"` {
		t.Fatal("ETag condition absent")
	}
	if _, _, e := store.Presign(ctx, "key", "image/png", delivery.MaxImageBytes+1, "sum", time.Minute); e == nil {
		t.Fatal("oversized presign")
	}
}

func TestS3Cancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	store := NewS3(server.URL, "region", "bucket", "key", "synthetic-secret", true)
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := store.Head(ctx, "test/pending/a"); e == nil {
		t.Fatal("timeout ignored")
	}
}
