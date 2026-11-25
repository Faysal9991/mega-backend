package fcm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

type rewrite struct {
	endpoint string
	calls    atomic.Int32
	status   int
	body     string
}

func (r *rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls.Add(1)
	body, e := io.ReadAll(req.Body)
	if e != nil {
		return nil, e
	}
	var message struct {
		Message struct {
			Token        string            `json:"token"`
			Data         map[string]string `json:"data"`
			Notification map[string]string `json:"notification"`
		} `json:"message"`
	}
	if e = json.Unmarshal(body, &message); e != nil {
		return nil, e
	}
	if len(message.Message.Data) != 2 || message.Message.Data["event_id"] == "" || message.Message.Token != "synthetic-device-token" {
		return nil, delivery.ErrPermanent
	}
	u, _ := url.Parse(r.endpoint)
	clone := req.Clone(req.Context())
	clone.URL.Scheme = u.Scheme
	clone.URL.Host = u.Host
	clone.Body = io.NopCloser(strings.NewReader(string(body)))
	return http.DefaultTransport.RoundTrip(clone)
}
func TestFirebaseOfficialClientAllowlistAndInvalidToken(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if invalid {
				w.WriteHeader(404)
				io.WriteString(w, `{"error":{"code":404,"message":"synthetic unregistered","status":"NOT_FOUND","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`)
			} else {
				io.WriteString(w, `{"name":"projects/synthetic/messages/event"}`)
			}
		}))
		transport := &rewrite{endpoint: server.URL}
		app, e := firebase.NewApp(context.Background(), &firebase.Config{ProjectID: "synthetic-project"}, option.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-token"})), option.WithHTTPClient(&http.Client{Transport: transport, Timeout: time.Second}))
		if e != nil {
			server.Close()
			t.Fatal(e)
		}
		client, e := app.Messaging(context.Background())
		if e != nil {
			server.Close()
			t.Fatal(e)
		}
		f := &Firebase{client: client}
		e = f.Send(context.Background(), delivery.Push{Token: "synthetic-device-token", Kind: "gift_received", EventID: strings.Repeat("a", 32)})
		server.Close()
		if invalid && e != delivery.ErrInvalidToken || !invalid && e != nil {
			t.Fatal("FCM classification", e)
		}
		if transport.calls.Load() != 1 {
			t.Fatal("unexpected SDK calls")
		}
		if e := f.Send(context.Background(), delivery.Push{Kind: "wallet_balance"}); e != delivery.ErrPermanent {
			t.Fatal("unsafe template")
		}
	}
}

func TestRejectCredentialTypesAndUntrustedTokenEndpoints(t *testing.T) {
	for _, body := range []string{`{"type":"external_account","project_id":"synthetic"}`, `{"type":"service_account","project_id":"synthetic","token_uri":"http://127.0.0.1/token"}`} {
		file := filepath.Join(t.TempDir(), "credentials.json")
		if e := os.WriteFile(file, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := NewFirebase(context.Background(), file); e == nil {
			t.Fatal("unapproved credential configuration")
		}
	}
}

func TestQuotaFailureRetriesWithoutPurgingRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"code":429,"message":"synthetic quota","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"QUOTA_EXCEEDED"}]}}`)
	}))
	defer server.Close()
	transport := &rewrite{endpoint: server.URL}
	app, e := firebase.NewApp(context.Background(), &firebase.Config{ProjectID: "synthetic-project"}, option.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-token"})), option.WithHTTPClient(&http.Client{Transport: transport, Timeout: time.Second}))
	if e != nil {
		t.Fatal(e)
	}
	client, e := app.Messaging(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e = (&Firebase{client: client}).Send(ctx, delivery.Push{Token: "synthetic-device-token", Kind: "gift_received", EventID: strings.Repeat("a", 32)})
	if e == nil || e == delivery.ErrPermanent || e == delivery.ErrInvalidToken {
		t.Fatal("quota classified as token invalid/permanent", e)
	}
}
