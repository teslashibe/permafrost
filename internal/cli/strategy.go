package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/backtest"
	"github.com/teslashibe/permafrost/pkg/strategy"

	// Register strategies that ship with the OSS framework. Local /
	// private strategies are registered from cmd/permafrostd; the CLI
	// only needs noop in scope so `strategy list` and `strategy backtest`
	// work out of the box on a fresh clone.
	_ "github.com/teslashibe/permafrost/strategies/noop"
)

func init() { addCommandFactory(newStrategyCmd) }

func newStrategyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "strategy",
		Short: "Inspect and backtest strategies",
	}
	cmd.AddCommand(newStrategyListCmd(), newStrategyBacktestCmd())
	return cmd
}

func newStrategyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered strategies",
		RunE: func(_ *cobra.Command, _ []string) error {
			for _, n := range strategy.List() {
				fmt.Println(n)
			}
			return nil
		},
	}
}

// newStrategyBacktestCmd runs a CSV of funding ticks through any registered
// strategy. The strategy is constructed via the registry (same path the
// daemon uses), so per-strategy config is supplied as a JSON blob — the
// same shape `agent create --config-json` accepts.
func newStrategyBacktestCmd() *cobra.Command {
	var (
		csvPath     string
		startingNAV string
		stepHours   int
		perpFeeBps  int
		swapFeeBps  int
		gasUSD      string
		slippageBps int
		configJSON  string
	)
	cmd := &cobra.Command{
		Use:   "backtest <strategy>",
		Short: "Replay a CSV of funding ticks against a registered strategy",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			if csvPath == "" {
				return errors.New("--csv is required")
			}
			ticks, err := backtest.LoadCSV(csvPath)
			if err != nil {
				return err
			}
			ctor, err := strategy.Get(name)
			if err != nil {
				return err
			}
			cfg := map[string]any{}
			if configJSON != "" {
				if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
					return fmt.Errorf("--config-json: %w", err)
				}
			}
			strat, err := ctor(cfg)
			if err != nil {
				return err
			}
			runner := backtest.NewRunner(strat, mustDec(startingNAV), time.Duration(stepHours)*time.Hour, backtest.Costs{
				PerpFeeBps:    perpFeeBps,
				SwapFeeBps:    swapFeeBps,
				GasUSDPerSwap: mustDec(gasUSD),
				SlippageBps:   slippageBps,
			})
			res, err := runner.Run(c.Context(), ticks)
			if err != nil {
				return err
			}
			printBacktestResult(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&csvPath, "csv", "", "path to CSV of funding ticks (header: time,symbol,rate,interval_seconds)")
	cmd.Flags().StringVar(&startingNAV, "starting-nav", "10000", "starting NAV in USDC")
	cmd.Flags().IntVar(&stepHours, "step-hours", 1, "simulation step interval (hours)")
	cmd.Flags().IntVar(&perpFeeBps, "perp-fee-bps", 4, "perp taker fee assumption (bps)")
	cmd.Flags().IntVar(&swapFeeBps, "swap-fee-bps", 25, "DEX fee assumption (bps)")
	cmd.Flags().StringVar(&gasUSD, "gas-usd", "0.05", "USD gas per swap")
	cmd.Flags().IntVar(&slippageBps, "slippage-bps", 10, "spot slippage assumption (bps)")
	cmd.Flags().StringVar(&configJSON, "config-json", "", "strategy-specific config as JSON (same shape as agent create)")
	return cmd
}

func printBacktestResult(r backtest.Result) {
	fmt.Printf("period:        %s → %s\n", r.Start.Format(time.RFC3339), r.End.Format(time.RFC3339))
	fmt.Printf("starting nav:  $%s\n", r.StartingNAV)
	fmt.Printf("ending nav:    $%s\n", r.EndingNAV)
	fmt.Printf("total return:  %s\n", r.TotalReturn)
	fmt.Printf("max drawdown:  %s\n", r.MaxDrawdown)
	fmt.Printf("decisions:     %d\n", r.NumDecisions)
	fmt.Printf("opens/closes:  %d / %d\n", r.NumOpens, r.NumCloses)
	fmt.Printf("funding:       $%s\n", r.TotalFunding)
	fmt.Printf("fees:          $%s\n", r.TotalFees)
	if len(r.Trades) > 0 {
		fmt.Println("\ntrade log (last 10):")
		from := 0
		if len(r.Trades) > 10 {
			from = len(r.Trades) - 10
		}
		for _, t := range r.Trades[from:] {
			fmt.Printf("  %s %-6s %-8s notional=%s funding=%s fees=%s pnl=%s\n",
				t.Time.Format(time.RFC3339), t.Action, t.Symbol, t.Notional, t.Funding, t.Fees, t.NetPnL)
		}
	}
}

// mustDec parses a decimal flag value. Backtest CLI inputs come from the
// operator on the command line; an unparseable value is a usage error.
var _ = decimal.Zero
