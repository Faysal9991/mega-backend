package rediscache

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/redis/go-redis/v9/maintnotifications"
)

type fakeEntry struct {
	value   string
	expires int64
}
type fakeRedis struct {
	listener                                 net.Listener
	mu                                       sync.Mutex
	values                                   map[string]fakeEntry
	conns                                    map[net.Conn]bool
	wg                                       sync.WaitGroup
	clock                                    atomic.Int64
	badAuth, down, wrongType, block, loading atomic.Bool
	getCount, acceptCount                    atomic.Int64
	entered                                  chan struct{}
	release                                  chan struct{}
}

func newFakeRedis(t *testing.T) *fakeRedis {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeRedis{listener: tls.NewListener(raw, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}), values: map[string]fakeEntry{}, conns: map[net.Conn]bool{}, entered: make(chan struct{}, 64), release: make(chan struct{})}
	s.clock.Store(time.Now().UnixNano())
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				return
			}
			s.acceptCount.Add(1)
			s.mu.Lock()
			s.conns[conn] = true
			s.mu.Unlock()
			s.wg.Add(1)
			go s.serve(conn)
		}
	}()
	t.Cleanup(func() {
		_ = s.listener.Close()
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.mu.Unlock()
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		s.wg.Wait()
	})
	return s
}
func readCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) < 3 || line[0] != '*' {
		return nil, io.ErrUnexpectedEOF
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil || count < 1 || count > 16 {
		return nil, io.ErrUnexpectedEOF
	}
	args := make([]string, count)
	for i := range args {
		line, err = r.ReadString('\n')
		if err != nil || len(line) < 3 || line[0] != '$' {
			return nil, io.ErrUnexpectedEOF
		}
		n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
		if err != nil || n < 0 || n > 128<<10 {
			return nil, io.ErrUnexpectedEOF
		}
		b := make([]byte, n+2)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
		args[i] = string(b[:n])
	}
	return args, nil
}
func (s *fakeRedis) serve(conn net.Conn) {
	defer s.wg.Done()
	defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.conns, conn); s.mu.Unlock() }()
	reader := bufio.NewReader(conn)
	for {
		args, err := readCommand(reader)
		if err != nil {
			return
		}
		if s.down.Load() {
			return
		}
		var reply string
		switch strings.ToLower(args[0]) {
		case "hello":
			reply = "-ERR unknown command\r\n"
		case "auth":
			if s.badAuth.Load() {
				reply = "-WRONGPASS PRIVATE_SENTINEL\r\n"
			} else {
				reply = "+OK\r\n"
			}
		case "ping":
			reply = "+PONG\r\n"
		case "get":
			s.getCount.Add(1)
			if s.loading.Load() {
				reply = "-LOADING PRIVATE_SENTINEL\r\n"
				break
			}
			if s.block.Load() {
				s.entered <- struct{}{}
				<-s.release
			}
			if s.wrongType.Load() {
				reply = "-WRONGTYPE PRIVATE_SENTINEL\r\n"
				break
			}
			s.mu.Lock()
			entry, ok := s.values[args[1]]
			s.mu.Unlock()
			if !ok || entry.expires <= s.clock.Load() {
				reply = "$-1\r\n"
			} else {
				reply = fmt.Sprintf("$%d\r\n%s\r\n", len(entry.value), entry.value)
			}
		case "set":
			ttl := int64(time.Second)
			if len(args) > 4 {
				n, _ := strconv.ParseInt(args[4], 10, 64)
				ttl = n * int64(time.Millisecond)
				if strings.EqualFold(args[3], "ex") {
					ttl = n * int64(time.Second)
				}
			}
			s.mu.Lock()
			s.values[args[1]] = fakeEntry{args[2], s.clock.Load() + ttl}
			s.mu.Unlock()
			reply = "+OK\r\n"
		case "del":
			s.mu.Lock()
			_, ok := s.values[args[1]]
			delete(s.values, args[1])
			s.mu.Unlock()
			reply = ":0\r\n"
			if ok {
				reply = ":1\r\n"
			}
		default:
			reply = "-ERR PRIVATE_SENTINEL\r\n"
		}
		if _, err := io.WriteString(conn, reply); err != nil {
			return
		}
	}
}
func redisSettings(t *testing.T, s *fakeRedis, changes map[string]string) config.RedisConfig {
	t.Helper()
	v := map[string]string{"REDIS_ENABLED": "true", "REDIS_URL": "rediss://default:synthetic-password@" + s.listener.Addr().String() + "/0", "REDIS_TLS_INSECURE_SKIP_VERIFY": "true", "REDIS_DIAL_TIMEOUT": "300ms", "REDIS_READ_TIMEOUT": "300ms", "REDIS_WRITE_TIMEOUT": "300ms", "REDIS_OPERATION_TIMEOUT": "1s", "REDIS_STARTUP_TIMEOUT": "1s"}
	for k, x := range changes {
		v[k] = x
	}
	cfg, err := config.LoadRedis(func(k string) (string, bool) { x, ok := v[k]; return x, ok }, config.Test)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func openTestCache(t *testing.T, cfg config.RedisConfig, out io.Writer) *Cache {
	t.Helper()
	c, err := Open(context.Background(), cfg, slog.New(slog.NewJSONHandler(out, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}
func TestCacheSemanticsExpiryAndLimits(t *testing.T) {
	s := newFakeRedis(t)
	c := openTestCache(t, redisSettings(t, s, nil), io.Discard)
	ctx := context.Background()
	if v, hit, err := c.Get(ctx, "one"); err != nil || hit || v != nil {
		t.Fatal("missing cache key")
	}
	if err := c.Set(ctx, "one", []byte("value"), time.Second); err != nil {
		t.Fatal(err)
	}
	if v, hit, err := c.Get(ctx, "one"); err != nil || !hit || string(v) != "value" {
		t.Fatal("cache hit")
	}
	s.mu.Lock()
	_, ok := s.values["biho_live:test:cache:v1:one"]
	s.mu.Unlock()
	if !ok {
		t.Fatal("unversioned or cross-environment key")
	}
	s.clock.Add(int64(time.Second))
	if _, hit, err := c.Get(ctx, "one"); hit || err != nil {
		t.Fatal("expiry")
	}
	if err := c.Set(ctx, "empty", nil, time.Second); err != nil {
		t.Fatal(err)
	}
	if v, hit, err := c.Get(ctx, "empty"); err != nil || !hit || len(v) != 0 {
		t.Fatal("empty hit confused with miss")
	}
	if c.Delete(ctx, "empty") != nil || c.Delete(ctx, "empty") != nil {
		t.Fatal("idempotent delete")
	}
	for _, key := range []string{"", strings.Repeat("a", 129), "bad key", "{other}"} {
		if _, _, err := c.Get(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Fatal("unsafe key")
		}
	}
	for _, ttl := range []time.Duration{0, -1, time.Nanosecond, MaxTTL + time.Second} {
		if !errors.Is(c.Set(ctx, "x", nil, ttl), ErrInvalidTTL) {
			t.Fatal("invalid TTL")
		}
	}
	if !errors.Is(c.Set(ctx, "x", make([]byte, MaxValueBytes+1), time.Second), ErrValueTooLarge) {
		t.Fatal("oversized send")
	}
	wire, _ := c.key("large")
	s.mu.Lock()
	s.values[wire] = fakeEntry{strings.Repeat("x", MaxValueBytes+1), s.clock.Load() + int64(time.Second)}
	s.mu.Unlock()
	if v, hit, err := c.Get(ctx, "large"); v != nil || hit || !errors.Is(err, ErrValueTooLarge) {
		t.Fatal("oversized receive exposed")
	}
	s.wrongType.Store(true)
	if _, _, err := c.Get(ctx, "x"); !errors.Is(err, ErrCommand) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
		t.Fatal("command failure classification/redaction")
	}
}
func TestClientPolicyTLSAndHardCap(t *testing.T) {
	s := newFakeRedis(t)
	cfg := redisSettings(t, s, map[string]string{"REDIS_MAX_CONNS": "2"})
	c := openTestCache(t, cfg, io.Discard)
	opt := c.client.Options()
	if opt.MaxRetries != 0 || opt.DialerRetries != 1 || opt.PoolSize != 2 || opt.MaxActiveConns != 2 || opt.MinIdleConns != 0 || opt.PipelinePoolSize != -1 || opt.Protocol != 2 || !opt.ContextTimeoutEnabled || opt.MaintNotificationsConfig.Mode != maintnotifications.ModeDisabled {
		t.Fatal("client option semantics changed")
	}
	opts, err := options(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := opts.Dialer(context.Background(), "tcp", opts.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := opts.Dialer(context.Background(), "tcp", opts.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	before := s.acceptCount.Load()
	if extra, err := opts.Dialer(context.Background(), "tcp", opts.Addr); err == nil || extra != nil || s.acceptCount.Load() != before {
		t.Fatal("auxiliary dial bypassed shared hard cap")
	}
	_ = first.Close()
	_ = first.Close()
	replacement, err := opts.Dialer(context.Background(), "tcp", opts.Addr)
	if err != nil {
		t.Fatal("socket cap leaked")
	}
	_ = replacement.Close()
	// Default verified TLS must refuse the isolated self-signed server.
	var logs bytes.Buffer
	verified := openTestCache(t, redisSettings(t, s, map[string]string{"REDIS_TLS_INSECURE_SKIP_VERIFY": "false"}), &logs)
	if _, _, err := verified.Get(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("untrusted certificate accepted")
	}
	if !strings.Contains(logs.String(), "cache.startup_unavailable") || strings.Contains(logs.String(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe startup diagnostics")
	}
	mutated := cfg
	mutated.Environment = config.Production
	if client, err := Open(context.Background(), mutated, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || client != nil {
		t.Fatal("constructor TLS bypass in production")
	}
}
func TestOptionalAuthFailureRecoveryAndNoReplay(t *testing.T) {
	s := newFakeRedis(t)
	s.badAuth.Store(true)
	var logs bytes.Buffer
	c := openTestCache(t, redisSettings(t, s, nil), &logs)
	if _, _, err := c.Get(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("authentication unavailable")
	}
	if strings.Contains(logs.String(), "PRIVATE_SENTINEL") {
		t.Fatal("server detail leaked")
	}
	s.badAuth.Store(false)
	if err := c.Set(context.Background(), "x", []byte("v"), time.Second); err != nil {
		t.Fatal("authentication recovery failed")
	}
	s.loading.Store(true)
	beforeLoading := s.getCount.Load()
	if _, _, err := c.Get(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("retryable server unavailability")
	}
	if s.getCount.Load()-beforeLoading != 1 {
		t.Fatal("automatic command retry was enabled")
	}
	s.loading.Store(false)
	s.down.Store(true)
	before := s.getCount.Load()
	if _, _, err := c.Get(context.Background(), "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("outage not unavailable")
	}
	if s.getCount.Load()-before > 1 {
		t.Fatal("command replayed")
	}
	s.down.Store(false)
	if err := c.Set(context.Background(), "recovered", nil, time.Second); err != nil {
		t.Fatal("outage did not recover")
	}
}
func TestCancellationBoundsAndClose(t *testing.T) {
	s := newFakeRedis(t)
	c := openTestCache(t, redisSettings(t, s, map[string]string{"REDIS_MAX_CONNS": "2"}), io.Discard)
	s.block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := c.Get(ctx, "blocked"); done <- err }()
	<-s.entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrUnavailable) {
			t.Fatal("cancellation classification")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cancellation waited for socket timeout")
	}
	second := make(chan error, 1)
	go func() { _, _, err := c.Get(context.Background(), "second"); second <- err }()
	<-s.entered
	if _, _, err := c.Get(context.Background(), "overflow"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("operation queue not bounded")
	}
	if c.client.PoolStats().TotalConns > 2 {
		t.Fatal("pool exceeded cap")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- c.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not unblock bounded in-flight operations")
	}
	if err := <-second; !errors.Is(err, ErrUnavailable) {
		t.Fatal("in-flight close was not unavailable")
	}
	close(s.release)
	if err := c.Close(); err != nil {
		t.Fatal("close not idempotent")
	}
	if _, _, err := c.Get(context.Background(), "closed"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed client used")
	}
}
func TestDisabledAndEarlierDeadline(t *testing.T) {
	cfg, err := config.LoadRedis(func(string) (string, bool) { return "", false }, config.Test)
	if err != nil {
		t.Fatal(err)
	}
	c := openTestCache(t, cfg, io.Discard)
	if c.client != nil {
		t.Fatal("disabled Redis constructed network client")
	}
	if _, _, err := c.Get(context.Background(), "x"); !errors.Is(err, ErrDisabled) {
		t.Fatal("disabled classification")
	}
	if !errors.Is(c.Set(context.Background(), "x", nil, time.Second), ErrDisabled) || !errors.Is(c.Delete(context.Background(), "x"), ErrDisabled) {
		t.Fatal("disabled commands")
	}
	s := newFakeRedis(t)
	active := openTestCache(t, redisSettings(t, s, nil), io.Discard)
	s.block.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := active.Get(ctx, "deadline"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("earlier deadline extended")
	}
	close(s.release)
}

// A completed operation must release admission before its result is visible.
func TestSequentialSingleSlotOperations(t *testing.T) {
	c := &Cache{cfg: config.RedisConfig{Enabled: true}, slots: make(chan struct{}, 1)}
	defer c.Close()
	for range 100000 {
		if _, err := c.execute(context.Background(), func(context.Context) result { return result{} }); err != nil {
			t.Fatal("completed operation still occupies the only admission slot", err)
		}
	}
}

func BenchmarkAdapterDispatch(b *testing.B) {
	c := &Cache{cfg: config.RedisConfig{Enabled: true}, slots: make(chan struct{}, 1)}
	defer c.Close()
	b.ReportAllocs()
	for b.Loop() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := c.execute(ctx, func(context.Context) result { return result{value: "synthetic-cache-value"} })
		cancel()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestConcurrentOperationsAndClose(t *testing.T) {
	s := newFakeRedis(t)
	c := openTestCache(t, redisSettings(t, s, map[string]string{"REDIS_MAX_CONNS": "2"}), io.Discard)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 16 {
		wg.Go(func() {
			<-start
			key := fmt.Sprintf("concurrent:%d", i)
			for range 30 {
				if err := c.Set(context.Background(), key, []byte("value"), time.Second); err != nil && !errors.Is(err, ErrUnavailable) {
					t.Error("unexpected concurrent SET failure")
				}
				if _, _, err := c.Get(context.Background(), key); err != nil && !errors.Is(err, ErrUnavailable) {
					t.Error("unexpected concurrent GET failure")
				}
				if err := c.Delete(context.Background(), key); err != nil && !errors.Is(err, ErrUnavailable) {
					t.Error("unexpected concurrent DELETE failure")
				}
			}
		})
	}
	close(start)
	wg.Go(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Go(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	if len(c.slots) != 0 {
		t.Fatal("admission leaked after concurrent close")
	}
}

func TestOperationTimeoutAndPanicAdmission(t *testing.T) {
	s := newFakeRedis(t)
	c := openTestCache(t, redisSettings(t, s, map[string]string{"REDIS_OPERATION_TIMEOUT": "20ms"}), io.Discard)
	s.block.Store(true)
	start := time.Now()
	if _, _, err := c.Get(context.Background(), "timeout"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("operation timeout not enforced", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("operation timeout exceeded bound")
	}
	close(s.release)
	local := &Cache{cfg: config.RedisConfig{Enabled: true}, slots: make(chan struct{}, 1)}
	defer local.Close()
	r, err := local.execute(context.Background(), func(context.Context) result { panic("PRIVATE_SENTINEL") })
	if err != nil || !errors.Is(r.err, ErrCommand) {
		t.Fatal("panic not classified safely")
	}
	if _, err := local.execute(context.Background(), func(context.Context) result { return result{} }); err != nil {
		t.Fatal("panic leaked admission")
	}
}
