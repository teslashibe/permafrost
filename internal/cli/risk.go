package cli

import (
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/internal/types"
)

func init() { addCommandFactory(newRiskCmd) }

func newRiskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "risk",
		Short: "Inspect risk-engine state",
	}
	cmd.AddCommand(newRiskShowCmd())
	return cmd
}

func newRiskShowCmd() *cobra.Command {
	var (
		maxNotional, maxExposure, maxDailyLoss string
		maxSlippageBps, maxConcurrent          int
		maxDrawdownPct                         string
	)
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the active risk policy and its breakers",
		RunE: func(c *cobra.Command, _ []string) error {
			limits := types.RiskLimits{
				MaxNotionalPerLeg:      mustDec(maxNotional),
				MaxTotalBasisExposure:  mustDec(maxExposure),
				MaxDailyLoss:           mustDec(maxDailyLoss),
				MaxSpotSlippageBps:     maxSlippageBps,
				MaxConcurrentPositions: maxConcurrent,
			}
			breakers := []risk.CircuitBreaker{
				risk.MaxDrawdownBreaker{MaxFraction: mustDec(maxDrawdownPct)},
				risk.DailyLossBreaker{},
			}
			fmt.Println("limits:")
			fmt.Printf("  max_notional_per_leg:        %s\n", limits.MaxNotionalPerLeg)
			fmt.Printf("  max_total_basis_exposure:    %s\n", limits.MaxTotalBasisExposure)
			fmt.Printf("  max_daily_loss:              %s\n", limits.MaxDailyLoss)
			fmt.Printf("  max_spot_slippage_bps:       %d\n", limits.MaxSpotSlippageBps)
			fmt.Printf("  max_concurrent_positions:    %d\n", limits.MaxConcurrentPositions)
			fmt.Println("breakers:")
			for _, b := range breakers {
				fmt.Printf("  - %s\n", b.Name())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&maxNotional, "max-notional", "0", "USDC notional cap per leg")
	cmd.Flags().StringVar(&maxExposure, "max-exposure", "0", "USDC total basis exposure cap")
	cmd.Flags().StringVar(&maxDailyLoss, "max-daily-loss", "0", "USDC max daily loss before circuit breaker")
	cmd.Flags().IntVar(&maxSlippageBps, "max-slippage-bps", 0, "swap slippage cap (bps)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 0, "concurrent basis positions cap")
	cmd.Flags().StringVar(&maxDrawdownPct, "max-drawdown", "0.10", "NAV drawdown halt threshold (0..1)")
	return cmd
}

func mustDec(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}
