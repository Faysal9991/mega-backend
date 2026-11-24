// Command migrate upgrades schemas through a direct PostgreSQL connection.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/postgres"
)

func main() { os.Exit(run()) }
func run() int {
	env, ok := os.LookupEnv("APP_ENV")
	if !ok {
		env = "development"
	}
	cfg, err := config.LoadDatabase(config.MigrationLookup(os.LookupEnv), config.Environment(env))
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration: invalid direct connection settings")
		return 1
	}
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration: initialization failed")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err = migrator.Up(ctx)
	closeErr := migrator.Close()
	if err != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "migration: upgrade failed")
		return 1
	}
	fmt.Println("migration: upgrade complete")
	return 0
}
