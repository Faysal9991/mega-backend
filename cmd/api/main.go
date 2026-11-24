package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Faysal9991/mega-backend/internal/platform/config"
)

func main() {
	os.Exit(run(os.LookupEnv, os.Stdout, os.Stderr))
}

func run(lookup config.LookupEnv, stdout, stderr io.Writer) int {
	// Load once at the composition root. Future dependencies will receive the
	// validated snapshot explicitly; this step does not start an HTTP server.
	if _, err := config.Load(lookup); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "Configuration validated; HTTP server is not implemented."); err != nil {
		return 1
	}
	return 0
}
