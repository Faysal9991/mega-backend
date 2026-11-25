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

	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesshttp "github.com/Faysal9991/mega-backend/internal/modules/access/httpapi"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/agency"
	"github.com/Faysal9991/mega-backend/internal/modules/identity"
	"github.com/Faysal9991/mega-backend/internal/modules/identity/httpapi"
	identitypg "github.com/Faysal9991/mega-backend/internal/modules/identity/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/live"
	livehttp "github.com/Faysal9991/mega-backend/internal/modules/live/httpapi"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	liveprovider "github.com/Faysal9991/mega-backend/internal/modules/live/provider"
	"github.com/Faysal9991/mega-backend/internal/modules/wallet"
	wallethttp "github.com/Faysal9991/mega-backend/internal/modules/wallet/httpapi"
	walletpg "github.com/Faysal9991/mega-backend/internal/modules/wallet/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/httpserver"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/rediscache"
	"github.com/go-chi/chi/v5"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.LookupEnv, os.Stdout, os.Stderr, net.Listen)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, lookup config.LookupEnv, stdout, stderr io.Writer, listen func(string, string) (net.Listener, error)) int {
	return runWithDatabase(ctx, lookup, stdout, stderr, listen, func(ctx context.Context, cfg config.DatabaseConfig) (databaseRuntime, error) {
		authConfig, err := config.LoadIdentity(lookup)
		if err != nil {
			return databaseRuntime{}, err
		}
		liveConfig, err := config.LoadLive(lookup)
		if err != nil {
			return databaseRuntime{}, err
		}
		pool, err := postgres.Open(ctx, cfg)
		if err != nil {
			return databaseRuntime{}, err
		}
		availability, err := postgres.NewAvailability(pool, 2*time.Second, time.Second)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}
		repository, err := identitypg.New(pool)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}

		var source agency.ActivitySource
		limits := live.Limits{DatabaseTimeout: liveConfig.Limits.DatabaseTimeout, Capacity: liveConfig.Limits.Capacity, Seats: liveConfig.Limits.Seats, TokenTTL: liveConfig.Limits.TokenTTL, ProviderTimeout: liveConfig.Limits.ProviderTimeout, EventAge: liveConfig.Limits.EventAge, Compatibility: live.Compatibility(liveConfig.Limits.Compatibility)}
		liveRepo, err := livepg.New(pool, limits)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}
		var provider live.Provider = liveprovider.Unavailable{}
		var verifier livehttp.Verifier = liveprovider.Unavailable{}.Verify
		if liveConfig.Enabled {
			client := liveprovider.New(liveConfig.URL, liveConfig.Key, liveConfig.Secret)
			provider = client
			verifier = client.Verify
			source = liveRepo
		}
		liveService, err := live.New(liveRepo, provider, limits, liveConfig.URL)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}

		financial, err := walletpg.New(pool, source)
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}
		if err := checkIdentitySchema(ctx, cfg.ConnectTimeout, func(check context.Context) error {
			if err := repository.Ready(check); err != nil {
				return err
			}
			if err := financial.Ready(check); err != nil {
				return err
			}
			return liveRepo.Ready(check)
		}, pool.Close); err != nil {
			return databaseRuntime{}, err
		}
		service, err := identity.New(repository, identity.NewGoogle(authConfig.GoogleClientID, authConfig.GoogleAuthorizedParties...), identity.NewPasswords())
		if err != nil {
			pool.Close()
			return databaseRuntime{}, err
		}
		return databaseRuntime{close: pool.Close, available: availability.Available, register: func(router chi.Router, logger *slog.Logger) error {
			handler, err := httpapi.New(service, logger)
			if err != nil {
				return err
			}
			handler.Mount(router)
			ar, err := accesspg.New(pool)
			if err != nil {
				return err
			}
			as, err := access.New(ar)
			if err != nil {
				return err
			}
			ah, err := accesshttp.New(as, logger)
			if err != nil {
				return err
			}
			ah.Mount(router)
			wh, err := wallethttp.New(wallet.New(financial), logger)
			if err != nil {
				return err
			}
			wh.Mount(router)
			if liveService != nil {
				lh, err := livehttp.New(liveService, verifier, logger)
				if err != nil {
					return err
				}
				lh.Mount(router)
			}
			return nil
		}}, nil
	})
}

// checkIdentitySchema owns failure cleanup for the already-open runtime pool.
// PostgreSQL Open has completed its own budget; this subsequent query must also
// be bounded and must retain an earlier process deadline/cancellation.
func checkIdentitySchema(ctx context.Context, budget time.Duration, ready func(context.Context) error, closePool func()) error {
	check, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	err := check.Err()
	if err == nil {
		err = ready(check)
	}
	if err == nil {
		err = check.Err()
	}
	if err != nil {
		closePool()
	}
	return err
}

type databaseRuntime struct {
	close     func()
	available func(context.Context) bool
	register  func(chi.Router, *slog.Logger) error
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
	var registrationErr error
	var mounts []func(chi.Router)
	if runtimeDatabase.register != nil {
		mounts = append(mounts, func(router chi.Router) { registrationErr = runtimeDatabase.register(router, logger) })
	}
	router, err := httpserver.NewRouter(cfg.HTTP, logger, lifecycle, mounts...)
	if registrationErr != nil {
		fmt.Fprintln(stderr, "identity: transport initialization failed")
		return 1
	}
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
