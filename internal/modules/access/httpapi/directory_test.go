package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/go-chi/chi/v5"
)

type directoryFixture struct {
	*fixture
	users   []access.User
	queries int
}

func (f *directoryFixture) Within(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(access.Unit) error) error {
	return f.fixture.Within(ctx, c, p, owner, target, func(access.Unit) error { return fn(f) })
}
func (f *directoryFixture) Directory(_ context.Context, cursor string, limit int) ([]access.User, error) {
	f.queries++
	out := []access.User{}
	for _, u := range f.users {
		if u.AccountID > cursor {
			out = append(out, u)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func TestDirectoryEscapedResponseBudgetAndCursor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		large bool
	}{
		{"review_reproduction", 100, true},
		{"lookahead_and_budget_pages", 201, true},
		{"ordinary_full_page", 100, false},
		{"empty", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &directoryFixture{fixture: &fixture{owner: true}}
			for i := 0; i < tc.count; i++ {
				p := profiles.Profile{AccountID: fmt.Sprintf("%032x", i+1), DisplayName: "Synthetic", Bio: "", AvatarURL: ""}
				if tc.large {
					p.DisplayName = strings.Repeat("<", 80)
					p.Bio = strings.Repeat("<", 500)
					prefix := "https://example.invalid/"
					p.AvatarURL = prefix + strings.Repeat("&", 2048-len(prefix))
				}
				f.users = append(f.users, access.User{Profile: p, Status: "active"})
			}
			s, e := access.New(f)
			if e != nil {
				t.Fatal(e)
			}
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			h, e := New(s, logger)
			if e != nil {
				t.Fatal(e)
			}
			cfg, e := config.Load(func(string) (string, bool) { return "", false })
			if e != nil {
				t.Fatal(e)
			}
			router, e := httpserver.NewRouter(cfg.HTTP, logger, httpserver.NewLifecycle(nil), func(r chi.Router) { h.Mount(r) })
			if e != nil {
				t.Fatal(e)
			}
			cursor := ""
			seen := 0
			pages := 0
			for {
				r := httptest.NewRequest("GET", "/api/v1/admin/users?limit=100"+func() string {
					if cursor != "" {
						return "&cursor=" + cursor
					}
					return ""
				}(), nil)
				r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
				r.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
				r.Header.Set("X-Request-ID", strings.Repeat("r", 64))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				pages++
				if w.Code != 200 {
					t.Fatalf("directory status=%d body=%s", w.Code, w.Body.String())
				}
				if w.Body.Len() > response.MaxJSONBytes || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
					t.Fatal("response byte budget or content length mismatch")
				}
				var out struct {
					Success   bool          `json:"success"`
					Code      string        `json:"code"`
					Data      []access.User `json:"data"`
					Meta      response.Meta `json:"meta"`
					RequestID string        `json:"request_id"`
				}
				if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
					t.Fatal(e)
				}
				if out.Data == nil || out.Meta.Pagination.Limit != 100 {
					t.Fatal("array or requested limit semantics changed")
				}
				if !out.Success || out.Code != "USERS_FETCHED" || out.RequestID != strings.Repeat("r", 64) || out.Data == nil || out.Meta.Pagination.Limit != 100 {
					t.Fatal("envelope/pagination contract drift")
				}
				if len(out.Data) > 100 || seen+len(out.Data) > len(f.users) {
					t.Fatal("page bound exceeded")
				}
				for i, u := range out.Data {
					if !reflect.DeepEqual(u, f.users[seen+i]) {
						t.Fatal("row skipped, duplicated or truncated")
					}
				}
				seen += len(out.Data)
				more := seen < len(f.users)
				if out.Meta.Pagination.HasMore != more {
					t.Fatal("incorrect has_more")
				}
				if !more {
					if out.Meta.Pagination.NextCursor != nil {
						t.Fatal("terminal cursor must be null")
					}
					break
				}
				if len(out.Data) == 0 || out.Meta.Pagination.NextCursor == nil || *out.Meta.Pagination.NextCursor != out.Data[len(out.Data)-1].AccountID {
					t.Fatal("cursor did not advance from last returned row")
				}
				cursor = *out.Meta.Pagination.NextCursor
			}
			if seen != tc.count || f.queries != pages {
				t.Fatal("incomplete directory or extra queries")
			}
			if tc.large && pages < 2 {
				t.Fatal("fixture did not exercise byte-limited pagination")
			}
			if !tc.large && pages != 1 {
				t.Fatal("ordinary/empty page behavior changed")
			}
		})
	}
}
