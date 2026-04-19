package cli

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/backtest"
	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	_ "github.com/teslashibe/permafrost/strategies/noop" // register noop
	fab "github.com/teslashibe/permafrost/strategies/private/funding_arb_basic"
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
			names := strategy.List()
			// funding_arb_basic is constructed manually (needs registry +
			// inference); list it explicitly so operators see what exists.
			has := false
			for _, n := range names {
				if n == fab.Name {
					has = true
					break
				}
			}
			if !has {
				names = append(names, fab.Name)
				sort.Strings(names)
			}
			for _, n := range names {
				fmt.Println(n)
			}
			return nil
		},
	}
}

func newStrategyBacktestCmd() *cobra.Command {
	var (
		csvPath        string
		startingNAV    string
		entryAnn       string
		exitAnn        string
		positionCap    string
		stepHours      int
		perpFeeBps     int
		swapFeeBps     int
		gasUSD         string
		slippageBps    int
	)
	cmd := &cobra.Command{
		Use:   "backtest funding_arb_basic --csv <path>",
		Short: "Replay a CSV of funding ticks against funding_arb_basic",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if args[0] != fab.Name {
				return fmt.Errorf("only strategy %q is backtest-able in v1", fab.Name)
			}
			if csvPath == "" {
				return errors.New("--csv is required")
			}
			ticks, err := backtest.LoadCSV(csvPath)
			if err != nil {
				return err
			}
			reg, err := assets.LoadEmbedded()
			if err != nil {
				return err
			}
			cfg := fab.Config{
				EntryAnnualisedFunding: mustDec(entryAnn),
				ExitAnnualisedFunding:  mustDec(exitAnn),
				PositionCapUSDC:        mustDec(positionCap),
				SlippageBps:            slippageBps,
			}
			strat, err := fab.New(cfg, reg, inference.Provider(nil))
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
	cmd.Flags().StringVar(&entryAnn, "entry", "0.50", "entry annualised funding (e.g. 0.50)")
	cmd.Flags().StringVar(&exitAnn, "exit", "0.10", "exit annualised funding (e.g. 0.10)")
	cmd.Flags().StringVar(&positionCap, "position-cap", "1000", "USDC notional per basis position")
	cmd.Flags().IntVar(&stepHours, "step-hours", 1, "simulation step interval (hours)")
	cmd.Flags().IntVar(&perpFeeBps, "perp-fee-bps", 4, "Hyperliquid taker fee assumption (bps)")
	cmd.Flags().IntVar(&swapFeeBps, "swap-fee-bps", 25, "DEX fee assumption (bps)")
	cmd.Flags().StringVar(&gasUSD, "gas-usd", "0.05", "USD gas per swap")
	cmd.Flags().IntVar(&slippageBps, "slippage-bps", 10, "spot slippage assumption (bps)")
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

// keep decimal in scope so future flag additions don't need re-importing.
var _ = decimal.Zero
