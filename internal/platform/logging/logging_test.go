package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
)

func settings(t testing.TB, values map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func jsonLog(t testing.TB, output string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	checkJSONValue(t, decoder)
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatal("extra tokens after JSON record")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatalf("invalid JSON log: %v", err)
	}
	return got
}

// Unmarshal alone hides duplicate object keys by overwriting earlier values.
func checkJSONValue(t testing.TB, decoder *json.Decoder) {
	t.Helper()
	token, err := decoder.Token()
	if err != nil {
		t.Fatal(err)
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				t.Fatal(err)
			}
			name := key.(string)
			if seen[name] {
				t.Fatalf("duplicate JSON key: %s", name)
			}
			seen[name] = true
			checkJSONValue(t, decoder)
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			t.Fatal("invalid JSON object")
		}
	case json.Delim('['):
		for decoder.More() {
			checkJSONValue(t, decoder)
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			t.Fatal("invalid JSON array")
		}
	}
}

func TestFormatsMetadataUTCAndLevels(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
			t.Run(format+"/"+level.String(), func(t *testing.T) {
				var out bytes.Buffer
				cfg := settings(t, map[string]string{"LOG_FORMAT": format, "LOG_LEVEL": level.String(), "INSTANCE_ID": "replica-2"})
				logger, err := logging.New(cfg, &out)
				if err != nil {
					t.Fatal(err)
				}
				for _, candidate := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
					before := out.Len()
					logger.LogAttrs(context.Background(), candidate, "test.level", slog.Int("count", 1))
					if (out.Len() > before) != (candidate >= level) {
						t.Fatal("incorrect level filtering")
					}
				}
				out.Reset()
				stamp := time.Date(2026, 11, 24, 12, 30, 0, 0, time.FixedZone("test", 6*3600))
				record := slog.NewRecord(stamp, level, "test.metadata", 0)
				record.AddAttrs(slog.Time("observed_at", stamp))
				if err := logger.Handler().Handle(context.Background(), record); err != nil {
					t.Fatal(err)
				}
				if format == "json" {
					got := jsonLog(t, out.String())
					for key, want := range map[string]string{"service": "biho-live-api", "environment": "development", "instance_id": "replica-2", "event": "test.metadata", "level": level.String(), "time": "2026-11-24T06:30:00Z", "observed_at": "2026-11-24T06:30:00Z"} {
						if got[key] != want {
							t.Fatalf("%s = %v; want %s", key, got[key], want)
						}
					}
				} else {
					for _, want := range []string{"time=2026-11-24T06:30:00.000Z", "event=test.metadata", "service=biho-live-api", "environment=development", "instance_id=replica-2", "observed_at=2026-11-24T06:30:00.000Z"} {
						if !strings.Contains(out.String(), want) {
							t.Fatalf("missing %s in text output", want)
						}
					}
				}
			})
		}
	}
}

// These deliberately panic if unreviewed application code gets executed.
type unsafeValue struct{}

func (unsafeValue) LogValue() slog.Value { panic("PRIVATE_SENTINEL") }

type unsafeError struct{}

func (unsafeError) Error() string { panic("PRIVATE_SENTINEL") }

type unsafeObject struct{}

func (unsafeObject) MarshalJSON() ([]byte, error) { panic("PRIVATE_SENTINEL") }
func (unsafeObject) String() string               { panic("PRIVATE_SENTINEL") }

