package risk

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/types"
)

// CircuitBreaker is a portfolio-level check that may halt the agent.
type CircuitBreaker interface {
	Name() string
	Check(snap types.PortfolioSnapshot, limits types.RiskLimits, drawdown decimal.Decimal) types.Verdict
}

// MaxDrawdownBreaker halts the agent if NAV has fallen by more than
// MaxFraction of HWM.
type MaxDrawdownBreaker struct {
	MaxFraction decimal.Decimal // 0.10 = halt at 10% drawdown
}

func (b MaxDrawdownBreaker) Name() string { return "max_drawdown" }

func (b MaxDrawdownBreaker) Check(_ types.PortfolioSnapshot, _ types.RiskLimits, dd decimal.Decimal) types.Verdict {
	if b.MaxFraction.IsZero() {
		return allow()
	}
	if dd.GreaterThanOrEqual(b.MaxFraction) {
		return block(b.Name(),
			fmt.Sprintf("drawdown %s >= limit %s", dd, b.MaxFraction))
	}
	if dd.GreaterThanOrEqual(b.MaxFraction.Mul(decimal.NewFromFloat(0.8))) {
		return warn(b.Name(),
			fmt.Sprintf("drawdown %s within 80%% of halt threshold", dd))
	}
	return allow()
}

// DailyLossBreaker halts the agent if today's PnL is below -MaxLossUSDC.
type DailyLossBreaker struct{}

func (DailyLossBreaker) Name() string { return "daily_loss" }

func (DailyLossBreaker) Check(snap types.PortfolioSnapshot, limits types.RiskLimits, _ decimal.Decimal) types.Verdict {
	if limits.MaxDailyLoss.IsZero() {
		return allow()
	}
	loss := snap.DailyPnL.Neg()
	if loss.GreaterThanOrEqual(limits.MaxDailyLoss) {
		return block("daily_loss",
			fmt.Sprintf("today's loss %s >= limit %s", loss, limits.MaxDailyLoss))
	}
	return allow()
}

// FundingFlipBreaker emits a Warn (not Block) when an open basis position's
// funding has flipped negative — the strategy should consider closing.
type FundingFlipBreaker struct {
	// FundingByPositionID maps a basis position id to its current funding
	// rate (per-interval, fractional). Supplied by the agent runtime.
	FundingByPositionID map[string]decimal.Decimal
}

func (FundingFlipBreaker) Name() string { return "funding_flip" }

func (b FundingFlipBreaker) Check(snap types.PortfolioSnapshot, _ types.RiskLimits, _ decimal.Decimal) types.Verdict {
	for _, p := range snap.OpenBasis {
		rate, ok := b.FundingByPositionID[p.ID]
		if !ok {
			continue
		}
		if rate.IsNegative() {
			return warn("funding_flip",
				fmt.Sprintf("position %s funding=%s is negative", p.ID, rate))
		}
	}
	return allow()
}
