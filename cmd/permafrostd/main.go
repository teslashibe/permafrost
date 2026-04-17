// Command permafrostd is the long-running Permafrost daemon. It owns market
// data subscriptions, agent supervisors, the Fiber API, and the scheduler.
//
// permafrostd is functionally equivalent to `permafrost serve` and shares
// all of its initialisation logic. It exists as a separate binary so
// docker-compose, systemd units, etc. can target it directly.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/teslashibe/permafrost/internal/cli"
	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/telemetry"
)

func main() {
	configPath := os.Getenv("PERMAFROST_CONFIG")
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	g := &cli.Globals{
		Config: cfg,
		Log:    telemetry.NewLogger(cfg.Logging, cfg.Env),
	}
	opts := cli.ServeOptions{
		// PERMAFROST_HYPERLIQUID_NETWORK acts as the global override; empty
		// means "use each agent's stored network". Default left empty so
		// the daemon respects per-agent network choices out of the box.
		HyperliquidNetworkOverride: os.Getenv("PERMAFROST_HYPERLIQUID_NETWORK"),
	}
	if err := cli.Serve(context.Background(), g, opts); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}