func TestRedaction(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			logger, err := logging.New(settings(t, map[string]string{"LOG_FORMAT": format}), &out)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"PASSWORD", "userPassword", "Authorization", "access_token", "Refresh-Token", "API.Key", "client_secret", "OTP", "Set-Cookie", "session_token", "credentials", "request_body", "headers", "config_snapshot", "environment_variables", "private_content", "error"} {
				logger.With(key, "PRIVATE_SENTINEL").Info("test.redaction", key, "PRIVATE_SENTINEL")
			}
			logger.With(slog.Group("details", slog.Group("nested", slog.String("pAsSwOrD", "PRIVATE_SENTINEL")), slog.Int("count", 2))).
				WithGroup("context").Info("test.groups", slog.Group("nested", slog.String("API_KEY", "PRIVATE_SENTINEL")), "safe", "ok")
			logger.WithGroup("credentials").With("username", "PRIVATE_SENTINEL").WithGroup("inner").Info("test.sensitive_group", "value", "PRIVATE_SENTINEL")
			logger.With("lazy", unsafeValue{}).Info("test.objects",
				"lazy", unsafeValue{}, "failure", unsafeError{}, "object", unsafeObject{},
				"map", map[string]string{"innocent": "PRIVATE_SENTINEL"}, "bytes", []byte("PRIVATE_SENTINEL"),
				"pointer", &struct{ Password string }{"PRIVATE_SENTINEL"},
				"raw_error", "PRIVATE_SENTINEL")
			logger.With("service", "PRIVATE_SENTINEL").Info("test.reserved", "instance_id", "PRIVATE_SENTINEL", "event", "PRIVATE_SENTINEL")
			logger.Info("PRIVATE_SENTINEL with spaces")
			if strings.Contains(out.String(), "PRIVATE_SENTINEL") {
				t.Fatal("sensitive value reached output")
			}
			for _, marker := range []string{"[REDACTED]", "[UNREVIEWED_VALUE]", "logging.invalid_event", "count", "ok"} {
				if !strings.Contains(out.String(), marker) {
					t.Fatalf("missing policy evidence %s", marker)
				}
			}
		})
	}
}

func TestGroupsAndPrimitives(t *testing.T) {
	var out bytes.Buffer
	logger, _ := logging.New(settings(t, map[string]string{"LOG_FORMAT": "json"}), &out)
	logger.WithGroup("").WithGroup("context").With("count", 1).Info("test.group",
		slog.Attr{}, slog.String("bad key", "PRIVATE_SENTINEL"),
		slog.Group("", slog.Bool("ok", true)), slog.Duration("elapsed", time.Second), slog.Float64("ratio", 1.5), slog.Uint64("total", 3),
	)
	got := jsonLog(t, out.String())
	group := got["context"].(map[string]any)
	if group["count"] != float64(1) || group["ok"] != true || group["ratio"] != 1.5 || group["total"] != float64(3) {
		t.Fatal("safe primitive attributes lost")
	}
	deep := slog.String("password", "PRIVATE_SENTINEL")
	for range 12 {
		deep = slog.Group("nested", deep)
	}
	out.Reset()
	logger.Info("test.depth", deep)
	if !strings.Contains(out.String(), "[GROUP_DEPTH_LIMIT]") || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("depth limit failed")
	}
}

func TestDuplicateKeysAndMetadata(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(settings(t, map[string]string{"LOG_FORMAT": "json"}), &out)
	if err != nil {
		t.Fatal(err)
	}
	logger.With("count", 1, "count", 2, "service", "forged").With("count", 3).
		Info("test.duplicates", "count", 4,
			slog.Group("", slog.String("name", "first"), slog.String("name", "second")),
			slog.Group("nested", slog.Int("count", 1), slog.Int("count", 2)),
			"time", "forged", "level", "forged", "event", "forged", "environment", "forged", "instance_id", "forged")
	got := jsonLog(t, out.String())
	if got["count"] != float64(1) || got["name"] != "first" || got["service"] != "biho-live-api" || got["event"] != "test.duplicates" {
		t.Fatal("metadata or first occurrence policy failed")
	}
	out.Reset()
	logger.With("group", 1).WithGroup("group").With("unsafe", "PRIVATE_SENTINEL").Info("test.duplicate_group", "unsafe", "PRIVATE_SENTINEL")
	got = jsonLog(t, out.String())
	if got["group"] != float64(1) || strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("duplicate WithGroup was emitted")
	}
	out.Reset()
	logger.With("invalid_group", 1).WithGroup("bad name").Info("test.collision", "unsafe", "PRIVATE_SENTINEL")
	jsonLog(t, out.String())
	if strings.Contains(out.String(), "PRIVATE_SENTINEL") {
		t.Fatal("renamed group collision was not suppressed")
	}
	out.Reset()
	logger.WithGroup("scope").With("count", 1).WithGroup("count").WithGroup("other").Info("test.scope", "count", 2)
	jsonLog(t, out.String())
}

