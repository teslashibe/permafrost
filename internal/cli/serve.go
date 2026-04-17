package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/api"
	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/store"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// newServeCmd returns the `permafrost serve` subcommand which runs the
// permafrostd daemon in the foreground.
func newServeCmd() *cobra.Command {
	var hlNetwork string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the permafrostd daemon in the foreground",
		RunE: func(c *cobra.Command, _ []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			return Serve(c.Context(), g, ServeOptions{HyperliquidNetwork: hlNetwork})
		},
	}
	cmd.Flags().StringVar(&hlNetwork, "hyperliquid-network", "testnet",
		"hyperliquid network for supervised agents (mainnet | testnet)")
	return cmd
}

// ServeOptions configures the daemon at runtime.
type ServeOptions struct {
	HyperliquidNetwork string
}

// Serve runs the permafrostd HTTP server, loads any agents that are
// persisted with status='running', and supervises their tick loops until
// the process receives SIGINT/SIGTERM or the supplied context is cancelled.
//
// It is exported so the dedicated permafrostd binary can call it directly.
func Serve(ctx context.Context, g *Globals, opts ServeOptions) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g.Log.Info("permafrostd starting",
		"env", g.Config.Env, "bind", g.Config.Server.Bind,
		"hyperliquid_network", opts.HyperliquidNetwork)

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

	// Supervisor: load any running agents and start their tick loops.
	var sup *agent.Supervisor
	if db != nil {
		sup = agent.NewSupervisor(g.Log)
		ks := openKeystoreForServe(g) // best-effort; nil is OK
		reg, err := assets.LoadEmbedded()
		if err != nil {
			g.Log.Warn("supervisor: load registry failed; agents will not start", "err", err)
		} else {
			loader := &agent.Loader{
				Store:      agent.NewStore(db.Pool),
				Registry:   reg,
				Keystore:   ks,
				Logger:     g.Log,
				BuildOpts:  agent.BuildOptions{HyperliquidNetwork: opts.HyperliquidNetwork},
				Supervisor: sup,
			}
			n, err := loader.LoadAndStartRunning(ctx)
			if err != nil {
				g.Log.Warn("supervisor: load failed", "err", err)
			} else {
				g.Log.Info("supervisor: agents started", "count", n)
			}
		}
	}

	srv := api.NewServer(g.Config, g.Log, db)
	err := srv.Listen(ctx)

	// Graceful supervisor shutdown.
	if sup != nil {
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for _, id := range sup.IDs() {
			_ = sup.Stop(shutdownCtx, id, "daemon shutdown")
		}
	}
	return err
}

// openKeystoreForServe loads the keystore from disk if both a path and the
// configured passphrase env var are populated. Returns nil silently
// otherwise — the daemon can still start in funding-only mode.
func openKeystoreForServe(g *Globals) wallet.Keystore {
	path := g.Config.Wallet.KeystorePath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".permafrost", "keystore.json")
	}
	envName := g.Config.Wallet.PassphraseEnv
	if envName == "" {
		envName = "PERMAFROST_KEYSTORE_PASSPHRASE"
	}
	pass := os.Getenv(envName)
	if pass == "" {
		return nil
	}
	ks, err := wallet.NewLocalKeystore(path, pass)
	if err != nil {
		g.Log.Warn("supervisor: keystore unavailable; running funding-only", "err", err)
		return nil
	}
	return ks
}
