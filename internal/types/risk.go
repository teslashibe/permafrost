package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// RiskLimits captures the hard limits applied to a single agent. All values
// are evaluated in USDC unless noted.
type RiskLimits struct {
	MaxNotionalPerLeg       decimal.Decimal `json:"max_notional_per_leg"`
	MaxTotalBasisExposure   decimal.Decimal `json:"max_total_basis_exposure"`
	MaxSwapsPerMin          int             `json:"max_swaps_per_min"`
	MaxOrdersPerMin         int             `json:"max_orders_per_min"`
	MaxDailyLoss            decimal.Decimal `json:"max_daily_loss"`
	MaxSpotSlippageBps      int             `json:"max_spot_slippage_bps"`
	MaxEntryBasisBps        int             `json:"max_entry_basis_bps"`
	MaxConcurrentPositions  int             `json:"max_concurrent_positions"`
}

// VerdictKind is the outcome of a risk check.
type VerdictKind string

const (
	VerdictAllow VerdictKind = "allow"
	VerdictWarn  VerdictKind = "warn"
	VerdictBlock VerdictKind = "block"
)

// Verdict is what the risk engine returns for a check. Reason is a short
// machine-friendly tag (e.g. "max_notional"); Detail is for humans/logs.
type Verdict struct {
	Kind   VerdictKind `json:"kind"`
	Reason string      `json:"reason,omitempty"`
	Detail string      `json:"detail,omitempty"`
}

// IsBlock reports whether this verdict prohibits the action.
func (v Verdict) IsBlock() bool { return v.Kind == VerdictBlock }

// PortfolioSnapshot is the consolidated view passed to risk checks.
type PortfolioSnapshot struct {
	Time           time.Time
	NAV            decimal.Decimal
	HighWaterMark  decimal.Decimal
	DailyPnL       decimal.Decimal
	OpenBasis      []BasisPosition
	PerpPositions  []Position
	WalletBalances []WalletBalance
	VenueBalances  []Balance
}

// TotalBasisExposure sums |notional| across open basis positions, valued at
// each position's perp leg avg-entry. Returns USDC.
func (s PortfolioSnapshot) TotalBasisExposure() decimal.Decimal {
	total := decimal.Zero
	for _, p := range s.OpenBasis {
		for _, leg := range p.Legs {
			if leg.Kind == BasisLegPerp {
				total = total.Add(leg.AvgPrice.Mul(leg.Qty).Abs())
				break
			}
		}
	}
	return total
}
