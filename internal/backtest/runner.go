package backtest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/strategy"
	"github.com/teslashibe/permafrost/internal/types"
)

// Runner replays a series of FundingTicks through a Strategy in time order,
// stepping at the supplied StepInterval. At each step it builds a
// MarketSnapshot from the ticks visible up to that time, calls
// Strategy.Decide, and applies the returned intents to a simulated
// portfolio.
type Runner struct {
	Strategy     strategy.Strategy
	StepInterval time.Duration   // typically 1h
	StartingNAV  decimal.Decimal // USDC
	Costs        Costs
}

// NewRunner constructs a Runner with sane defaults applied.
func NewRunner(s strategy.Strategy, startingNAV decimal.Decimal, step time.Duration, costs Costs) *Runner {
	if step == 0 {
		step = time.Hour
	}
	if startingNAV.IsZero() {
		startingNAV = decimal.NewFromInt(10_000)
	}
	costs.Defaults()
	return &Runner{
		Strategy:     s,
		StepInterval: step,
		StartingNAV:  startingNAV,
		Costs:        costs,
	}
}

// Run replays ticks (which may be unsorted) and returns the simulation
// result. The simulation runs from min(ticks.Time) to max(ticks.Time)
// (rounded to StepInterval). The provided context is honoured between
// steps.
func (r *Runner) Run(ctx context.Context, ticks []FundingTick) (Result, error) {
	if r.Strategy == nil {
		return Result{}, fmt.Errorf("backtest: strategy is required")
	}
	if len(ticks) == 0 {
		return Result{}, fmt.Errorf("backtest: no ticks supplied")
	}
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].Time.Before(ticks[j].Time) })

	start := ticks[0].Time.Truncate(r.StepInterval)
	end := ticks[len(ticks)-1].Time.Truncate(r.StepInterval).Add(r.StepInterval)

	pf := newPortfolio(r.StartingNAV)
	res := Result{Start: start, EndingNAV: r.StartingNAV, StartingNAV: r.StartingNAV}

	for t := start; !t.After(end); t = t.Add(r.StepInterval) {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		// Accrue funding on open positions for this step.
		stepFunding := pf.accrueFunding(t, ticks, r.StepInterval)
		res.TotalFunding = res.TotalFunding.Add(stepFunding)

		// Build snapshot, run Decide.
		snap := MarketSnapshotAt(t, ticks)
		dec, err := r.Strategy.Decide(ctx, strategy.DecisionInput{
			AgentID:        "backtest",
			Now:            t,
			Market:         snap,
			BasisPositions: pf.openPositions(),
		})
		if err != nil {
			return res, fmt.Errorf("decide at %s: %w", t.Format(time.RFC3339), err)
		}
		res.NumDecisions++

		// Apply: closes first (they're idempotent if already gone), then opens.
		for _, o := range dec.Orders {
			if o.ReduceOnly {
				if tr, ok := pf.close(t, o.Symbol, r.Costs); ok {
					res.NumCloses++
					res.TotalFees = res.TotalFees.Add(tr.Fees)
					res.Trades = append(res.Trades, tr)
				}
			}
		}
		for _, sw := range dec.Swaps {
			// Opens have OutToken != USDC.
			if sw.OutToken.Symbol == "USDC" {
				continue
			}
			if tr, ok := pf.open(t, sw.OutToken.Symbol, sw.InAmount, r.Costs); ok {
				res.NumOpens++
				res.TotalFees = res.TotalFees.Add(tr.Fees)
				res.Trades = append(res.Trades, tr)
			}
		}

		nav := pf.nav()
		res.NAVCurve = append(res.NAVCurve, NAVPoint{Time: t, NAV: nav})
		res.EndingNAV = nav
	}

	res.End = end
	if !r.StartingNAV.IsZero() {
		res.TotalReturn = res.EndingNAV.Div(r.StartingNAV).Sub(decimal.NewFromInt(1))
	}
	res.MaxDrawdown = computeMaxDrawdown(res.NAVCurve)
	return res, nil
}

// computeMaxDrawdown returns the largest peak-to-trough decline as a fraction.
func computeMaxDrawdown(curve []NAVPoint) decimal.Decimal {
	if len(curve) == 0 {
		return decimal.Zero
	}
	peak := curve[0].NAV
	maxDD := decimal.Zero
	for _, p := range curve[1:] {
		if p.NAV.GreaterThan(peak) {
			peak = p.NAV
		} else if peak.IsPositive() {
			dd := peak.Sub(p.NAV).Div(peak)
			if dd.GreaterThan(maxDD) {
				maxDD = dd
			}
		}
	}
	return maxDD
}

// portfolio is the in-memory state of the simulation.
type portfolio struct {
	cash      decimal.Decimal
	positions map[string]*openPosition
}

