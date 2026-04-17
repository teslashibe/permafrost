package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/store"
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
	)
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
