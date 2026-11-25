// Command bootstrap-owner explicitly grants the single protected initial owner.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Faysal9991/mega-backend/internal/modules/access"
	accesspg "github.com/Faysal9991/mega-backend/internal/modules/access/postgres"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	platform "github.com/Faysal9991/mega-backend/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr)) }
func run(args []string, lookup config.LookupEnv, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bootstrap-owner", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	account := flags.String("account-id", "", "existing Google-linked account ID")
	reason := flags.String("reason", "", "safe operator reason, no personal values")
	correlation := flags.String("operation-id", "", "unique operator correlation")
	if flags.Parse(args) != nil {
		return 1
	}
	if flags.NArg() != 0 || !access.ValidID(*account) || !access.ValidReason(*reason) || !access.ValidCorrelation(*correlation) {
		fmt.Fprintln(stderr, "bootstrap: explicit account, safe reason and operation ID required")
		return 1
	}
	env, _ := lookup("APP_ENV")
	if env == "" {
		env = "development"
	}
	cfg, e := config.LoadDatabase(config.MigrationLookup(lookup), config.Environment(env))
	if e != nil || cfg.User != cfg.Name+"_migrator" {
		fmt.Fprintln(stderr, "bootstrap: dedicated migration credentials required")
		return 1
	}
	connCfg, e := platform.ConnectionConfig(cfg)
	if e != nil {
		fmt.Fprintln(stderr, "bootstrap: invalid connection policy")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, e := pgx.ConnectConfig(ctx, connCfg)
	if e != nil {
		fmt.Fprintln(stderr, "bootstrap: connection failed")
		return 1
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = conn.Close(cleanup)
	}()
	var db, role string
	if e = conn.QueryRow(ctx, "SELECT current_database(),current_user").Scan(&db, &role); e != nil || db != cfg.Name || role != cfg.User {
		fmt.Fprintln(stderr, "bootstrap: connection identity mismatch")
		return 1
	}
	transactions, e := platform.NewTransactions(conn, time.Second)
	if e == nil {
		e = transactions.Within(ctx, pgx.TxOptions{}, func(ctx context.Context, tx pgx.Tx) error {
			return accesspg.Bootstrap(ctx, tx, *account, *reason, *correlation)
		})
	}
	if e != nil {
		fmt.Fprintln(stderr, "bootstrap: denied or unavailable; inspect selected target securely")
		return 1
	}
	fmt.Fprintln(stdout, "bootstrap: protected owner initialized")
	return 0
}
