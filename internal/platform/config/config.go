// Package config loads and validates startup configuration without global state.
package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"time"
)

type Environment string

const (
	Development Environment = "development"
	Test        Environment = "test"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

// Config is a startup snapshot. Pass it explicitly to consumers; do not mutate it
// after constructing application dependencies.
type Config struct {
	Environment Environment
	AppName     string
	HTTP        HTTPConfig
	Logging     LoggingConfig
}

type LogFormat string

const (
	LogText LogFormat = "text"
	LogJSON LogFormat = "json"
)

type LoggingConfig struct {
	Level      slog.Level
	Format     LogFormat
	InstanceID string
}

// HTTPConfig defines transport and middleware limits; it starts no listener.
type HTTPConfig struct {
	Host                  string
	Port                  int
	ReadHeaderTimeout     time.Duration
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	IdleTimeout           time.Duration
	ShutdownTimeout       time.Duration
	MaxBodyBytes          int64
	MaxHeaderBytes        int
	RequestTimeout        time.Duration
	MaxConcurrentRequests int
}

// LookupEnv distinguishes an absent setting from an explicitly empty value.
// Production startup supplies os.LookupEnv; tests supply isolated functions.
type LookupEnv func(string) (string, bool)

// Load returns a complete validated snapshot or a zero Config and safe error.
// It does not read files, change the environment, or retain the lookup function.
func Load(lookup LookupEnv) (Config, error) {
	if lookup == nil {
		return Config{}, fmt.Errorf("configuration: environment lookup is required")
	}
	env, err := text(lookup, "APP_ENV", string(Development), false)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Environment: Environment(env)}
	switch cfg.Environment {
	case Development, Test, Staging, Production:
	default:
		return Config{}, invalid("APP_ENV", "must be development, test, staging or production")
	}
	required := cfg.Environment == Staging || cfg.Environment == Production
	cfg.AppName, err = text(lookup, "APP_NAME", "biho-live-api", required)
	if err != nil {
		return Config{}, err
	}
	if !validName(cfg.AppName) {
		return Config{}, invalid("APP_NAME", "must contain 1 to 64 ASCII letters, digits, dots, underscores or hyphens")
	}
	cfg.HTTP.Host, err = text(lookup, "HTTP_HOST", "127.0.0.1", required)
	if err != nil {
		return Config{}, err
	}
	// Bind only to literal addresses: no DNS lookup or ambiguous host:port input.
	if address, err := netip.ParseAddr(cfg.HTTP.Host); err != nil || address.Zone() != "" {
		return Config{}, invalid("HTTP_HOST", "must be an IPv4 or IPv6 address without brackets, zone or port")
	}
	port, err := integer(lookup, "HTTP_PORT", "8080", 1, 65535)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTP.Port = int(port)
	for _, setting := range []struct {
		key      string
		fallback string
		maximum  time.Duration
		target   *time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", "5s", 5 * time.Minute, &cfg.HTTP.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", "15s", 5 * time.Minute, &cfg.HTTP.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", "30s", 5 * time.Minute, &cfg.HTTP.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", "60s", 15 * time.Minute, &cfg.HTTP.IdleTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", "10s", 5 * time.Minute, &cfg.HTTP.ShutdownTimeout},
		{"HTTP_REQUEST_TIMEOUT", "10s", 5 * time.Minute, &cfg.HTTP.RequestTimeout},
	} {
		value, err := text(lookup, setting.key, setting.fallback, false)
		if err != nil {
			return Config{}, err
		}
		duration, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, invalid(setting.key, "must be a valid Go duration")
		}
		if duration < time.Millisecond || duration > setting.maximum {
			return Config{}, invalid(setting.key, "must be between 1ms and "+setting.maximum.String())
		}
		*setting.target = duration
	}
	if cfg.HTTP.ReadTimeout < cfg.HTTP.ReadHeaderTimeout {
		return Config{}, invalid("HTTP_READ_TIMEOUT", "must be at least HTTP_READ_HEADER_TIMEOUT")
	}
	cfg.HTTP.MaxBodyBytes, err = integer(lookup, "HTTP_MAX_BODY_BYTES", "1048576", 1024, 16*1024*1024)
	if err != nil {
		return Config{}, err
	}
	headers, err := integer(lookup, "HTTP_MAX_HEADER_BYTES", "32768", 1024, 1048576)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTP.MaxHeaderBytes = int(headers)
	concurrent, err := integer(lookup, "HTTP_MAX_CONCURRENT_REQUESTS", "256", 1, 4096)
	if err != nil {
		return Config{}, err
	}
	cfg.HTTP.MaxConcurrentRequests = int(concurrent)
	level, err := text(lookup, "LOG_LEVEL", "INFO", false)
	if err != nil {
		return Config{}, err
	}
	switch level {
	case "DEBUG":
		cfg.Logging.Level = slog.LevelDebug
	case "INFO":
		cfg.Logging.Level = slog.LevelInfo
	case "WARN":
		cfg.Logging.Level = slog.LevelWarn
	case "ERROR":
		cfg.Logging.Level = slog.LevelError
	default:
		return Config{}, invalid("LOG_LEVEL", "must be DEBUG, INFO, WARN or ERROR")
	}
	formatDefault := string(LogText)
	if required {
		formatDefault = string(LogJSON)
	}
	format, err := text(lookup, "LOG_FORMAT", formatDefault, false)
	if err != nil {
		return Config{}, err
	}
	cfg.Logging.Format = LogFormat(format)
	if cfg.Logging.Format != LogText && cfg.Logging.Format != LogJSON {
		return Config{}, invalid("LOG_FORMAT", "must be text or json")
	}
	if required && cfg.Logging.Format != LogJSON {
		return Config{}, invalid("LOG_FORMAT", "must be json in staging and production")
	}
	cfg.Logging.InstanceID, err = text(lookup, "INSTANCE_ID", "local", required)
	if err != nil {
		return Config{}, err
	}
	if !validName(cfg.Logging.InstanceID) {
		return Config{}, invalid("INSTANCE_ID", "must contain 1 to 64 ASCII letters, digits, dots, underscores or hyphens")
	}
	return cfg, nil
}

func text(lookup LookupEnv, key, fallback string, required bool) (string, error) {
	value, exists := lookup(key)
	if !exists {
		if required {
			return "", invalid(key, "is required")
		}
		return fallback, nil
	}
	if value == "" {
		return "", invalid(key, "must not be empty")
	}
	return value, nil
}

func integer(lookup LookupEnv, key, fallback string, minimum, maximum int64) (int64, error) {
	value, err := text(lookup, key, fallback, false)
	if err != nil {
		return 0, err
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, invalid(key, "must be a base-10 integer within the supported range")
	}
	if number < minimum || number > maximum {
		return 0, invalid(key, fmt.Sprintf("must be between %d and %d", minimum, maximum))
	}
	return number, nil
}

func validName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func invalid(key, reason string) error {
	// Never wrap parser errors: they can include the supplied value.
	return fmt.Errorf("configuration: %s: %s", key, reason)
}
