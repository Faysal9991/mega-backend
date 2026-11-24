package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
)

func main() {
	os.Exit(run(os.LookupEnv, os.Stdout, os.Stderr))
}

func run(lookup config.LookupEnv, stdout, stderr io.Writer) int {
	// Load once at the composition root. Future dependencies will receive the
	// validated snapshot explicitly; this step does not start an HTTP server.
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
	return 0
}

func validateStartup(ctx context.Context, logger *slog.Logger) error {
	if !logger.Enabled(ctx, slog.LevelInfo) {
		return nil
	}
	// Handle returns the startup write error, unlike slog convenience methods.
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "startup.configuration_validated", 0)
	record.AddAttrs(slog.Bool("http_server_implemented", false))
	return logger.Handler().Handle(ctx, record)
}
