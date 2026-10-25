// Command pgs-converter renders text subtitles into standalone PGS/SUP streams.
// Process lifetime and signals stay here; internal/cli is reusable by tests.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/ninghaofeng/pgs_converter/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	// os.Exit skips deferred functions, so unregister signal handling explicitly.
	stop()
	os.Exit(code)
}