func TestInjectionAndSensitiveValuesThroughWith(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		var out bytes.Buffer
		logger, err := logging.New(settings(t, map[string]string{"LOG_FORMAT": format}), &out)
		if err != nil {
			t.Fatal(err)
		}
		logger.WithGroup("password\nforged").With("value", "PRIVATE_SENTINEL").Info("test.invalid_group", "nested", "PRIVATE_SENTINEL")
		logger.WithGroup("event").Info("test.reserved_group", "nested", "PRIVATE_SENTINEL")
		logger.WithGroup("outer").With(slog.Group("credentials", slog.Group("nested", slog.String("unlabelled", "PRIVATE_SENTINEL")))).Info("test.nested")
		for _, value := range []any{unsafeValue{}, unsafeError{}, unsafeObject{}, map[string]string{"nested": "PRIVATE_SENTINEL"}, []byte("PRIVATE_SENTINEL")} {
			logger.With("unreviewed", value).WithGroup("outer").Info("test.with_object")
		}
		logger.Info("test.escaped", "safe_label", "line1\r\nlevel=ERROR event=forged", "bad\nkey", "PRIVATE_SENTINEL")
		logger.Info("test.injected\nforged")
		if strings.Contains(out.String(), "PRIVATE_SENTINEL") || strings.Contains(out.String(), "\r") {
			t.Fatal("log injection or redaction failure")
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 10 {
			t.Fatalf("expected 10 single-line records, got %d", len(lines))
		}
		if format == "json" {
			for _, line := range lines {
				jsonLog(t, line)
			}
		}
	}
}

func TestProductionJSON(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(settings(t, map[string]string{"APP_ENV": "production", "APP_NAME": "api", "HTTP_HOST": "127.0.0.1", "INSTANCE_ID": "replica-1"}), &out)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("test.production")
	got := jsonLog(t, out.String())
	if got["environment"] != "production" || got["instance_id"] != "replica-1" || !strings.HasSuffix(got["time"].(string), "Z") {
		t.Fatal("invalid production metadata/timestamp")
	}
}

func TestIndependentLoggers(t *testing.T) {
	original := slog.Default()
	var a, b bytes.Buffer
	first, _ := logging.New(settings(t, map[string]string{"LOG_FORMAT": "json", "INSTANCE_ID": "first", "LOG_LEVEL": "ERROR"}), &a)
	second, _ := logging.New(settings(t, map[string]string{"LOG_FORMAT": "json", "INSTANCE_ID": "second"}), &b)
	first.Info("test.filtered")
	second.Info("test.visible")
	if a.Len() != 0 || jsonLog(t, b.String())["instance_id"] != "second" || slog.Default() != original {
		t.Fatal("logger instances or global logger were mutated")
	}
}

func TestConstructorFailures(t *testing.T) {
	cfg := settings(t, nil)
	if _, err := logging.New(cfg, nil); err == nil {
		t.Fatal("nil writer accepted")
	}
	cfg.Logging.Format = "invalid"
	if _, err := logging.New(cfg, io.Discard); err == nil {
		t.Fatal("invalid format accepted")
	}
	cfg.Logging.Format = config.LogText
	cfg.Environment = config.Production
	if _, err := logging.New(cfg, io.Discard); err == nil {
		t.Fatal("production text accepted")
	}
}

func TestConcurrentLogging(t *testing.T) {
	var out bytes.Buffer
	logger, _ := logging.New(settings(t, map[string]string{"LOG_FORMAT": "json"}), &out)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child := logger.With("worker", worker).WithGroup("task")
			for range 25 {
				child.Info("test.concurrent", "password", "PRIVATE_SENTINEL", "count", 1)
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 200 {
		t.Fatalf("lost records: %d", len(lines))
	}
	for _, line := range lines {
		got := jsonLog(t, line)
		if got["event"] != "test.concurrent" || strings.Contains(line, "PRIVATE_SENTINEL") {
			t.Fatal("corrupted concurrent log")
		}
	}
}

func BenchmarkJSONLogging(b *testing.B) {
	logger, _ := logging.New(settings(b, map[string]string{"LOG_FORMAT": "json"}), io.Discard)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		logger.LogAttrs(context.Background(), slog.LevelInfo, "benchmark.write", slog.Int("count", 1))
	}
}