type openPosition struct {
	OpenedAt    time.Time
	Symbol      string
	Notional    decimal.Decimal
	Funding     decimal.Decimal // accumulated USDC funding on this position
	OpenFees    decimal.Decimal
}

func newPortfolio(startingNAV decimal.Decimal) *portfolio {
	return &portfolio{cash: startingNAV, positions: map[string]*openPosition{}}
}

func (p *portfolio) nav() decimal.Decimal {
	v := p.cash
	for _, op := range p.positions {
		v = v.Add(op.Notional).Add(op.Funding)
	}
	return v
}

func (p *portfolio) openPositions() []types.BasisPosition {
	out := make([]types.BasisPosition, 0, len(p.positions))
	for _, op := range p.positions {
		out = append(out, types.BasisPosition{
			ID:               "bt:" + op.Symbol,
			Underlying:       op.Symbol,
			State:            types.BasisStateOpen,
			Legs: []types.BasisLeg{
				{Kind: types.BasisLegPerp, Symbol: op.Symbol, Qty: op.Notional, AvgPrice: decimal.NewFromInt(1)},
				{Kind: types.BasisLegSpot, Qty: op.Notional, AvgPrice: decimal.NewFromInt(1)},
			},
			OpenedAt:        op.OpenedAt,
			RealizedFunding: op.Funding,
		})
	}
	return out
}

// accrueFunding walks ticks for the elapsed step and credits funding to
// each open position. For a SHORT perp the convention is that POSITIVE
// funding rate means the short receives funding (longs pay shorts).
func (p *portfolio) accrueFunding(t time.Time, ticks []FundingTick, step time.Duration) decimal.Decimal {
	if len(p.positions) == 0 {
		return decimal.Zero
	}
	stepFunding := decimal.Zero
	stepStart := t.Add(-step)
	for _, tk := range ticks {
		if tk.Time.Before(stepStart) || !tk.Time.Before(t) {
			continue
		}
		op, ok := p.positions[tk.Symbol]
		if !ok {
			continue
		}
		// short receives `rate * notional` per interval; partial intervals
		// are pro-rated by step/interval.
		intervals := decimal.NewFromInt(int64(step)).Div(decimal.NewFromInt(int64(tk.Interval)))
		amt := op.Notional.Mul(tk.Rate).Mul(intervals)
		op.Funding = op.Funding.Add(amt)
		stepFunding = stepFunding.Add(amt)
	}
	return stepFunding
}

// open enters a new basis position if not already open. Returns the trade
// row (with open fees applied) and whether the open actually happened.
func (p *portfolio) open(t time.Time, symbol string, notional decimal.Decimal, costs Costs) (Trade, bool) {
	if _, ok := p.positions[symbol]; ok {
		return Trade{}, false
	}
	if notional.IsZero() {
		return Trade{}, false
	}
	fees := openFees(notional, costs)
	if p.cash.LessThan(notional.Add(fees)) {
		return Trade{}, false // not enough cash to open this position
	}
	p.cash = p.cash.Sub(notional).Sub(fees)
	op := &openPosition{
		OpenedAt: t,
		Symbol:   symbol,
		Notional: notional,
		OpenFees: fees,
	}
	p.positions[symbol] = op
	return Trade{
		Time: t, Action: "open", Symbol: symbol,
		Notional: notional, Fees: fees,
	}, true
}

// close exits an open position, returning the realised PnL (funding minus
// open + close fees).
func (p *portfolio) close(t time.Time, symbol string, costs Costs) (Trade, bool) {
	op, ok := p.positions[symbol]
	if !ok {
		return Trade{}, false
	}
	closeFee := closeFees(op.Notional, costs)
	totalFees := op.OpenFees.Add(closeFee)
	delete(p.positions, symbol)
	p.cash = p.cash.Add(op.Notional).Add(op.Funding).Sub(closeFee)
	return Trade{
		Time: t, Action: "close", Symbol: symbol,
		Notional: op.Notional, Funding: op.Funding,
		Fees: totalFees, NetPnL: op.Funding.Sub(totalFees),
	}, true
}

func openFees(notional decimal.Decimal, c Costs) decimal.Decimal {
	bps := decimal.NewFromInt(int64(c.PerpFeeBps + c.SwapFeeBps + c.SlippageBps))
	return notional.Mul(bps).Div(decimal.NewFromInt(10_000)).Add(c.GasUSDPerSwap)
}

func closeFees(notional decimal.Decimal, c Costs) decimal.Decimal {
	bps := decimal.NewFromInt(int64(c.PerpFeeBps + c.SwapFeeBps + c.SlippageBps))
	return notional.Mul(bps).Div(decimal.NewFromInt(10_000)).Add(c.GasUSDPerSwap)
}

