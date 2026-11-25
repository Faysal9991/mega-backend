package fcm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"

	"cloud.google.com/go/auth/credentials"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	"google.golang.org/api/option"
)

type Firebase struct {
	client        *messaging.Client
	authTransport *http.Transport
}

func NewFirebase(ctx context.Context, file string) (*Firebase, error) {
	fail := func() (*Firebase, error) { return nil, errors.New("FCM initialization failed") }
	input, e := os.Open(file)
	if e != nil {
		return fail()
	}
	defer input.Close()
	data, e := io.ReadAll(io.LimitReader(input, 65537))
	if e != nil || len(data) > 65536 {
		return fail()
	}
	var meta struct {
		Type     string `json:"type"`
		Project  string `json:"project_id"`
		TokenURL string `json:"token_uri"`
	}
	if e = json.Unmarshal(data, &meta); e != nil || meta.Type != "service_account" || !regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`).MatchString(meta.Project) || meta.TokenURL != "https://oauth2.googleapis.com/token" && meta.TokenURL != "https://accounts.google.com/o/oauth2/token" {
		return fail()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxConnsPerHost = 4
	transport.MaxIdleConnsPerHost = 4
	transport.ResponseHeaderTimeout = 5 * time.Second
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds, e := credentials.NewCredentialsFromJSON(credentials.ServiceAccount, data, &credentials.DetectOptions{Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}, Client: &http.Client{Transport: transport, Timeout: 5 * time.Second}, DisableAsyncRefresh: true, Logger: quiet})
	if e != nil {
		transport.CloseIdleConnections()
		return fail()
	}
	app, e := firebase.NewApp(ctx, &firebase.Config{ProjectID: meta.Project}, option.WithAuthCredentials(creds), option.WithLogger(quiet))
	if e != nil {
		transport.CloseIdleConnections()
		return fail()
	}
	client, e := app.Messaging(ctx)
	if e != nil {
		transport.CloseIdleConnections()
		return fail()
	}
	return &Firebase{client: client, authTransport: transport}, nil
}
func (f *Firebase) Close() error {
	if f.authTransport != nil {
		f.authTransport.CloseIdleConnections()
	}
	return nil
}
func (f *Firebase) Send(ctx context.Context, p delivery.Push) error {
	title, body, ok := delivery.PushTemplate(p.Kind, p.Template)
	if !ok {
		return delivery.ErrPermanent
	}
	_, e := f.client.Send(ctx, &messaging.Message{Token: p.Token, Notification: &messaging.Notification{Title: title, Body: body}, Data: map[string]string{"event_id": p.EventID, "type": p.Kind}, Android: &messaging.AndroidConfig{Priority: "normal", CollapseKey: p.EventID}, APNS: &messaging.APNSConfig{Headers: map[string]string{"apns-collapse-id": p.EventID}}})
	if e == nil {
		return nil
	}
	if messaging.IsRegistrationTokenNotRegistered(e) {
		return delivery.ErrInvalidToken
	}
	if messaging.IsInvalidArgument(e) || messaging.IsSenderIDMismatch(e) || messaging.IsThirdPartyAuthError(e) {
		return delivery.ErrPermanent
	}
	return errors.New("FCM transient failure")
}

// Keep provider errors/transport headers private; callers persist stable codes.
