// Package backtest provides a deterministic time-stepped harness that
// replays historical funding-rate data against any strategy.Strategy and
// reports realised PnL, decision counts, and a NAV curve.
//
// Scope is intentionally narrow: spot price moves are NOT simulated (we
// assume the basis is held delta-neutral, so spot+perp PnL nets out and
// only funding flows generate P&L). Fees and slippage are modelled as
// flat per-trade costs; gas is included as a per-swap charge.
package backtest

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/types"
)

// FundingTick is one observation in the input series.
//
// MarkPrice is optional; if zero, MarketSnapshotAt fills in a sentinel
// $1.00 mark so strategies that gate on mark price (e.g. funding_arb_basic
// which sizes perp legs as cap_usdc / mark) keep working in synthetic
// backtests. Real CSVs from venue history should include the mark.
type FundingTick struct {
	Time      time.Time
	Symbol    string
	Rate      decimal.Decimal // per-interval, fractional
	Interval  time.Duration   // typically 1h or 8h
	MarkPrice decimal.Decimal // optional; defaults to 1.00 if zero
}

// Costs models the per-trade frictions applied during the simulation.
type Costs struct {
	PerpFeeBps     int             // taker fee on Hyperliquid in bps
	SwapFeeBps     int             // DEX fee in bps
	GasUSDPerSwap  decimal.Decimal // priority fee + base gas per swap
	SlippageBps    int             // simulated slippage on spot legs (bps)
}

// Defaults applies conservative defaults.
func (c *Costs) Defaults() {
	if c.PerpFeeBps == 0 {
		c.PerpFeeBps = 4 // 4 bps taker on HL
	}
	if c.SwapFeeBps == 0 {
		c.SwapFeeBps = 25 // 0.25% typical Solana DEX
	}
	if c.GasUSDPerSwap.IsZero() {
		c.GasUSDPerSwap = decimal.NewFromFloat(0.05)
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 10
	}
}

// Trade is one backtested swap+order pair.
type Trade struct {
	Time     time.Time
	Action   string          // "open" | "close"
	Symbol   string
	Notional decimal.Decimal
	Funding  decimal.Decimal // funding paid/received over the position's lifetime
	Fees     decimal.Decimal // perp fees + swap fees + gas + slippage
	NetPnL   decimal.Decimal // funding - fees
}

// Result summarises a backtest.
type Result struct {
	Start          time.Time
	End            time.Time
	StartingNAV    decimal.Decimal
	EndingNAV      decimal.Decimal
	TotalReturn    decimal.Decimal // (End/Start) - 1
	MaxDrawdown    decimal.Decimal // fraction
	NumDecisions   int
	NumOpens       int
	NumCloses      int
	TotalFunding   decimal.Decimal
	TotalFees      decimal.Decimal
	NAVCurve       []NAVPoint
	Trades         []Trade
}

// NAVPoint is one entry in the NAV curve.
type NAVPoint struct {
	Time time.Time
	NAV  decimal.Decimal
}

// MarketSnapshotAt builds a strategy MarketSnapshot from a slice of ticks at
// time t (the most recent tick per symbol with Time <= t wins). Exposed for
// test reuse.
func MarketSnapshotAt(t time.Time, ticks []FundingTick) types.MarketSnapshot {
	defaultMark := decimal.NewFromInt(1) // sentinel so strategies can size
	snap := types.MarketSnapshot{Time: t, Symbols: map[string]types.SymbolSnap{}}
	for _, tk := range ticks {
		if tk.Time.After(t) {
			continue
		}
		cur, ok := snap.Symbols[tk.Symbol]
		if ok && tk.Time.Before(cur.Funding.Time) {
			continue
		}
		mark := tk.MarkPrice
		if mark.IsZero() {
			mark = defaultMark
		}
		snap.Symbols[tk.Symbol] = types.SymbolSnap{
			Funding: types.FundingRate{
				Time:      tk.Time,
				Symbol:    tk.Symbol,
				Rate:      tk.Rate,
				Interval:  tk.Interval,
				MarkPrice: mark,
			},
		}
	}
	return snap
}
