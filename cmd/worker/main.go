// Command worker owns the bounded recoverable delivery dispatcher lifecycle.
package main

import (
	"context"
	"fmt"
	livedomain "github.com/Faysal9991/mega-backend/internal/modules/live"
	livepg "github.com/Faysal9991/mega-backend/internal/modules/live/postgres"
	liveprovider "github.com/Faysal9991/mega-backend/internal/modules/live/provider"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Faysal9991/mega-backend/internal/modules/delivery"
	deliverypg "github.com/Faysal9991/mega-backend/internal/modules/delivery/postgres"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/providers/fcm"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/providers/objectstore"
	"github.com/Faysal9991/mega-backend/internal/modules/delivery/providers/roomdata"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/logging"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	e := run(ctx, os.LookupEnv)
	cancel()
	if e != nil {
		fmt.Fprintln(os.Stderr, "worker initialization or dispatch failed")
		os.Exit(1)
	}
}
func run(ctx context.Context, lookup config.LookupEnv) error {
	base, e := config.Load(lookup)
	if e != nil {
		return e
	}
	db, e := config.LoadDatabase(lookup, base.Environment)
	if e != nil {
		return e
	}
	cfg, e := config.LoadDelivery(lookup, base.Environment)
	if e != nil {
		return e
	}
	live, e := config.LoadLive(lookup)
	if e != nil {
		return e
	}
	pool, e := postgres.Open(ctx, db)
	if e != nil {
		return e
	}
	defer pool.Close()
	var store delivery.ObjectStore = delivery.UnavailableStore{}
	if cfg.MediaEnabled {
		s := objectstore.NewS3(cfg.Endpoint, cfg.Region, cfg.Bucket, cfg.AccessKey, cfg.SecretKey.Value(), cfg.PathStyle)
		store = s
		defer s.Close()
	}
	repo, e := deliverypg.New(pool, nil, store, cfg)
	if e != nil {
		return e
	}
	ready, done := context.WithTimeout(ctx, db.ConnectTimeout)
	e = repo.Ready(ready)
	done()
	if e != nil {
		return e
	}
	logger, e := logging.New(base, os.Stdout)
	if e != nil {
		return e
	}
	d := &deliverypg.Dispatcher{Repo: repo, Workers: cfg.Workers, Logger: logger.With("component", "delivery_worker")}
	if live.Enabled {
		d.Rooms = roomdata.NewLiveKit(live.URL, live.Key, live.Secret)
	}
	if cfg.PushEnabled {
		client, err := fcm.NewFirebase(ctx, cfg.CredentialsFile)
		if err != nil {
			return err
		}
		defer client.Close()
		d.Push = client
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	if live.Enabled {
		rooms, e := livepg.New(pool, livedomain.Limits{DatabaseTimeout: live.Limits.DatabaseTimeout, Capacity: live.Limits.Capacity, Seats: live.Limits.Seats, TokenTTL: live.Limits.TokenTTL, ProviderTimeout: live.Limits.ProviderTimeout, EventAge: live.Limits.EventAge, Compatibility: livedomain.Compatibility(live.Limits.Compatibility)})
		if e != nil {
			return e
		}
		provider := liveprovider.New(live.URL, live.Key, live.Secret)
		wg.Go(func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			room, account := "", ""
			delay := time.Second
			var retryAt time.Time
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					call, done := context.WithTimeout(workerCtx, 30*time.Second)
					attempt := !time.Now().Before(retryAt)
					var remote livedomain.Provider
					if attempt {
						remote = provider
					}
					nr, na, e := rooms.RecoverCapabilities(call, room, account, remote)
					done()
					room, account = nr, na
					if e != nil {
						logger.Warn("live.capability_recovery_failed", "failure_code", "LIVE_RECOVERY_UNAVAILABLE")
						if attempt {
							retryAt = time.Now().Add(delay + time.Duration(rand.Int64N(int64(delay/4+1))))
							delay = min(30*time.Second, delay*2)
						}
					} else if attempt {
						delay = time.Second
						retryAt = time.Time{}
					}

				}
			}
		})
	}
	e = d.Run(workerCtx)
	cancel()
	wg.Wait()
	return e
}
