package config_test

import (
	"encoding/json"
	"fmt"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"strings"
	"testing"
	"time"
)

func redisValues() map[string]string {
	return map[string]string{"REDIS_ENABLED": "true", "REDIS_URL": "rediss://default:PRIVATE_SENTINEL@127.0.0.1:7200/0"}
}
func TestRedisDisabledAndConfiguration(t *testing.T) {
	calls := map[string]int{}
	cfg, err := config.LoadRedis(func(key string) (string, bool) { calls[key]++; return "", false }, config.Development)
	if err != nil || cfg.Enabled || len(calls) != 2 {
		t.Fatal("disabled Redis loaded credentials or resources")
	}
	v := redisValues()
	cfg, err = config.LoadRedis(lookup(v), config.Test)
	if err != nil || cfg.MaxConns != 8 || cfg.PoolTimeout != 500*time.Millisecond || cfg.OperationTimeout != 2*time.Second || cfg.TLSInsecureSkipVerify {
		t.Fatal("unsafe Redis defaults")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v %s", cfg, cfg, cfg, string(encoded)), "PRIVATE_SENTINEL") {
		t.Fatal("Redis configuration leaked secret")
	}
	for _, env := range []config.Environment{config.Development, config.Test, config.Staging, config.Production} {
		v = redisValues()
		v["REDIS_TLS_INSECURE_SKIP_VERIFY"] = "true"
		_, err := config.LoadRedis(lookup(v), env)
		if (err != nil) != (env == config.Staging || env == config.Production) {
			t.Fatal("TLS verification bypass policy")
		}
	}
	for _, tt := range []struct{ key, value string }{
		{"REDIS_ENABLED", "yes"}, {"REDIS_ENABLED", ""}, {"REDIS_TLS_INSECURE_SKIP_VERIFY", "1"},
		{"REDIS_URL", "redis://default:PRIVATE_SENTINEL@127.0.0.1:7200/0"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost/0"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:0/0"},
		{"REDIS_URL", "rediss://localhost:7200/0"}, {"REDIS_URL", "rediss://default:@localhost:7200/0"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:7200/1"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:7200/0?max_retries=99"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:7200/0?"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:7200/0#PRIVATE_SENTINEL"},
		{"REDIS_URL", "rediss://default:PRIVATE_SENTINEL@localhost:7200/%30"},
		{"REDIS_URL", "rediss://default:%00@localhost:7200/0"}, {"REDIS_URL", "%PRIVATE_SENTINEL"},
		{"REDIS_MAX_CONNS", "0"}, {"REDIS_MAX_CONNS", "33"}, {"REDIS_MAX_CONNS", "PRIVATE_SENTINEL"},
	} {
		v := redisValues()
		v[tt.key] = tt.value
		cfg, err := config.LoadRedis(lookup(v), config.Test)
		if err == nil || cfg != (config.RedisConfig{}) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid Redis configuration accepted or exposed", tt.key)
		}
	}
	for _, key := range []string{"REDIS_DIAL_TIMEOUT", "REDIS_POOL_TIMEOUT", "REDIS_READ_TIMEOUT", "REDIS_WRITE_TIMEOUT", "REDIS_STARTUP_TIMEOUT", "REDIS_OPERATION_TIMEOUT"} {
		for _, value := range []string{"0s", "-1s", "11s", "PRIVATE_SENTINEL", ""} {
			v := redisValues()
			v[key] = value
			if _, err := config.LoadRedis(lookup(v), config.Test); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("unbounded timeout accepted or leaked", key)
			}
		}
	}
	if _, err := config.LoadRedis(nil, config.Test); err == nil {
		t.Fatal("nil lookup")
	}
	if _, err := config.LoadRedis(lookup(redisValues()), "unknown"); err == nil {
		t.Fatal("invalid environment")
	}
	v = redisValues()
	delete(v, "REDIS_URL")
	if _, err := config.LoadRedis(lookup(v), config.Test); err == nil {
		t.Fatal("enabled missing URL")
	}
}

func TestRedisDefaultACL(t *testing.T) {
	endpoint, err := config.RedisEndpoint("rediss://:synthetic-password@localhost:7200/0")
	if err != nil || endpoint.User.Username() != "default" {
		t.Fatal("implicit default ACL not normalized")
	}
	v := map[string]string{"REDIS_ENABLED": "false", "REDIS_TLS_INSECURE_SKIP_VERIFY": "true"}
	if _, err := config.LoadRedis(lookup(v), config.Production); err == nil {
		t.Fatal("production accepted dormant TLS bypass")
	}
}
