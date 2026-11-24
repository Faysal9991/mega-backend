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
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stdout, os.Stderr, net.Listen)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, lookup config.LookupEnv, stdout, stderr io.Writer, listen func(string, string) (net.Listener, error)) int {
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
	router, err := httpserver.NewRouter(cfg.HTTP, logger)
	if err != nil {
		fmt.Fprintln(stderr, "http: router construction failed")
		return 1
	}
	server, err := httpserver.New(cfg.HTTP, logger, router)
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
