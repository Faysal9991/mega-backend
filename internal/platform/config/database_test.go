package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func databaseValues() map[string]string {
	return map[string]string{"DB_HOST": "db.example.invalid", "DB_NAME": "biho_live_dev", "DB_USER": "biho_live_dev_app", "DB_PASSWORD": "PRIVATE_SENTINEL", "DB_SSLMODE": "require"}
}
func TestDatabaseConfiguration(t *testing.T) {
	v := databaseValues()
	cfg, err := config.LoadDatabase(lookup(v), config.Development)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 8 || cfg.Port != 5432 || cfg.ConnectTimeout != 5*time.Second || cfg.Password.Value() != "PRIVATE_SENTINEL" {
		t.Fatal("database defaults")
	}
	for _, formatted := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprintf("%v", cfg.Password), fmt.Sprintf("%#v", cfg.Password)} {
		if strings.Contains(formatted, "PRIVATE_SENTINEL") {
			t.Fatal("configuration formatting leaked secret")
		}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(encoded), "PRIVATE_SENTINEL") {
		t.Fatal("JSON leaked secret")
	}
	for _, env := range []config.Environment{config.Development, config.Test, config.Staging, config.Production} {
		switch env {
		case config.Development:
			v["DB_NAME"] = "biho_live_dev"
		case config.Test:
			v["DB_NAME"] = "biho_live_test"
		default:
			v["DB_NAME"] = "biho_live_prod"
		}
		v["DB_USER"] = v["DB_NAME"] + "_app"
		v["DB_SSLMODE"] = "verify-full"
		if _, err := config.LoadDatabase(lookup(v), env); err != nil {
			t.Fatal(err)
		}
		v["DB_SSLMODE"] = "require"
		_, err := config.LoadDatabase(lookup(v), env)
		if (err != nil) != (env == config.Staging || env == config.Production) {
			t.Fatal("TLS environment policy")
		}
	}
	for _, tt := range []struct{ key, value string }{
		{"DB_HOST", "bad/PRIVATE_SENTINEL"}, {"DB_HOST", "localhost:5432"}, {"DB_HOST", " "}, {"DB_HOST", strings.Repeat("a", 254)},
		{"DB_NAME", "postgres"}, {"DB_NAME", "template1"}, {"DB_NAME", "bad;PRIVATE_SENTINEL"}, {"DB_USER", "postgres"}, {"DB_USER", "bad-role"},
		{"DB_SSLMODE", "disable"}, {"DB_SSLMODE", "prefer"}, {"DB_SSLMODE", "allow"}, {"DB_SSLMODE", "verify-ca"},
		{"DB_PASSWORD", strings.Repeat("a", 4097)}, {"DB_PASSWORD", "PRIVATE_SENTINEL\x00"}, {"DB_PORT", "0"}, {"DB_PORT", "65536"}, {"DB_PORT", "PRIVATE_SENTINEL"},
		{"DB_MAX_CONNS", "0"}, {"DB_MAX_CONNS", "33"}, {"DB_CONNECT_TIMEOUT", "0s"}, {"DB_CONNECT_TIMEOUT", "31s"}, {"DB_CONNECT_TIMEOUT", "PRIVATE_SENTINEL"}, {"DB_SSLROOTCERT", ""},
	} {
		t.Run(tt.key+tt.value[:min(len(tt.value), 10)], func(t *testing.T) {
			v := databaseValues()
			v[tt.key] = tt.value
			cfg, err := config.LoadDatabase(lookup(v), config.Development)
			if err == nil || cfg != (config.DatabaseConfig{}) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("invalid database setting exposed or accepted")
			}
		})
	}
	for _, key := range []string{"DB_HOST", "DB_NAME", "DB_USER", "DB_PASSWORD", "DB_SSLMODE"} {
		for _, missing := range []bool{false, true} {
			v := databaseValues()
			if missing {
				delete(v, key)
			} else {
				v[key] = ""
			}
			if _, err := config.LoadDatabase(lookup(v), config.Development); err == nil {
				t.Fatal("missing/empty database setting", key)
			}
		}
	}
	if _, err := config.LoadDatabase(nil, config.Test); err == nil {
		t.Fatal("nil lookup")
	}
	if _, err := config.LoadDatabase(lookup(databaseValues()), "unknown"); err == nil {
		t.Fatal("invalid environment")
	}
	v = databaseValues()
	v["DB_NAME"] = "biho_live_test"
	v["DB_USER"] = "biho_live_test_app"
	v["DB_HOST"] = "::1"
	v["DB_PORT"] = "65535"
	v["DB_MAX_CONNS"] = "32"
	v["DB_CONNECT_TIMEOUT"] = "100ms"
	v["DB_SSLROOTCERT"] = "test-ca.pem"
	if _, err := config.LoadDatabase(lookup(v), config.Test); err != nil {
		t.Fatal(err)
	}
	var calls = map[string]int{}
	if _, err := config.LoadDatabase(func(key string) (string, bool) { calls[key]++; return lookup(databaseValues())(key) }, config.Development); err != nil {
		t.Fatal(err)
	}
	for _, count := range calls {
		if count != 1 {
			t.Fatal("duplicate database setting read")
		}
	}
}
func TestDirectMigrationSettings(t *testing.T) {
	v := databaseValues()
	wrapped := config.MigrationLookup(lookup(v))
	if _, err := config.LoadDatabase(wrapped, config.Test); err == nil {
		t.Fatal("migration fell back to runtime settings")
	}
	for key, value := range databaseValues() {
		v["MIGRATION_"+key] = value
	}
	v["MIGRATION_DB_USER"] = "biho_live_dev_migrator"
	if _, err := config.LoadDatabase(config.MigrationLookup(lookup(v)), config.Development); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseEnvironmentIsolation(t *testing.T) {
	for _, tt := range []struct {
		env  config.Environment
		name string
	}{{config.Development, "biho_live_test"}, {config.Test, "biho_live_dev"}, {config.Production, "biho_live_test"}, {config.Staging, "biho_live_dev"}} {
		v := databaseValues()
		v["DB_NAME"] = tt.name
		v["DB_SSLMODE"] = "verify-full"
		if _, err := config.LoadDatabase(lookup(v), tt.env); err == nil {
			t.Fatal("database isolation bypassed")
		}
	}
}
