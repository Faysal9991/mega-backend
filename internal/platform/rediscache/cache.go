// Package rediscache provides optional disposable cache; never authoritative state.
package rediscache

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	redis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

const (
	MaxKeyBytes   = 128
	MaxValueBytes = 64 << 10
	MaxTTL        = 24 * time.Hour
)

type Failure string

func (e Failure) Error() string { return string(e) }

const (
	ErrDisabled      Failure = "CACHE_DISABLED"
	ErrUnavailable   Failure = "CACHE_UNAVAILABLE"
	ErrInvalidKey    Failure = "CACHE_INVALID_KEY"
	ErrInvalidTTL    Failure = "CACHE_INVALID_TTL"
	ErrValueTooLarge Failure = "CACHE_VALUE_TOO_LARGE"
	ErrCommand       Failure = "CACHE_COMMAND_FAILED"
)

type failure struct {
	code  Failure
	cause error
}

func (e *failure) Error() string        { return e.code.Error() }
func (e *failure) GoString() string     { return e.Error() }
func (e *failure) Unwrap() error        { return e.cause }
func (e *failure) Is(target error) bool { return target == e.code }

// go-redis v9.23 offers only a process-scoped logger. Install the same immutable
// sink before construction: its default pool logs can expose raw server/errors.
// Application diagnostics below use the injected safe logger and static fields.
type quietDriverLogger struct{}

func (quietDriverLogger) Printf(context.Context, string, ...interface{}) {}

type Cache struct {
	cfg       config.RedisConfig
	client    *redis.Client // Private: no Pub/Sub, pipeline, scripts or arbitrary commands.
	prefix    string
	slots     chan struct{}
	mu        sync.Mutex
	closed    bool
	workers   sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}
type result struct {
	value string
	err   error
}

func validated(cfg config.RedisConfig) (config.RedisConfig, error) {
	values := map[string]string{"REDIS_ENABLED": strconv.FormatBool(cfg.Enabled), "REDIS_TLS_INSECURE_SKIP_VERIFY": strconv.FormatBool(cfg.TLSInsecureSkipVerify)}
	if cfg.Enabled {
		values["REDIS_URL"] = cfg.URL.Value()
		values["REDIS_MAX_CONNS"] = strconv.Itoa(cfg.MaxConns)
		for k, d := range map[string]time.Duration{"REDIS_DIAL_TIMEOUT": cfg.DialTimeout, "REDIS_POOL_TIMEOUT": cfg.PoolTimeout, "REDIS_READ_TIMEOUT": cfg.ReadTimeout, "REDIS_WRITE_TIMEOUT": cfg.WriteTimeout, "REDIS_STARTUP_TIMEOUT": cfg.StartupTimeout, "REDIS_OPERATION_TIMEOUT": cfg.OperationTimeout} {
			values[k] = d.String()
		}
	}
	return config.LoadRedis(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, cfg.Environment)
}

// Open is fatal only for invalid configuration/programmer dependencies. PING
// failure emits one safe diagnostic and retains the client for later recovery.
func Open(ctx context.Context, cfg config.RedisConfig, logger *slog.Logger) (*Cache, error) {
	checked, err := validated(cfg)
	if err != nil {
		return nil, err
	}
	cfg = checked
	if logger == nil {
		return nil, errors.New("cache: logger required")
	}
	c := &Cache{cfg: cfg, prefix: "biho_live:" + string(cfg.Environment) + ":cache:v1:"}
	if !cfg.Enabled {
		return c, nil
	}
	redis.SetLogger(quietDriverLogger{})
	c.slots = make(chan struct{}, cfg.MaxConns)
	opts, err := options(cfg)
	if err != nil {
		return nil, err
	}
	c.client = redis.NewClient(opts)
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	startupResult, startupErr := c.execute(startup, func(ctx context.Context) result { return result{err: c.client.Ping(ctx).Err()} })
	if startupErr != nil || startupResult.err != nil {
		logger.WarnContext(ctx, "cache.startup_unavailable", slog.String("failure_code", "CACHE_UNAVAILABLE"))
	}
	return c, nil
}

func options(cfg config.RedisConfig) (*redis.Options, error) {
	u, err := config.RedisEndpoint(cfg.URL.Value())
	if err != nil {
		return nil, err
	}
	password, _ := u.User.Password()
	security := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), InsecureSkipVerify: cfg.TLSInsecureSkipVerify}
	// A shared dial gate caps *all* physical sockets, even auxiliary pools. Return
	// slots exactly once on Close, and do not queue new dialing work when full.
	connections := make(chan struct{}, cfg.MaxConns)
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		select {
		case connections <- struct{}{}:
		default:
			return nil, ErrUnavailable
		}
		dialing, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		defer cancel()
		d := tls.Dialer{NetDialer: &net.Dialer{Timeout: cfg.DialTimeout, FallbackDelay: -1}, Config: security}
		conn, err := d.DialContext(dialing, network, address)
		if err != nil {
			<-connections
			return nil, ErrUnavailable
		}
		return &limitedConn{Conn: conn, release: func() { <-connections }}, nil
	}
	return &redis.Options{
		Addr: net.JoinHostPort(u.Hostname(), u.Port()), Username: u.User.Username(), Password: password, DB: 0,
		TLSConfig: security, Dialer: dial, Protocol: 2, DisableIdentity: true,
		PoolSize: cfg.MaxConns, MaxActiveConns: cfg.MaxConns, MinIdleConns: 0, MaxIdleConns: cfg.MaxConns, MaxConcurrentDials: 1,
		PipelinePoolSize: -1, MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
		MaxRetries: -1, DialerRetries: 1, ContextTimeoutEnabled: true,
		DialTimeout: cfg.DialTimeout, PoolTimeout: cfg.PoolTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout,
		ConnMaxIdleTime: 5 * time.Minute, ConnMaxLifetime: 30 * time.Minute,
		ReadBufferSize: 4096, WriteBufferSize: 4096,
	}, nil
}

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); c.release() })
	return err
}

