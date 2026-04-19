package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/config"
	"github.com/teslashibe/permafrost/internal/exchange"
	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/internal/store"
	"github.com/teslashibe/permafrost/pkg/types"
)

func init() { addCommandFactory(newAgentCmd) }

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage strategy agents",
	}
	cmd.AddCommand(
		newAgentCreateCmd(),
		newAgentListCmd(),
		newAgentStatusCmd(),
		newAgentDecisionsCmd(),
		newAgentSetModeCmd(),
		newAgentSetNetworkCmd(),
		newAgentStopCmd(),
		newAgentTickCmd(),
		newAgentRunCmd(),
	)
	return cmd
}

func newAgentSetNetworkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-network <id> <mainnet|testnet>",
		Short: "Switch an agent between mainnet and testnet (Hyperliquid).",
		Long: `Switches an agent's data source. Paper-mode agents are safe to
run against mainnet — no orders are submitted. Live-mode agents (when
signing lands) MUST be re-confirmed before flipping to mainnet because
real money is at stake.

The daemon picks up the change on next tick — no restart needed for
read-only effects, but if the agent is currently running you may want
to stop+start it so the venue client reconnects with the new endpoint.`,
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			st, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			network := agent.Network(args[1])
			if network != agent.NetworkMainnet && network != agent.NetworkTestnet {
				return fmt.Errorf("network must be mainnet or testnet")
			}
			a, err := st.Get(c.Context(), args[0])
			if err != nil {
				return err
			}
			if a.Mode == agent.ModeLive && network == agent.NetworkMainnet {
				fmt.Printf("WARNING: agent %q is live-mode. Switching to mainnet means real money on the next tick.\n", a.ID)
			}
			if err := st.SetNetwork(c.Context(), a.ID, network); err != nil {
				return err
			}
			fmt.Printf("agent %s network -> %s\n", a.ID, network)
			return nil
		},
	}
}

func newAgentStopCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "stop [id]",
		Short: "Halt an agent (or all agents). Marks status=halted.",
		Long: `Halt an agent: marks status=halted in the database. The full kill switch
(cancel orders + close positions) requires a running daemon with a live
supervisor. This subcommand performs the persistent state change so a
subsequent 'permafrost serve' will not auto-restart the agent.`,
		Args: func(c *cobra.Command, args []string) error {
			if all && len(args) != 0 {
				return errors.New("either --all or an id, not both")
			}
			if !all && len(args) != 1 {
				return errors.New("provide --all or an agent id")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			_, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			if all {
				res, err := db.Pool.Exec(c.Context(),
					`UPDATE agents SET status = 'halted', updated_at = now() WHERE status <> 'halted'`)
				if err != nil {
					return err
				}
				fmt.Printf("halted %d agents\n", res.RowsAffected())
				return nil
			}
			res, err := db.Pool.Exec(c.Context(),
				`UPDATE agents SET status = 'halted', updated_at = now() WHERE id = $1`, args[0])
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("agent %q not found", args[0])
			}
			fmt.Printf("agent %s halted\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "halt every agent (kill switch)")
	return cmd
}

func openAgentStore(c *cobra.Command) (*agent.Store, *store.DB, error) {
	g := FromContext(c.Context())
	if g == nil {
		return nil, nil, errors.New("globals not initialised")
	}
	db, err := store.Open(c.Context(), g.Config.Database)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	return agent.NewStore(db.Pool), db, nil
}

func newAgentCreateCmd() *cobra.Command {
	var (
		id, name, strategy, perp, spot, inferenceRef, modeStr, networkStr string
		universeCSV                                                       string
		alloc                                                             string
		tickSecs                                                          int
		configJSON                                                        string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a strategy agent (paper mode + mainnet data by default)",
		Long: `Create a new agent.

Paper mode is the default and is safe — no orders are submitted. It pairs
naturally with mainnet data so the strategy makes decisions against real
funding rates. Use --network testnet if you specifically want sparse
testnet data (e.g. while developing a live-mode signing flow).`,
		RunE: func(c *cobra.Command, _ []string) error {
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			if id == "" {
				id = "ag_" + uuid.NewString()[:8]
			}
			amt := decimal.Zero
			if alloc != "" {
				amt, err = decimal.NewFromString(alloc)
				if err != nil {
					return fmt.Errorf("alloc: %w", err)
				}
			}
			universe := splitCSV(universeCSV)
			cfgMap := map[string]any{}
			if configJSON != "" {
				if err := json.Unmarshal([]byte(configJSON), &cfgMap); err != nil {
					return fmt.Errorf("--config-json: %w", err)
				}
			}
			a := agent.Agent{
				ID:             id,
				Name:           name,
				Strategy:       strategy,
				Mode:           agent.Mode(modeStr),
				Network:        agent.Network(networkStr),
				PerpVenue:      perp,
				SpotVenue:      spot,
				Inference:      inferenceRef,
				Universe:       universe,
				AllocationUSDC: amt,
				TickSecs:       tickSecs,
				Config:         cfgMap,
			}
			out, err := s.Create(c.Context(), a)
			if err != nil {
				return err
			}
			fmt.Printf("created agent id=%s name=%s strategy=%s mode=%s network=%s alloc=%s\n",
				out.ID, out.Name, out.Strategy, out.Mode, out.Network, out.AllocationUSDC)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "agent id (defaults to a generated value)")
	cmd.Flags().StringVar(&name, "name", "", "human-readable label")
	cmd.Flags().StringVar(&strategy, "strategy", "noop", "registered strategy name")
	cmd.Flags().StringVar(&perp, "perp", "hyperliquid", "perp venue")
	cmd.Flags().StringVar(&spot, "spot", "jupiter", "spot venue")
	cmd.Flags().StringVar(&inferenceRef, "inference", "", "inference provider:model (e.g. openrouter:anthropic/claude-sonnet-4.5)")
	cmd.Flags().StringVar(&modeStr, "mode", string(agent.ModePaper), "paper | live")
	cmd.Flags().StringVar(&networkStr, "network", string(agent.NetworkMainnet),
		"hyperliquid network: mainnet (real funding data) | testnet (sparse / synthetic)")
	cmd.Flags().StringVar(&universeCSV, "universe", "", "comma-separated symbols (e.g. WIF,BONK,POPCAT)")
	cmd.Flags().StringVar(&alloc, "alloc", "", "allocation in USDC")
	cmd.Flags().IntVar(&tickSecs, "tick-secs", 60, "tick interval seconds")
	cmd.Flags().StringVar(&configJSON, "config-json", "",
		`strategy-specific config as JSON (e.g. '{"entry_annualised_funding":0.05,"position_cap_usdc":100}')`)
	return cmd
}

func newAgentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List agents",
		RunE: func(c *cobra.Command, _ []string) error {
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := s.List(c.Context())
			if err != nil {
				return err
			}
			for _, a := range rows {
				fmt.Printf("%-12s  %-20s  %-20s  %-7s  %-7s  %-7s  alloc=%s  created=%s\n",
					a.ID, a.Name, a.Strategy, a.Mode, a.Network, a.Status, a.AllocationUSDC, a.CreatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func newAgentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "Show one agent's full state",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			a, err := s.Get(c.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("id:         %s\nname:       %s\nstrategy:   %s\nmode:       %s\nnetwork:    %s\nstatus:     %s\nperp:       %s\nspot:       %s\ninference:  %s\nuniverse:   %s\nalloc:      %s\ntick_secs:  %d\ncreated:    %s\n",
				a.ID, a.Name, a.Strategy, a.Mode, a.Network, a.Status, a.PerpVenue, a.SpotVenue,
				a.Inference, strings.Join(a.Universe, ","), a.AllocationUSDC, a.TickSecs, a.CreatedAt.Format(time.RFC3339))
			return nil
		},
	}
}

func newAgentDecisionsCmd() *cobra.Command {
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "decisions <id>",
		Short: "Show recent decisions for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			dur, err := time.ParseDuration(since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			rows, err := s.RecentDecisions(c.Context(), args[0], time.Now().Add(-dur), limit)
			if err != nil {
				return err
			}
			for _, r := range rows {
				fmt.Printf("%s  %s  cost=$%.4f  tokens(in/out)=%d/%d  %s\n",
					r.Time.Format(time.RFC3339), r.Model, r.CostUSD, r.TokensIn, r.TokensOut, r.Rationale)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "1h", "lookback duration")
	cmd.Flags().IntVar(&limit, "limit", 100, "max rows")
	return cmd
}

func newAgentSetModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-mode <id> <paper|live>",
		Short: "Switch an agent between paper and live mode",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			_, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			mode := agent.Mode(args[1])
			if mode != agent.ModePaper && mode != agent.ModeLive {
				return fmt.Errorf("mode must be paper or live")
			}
			res, err := db.Pool.Exec(c.Context(),
				`UPDATE agents SET mode = $1, updated_at = now() WHERE id = $2`,
				string(mode), args[0])
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("agent %q not found", args[0])
			}
			fmt.Printf("agent %s mode -> %s\n", args[0], mode)
			return nil
		},
	}
}

