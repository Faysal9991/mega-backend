package config

import (
	"encoding/json"
	"net"
	"regexp"
	"strings"
	"time"
)

// Secret must be explicitly revealed at a connection boundary, never logged.
type Secret string

func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal("[REDACTED]") }
func (s Secret) Value() string              { return string(s) }

type DatabaseConfig struct {
	Environment    Environment
	Host           string
	Port           int
	Name           string
	User           string
	Password       Secret
	SSLMode        string
	SSLRootCert    string
	MaxConns       int32
	ConnectTimeout time.Duration
}

func (DatabaseConfig) String() string   { return "[database configuration redacted]" }
func (DatabaseConfig) GoString() string { return "[database configuration redacted]" }

// LoadDatabase does no I/O and accepts no insecure/plaintext connection mode.
// require is an explicit temporary dev/test exception; staging/production verify
// both server identity and certificate trust. No secret or host has a default.
func LoadDatabase(lookup LookupEnv, environment Environment) (DatabaseConfig, error) {
	cfg := DatabaseConfig{Environment: environment}
	if lookup == nil {
		return cfg, invalid("DB_HOST", "environment lookup is required")
	}
	for _, setting := range []struct {
		key    string
		target *string
	}{
		{"DB_HOST", &cfg.Host}, {"DB_NAME", &cfg.Name}, {"DB_USER", &cfg.User}, {"DB_SSLMODE", &cfg.SSLMode},
	} {
		v, err := text(lookup, setting.key, "", true)
		if err != nil {
			return DatabaseConfig{}, err
		}
		*setting.target = v
	}
	if strings.TrimSpace(cfg.Host) != cfg.Host || strings.ContainsAny(cfg.Host, "/\\\x00 \t\r\n") || len(cfg.Host) > 253 {
		return DatabaseConfig{}, invalid("DB_HOST", "must be a host without port, path or whitespace")
	}
	if strings.Contains(cfg.Host, ":") && net.ParseIP(cfg.Host) == nil {
		return DatabaseConfig{}, invalid("DB_HOST", "must not include a port")
	}
	validIdentifier := regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	if !validIdentifier.MatchString(cfg.Name) || cfg.Name == "postgres" || strings.HasPrefix(cfg.Name, "template") {
		return DatabaseConfig{}, invalid("DB_NAME", "must identify an isolated application database")
	}
	if !validIdentifier.MatchString(cfg.User) || cfg.User == "postgres" {
		return DatabaseConfig{}, invalid("DB_USER", "must identify a dedicated application or migration role")
	}
	if environment != Development && environment != Test && environment != Staging && environment != Production {
		return DatabaseConfig{}, invalid("APP_ENV", "unsupported database environment")
	}
	if environment == Development && cfg.Name != "biho_live_dev" || environment == Test && cfg.Name != "biho_live_test" || (environment == Staging || environment == Production) && (cfg.Name == "biho_live_dev" || cfg.Name == "biho_live_test") {
		return DatabaseConfig{}, invalid("DB_NAME", "must match the isolated target for APP_ENV")
	}
	if cfg.SSLMode != "verify-full" && (cfg.SSLMode != "require" || environment != Development && environment != Test) {
		return DatabaseConfig{}, invalid("DB_SSLMODE", "must be verify-full; require is allowed only in development/test")
	}
	password, err := text(lookup, "DB_PASSWORD", "", true)
	if err != nil {
		return DatabaseConfig{}, err
	}
	if len(password) > 4096 || strings.ContainsRune(password, 0) {
		return DatabaseConfig{}, invalid("DB_PASSWORD", "must be nonempty, bounded and contain no NUL")
	}
	cfg.Password = Secret(password)
	port, err := integer(lookup, "DB_PORT", "5432", 1, 65535)
	if err != nil {
		return DatabaseConfig{}, err
	}
	cfg.Port = int(port)
	maximum, err := integer(lookup, "DB_MAX_CONNS", "8", 1, 32)
	if err != nil {
		return DatabaseConfig{}, err
	}
	cfg.MaxConns = int32(maximum)
	timeout, err := text(lookup, "DB_CONNECT_TIMEOUT", "5s", false)
	if err != nil {
		return DatabaseConfig{}, err
	}
	cfg.ConnectTimeout, err = time.ParseDuration(timeout)
	if err != nil || cfg.ConnectTimeout < 100*time.Millisecond || cfg.ConnectTimeout > 30*time.Second {
		return DatabaseConfig{}, invalid("DB_CONNECT_TIMEOUT", "must be between 100ms and 30s")
	}
	if root, exists := lookup("DB_SSLROOTCERT"); exists {
		if root == "" || strings.ContainsRune(root, 0) {
			return DatabaseConfig{}, invalid("DB_SSLROOTCERT", "must be a nonempty CA file path")
		}
		cfg.SSLRootCert = root
	}
	return cfg, nil
}

// MigrationLookup requires its own explicit direct host/port/name/credentials.
// It never falls back to runtime (possibly future PgBouncer) credentials/routes.
func MigrationLookup(lookup LookupEnv) LookupEnv {
	return func(key string) (string, bool) { return lookup("MIGRATION_" + key) }
}
