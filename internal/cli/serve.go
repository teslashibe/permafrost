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
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/inference/openai"
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
			return Serve(c.Context(), g, ServeOptions{HyperliquidNetworkOverride: hlNetwork})
		},
	}
	cmd.Flags().StringVar(&hlNetwork, "hyperliquid-network", "",
		"force ALL supervised agents onto this network (emergency override; empty = use each agent's stored network)")
	return cmd
}

// ServeOptions configures the daemon at runtime.
type ServeOptions struct {
	// HyperliquidNetworkOverride, if non-empty, forces every supervised
	// agent onto this network regardless of the network stored on its
	// own DB record. Useful as an emergency switch ("force everything to
	// testnet"). Leave empty in normal operation so each agent runs on
	// its own configured network.
	HyperliquidNetworkOverride string
}

// Serve runs the permafrostd HTTP server, loads any agents that are
// persisted with status='running', and supervises their tick loops until
// the process receives SIGINT/SIGTERM or the supplied context is cancelled.
//
// It is exported so the dedicated permafrostd binary can call it directly.
func Serve(ctx context.Context, g *Globals, opts ServeOptions) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startFields := []any{"env", g.Config.Env, "bind", g.Config.Server.Bind}
	if opts.HyperliquidNetworkOverride != "" {
		startFields = append(startFields, "hyperliquid_network_override", opts.HyperliquidNetworkOverride)
	}
	g.Log.Info("permafrostd starting", startFields...)

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
			// Inference is best-effort at the daemon level: the registry
			// is built from config.yaml. Agents that don't reference any
			// provider work fine with no inference configured. Agents
			// that do reference one but find it missing will surface a
			// clear error in BuildDeps.
			infReg, infErr := inference.NewRegistry(g.Config.Inference, openai.NewProvider)
			if infErr != nil {
				g.Log.Warn("supervisor: inference registry build failed; agents that need inference will fail to start", "err", infErr)
				infReg = nil
			}
			loader := &agent.Loader{
				Store:     agent.NewStore(db.Pool),
				Registry:  reg,
				Keystore:  ks,
				Inference: infReg,
				Logger:    g.Log,
				BuildOpts: agent.BuildOptions{
					HyperliquidNetwork: opts.HyperliquidNetworkOverride,
					Solana:             solanaSpotFromConfig(g.Config.Solana),
					EVM:                evmSpotsFromConfig(g.Config.EVM, os.Getenv),
				},
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