// newAgentRunCmd runs the full Runtime tick loop for ONE agent in the
// foreground. Paper-mode requires no signer (Hyperliquid funding is
// public); live-mode requires a Hyperliquid signer in the keystore AND
// the explicit --confirm-live flag (real money).
//
// Stops on SIGINT/SIGTERM or after --ticks N (whichever first).
func newAgentRunCmd() *cobra.Command {
	var (
		networkOverride string
		hlAddress       string
		maxTicks        int
		dryRun          bool
		confirmLive     bool
	)
	cmd := &cobra.Command{
		Use:   "run <id>",
		Short: "Run an agent's tick loop in the foreground (paper-mode only in v1)",
		Long: `Runs ONE agent's Strategy.Decide loop in the foreground, against the real
Hyperliquid public funding-rate API. Decisions and (paper) orders/swaps are
persisted to the database.

By default the agent runs on its stored network (--network on agent
create, or 'mainnet' if unspecified). Pass --network here to override
just for this run — useful for ad-hoc testnet smoke tests.

Stops on SIGINT/SIGTERM or after --ticks N (whichever comes first).`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			g := FromContext(c.Context())
			if g == nil {
				return errors.New("globals not initialised")
			}
			st, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			a, err := st.Get(c.Context(), args[0])
			if err != nil {
				return err
			}
			// Live mode requires a Hyperliquid signer in the keystore so
			// the runtime can sign real orders. Basis strategies (which
			// hedge a perp short with a Solana spot long) ALSO require a
			// Solana signer + Solana RPC so the spot leg can settle live.
			// Paper mode runs without any signer (funding-only reads).
			if a.Mode == agent.ModeLive {
				if !confirmLive {
					return fmt.Errorf("agent %q is mode=live; pass --confirm-live to acknowledge real-money risk", a.ID)
				}
				ks, err := openKeystore(c)
				if err != nil {
					return fmt.Errorf("live mode requires keystore: %w", err)
				}
				if _, err := ks.Signer(types.ChainHyperliquid); err != nil {
					return fmt.Errorf("live mode requires a hyperliquid signer in the keystore: %w", err)
				}
				// Basis-style strategies (those that need a spot leg) are
				// identified by the agent's stored SpotVenue. If set, the
				// runtime expects a Solana signer + RPC for the spot side.
				// This is a property of the agent's wiring, not the strategy
				// code, so the framework no longer special-cases by name.
				if a.SpotVenue != "" {
					if _, err := ks.Signer(types.ChainSolana); err != nil {
						return fmt.Errorf("live mode for agent %q (spot_venue=%s) requires a Solana signer: %w",
							a.ID, a.SpotVenue, err)
					}
					if g.Config.Solana.RPCURL == "" {
						return fmt.Errorf("live mode for agent %q (spot_venue=%s) requires solana.rpc_url in config",
							a.ID, a.SpotVenue)
					}
				}
			}

			reg, err := assets.LoadEmbedded()
			if err != nil {
				return err
			}
			// Keystore is best-effort here; funding rates work without one.
			ks, _ := openKeystore(c)
			// Empty networkOverride lets the builder fall back to a.Network
			// (which is the operator's stored choice, default mainnet).
			deps, err := agent.BuildDeps(a, reg, st, ks, g.Log, agent.BuildOptions{
				HyperliquidNetwork: networkOverride,
				HyperliquidAddress: hlAddress,
				Solana:             solanaSpotFromConfig(g.Config.Solana),
				EVM:                evmSpotsFromConfig(g.Config.EVM, os.Getenv),
			})
			if err != nil {
				return err
			}
			if dryRun {
				deps.Store = nil
				g.Log.Info("dry-run: no DB writes")
			}
			rt := agent.NewRuntime(a, deps)

			ctx, stop := signal.NotifyContext(c.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			effectiveNet := networkOverride
			if effectiveNet == "" {
				effectiveNet = string(a.Network.OrDefault(agent.NetworkMainnet))
			}
			g.Log.Info("agent run starting",
				"agent_id", a.ID, "strategy", a.Strategy, "mode", a.Mode,
				"tick_secs", a.TickSecs, "universe", a.Universe,
				"hyperliquid_network", effectiveNet)

			ticks := 0
			tickInterval := a.Interval()
			ticker := time.NewTicker(tickInterval)
			defer ticker.Stop()

			doTick := func() error {
				if _, err := rt.TickOnce(ctx); err != nil {
					g.Log.Error("tick failed", "err", err)
					return nil // don't bail the loop on transient errors
				}
				ticks++
				return nil
			}

			if err := doTick(); err != nil {
				return err
			}
			for {
				if maxTicks > 0 && ticks >= maxTicks {
					g.Log.Info("max ticks reached, exiting", "ticks", ticks)
					return nil
				}
				select {
				case <-ctx.Done():
					g.Log.Info("agent run stopping", "ticks", ticks)
					return nil
				case <-ticker.C:
					if err := doTick(); err != nil {
						return err
					}
				}
			}
		},
	}
	cmd.Flags().StringVar(&networkOverride, "network", "",
		"override the agent's stored network for this run (mainnet | testnet); empty = use agent record")
	cmd.Flags().StringVar(&hlAddress, "address", "", "hyperliquid address override (default: derived from keystore)")
	cmd.Flags().IntVar(&maxTicks, "ticks", 0, "stop after N ticks (0 = run until SIGINT)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not persist decisions/orders/swaps")
	cmd.Flags().BoolVar(&confirmLive, "confirm-live", false,
		"required for mode=live: acknowledge that real funds will be at risk")
	return cmd
}

