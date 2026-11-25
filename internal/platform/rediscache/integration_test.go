//go:build redis_integration

package rediscache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func integrationConfig(t testing.TB) config.RedisConfig {
	t.Helper()
	if os.Getenv("BIHO_REDIS_INTEGRATION_ACK") != "169.58.127.58:7200/0" {
		t.Fatal("BLOCKED: explicit acknowledgment of the authorized Redis test endpoint required")
	}
	cfg, err := config.LoadRedis(os.LookupEnv, config.Test)
	if err != nil || !cfg.Enabled {
		t.Fatal("BLOCKED: enabled Redis integration configuration unavailable or invalid; credentials suppressed")
	}
	endpoint, err := config.RedisEndpoint(cfg.URL.Value())
	if err != nil || endpoint.Hostname() != "169.58.127.58" || endpoint.Port() != "7200" || endpoint.User.Username() != "default" {
		t.Fatal("BLOCKED: integration may use only the authorized rediss endpoint/ACL/database")
	}
	return cfg
}

func TestRedisIntegrationDisposableCache(t *testing.T) {
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cache, err := Open(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("BLOCKED: Redis integration client configuration failed")
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Error("Redis integration close failed")
		}
	}()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("unique prefix generation failed")
	}
	prefix := "integration:" + hex.EncodeToString(random[:]) + ":"
	keys := []string{prefix + "value", prefix + "empty", prefix + "expiry"}
	// Track every planned key before any SET, including ambiguous write outcomes.
	// No enumeration/global deletion; even failed tests leave only short-lived data.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		for _, key := range keys {
			if err := cache.Delete(cleanup, key); err != nil {
				t.Error("tracked Redis test-key cleanup unavailable; keys retain short TTL")
			}
			if _, hit, err := cache.Get(cleanup, key); err != nil || hit {
				t.Error("tracked Redis test-key cleanup verification failed")
			}
		}
	}()
	if err := cache.Set(ctx, keys[0], []byte("synthetic-cache-value"), 30*time.Second); err != nil {
		t.Fatal("BLOCKED: Redis integration SET unavailable (connectivity/auth/TLS/ACL); raw error suppressed")
	}
	if value, hit, err := cache.Get(ctx, keys[0]); err != nil || !hit || string(value) != "synthetic-cache-value" {
		t.Fatal("real Redis cache hit contract failed")
	}
	if err := cache.Set(ctx, keys[1], nil, 30*time.Second); err != nil {
		t.Fatal("real Redis empty value SET failed")
	}
	if value, hit, err := cache.Get(ctx, keys[1]); err != nil || !hit || len(value) != 0 {
		t.Fatal("real Redis empty value confused with miss")
	}
	var wg sync.WaitGroup
	var hits, unavailable atomic.Int64
	for range 16 {
		wg.Go(func() {
			value, hit, err := cache.Get(ctx, keys[0])
			if errors.Is(err, ErrUnavailable) {
				unavailable.Add(1)
				return
			}
			if err != nil || !hit || string(value) != "synthetic-cache-value" {
				t.Error("real Redis concurrent cache result invalid")
				return
			}
			hits.Add(1)
		})
	}
	wg.Wait()
	t.Logf("concurrent operations: hits=%d, bounded rejections=%d, pool connections=%d, cap=%d", hits.Load(), unavailable.Load(), cache.client.PoolStats().TotalConns, cfg.MaxConns)
	if hits.Load() == 0 || cache.client.PoolStats().TotalConns > uint32(cfg.MaxConns) {
		t.Fatal("real Redis concurrency/cap contract failed")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, _, err := cache.Get(canceled, keys[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("real client canceled-operation contract")
	}
	if err := cache.Delete(ctx, keys[0]); err != nil {
		t.Fatal("real Redis delete failed")
	}
	if _, hit, err := cache.Get(ctx, keys[0]); err != nil || hit {
		t.Fatal("real Redis miss after delete failed")
	}
	if err := cache.Set(ctx, keys[2], []byte("expires"), 100*time.Millisecond); err != nil {
		t.Fatal("real Redis short TTL SET failed")
	}
	expiry := time.NewTimer(200 * time.Millisecond) // Derived from the 100ms TTL; no busy polling or server-wide command.
	defer expiry.Stop()
	select {
	case <-expiry.C:
	case <-ctx.Done():
		t.Fatal("expiry wait canceled")
	}
	if _, hit, err := cache.Get(ctx, keys[2]); err != nil || hit {
		t.Fatal("real Redis short TTL expiry failed")
	}

	t.Log("PASS: real rediss PING/Get/Set/Delete, short TTL, unique tracked keys, concurrent cap and cancellation; identity verification bypass:", cfg.TLSInsecureSkipVerify)
}

// Serial warmed GETs: the public adapter versus its private driver, using the
// same client/key. Both include remote TLS/network/server latency, not just Go.
func BenchmarkRedisRemoteGet(b *testing.B) {
	cfg := integrationConfig(b)
	cache, err := Open(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatal("integration configuration failed")
	}
	defer func() {
		if err := cache.Close(); err != nil {
			b.Error("benchmark client close failed")
		}
	}()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		b.Fatal("random key failed")
	}
	key := "benchmark:" + hex.EncodeToString(random[:])
	// Register cleanup before any potentially ambiguous write outcome.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.Delete(ctx, key); err != nil {
			b.Error("benchmark cleanup failed")
		}
		if _, hit, err := cache.Get(ctx, key); err != nil || hit {
			b.Error("benchmark cleanup verification failed")
		}
	}()
	wire, _ := cache.key(key)
	for _, mode := range []string{"Adapter", "Driver"} {
		b.Run(mode, func(b *testing.B) {
			if b.N > 50 {
				b.Fatal("use -benchtime=20x (maximum 50 samples per mode)")
			}
			// Refresh this owned key outside timing so two remote samples do not
			// share one expiring fixture. Never remove the short TTL safeguard.
			if err := cache.Set(context.Background(), key, []byte("synthetic-cache-value"), 30*time.Second); err != nil {
				b.Fatal("benchmark fixture refresh failed")
			}
			samples := make([]time.Duration, 0, b.N)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), cfg.OperationTimeout)
				if mode == "Adapter" {
					value, hit, err := cache.Get(ctx, key)
					if err != nil || !hit || string(value) != "synthetic-cache-value" {
						b.Fatal("benchmark GET failed")
					}
				} else {
					value, err := cache.client.Get(ctx, wire).Result()
					if err != nil || value != "synthetic-cache-value" {
						b.Fatal("driver GET failed")
					}
				}
				cancel()
				samples = append(samples, time.Since(start))
			}
			b.StopTimer()
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			for _, p := range []int{50, 95, 99} {
				b.ReportMetric(float64(samples[(len(samples)*p+99)/100-1].Nanoseconds())/1e6, "p"+fmt.Sprint(p)+"_ms")
			}
		})
	}
}
