package cli

import (
	"context"
	"errors"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/api"
	"github.com/teslashibe/permafrost/internal/store"
)

// newServeCmd returns the `permafrost serve` subcommand which runs the
// permafrostd daemon in the foreground.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the permafrostd daemon in the foreground",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			return Serve(c.Context(), g)
		},
	}
}

// Serve runs the permafrostd HTTP server until the process receives
// SIGINT/SIGTERM or the supplied context is cancelled. It is exported so
// the dedicated permafrostd binary can call it directly.
func Serve(ctx context.Context, g *Globals) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g.Log.Info("permafrostd starting", "env", g.Config.Env, "bind", g.Config.Server.Bind)

	var db *store.DB
	if g.Config.Database.URL != "" {
		conn, err := store.Open(ctx, g.Config.Database)
		if err != nil {
			g.Log.Warn("database unavailable, continuing in degraded mode", "err", err)
		} else {
			db = conn
			defer db.Close()
		}
	}

	srv := api.NewServer(g.Config, g.Log, db)
	return srv.Listen(ctx)
}