// newAgentTickCmd runs a single Decide tick for an agent and persists the
// decision. Designed for paper-mode integration testing without waiting for
// the background scheduler. Defaults to a no-op perp venue with synthetic
// funding for whatever symbols the operator passes via --funding.
func newAgentTickCmd() *cobra.Command {
	var fundingArgs []string
	cmd := &cobra.Command{
		Use:   "tick <id>",
		Short: "Run one Strategy.Decide tick (paper-mode integration test)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			st, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			a, err := st.Get(c.Context(), args[0])
			if err != nil {
				return err
			}
			if a.Mode != agent.ModePaper {
				return fmt.Errorf("agent %q is mode=%q; tick is only safe for paper", a.ID, a.Mode)
			}

			strat, err := agent.BuildStrategy(a)
			if err != nil {
				return err
			}

			perp := &tickFundingVenue{
				Venue: exchangenoop.New(),
				rates: parseFundingArgs(fundingArgs),
			}
			deps := agent.Deps{
				Strategy: strat,
				Perp:     perp,
				Store:    st,
			}
			rt := agent.NewRuntime(a, deps)
			dec, err := rt.TickOnce(c.Context())
			if err != nil {
				return err
			}
			fmt.Printf("decision: confidence=%.2f swaps=%d orders=%d cancels=%d\n",
				dec.Confidence, len(dec.Swaps), len(dec.Orders), len(dec.Cancels))
			if dec.Notes != "" {
				fmt.Println("notes:")
				for _, line := range strings.Split(dec.Notes, "\n") {
					fmt.Println("  " + line)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&fundingArgs, "funding", nil,
		"synthetic funding rates as SYMBOL=ANNUAL_PCT (e.g. WIF=80,BONK=15). Repeatable or comma-separated.")
	return cmd
}

// (BuildStrategy + BuildHyperliquidVenue + helpers live in
// internal/agent/builder.go so the daemon supervisor and the CLI share one
// source of truth.)

// tickFundingVenue is a synthetic Venue for `agent tick`. It returns no
// positions / balances but does emit synthetic funding rates so the
// strategy has signal to act on.
type tickFundingVenue struct {
	*exchangenoop.Venue
	rates []agentFundingArg
}

// agentFundingArg is one parsed --funding entry.
type agentFundingArg struct {
	Symbol     string
	Annualised decimal.Decimal
}

func parseFundingArgs(args []string) []agentFundingArg {
	out := make([]agentFundingArg, 0, len(args))
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val, err := decimal.NewFromString(strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		// Convert percent → fraction (80 → 0.80).
		ann := val.Div(decimal.NewFromInt(100))
		out = append(out, agentFundingArg{
			Symbol:     strings.TrimSpace(parts[0]),
			Annualised: ann,
		})
	}
	return out
}

// FundingRates implements the Venue method we override; it returns synthetic
// per-hour rates derived from the requested annualised values. MarkPrice
// defaults to 1.0 so the strategy can size the perp leg (cap_usdc / mark)
// without an explicit price feed — fine for paper-mode plumbing tests.
func (v *tickFundingVenue) FundingRates(_ context.Context, _ []string) ([]types.FundingRate, error) {
	now := time.Now().UTC()
	hoursPerYear := decimal.NewFromInt(24 * 365)
	out := make([]types.FundingRate, 0, len(v.rates))
	for _, r := range v.rates {
		out = append(out, types.FundingRate{
			Time:      now,
			Venue:     "synthetic",
			Symbol:    r.Symbol,
			Rate:      r.Annualised.Div(hoursPerYear),
			Interval:  time.Hour,
			MarkPrice: decimal.NewFromInt(1),
		})
	}
	return out, nil
}

// Compile-time check.
var _ exchange.Venue = (*tickFundingVenue)(nil)

// solanaSpotFromConfig translates the cli config view into the agent
// builder's view (the agent package can't import config without an
// import cycle).
func solanaSpotFromConfig(cfg config.SolanaConfig) agent.SolanaSpot {
	return agent.SolanaSpot{
		RPCURL:                   cfg.RPCURL,
		JupiterBaseURL:           cfg.JupiterBaseURL,
		JupiterAPIKey:            cfg.JupiterAPIKey,
		SubmitMode:               cfg.SubmitMode,
		JitoBundleURL:            cfg.JitoBundleURL,
		PriorityFeeMicroLamports: cfg.PriorityFeeMicroLamports,
		ConfirmationTimeoutSecs:  cfg.ConfirmationTimeoutSecs,
	}
}

// evmSpotsFromConfig translates the cli config view into the agent
// builder's per-chain map. Unknown chain names in evm.chains.{name}
// are skipped with a no-op (operators are warned via a log line in
// the supervisor — here we silently drop to keep the helper pure).
//
// API key resolution: env var OneInchAPIKeyEnv wins over inline
// OneInchAPIKey; both empty disables EVM swaps entirely.
func evmSpotsFromConfig(cfg config.EVMConfig, getenv func(string) string) map[types.ChainID]agent.EVMSpot {
	if len(cfg.Chains) == 0 {
		return nil
	}
	apiKey := cfg.ResolvedOneInchAPIKey(getenv)
	if apiKey == "" {
		return nil
	}
	out := map[types.ChainID]agent.EVMSpot{}
	for name, c := range cfg.Chains {
		chain := types.ChainID(strings.ToLower(name))
		if !chain.IsEVM() {
			continue
		}
		out[chain] = agent.EVMSpot{
			RPCURL:                  c.RPCURL,
			OneInchAPIKey:           apiKey,
			OneInchBaseURL:          cfg.OneInchBaseURL,
			DefaultSlippageBps:      cfg.DefaultSlippageBps,
			ConfirmationTimeoutSecs: cfg.ConfirmationTimeoutSecs,
		}
	}
	return out
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
