// Command permafrost is the operator CLI for the Permafrost protocol.
//
// See `permafrost --help` for the available subcommands. Most commands
// open the configured TimescaleDB directly (mutating agent / vault state,
// reading decisions and PnL); a few (`db migrate`, `strategy backtest`,
// `wallet`) operate purely locally. The `serve` subcommand runs the
// daemon in the foreground and is functionally identical to the
// dedicated permafrostd binary.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/teslashibe/permafrost/internal/cli"
)

func main() {
	root := cli.NewRootCmd(context.Background())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
