package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestIdentitySchemaStartupBound(t *testing.T) {
	for _, mode := range []string{"timeout", "earlier_deadline", "expired_deadline", "cancellation", "query_failure"} {
		t.Run(mode, func(t *testing.T) {
			process, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget := 20 * time.Millisecond
			var stdout, stderr bytes.Buffer
			closes, calls := 0, 0
			code := runWithServices(process, databaseTestLookup, &stdout, &stderr, func(string, string) (net.Listener, error) {
				t.Fatal("failed schema initialization bound listener")
				return nil, nil
			}, func(ctx context.Context, cfg config.DatabaseConfig) (databaseRuntime, error) {
				// Simulate the completed PostgreSQL Open stage, then stall only schema Ready.
				if mode == "earlier_deadline" || mode == "expired_deadline" {
					duration := 10 * time.Millisecond
					if mode == "expired_deadline" {
						duration = -time.Second
					}
					var stop context.CancelFunc
					ctx, stop = context.WithDeadline(ctx, time.Now().Add(duration))
					defer stop()
				}
				parentDeadline, hasParentDeadline := ctx.Deadline()
				err := checkIdentitySchema(ctx, budget, func(check context.Context) error {
					calls++
					deadline, ok := check.Deadline()
					if !ok || time.Until(deadline) > budget {
						t.Error("schema query missing startup deadline")
						return errors.New("unbounded schema query")
					}
					if hasParentDeadline && !deadline.Equal(parentDeadline) {
						t.Error("earlier caller deadline was extended")
					}
					if mode == "query_failure" {
						return errors.New("PRIVATE_SCHEMA_FAILURE")
					}
					if mode == "cancellation" {
						cancel()
					}
					<-check.Done() // Deterministically stalls until the effective bound publishes.
					return check.Err()
				}, func() { closes++ })
				if err == nil {
					t.Fatal("failed/stalled schema check succeeded")
				}
				if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("startup timeout cause lost")
				}
				return databaseRuntime{}, err
			}, func(context.Context, config.RedisConfig, *slog.Logger) (io.Closer, error) {
				t.Fatal("failed schema startup constructed cache")
				return nil, nil
			})
			want := 1
			if mode == "cancellation" {
				want = 0
			}
			if code != want || closes != 1 {
				t.Fatal("schema failure cleanup/exit status", code, closes)
			}
			if mode == "expired_deadline" && calls != 0 {
				t.Fatal("expired earlier deadline still ran schema query")
			}
			if bytes.Contains(stderr.Bytes(), []byte("PRIVATE_SCHEMA_FAILURE")) || bytes.Contains(stdout.Bytes(), []byte("PRIVATE_SCHEMA_FAILURE")) {
				t.Fatal("raw startup failure exposed")
			}
		})
	}
}
func TestIdentitySchemaStartupSuccessAndLateCancellation(t *testing.T) {
	closes := 0
	if err := checkIdentitySchema(context.Background(), time.Second, func(context.Context) error { return nil }, func() { closes++ }); err != nil || closes != 0 {
		t.Fatal("successful check closed its pool")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := checkIdentitySchema(ctx, time.Second, func(context.Context) error { cancel(); return nil }, func() { closes++ })
	if !errors.Is(err, context.Canceled) || closes != 1 {
		t.Fatal("canceled initialization published success")
	}
}
