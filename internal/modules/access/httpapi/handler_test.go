package httpapi

import (
	"context"
	"encoding/json"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	"github.com/Faysal9991/mega-backend/internal/modules/profiles"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

type fixture struct {
	access.Unit
	fail     bool
	calls    int
	owner    bool
	conflict bool
	role     access.Role
	roleIDs  []string
	status   string
	profile  profiles.Profile
}

func (f *fixture) Within(ctx context.Context, c access.Credentials, p string, owner bool, target string, fn func(access.Unit) error) error {
	f.calls++
	if f.fail {
		return access.Failure(apperror.ServiceUnavailable)
	}
	if c.Access != strings.Repeat("a", 43) || c.Installation != strings.Repeat("b", 32) {
		return access.Failure(apperror.Unauthenticated)
	}
	if !f.owner && (p != "" || owner) {
		return access.Failure(apperror.Forbidden)
	}
	return fn(f)
}
func (f *fixture) Actor() string                                             { return strings.Repeat("d", 32) }
func (f *fixture) Profile(context.Context, string) (profiles.Profile, error) { return f.profile, nil }
func (f *fixture) UpdateProfile(_ context.Context, _ string, p profiles.Patch) (profiles.Profile, error) {
	if p.DisplayName != nil {
		f.profile.DisplayName = *p.DisplayName
	}
	if p.Bio != nil {
		f.profile.Bio = *p.Bio
	}
	return f.profile, nil
}
func (f *fixture) Effective() access.Effective { return access.Effective{Permissions: []string{}} }
func TestHTTPStrictProfilesAndScope(t *testing.T) {
	f := &fixture{profile: profiles.Profile{AccountID: strings.Repeat("d", 32), DisplayName: "নাম", Bio: "bio", AvatarURL: ""}}
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
	for _, c := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/profiles/me", "", 200}, {"GET", "/profiles/" + strings.Repeat("d", 32), "", 200},
		{"PATCH", "/profiles/me", `{"display_name":""}`, 200}, {"PATCH", "/profiles/me", `{"bio":"নতুন"}`, 200},
		{"PATCH", "/profiles/me", `{}`, 422}, {"PATCH", "/profiles/me", `{"bio":null}`, 422},
		{"PATCH", "/profiles/me", `{"display_name":"a","DISPLAY_NAME":"b"}`, 400},
		{"PATCH", "/profiles/me", `{"avatar_url":"https://invalid"}`, 400}, {"PATCH", "/profiles/me", `{"status":"active"}`, 400},
		{"PATCH", "/profiles/me", `{"roles":["owner"]}`, 400}, {"PATCH", "/profiles/me", `{"password":"x"}`, 400},
		{"PATCH", "/profiles/me", `{"contact_email":"x"}`, 400}, {"PATCH", "/profiles/me", `{"username":"x"}`, 400},
		{"PATCH", "/profiles/me", `{"account_id":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}`, 400},
		{"GET", "/admin/users?sort=name", "", 422},
		{"GET", "/admin/users?limit=%GG", "", 422},
		{"GET", "/identity/me/permissions", "", 200}, {"GET", "/admin/users", "", 403},
		{"GET", "/admin/roles", "", 403}, {"GET", "/admin/users?limit=%xx", "", 422}, {"GET", "/admin/users?limit=101", "", 422}, {"GET", "/admin/users?limit=1&limit=2", "", 422},
		{"POST", "/admin/roles", `{"name":"owner","permissions":["owner"],"reason":"test"}`, 422},
		{"POST", "/admin/roles", `{"name":"owner","permissions":["users.read"],"reason":"test"}`, 403},
	} {
		t.Run(c.method+c.path+c.body, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/api/v1"+c.path, strings.NewReader(c.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
			r.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != c.status {
				t.Fatalf("status %d want %d body %s", w.Code, c.status, w.Body.String())
			}
			var envelope map[string]any
			if json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
				t.Fatal("invalid envelope")
			}
			if w.Code == 200 && strings.HasPrefix(c.path, "/profiles/") {
				data := envelope["data"].(map[string]any)
				if len(data) != 4 {
					t.Fatal("public data leak")
				}
				for _, key := range []string{"account_id", "display_name", "bio", "avatar_url"} {
					if _, ok := data[key]; !ok {
						t.Fatal("missing profile field")
					}
				}
			}
		})
	}
	if f.profile.DisplayName != "" || f.profile.Bio != "নতুন" {
		t.Fatal("partial update failed")
	}
	for _, fail := range []bool{false, true} {
		f.fail = fail
		r := httptest.NewRequest("GET", "/api/v1/profiles/me", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		want := 401
		if fail {
			want = 503
		}
		if w.Code != want {
			t.Fatal("failed open", w.Code)
		}
	}
}

func TestSharedProfileResponseFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../../contracts/openapi/fixtures/profiles.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures []struct {
		Schema string
		Body   map[string]any
	}
	if e := json.Unmarshal(raw, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, c := range fixtures {
		data := c.Body["data"].(map[string]any)
		f := &fixture{}
		path := "/identity/me/permissions"
		if c.Schema == "PROFILE_FETCHEDResponse" {
			path = "/profiles/me"
			f.profile = profiles.Profile{AccountID: data["account_id"].(string), DisplayName: data["display_name"].(string), Bio: data["bio"].(string), AvatarURL: data["avatar_url"].(string)}
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
		r := httptest.NewRequest("GET", "/api/v1"+path, nil)
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
		r.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
		r.Header.Set("X-Request-ID", c.Body["request_id"].(string))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var actual map[string]any
		if json.Unmarshal(w.Body.Bytes(), &actual) != nil || w.Code != 200 || !reflect.DeepEqual(actual, c.Body) {
			t.Fatalf("fixture drift %s", c.Schema)
		}
	}
}

func (f *fixture) User(context.Context, string) (access.User, error) {
	return access.User{Profile: f.profile, Status: "active"}, nil
}
func (f *fixture) Private(context.Context, string) (access.Private, error) {
	return access.Private{ContactEmail: "private@example.invalid", Username: "fixture", Phone: "+8801712345678"}, nil
}
func (f *fixture) Directory(context.Context, string, int) ([]access.User, error) {
	return []access.User{}, nil
}
func (f *fixture) Roles(context.Context) ([]access.Role, error) {
	if f.role.ID == "" {
		return []access.Role{}, nil
	}
	return []access.Role{f.role}, nil
}
func (f *fixture) PutRole(_ context.Context, r access.Role, _ bool) error {
	if f.conflict {
		return access.Failure(apperror.Conflict)
	}
	f.role = r
	return nil
}
func (f *fixture) DeleteRole(context.Context, string) error { f.role = access.Role{}; return nil }
func (f *fixture) Assignments(context.Context, string) ([]string, error) {
	if f.roleIDs == nil {
		return []string{}, nil
	}
	return f.roleIDs, nil
}
func (f *fixture) Assign(_ context.Context, _ string, roles []string) error {
	f.roleIDs = roles
	return nil
}
func (f *fixture) Restrict(_ context.Context, _ string, restricted bool) error {
	f.status = "active"
	if restricted {
		f.status = "restricted"
	}
	return nil
}
func (f *fixture) Audit(context.Context, access.Audit) error { return nil }
func TestAdminHTTPDTOContracts(t *testing.T) {
	f := &fixture{owner: true, profile: profiles.Profile{AccountID: strings.Repeat("e", 32), DisplayName: "Synthetic", Bio: "", AvatarURL: ""}}
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
	target := strings.Repeat("e", 32)
	role := strings.Repeat("c", 32)
	for _, c := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/admin/users?limit=1", "", 200, "USERS_FETCHED"},
		{"GET", "/admin/users/" + target, "", 200, "USER_FETCHED"},
		{"GET", "/admin/users/" + target + "/private", "", 200, "USER_PRIVATE_FETCHED"},
		{"PATCH", "/admin/users/" + target + "/profile", `{"bio":"edited","reason":"fixture"}`, 200, "PROFILE_UPDATED"},
		{"PUT", "/admin/users/" + target + "/restriction", `{"restricted":true,"reason":"fixture"}`, 200, "RESTRICTION_UPDATED"},
		{"PUT", "/admin/users/" + target + "/restriction", `{"reason":"fixture"}`, 422, "VALIDATION_FAILED"},
		{"GET", "/admin/roles", "", 200, "ROLES_FETCHED"},
		{"POST", "/admin/roles", `{"name":"fixture","permissions":["users.read"],"reason":"fixture"}`, 201, "ROLE_SAVED"},
		{"PUT", "/admin/roles/" + role, `{"name":"updated","permissions":[],"reason":"fixture"}`, 200, "ROLE_SAVED"},
		{"GET", "/admin/users/" + target + "/roles", "", 200, "ASSIGNMENTS_FETCHED"},
		{"PUT", "/admin/users/" + target + "/roles", `{"role_ids":["` + role + `"],"reason":"fixture"}`, 200, "ASSIGNMENTS_UPDATED"},
		{"DELETE", "/admin/roles/" + role, `{"reason":"fixture"}`, 200, "ROLE_DELETED"},
	} {
		r := httptest.NewRequest(c.method, "/api/v1"+c.path, strings.NewReader(c.body))
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
		r.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		var envelope struct {
			Code string
			Data json.RawMessage
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != c.status || envelope.Code != c.code {
			t.Fatalf("%s %s: %d %s", c.method, c.path, w.Code, w.Body.String())
		}
		if c.code == "USERS_FETCHED" && !strings.Contains(w.Body.String(), `"data":[]`) {
			t.Fatal("empty directory is not []")
		}
		if c.code == "USER_PRIVATE_FETCHED" {
			var projection map[string]any
			if json.Unmarshal(envelope.Data, &projection) != nil || len(projection) != 4 || projection["phone_verified"] != false {
				t.Fatal("private projection drift")
			}
		}
	}
	f.conflict = true
	r := httptest.NewRequest("POST", "/api/v1/admin/roles", strings.NewReader(`{"name":"duplicate","permissions":[],"reason":"fixture"}`))
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
	r.Header.Set("X-Installation-ID", strings.Repeat("b", 32))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"CONFLICT"`) {
		t.Fatal("conflict envelope drift")
	}
}
