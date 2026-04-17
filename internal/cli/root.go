// Package cli builds the cobra command tree for the `permafrost` binary.
//
// Commands are added in their respective milestone PRs; this package only
// owns the root command, the global flags, and the persistent pre-run hooks
// (config + logger initialisation).
package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/telemetry"
)

// Globals carry resources made available to every subcommand via PersistentPreRun.
type Globals struct {
	Config *config.Config
	Log    *slog.Logger
}

// NewRootCmd constructs the top-level `permafrost` command and wires
// shared dependencies into ctx for subcommands.
func NewRootCmd(ctx context.Context) *cobra.Command {
	g := &Globals{}
	var configPath string

	cmd := &cobra.Command{
		Use:           "permafrost",
		Short:         "Permafrost CLI — manage vaults, agents, and the Permafrost daemon",
		Long:          "Permafrost is an open-source DeFi market-making and hedge-fund protocol where AI agents deploy capital into delta-neutral funding-arb strategies. This CLI is the operator's control plane.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config.yaml (defaults to ./config.yaml)")

	cmd.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		g.Config = cfg
		g.Log = telemetry.NewLogger(cfg.Logging, cfg.Env)
		c.SetContext(WithGlobals(c.Context(), g))
		return nil
	}

	cmd.SetContext(ctx)
	cmd.AddCommand(
		newServeCmd(),
		newDBCmd(),
		newVersionCmd(),
	)
	for _, f := range commandFactories {
		cmd.AddCommand(f())
	}
	return cmd
}

// commandFactories is a registration table populated via init() functions
// in sibling files. This lets each milestone PR add its own CLI subcommand
// without editing root.go (which would create constant merge conflicts).
var commandFactories []func() *cobra.Command

// addCommandFactory registers a command constructor. Call from init().
func addCommandFactory(f func() *cobra.Command) {
	commandFactories = append(commandFactories, f)
}

type globalsKey struct{}

// WithGlobals attaches Globals to a context.
func WithGlobals(ctx context.Context, g *Globals) context.Context {
	return context.WithValue(ctx, globalsKey{}, g)
}

// FromContext extracts Globals previously attached by WithGlobals.
// Subcommands should call this in their RunE.
func FromContext(ctx context.Context) *Globals {
	g, _ := ctx.Value(globalsKey{}).(*Globals)
	return g
}
