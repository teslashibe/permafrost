package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/agent"
	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/pnl"
)

func init() { addCommandFactory(newPnLCmd) }

func newPnLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pnl",
		Short:   "Show P&L and NAV for one or all agents",
		Aliases: []string{"pl"},
	}
	cmd.AddCommand(newPnLSummaryCmd())
	cmd.AddCommand(newPnLPositionsCmd())
	cmd.AddCommand(newPnLHistoryCmd())
	return cmd
}

// ─── pnl summary ────────────────────────────────────────────────────────────

func newPnLSummaryCmd() *cobra.Command {
	var (
		agentID  string
		jsonOut  bool
		liveMark bool
	)
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Per-agent NAV summary (one row per agent)",
		Long: `Reads the most recent NAV snapshot for each agent and prints a
summary table.

By default uses the persisted snapshot (cheap; matches what the daemon
last computed). Pass --live to recompute against current marks — useful
when the daemon hasn't ticked recently or you want a fresh number.`,
		RunE: func(c *cobra.Command, _ []string) error {
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := c.Context()

			var agents []agent.Agent
			if agentID != "" {
				a, err := s.Get(ctx, agentID)
				if err != nil {
					return err
				}
				agents = []agent.Agent{a}
			} else {
				agents, err = s.List(ctx)
				if err != nil {
					return err
				}
			}

			rows := make([]pnlSummaryRow, 0, len(agents))
			for _, a := range agents {
				row, err := summaryFor(c, ctx, s, a, liveMark)
				if err != nil {
					return err
				}
				rows = append(rows, row)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].AgentID < rows[j].AgentID })

			if jsonOut {
				return writeJSON(rows)
			}
			renderSummary(rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "limit to a specific agent id (default: all agents)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&liveMark, "live", false, "recompute NAV against live marks instead of using the latest snapshot")
	return cmd
}

type pnlSummaryRow struct {
	AgentID            string          `json:"agent_id"`
	Strategy           string          `json:"strategy"`
	Mode               string          `json:"mode"`
	Status             string          `json:"status"`
	OpenPositions      int             `json:"open_positions"`
	NAVUSDC            decimal.Decimal `json:"nav_usdc"`
	RealizedUSDC       decimal.Decimal `json:"realized_usdc"`
	UnrealizedUSDC     decimal.Decimal `json:"unrealized_usdc"`
	GasUSDC            decimal.Decimal `json:"gas_usdc"`
	SnapshotAt         time.Time       `json:"snapshot_at"`
	Source             string          `json:"source"` // "snapshot" | "live"
}

func summaryFor(c *cobra.Command, ctx context.Context, s *agent.Store, a agent.Agent, live bool) (pnlSummaryRow, error) {
	row := pnlSummaryRow{
		AgentID:  a.ID,
		Strategy: a.Strategy,
		Mode:     string(a.Mode),
		Status:   string(a.Status),
	}
	if live {
		nav, err := computeLiveNAV(c, ctx, s, a)
		if err != nil {
			return row, err
		}
		row.NAVUSDC = nav.NAVUSDC
		row.RealizedUSDC = nav.RealizedPnLUSDC
		row.UnrealizedUSDC = nav.NAVUSDC.Sub(nav.RealizedPnLUSDC)
		row.GasUSDC = nav.CumulativeGasUSDC
		row.OpenPositions = nav.OpenPositions
		row.SnapshotAt = nav.Time
		row.Source = "live"
		return row, nil
	}
	snap, err := s.LatestNAVSnapshot(ctx, a.ID)
	if err != nil {
		return row, err
	}
	if snap == nil {
		row.Source = "none"
		return row, nil
	}
	row.NAVUSDC = snap.NAVUSDC
	row.RealizedUSDC = snap.RealizedPnLUSDC
	row.UnrealizedUSDC = snap.NAVUSDC.Sub(snap.RealizedPnLUSDC)
	row.GasUSDC = snap.CumulativeGasUSDC
	row.OpenPositions = snap.OpenPositions
	row.SnapshotAt = snap.Time
	row.Source = "snapshot"
	return row, nil
}

func renderSummary(rows []pnlSummaryRow) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tSTRATEGY\tMODE\tSTATUS\tOPEN\tREALIZED\tUNREALIZED\tNAV\tGAS\tSOURCE\tAGE")
	for _, r := range rows {
		age := "—"
		if !r.SnapshotAt.IsZero() {
			age = humanAge(time.Since(r.SnapshotAt))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.AgentID, r.Strategy, r.Mode, r.Status, r.OpenPositions,
			fmtUSDC(r.RealizedUSDC), fmtUSDC(r.UnrealizedUSDC),
			fmtUSDC(r.NAVUSDC), fmtUSDC(r.GasUSDC), r.Source, age,
		)
	}
	_ = w.Flush()
}

// ─── pnl positions ──────────────────────────────────────────────────────────

