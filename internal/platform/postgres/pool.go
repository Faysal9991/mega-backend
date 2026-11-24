// Package postgres owns bounded PostgreSQL connectivity, not business rules.
package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres/dbsql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectionConfig is shared by the pool and direct migration connection. No
// session SET, temp objects, LISTEN, session locks or named statement cache.
func ConnectionConfig(cfg config.DatabaseConfig) (*pgx.ConnConfig, error) {
	// Revalidate exported configuration at this boundary, without reading globals.
	values := map[string]string{"DB_HOST": cfg.Host, "DB_PORT": strconv.Itoa(cfg.Port), "DB_NAME": cfg.Name, "DB_USER": cfg.User, "DB_PASSWORD": cfg.Password.Value(), "DB_SSLMODE": cfg.SSLMode, "DB_MAX_CONNS": strconv.Itoa(int(cfg.MaxConns)), "DB_CONNECT_TIMEOUT": cfg.ConnectTimeout.String()}
	if cfg.SSLRootCert != "" {
		values["DB_SSLROOTCERT"] = cfg.SSLRootCert
	}
	_, err := config.LoadDatabase(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, cfg.Environment)
	if err != nil {
		return nil, errors.New("postgres: invalid connection configuration")
	}
	address := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Name, User: url.UserPassword(cfg.User, cfg.Password.Value())}
	query := url.Values{"sslmode": {cfg.SSLMode}}
	if cfg.SSLRootCert != "" {
		query.Set("sslrootcert", cfg.SSLRootCert)
	}
	address.RawQuery = query.Encode()
	connection, err := pgx.ParseConfig(address.String())
	if err != nil {
		return nil, errors.New("postgres: invalid connection configuration")
	}
	security := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.Host}
	if cfg.SSLMode == "require" {
		security.InsecureSkipVerify = true // Explicit owner-authorized temporary dev/test policy; loader enforces environment.
	} else {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if cfg.SSLRootCert != "" {
			pem, err := os.ReadFile(cfg.SSLRootCert)
			if err != nil || !roots.AppendCertsFromPEM(pem) {
				return nil, errors.New("postgres: trusted CA configuration failed")
			}
		}
		security.RootCAs = roots
	}
	connection.TLSConfig = security
	connection.Fallbacks = nil // Never retry with plaintext or weaker TLS.
	connection.ConnectTimeout = cfg.ConnectTimeout
	connection.RuntimeParams = map[string]string{"application_name": "biho-live-api"}
	connection.StatementCacheCapacity = 0
	connection.DescriptionCacheCapacity = 0
	connection.DefaultQueryExecMode = pgx.QueryExecModeExec // Extended protocol, unnamed execution: transaction-pool compatible.
	return connection, nil
}

func Open(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	if cfg.User != cfg.Name+"_app" {
		return nil, errors.New("postgres: dedicated runtime role is required")
	}
	conn, err := ConnectionConfig(cfg)
	if err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(conn.ConnString())
	if err != nil {
		return nil, errors.New("postgres: pool configuration failed")
	}
	pc.ConnConfig = conn
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = 0
	pc.MaxConnLifetime = 30 * time.Minute
	pc.MaxConnLifetimeJitter = 5 * time.Minute
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.BeforeConnect = nil
	pc.AfterConnect = nil
	pc.BeforeAcquire = nil
	pc.AfterRelease = nil
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, errors.New("postgres: pool initialization failed")
	}
	startup, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(startup); err != nil {
		pool.Close()
		return nil, errors.New("postgres: connection initialization failed")
	}
	identity, err := dbsql.New(pool).ConnectionIdentity(startup)
	if err != nil || identity.DatabaseName != cfg.Name || identity.RoleName != cfg.User || !identity.SchemaReady {
		pool.Close()
		return nil, errors.New("postgres: connection identity verification failed")
	}
	return pool, nil
}
