// Command worker owns the bounded recoverable delivery dispatcher lifecycle.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	return d.Run(ctx)
}