func newPnLPositionsCmd() *cobra.Command {
	var (
		agentID string
		jsonOut bool
		live    bool
	)
	cmd := &cobra.Command{
		Use:   "positions <agent-id>",
		Short: "Per-position breakdown for an agent (open basis positions)",
		RunE: func(c *cobra.Command, args []string) error {
			if agentID == "" && len(args) == 1 {
				agentID = args[0]
			}
			if agentID == "" {
				return errors.New("provide --agent or pass the agent id as an argument")
			}
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := c.Context()

			a, err := s.Get(ctx, agentID)
			if err != nil {
				return err
			}

			var positions []pnl.BasisValuation
			if live {
				nav, err := computeLiveNAV(c, ctx, s, a)
				if err != nil {
					return err
				}
				positions = nav.Positions
			} else {
				snap, err := s.LatestNAVSnapshot(ctx, agentID)
				if err != nil {
					return err
				}
				if snap == nil {
					fmt.Println("no NAV snapshot yet — run a tick or use --live")
					return nil
				}
				_ = json.Unmarshal(snap.PositionsJSON, &positions)
			}

			if jsonOut {
				return writeJSON(positions)
			}
			renderPositions(positions)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent id (alias for positional arg)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON instead of a table")
	cmd.Flags().BoolVar(&live, "live", false, "recompute against live marks")
	return cmd
}

func renderPositions(positions []pnl.BasisValuation) {
	if len(positions) == 0 {
		fmt.Println("(no open positions)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BASIS\tCHAIN\tSPOT QTY\tCOST USDC\tSPOT VAL\tPERP UPNL\tFUNDING\tGAS\tNET P&L\tSTATUS")
	for _, p := range positions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Underlying, p.Chain,
			fmtAmount(p.SpotQty),
			fmtUSDC(p.SpotCostBasisUSDC),
			fmtUSDC(p.SpotValueUSDC),
			fmtUSDC(p.PerpUnrealizedUSDC),
			fmtUSDC(p.FundingAccruedUSDC),
			fmtUSDC(p.GasPaidUSDC),
			fmtUSDC(p.NetUnrealizedUSDC),
			p.Status,
		)
	}
	_ = w.Flush()
}

// ─── pnl history ────────────────────────────────────────────────────────────

func newPnLHistoryCmd() *cobra.Command {
	var (
		agentID string
		since   string
		limit   int
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "history <agent-id>",
		Short: "NAV time series for an agent",
		RunE: func(c *cobra.Command, args []string) error {
			if agentID == "" && len(args) == 1 {
				agentID = args[0]
			}
			if agentID == "" {
				return errors.New("provide --agent or pass the agent id")
			}
			d, err := time.ParseDuration(since)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			s, db, err := openAgentStore(c)
			if err != nil {
				return err
			}
			defer db.Close()

			rows, err := s.NAVHistory(c.Context(), agentID, time.Now().Add(-d), limit)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(rows)
			}
			if len(rows) == 0 {
				fmt.Println("(no NAV snapshots in window)")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIME\tNAV\tREALIZED\tUNREALIZED\tOPEN\tGAS")
			for _, r := range rows {
				unrealized := r.NAVUSDC.Sub(r.RealizedPnLUSDC)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
					r.Time.Format(time.RFC3339),
					fmtUSDC(r.NAVUSDC),
					fmtUSDC(r.RealizedPnLUSDC),
					fmtUSDC(unrealized),
					r.OpenPositions,
					fmtUSDC(r.CumulativeGasUSDC),
				)
			}
			_ = w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent id (alias for positional arg)")
	cmd.Flags().StringVar(&since, "since", "24h", "lookback window (Go duration)")
	cmd.Flags().IntVar(&limit, "limit", 1_000, "max rows")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON")
	return cmd
}

// ─── shared helpers ─────────────────────────────────────────────────────────

// computeLiveNAV runs the same valuation the runtime persists, but
// against current state. Used by --live flags so operators can get a
// fresh number without waiting for the next tick.
func computeLiveNAV(c *cobra.Command, ctx context.Context, s *agent.Store, a agent.Agent) (pnl.AgentNAV, error) {
	g := FromContext(ctx)
	if g == nil {
		return pnl.AgentNAV{}, errors.New("globals not initialised")
	}
	reg, err := assets.LoadEmbedded()
	if err != nil {
		return pnl.AgentNAV{}, err
	}
	ks, _ := openKeystore(c) // best-effort
	deps, err := agent.BuildDeps(a, reg, s, ks, g.Log, agent.BuildOptions{
		Solana: solanaSpotFromConfig(g.Config.Solana),
		EVM:    evmSpotsFromConfig(g.Config.EVM, os.Getenv),
	})
	if err != nil {
		return pnl.AgentNAV{}, err
	}
	hist, err := s.AggregatesForAgent(ctx, a.ID)
	if err != nil {
		return pnl.AgentNAV{}, err
	}
	open, err := s.LoadOpenBasis(ctx, a.ID)
	if err != nil {
		return pnl.AgentNAV{}, err
	}
	return pnl.New(deps.Perp, deps.Swaps).ValueAgent(ctx, a.ID, pnl.History{
		OpenPositions:     open,
		RealizedPnLUSDC:   hist.RealizedPnLUSDC,
		CumulativeGasUSDC: hist.CumulativeGasUSDC,
	})
}

// fmtUSDC renders a decimal as a $-prefixed 4dp string with sign for
// negative values. Zero shows as "$0.0000".
func fmtUSDC(d decimal.Decimal) string {
	if d.IsZero() {
		return "$0.0000"
	}
	sign := ""
	if d.IsNegative() {
		sign = "-"
		d = d.Abs()
	}
	return sign + "$" + d.StringFixed(4)
}

// fmtAmount renders a non-USDC amount with adaptive precision.
func fmtAmount(d decimal.Decimal) string {
	if d.IsZero() {
		return "0"
	}
	s := d.StringFixed(8)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// humanAge condenses a Duration for the SOURCE column.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
