// Package logging constructs synchronous, explicitly injected slog loggers.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

// New accepts a validated startup snapshot and a process-owned writer. It does
// not change slog.Default, read environment variables or start background work.
func New(cfg config.Config, output io.Writer) (*slog.Logger, error) {
	if output == nil {
		return nil, fmt.Errorf("logging: output writer is required")
	}
	options := &slog.HandlerOptions{Level: cfg.Logging.Level, ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
		if len(groups) == 0 && attr.Key == slog.MessageKey {
			attr.Key = "event"
		}
		return attr
	}}
	var handler slog.Handler
	switch cfg.Logging.Format {
	case config.LogText:
		if cfg.Environment == config.Staging || cfg.Environment == config.Production {
			return nil, fmt.Errorf("logging: JSON is required in staging and production")
		}
		handler = slog.NewTextHandler(output, options)
	case config.LogJSON:
		handler = slog.NewJSONHandler(output, options)
	default:
		return nil, fmt.Errorf("logging: unsupported format")
	}
	// Metadata is attached beneath the policy wrapper, preventing callers from
	// replacing it with duplicate structured keys through With or LogAttrs.
	handler = handler.WithAttrs([]slog.Attr{
		slog.String("service", cfg.AppName),
		slog.String("environment", string(cfg.Environment)),
		slog.String("instance_id", cfg.Logging.InstanceID),
	})
	return slog.New(&safeHandler{next: handler}), nil
}

type safeHandler struct {
	next       slog.Handler
	depth      int
	sensitive  bool
	seen       map[string]bool // Immutable keys already attached in the current object.
	suppressed bool            // A duplicate group cannot create another JSON object key.
}

func (h *safeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *safeHandler) Handle(ctx context.Context, record slog.Record) error {
	// Build a new record: Clone/Resolve would allow user LogValuer evaluation.
	event := record.Message
	if !validEvent(event) {
		event = "logging.invalid_event"
	}
	safe := slog.NewRecord(record.Time.UTC(), record.Level, event, record.PC)
	seen := cloneKeys(h.seen)
	record.Attrs(func(attr slog.Attr) bool {
		if !h.suppressed {
			safe.AddAttrs(cleanAttrs([]slog.Attr{attr}, h.depth, h.sensitive, seen)...)
		}
		return true
	})
	return h.next.Handle(ctx, safe)
}

func (h *safeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h.suppressed {
		return h
	}
	seen := cloneKeys(h.seen)
	clean := cleanAttrs(attrs, h.depth, h.sensitive, seen)
	return &safeHandler{next: h.next.WithAttrs(clean), depth: h.depth, sensitive: h.sensitive, seen: seen}
}

func (h *safeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	if h.suppressed || h.seen[name] {
		return &safeHandler{next: h.next, suppressed: true}
	}
	// An invalid or reserved group name cannot introduce duplicate root keys.
	sensitive := h.sensitive || sensitiveKey(name)
	if !validKey(name) || reserved(name) {
		name = "invalid_group"
		sensitive = true
	}
	if h.seen[name] {
		return &safeHandler{next: h.next, suppressed: true}
	}
	return &safeHandler{next: h.next.WithGroup(name), depth: h.depth + 1, sensitive: sensitive}
}

func cloneKeys(seen map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(seen))
	maps.Copy(copy, seen)
	return copy
}

// First occurrence wins within each JSON object, including inline groups and
// attributes attached by With. Distinct records never share a mutable key set.
func cleanAttrs(attrs []slog.Attr, depth int, force bool, seen map[string]bool) []slog.Attr {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		attr = sanitize(attr, depth, force)
		if attr.Equal(slog.Attr{}) {
			continue
		}
		if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
			// Children are already sanitized; deduplicate in their enclosing scope.
			for _, child := range attr.Value.Group() {
				if !seen[child.Key] {
					seen[child.Key] = true
					clean = append(clean, child)
				}
			}
			continue
		}
		if !seen[attr.Key] {
			seen[attr.Key] = true
			clean = append(clean, attr)
		}
	}
	return clean
}

func sanitize(attr slog.Attr, depth int, force bool) slog.Attr {
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	if attr.Key != "" && (!validKey(attr.Key) || reserved(attr.Key)) {
		return slog.Attr{}
	}
	if force || sensitiveKey(attr.Key) {
		return slog.String(safeKey(attr.Key), "[REDACTED]")
	}
	switch attr.Value.Kind() {
	case slog.KindGroup:
		if depth >= 8 {
			return slog.String(safeKey(attr.Key), "[GROUP_DEPTH_LIMIT]")
		}
		children := attr.Value.Group()
		clean := cleanAttrs(children, depth+1, false, make(map[string]bool))
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(clean...)}
	case slog.KindAny, slog.KindLogValuer:
		// Never invoke Error, String, MarshalJSON or LogValue on arbitrary values.
		// This also drops maps, structs, pointers, slices and byte/request bodies.
		return slog.String(safeKey(attr.Key), "[UNREVIEWED_VALUE]")
	case slog.KindTime:
		return slog.Time(safeKey(attr.Key), attr.Value.Time().UTC())
	default:
		attr.Key = safeKey(attr.Key)
		return attr
	}
}

func safeKey(key string) string {
	if key == "" {
		return "value"
	}
	return key
}

func sensitiveKey(key string) bool {
	key = strings.Map(func(c rune) rune {
		if c == '_' || c == '-' || c == '.' {
			return -1
		}
		if c >= 'A' && c <= 'Z' {
			return c + ('a' - 'A')
		}
		return c
	}, key)
	for _, word := range []string{"password", "passwd", "pwd", "authorization", "token", "apikey", "secret", "otp", "cookie", "credential", "body", "header", "config", "environ", "content", "error"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return false
}

func reserved(key string) bool {
	switch strings.ToLower(key) {
	case "time", "level", "msg", "event", "service", "environment", "instance_id":
		return true
	}
	return false
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 64 {
		return false
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func validEvent(event string) bool {
	if !validKey(event) || event[0] < 'a' || event[0] > 'z' {
		return false
	}
	for _, c := range event {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
