package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/rediscache"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stdout, os.Stderr, net.Listen)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, lookup config.LookupEnv, stdout, stderr io.Writer, listen func(string, string) (net.Listener, error)) int {
	return runWithDatabase(ctx, lookup, stdout, stderr, listen, func(ctx context.Context, cfg config.DatabaseConfig) (databaseRuntime, error) {
		pool, err := postgres.Open(ctx, cfg)
		if err != nil {
			return databaseRuntime{}, err
		}
		availability, err := postgres.NewAvailability(pool, 2*time.Second, time.Second)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}
		return databaseRuntime{close: pool.Close, available: availability.Available}, nil
	})
}

type databaseRuntime struct {
	close     func()
	available func(context.Context) bool
}

// The injected starter isolates lifecycle tests from external database access.
func runWithDatabase(ctx context.Context, lookup config.LookupEnv, stdout, stderr io.Writer, listen func(string, string) (net.Listener, error), start func(context.Context, config.DatabaseConfig) (databaseRuntime, error)) int {
	return runWithServices(ctx, lookup, stdout, stderr, listen, start, func(ctx context.Context, cfg config.RedisConfig, logger *slog.Logger) (io.Closer, error) {
		return rediscache.Open(ctx, cfg, logger)
	})
}

func runWithServices(ctx context.Context, lookup config.LookupEnv, stdout, stderr io.Writer, listen func(string, string) (net.Listener, error), start func(context.Context, config.DatabaseConfig) (databaseRuntime, error), startCache func(context.Context, config.RedisConfig, *slog.Logger) (io.Closer, error)) int {
	// Load once at the composition root and inject the validated snapshot.
	cfg, err := config.Load(lookup)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	logger, err := logging.New(cfg, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := validateStartup(context.Background(), logger); err != nil {
		fmt.Fprintln(stderr, "logging: startup event write failed")
		return 1
	}
	if ctx.Err() != nil {
		return 0
	}
	cacheConfig, err := config.LoadRedis(lookup, cfg.Environment)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	database, err := config.LoadDatabase(lookup, cfg.Environment)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	runtimeDatabase, err := start(ctx, database)
	if err != nil {
		if ctx.Err() != nil {
			return 0
		}
		logger.Error("database.initialization_failed", slog.String("failure_code", "DATABASE_UNAVAILABLE"))
		fmt.Fprintln(stderr, "database: initialization failed")
		return 1
	}
	if runtimeDatabase.close == nil || runtimeDatabase.available == nil {
		if runtimeDatabase.close != nil {
			runtimeDatabase.close()
		}
		fmt.Fprintln(stderr, "database: incomplete runtime dependencies")
		return 1
	}
	defer runtimeDatabase.close()
	cache, err := startCache(ctx, cacheConfig, logger)
	if err != nil || cache == nil {
		fmt.Fprintln(stderr, "cache: invalid client configuration")
		return 1
	}
	defer func() {
		if err := cache.Close(); err != nil {
			logger.Warn("cache.close_failed", slog.String("failure_code", "CACHE_UNAVAILABLE"))
		}
	}()
	if ctx.Err() != nil {
		return 0
	}
	lifecycle := httpserver.NewLifecycle(runtimeDatabase.available)
	router, err := httpserver.NewRouter(cfg.HTTP, logger, lifecycle)
	if err != nil {
		fmt.Fprintln(stderr, "http: router construction failed")
		return 1
	}
	server, err := httpserver.New(cfg.HTTP, logger, lifecycle, router)
	if err != nil {
		fmt.Fprintln(stderr, "http: server construction failed")
		return 1
	}
	listener, err := listen("tcp", server.Address())
	if err != nil {
		logger.Error("http.listener_failed", slog.String("failure_code", "BIND_FAILED"))
		fmt.Fprintln(stderr, "http: listener binding failed")
		return 1
	}
	logger.Info("http.listener_bound")
	if err := server.Run(ctx, listener); err != nil {
		logger.Error("http.server_failed", slog.String("failure_code", "SERVE_OR_SHUTDOWN_FAILED"))
		fmt.Fprintln(stderr, "http: server lifecycle failed")
		return 1
	}
	return 0
}

func validateStartup(ctx context.Context, logger *slog.Logger) error {
	if !logger.Enabled(ctx, slog.LevelInfo) {
		return nil
	}
	// Handle returns the startup write error, unlike slog convenience methods.
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "startup.configuration_validated", 0)
	record.AddAttrs(slog.Bool("http_server_implemented", true))
	return logger.Handler().Handle(ctx, record)
}
