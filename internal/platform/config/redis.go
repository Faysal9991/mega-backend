package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type RedisConfig struct {
	Environment                                                                           Environment
	Enabled                                                                               bool
	URL                                                                                   Secret
	TLSInsecureSkipVerify                                                                 bool
	MaxConns                                                                              int
	DialTimeout, PoolTimeout, ReadTimeout, WriteTimeout, StartupTimeout, OperationTimeout time.Duration
}

func (RedisConfig) String() string   { return "[redis configuration redacted]" }
func (RedisConfig) GoString() string { return "[redis configuration redacted]" }

// RedisEndpoint accepts no URL options: none can override the explicitly
// validated TLS, protocol, retry, database or resource policy at construction.
func RedisEndpoint(raw string) (*url.URL, error) {
	if len(raw) > 8192 || !utf8.ValidString(raw) {
		return nil, invalid("REDIS_URL", "must be a bounded rediss URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "rediss" || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#") || u.Hostname() == "" || u.User == nil {
		return nil, invalid("REDIS_URL", "must be rediss with credentials, explicit port and no query/fragment")
	}
	password, ok := u.User.Password()
	if !ok || len(password) < 1 || len(password) > 4096 || strings.ContainsRune(password, 0) || len(u.User.Username()) > 128 || strings.ContainsAny(u.User.Username(), " \t\r\n\x00") {
		return nil, invalid("REDIS_URL", "must include a bounded ACL username/password")
	}
	if u.User.Username() == "" {
		u.User = url.UserPassword("default", password)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, invalid("REDIS_URL", "must include a valid explicit port")
	}
	host := u.Hostname()
	if strings.ContainsAny(host, "/\\@?# \t\r\n\x00") || len(host) > 253 || strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return nil, invalid("REDIS_URL", "has invalid host syntax")
	}
	if u.Path != "" && u.Path != "/" && u.Path != "/0" || u.RawPath != "" {
		return nil, invalid("REDIS_URL", "supports database 0 only")
	}
	return u, nil
}
func LoadRedis(lookup LookupEnv, environment Environment) (RedisConfig, error) {
	fail := func(key, reason string) (RedisConfig, error) { return RedisConfig{}, invalid(key, reason) }
	if lookup == nil {
		return fail("REDIS_ENABLED", "environment lookup required")
	}
	if environment != Development && environment != Test && environment != Staging && environment != Production {
		return fail("APP_ENV", "unsupported Redis environment")
	}
	cfg := RedisConfig{Environment: environment}
	for _, setting := range []struct {
		key    string
		target *bool
	}{{"REDIS_ENABLED", &cfg.Enabled}, {"REDIS_TLS_INSECURE_SKIP_VERIFY", &cfg.TLSInsecureSkipVerify}} {
		v, err := text(lookup, setting.key, "false", false)
		if err != nil {
			return RedisConfig{}, err
		}
		if v != "true" && v != "false" {
			return fail(setting.key, "must be true or false")
		}
		*setting.target = v == "true"
	}
	if cfg.TLSInsecureSkipVerify && environment != Development && environment != Test {
		return fail("REDIS_TLS_INSECURE_SKIP_VERIFY", "is forbidden in staging/production")
	}
	if !cfg.Enabled {
		return cfg, nil
	} // Do not even load credentials or network settings.
	raw, err := text(lookup, "REDIS_URL", "", true)
	if err != nil {
		return RedisConfig{}, err
	}
	if _, err := RedisEndpoint(raw); err != nil {
		return RedisConfig{}, err
	}
	cfg.URL = Secret(raw)
	maximum, err := integer(lookup, "REDIS_MAX_CONNS", "8", 1, 32)
	if err != nil {
		return RedisConfig{}, err
	}
	cfg.MaxConns = int(maximum)
	for _, setting := range []struct {
		key, fallback string
		target        *time.Duration
	}{
		{"REDIS_DIAL_TIMEOUT", "2s", &cfg.DialTimeout}, {"REDIS_POOL_TIMEOUT", "500ms", &cfg.PoolTimeout}, {"REDIS_READ_TIMEOUT", "1s", &cfg.ReadTimeout}, {"REDIS_WRITE_TIMEOUT", "1s", &cfg.WriteTimeout}, {"REDIS_STARTUP_TIMEOUT", "3s", &cfg.StartupTimeout}, {"REDIS_OPERATION_TIMEOUT", "2s", &cfg.OperationTimeout},
	} {
		value, err := text(lookup, setting.key, setting.fallback, false)
		if err != nil {
			return RedisConfig{}, err
		}
		d, err := time.ParseDuration(value)
		if err != nil || d < time.Millisecond || d > 10*time.Second {
			return fail(setting.key, "must be between 1ms and 10s")
		}
		*setting.target = d
	}
	return cfg, nil
}
