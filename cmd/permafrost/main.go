// Command permafrost is the operator CLI for the Permafrost protocol.
//
// See `permafrost --help` for the available subcommands. Most commands talk
// to the running permafrostd daemon via REST; some (db migrate, backtest,
// wallet) operate locally without a daemon.
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
