package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/exchange"
	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/internal/store"
	"github.com/teslashibe/permafrost/internal/types"
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
		newAgentStopCmd(),
		newAgentTickCmd(),
		newAgentRunCmd(),
	)
	return cmd
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
		id, name, strategy, perp, spot, inferenceRef, modeStr string
		universeCSV                                            string
		alloc                                                  string
		tickSecs                                               int
		configJSON                                             string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a strategy agent (paper mode by default)",
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
			fmt.Printf("created agent id=%s name=%s strategy=%s mode=%s alloc=%s\n",
				out.ID, out.Name, out.Strategy, out.Mode, out.AllocationUSDC)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "agent id (defaults to a generated value)")
	cmd.Flags().StringVar(&name, "name", "", "human-readable label")
	cmd.Flags().StringVar(&strategy, "strategy", "funding_arb_basic", "registered strategy name")
	cmd.Flags().StringVar(&perp, "perp", "hyperliquid", "perp venue")
	cmd.Flags().StringVar(&spot, "spot", "jupiter", "spot venue")
	cmd.Flags().StringVar(&inferenceRef, "inference", "", "inference provider:model (e.g. openrouter:anthropic/claude-sonnet-4.5)")
	cmd.Flags().StringVar(&modeStr, "mode", string(agent.ModePaper), "paper | live")
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
				fmt.Printf("%-12s  %-20s  %-20s  %-7s  %-7s  alloc=%s  created=%s\n",
					a.ID, a.Name, a.Strategy, a.Mode, a.Status, a.AllocationUSDC, a.CreatedAt.Format(time.RFC3339))
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
			fmt.Printf("id:         %s\nname:       %s\nstrategy:   %s\nmode:       %s\nstatus:     %s\nperp:       %s\nspot:       %s\ninference:  %s\nuniverse:   %s\nalloc:      %s\ntick_secs:  %d\ncreated:    %s\n",
				a.ID, a.Name, a.Strategy, a.Mode, a.Status, a.PerpVenue, a.SpotVenue,
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
// foreground. Designed for paper-mode end-to-end testing against the real
// Hyperliquid funding API: no signer required (Hyperliquid funding is
// public). Stops on SIGINT/SIGTERM or after --ticks N (whichever first).
//
// For live mode this command will refuse to start unless a signer is
// configured. The supervisor-driven multi-agent runner is a v1.1 concern.
func newAgentRunCmd() *cobra.Command {
	var (
		network    string
		hlAddress  string
		maxTicks   int
		dryRun     bool
	)
	cmd := &cobra.Command{
		Use:   "run <id>",
		Short: "Run an agent's tick loop in the foreground (paper-mode only in v1)",
		Long: `Runs ONE agent's Strategy.Decide loop in the foreground, against the real
Hyperliquid public funding-rate API. Decisions and (paper) orders/swaps are
persisted to the database.

This is the proper end-to-end paper-mode flow — equivalent to what a
multi-agent daemon will do once supervisor-driven agent loading lands.

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
			if a.Mode != agent.ModePaper {
				return fmt.Errorf("agent %q is mode=%q; `agent run` is paper-mode only in v1", a.ID, a.Mode)
			}

			reg, err := assets.LoadEmbedded()
			if err != nil {
				return err
			}
			// Keystore is best-effort here; funding rates work without one.
			ks, _ := openKeystore(c)
			deps, err := agent.BuildDeps(a, reg, st, ks, g.Log, agent.BuildOptions{
				HyperliquidNetwork: network,
				HyperliquidAddress: hlAddress,
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

			g.Log.Info("agent run starting",
				"agent_id", a.ID, "strategy", a.Strategy, "mode", a.Mode,
				"tick_secs", a.TickSecs, "universe", a.Universe,
				"hyperliquid_network", network)

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
	cmd.Flags().StringVar(&network, "network", "testnet", "hyperliquid network: mainnet | testnet")
	cmd.Flags().StringVar(&hlAddress, "address", "", "hyperliquid address override (default: derived from keystore)")
	cmd.Flags().IntVar(&maxTicks, "ticks", 0, "stop after N ticks (0 = run until SIGINT)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not persist decisions/orders/swaps")
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

			reg, err := assets.LoadEmbedded()
			if err != nil {
				return err
			}
			strat, err := agent.BuildStrategy(a, reg)
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
// per-hour rates derived from the requested annualised values.
func (v *tickFundingVenue) FundingRates(_ context.Context, _ []string) ([]types.FundingRate, error) {
	now := time.Now().UTC()
	hoursPerYear := decimal.NewFromInt(24 * 365)
	out := make([]types.FundingRate, 0, len(v.rates))
	for _, r := range v.rates {
		out = append(out, types.FundingRate{
			Time:     now,
			Venue:    "synthetic",
			Symbol:   r.Symbol,
			Rate:     r.Annualised.Div(hoursPerYear),
			Interval: time.Hour,
		})
	}
	return out, nil
}

// Compile-time check.
var _ exchange.Venue = (*tickFundingVenue)(nil)

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