// Bounded workers let callers observe cancellation immediately even though the
// pinned client only applies context *deadlines* to established socket I/O.
// At most MaxConns workers remain until their bounded socket/operation completes.
func (c *Cache) execute(ctx context.Context, fn func(context.Context) result) (result, error) {
	if !c.cfg.Enabled {
		return result{}, ErrDisabled
	}
	if ctx.Err() != nil {
		return result{}, &failure{ErrUnavailable, ctx.Err()}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return result{}, ErrUnavailable
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.mu.Unlock()
		return result{}, ErrUnavailable
	}
	c.workers.Add(1)
	c.mu.Unlock()
	done := make(chan result, 1)
	go func() {
		defer c.workers.Done()
		var r result
		defer func() {
			if recover() != nil {
				r = result{err: ErrCommand}
			}
			// Publish completion only after releasing admission. Otherwise a
			// sequential caller can spuriously exhaust a single-slot cache.
			<-c.slots
			done <- r
		}()
		r = fn(ctx)
	}()
	select {
	case r := <-done:
		if ctx.Err() != nil {
			return result{}, &failure{ErrUnavailable, ctx.Err()}
		}
		return r, nil
	case <-ctx.Done():
		return result{}, &failure{ErrUnavailable, ctx.Err()}
	}
}
func (c *Cache) key(key string) (string, error) {
	if len(key) < 1 || len(key) > MaxKeyBytes {
		return "", ErrInvalidKey
	}
	for _, b := range []byte(key) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == ':' || b == '_' || b == '-') {
			return "", ErrInvalidKey
		}
	}
	return c.prefix + key, nil
}
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrCommand) {
		return ErrCommand
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &failure{ErrUnavailable, err}
	}
	var server redis.Error
	if errors.As(err, &server) {
		for _, prefix := range []string{"WRONGPASS", "NOAUTH", "NOPERM", "LOADING", "CLUSTERDOWN", "MASTERDOWN", "READONLY", "OOM", "BUSY"} {
			if redis.HasErrorPrefix(err, prefix) {
				return ErrUnavailable
			}
		}
		return ErrCommand
	}
	return ErrUnavailable
}
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	wire, err := c.key(key)
	if err != nil {
		return nil, false, err
	}
	operation, cancel := context.WithTimeout(ctx, c.cfg.OperationTimeout)
	defer cancel()
	r, err := c.execute(operation, func(ctx context.Context) result { v, e := c.client.Get(ctx, wire).Result(); return result{v, e} })
	if err != nil {
		return nil, false, err
	}
	if errors.Is(r.err, redis.Nil) {
		return nil, false, nil
	}
	if r.err != nil {
		return nil, false, classify(r.err)
	}
	if len(r.value) > MaxValueBytes {
		return nil, false, ErrValueTooLarge
	}
	return []byte(r.value), true, nil
}
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	wire, err := c.key(key)
	if err != nil {
		return err
	}
	if len(value) > MaxValueBytes {
		return ErrValueTooLarge
	}
	if ttl < time.Millisecond || ttl > MaxTTL || ttl%time.Millisecond != 0 {
		return ErrInvalidTTL
	}
	data := string(value) // Own the value before a canceled caller can reuse its buffer.
	operation, cancel := context.WithTimeout(ctx, c.cfg.OperationTimeout)
	defer cancel()
	r, err := c.execute(operation, func(ctx context.Context) result { return result{err: c.client.Set(ctx, wire, data, ttl).Err()} })
	if err != nil {
		return err
	}
	return classify(r.err)
}
func (c *Cache) Delete(ctx context.Context, key string) error {
	wire, err := c.key(key)
	if err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, c.cfg.OperationTimeout)
	defer cancel()
	r, err := c.execute(operation, func(ctx context.Context) result { return result{err: c.client.Del(ctx, wire).Err()} })
	if err != nil {
		return err
	}
	return classify(r.err)
}
func (c *Cache) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		if c.client != nil {
			if err := c.client.Close(); err != nil {
				c.closeErr = ErrUnavailable
			}
		}
		c.workers.Wait()
	})
	return c.closeErr
}

var _ io.Closer = (*Cache)(nil)
