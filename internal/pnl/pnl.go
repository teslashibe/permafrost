// Package pnl values open basis positions at the current market and
// produces per-agent NAV snapshots.
//
// Design rule: this package is read-only with respect to chain state —
// it never broadcasts. The Engine pulls live marks from the configured
// venues (HL for perps, SwapVenue for spot) and combines them with
// historical state from the agent.Store to produce a single AgentNAV.
//
// For paper-mode agents that don't have live venues wired, the Engine
// degrades gracefully: spot legs are valued at cost basis and perp
// unrealized is zero. The numbers are honest about what we don't know.
package pnl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/types"
)

// Engine values open basis positions and aggregates per-agent NAV.
//
// Perp / Swaps are optional. When a venue is missing, the Engine
// fills the corresponding fields with zero (NOT an error) and sets
// the appropriate ValuationStatus on the per-position result.
type Engine struct {
	Perp  exchange.Venue
	Swaps map[types.ChainID]swap.SwapVenue
}

// New constructs an Engine. Both fields may be nil for fully degraded
// (paper-mode) operation.
func New(perp exchange.Venue, swaps map[types.ChainID]swap.SwapVenue) *Engine {
	return &Engine{Perp: perp, Swaps: swaps}
}

// SwapVenueFor returns the venue routed for the given chain or nil.
func (e *Engine) SwapVenueFor(c types.ChainID) swap.SwapVenue {
	if e == nil || e.Swaps == nil {
		return nil
	}
	return e.Swaps[c]
}

// ValuationStatus describes how confident we are in a Valuation.
type ValuationStatus string

const (
	// ValuationOK means both legs were marked at live venues.
	ValuationOK ValuationStatus = "ok"
	// ValuationDegradedSpot means we couldn't quote the spot leg
	// (no SwapVenue or quote failed) and used cost basis instead.
	ValuationDegradedSpot ValuationStatus = "spot_at_cost"
	// ValuationDegradedPerp means we couldn't fetch HL position data
	// (no Perp venue or no Address) and used zero unrealized.
	ValuationDegradedPerp ValuationStatus = "perp_no_unrealized"
	// ValuationDegradedBoth means neither leg could be marked.
	ValuationDegradedBoth ValuationStatus = "both_at_cost"
)

// BasisValuation values one open basis position at the current market.
//
// Sign conventions (USDC, all):
//   - SpotValueUSDC      ≥ 0
//   - SpotCostBasisUSDC  ≥ 0
//   - PerpUnrealizedUSDC may be positive or negative (for a short, positive
//     when the mark dropped below entry)
//   - FundingAccruedUSDC may be positive (received) or negative (paid)
//   - GasPaidUSDC        ≥ 0  (always a cost)
//   - NetUnrealizedUSDC  may be positive or negative — the bottom line if
//     the position closed RIGHT NOW
//     = (SpotValue − SpotCostBasis) + PerpUnrealized + FundingAccrued − GasPaid
type BasisValuation struct {
	BasisKey            string          `json:"basis_key"`
	Underlying          string          `json:"underlying"`
	Chain               types.ChainID   `json:"chain"`
	OpenedAt            time.Time       `json:"opened_at"`
	SpotQty             decimal.Decimal `json:"spot_qty"`
	SpotMarkUSDC        decimal.Decimal `json:"spot_mark_usdc"`
	SpotValueUSDC       decimal.Decimal `json:"spot_value_usdc"`
	SpotCostBasisUSDC   decimal.Decimal `json:"spot_cost_basis_usdc"`
	PerpSize            decimal.Decimal `json:"perp_size"`
	PerpEntryPrice      decimal.Decimal `json:"perp_entry_price"`
	PerpMarkUSDC        decimal.Decimal `json:"perp_mark_usdc"`
	PerpUnrealizedUSDC  decimal.Decimal `json:"perp_unrealized_usdc"`
	FundingAccruedUSDC  decimal.Decimal `json:"funding_accrued_usdc"`
	GasPaidUSDC         decimal.Decimal `json:"gas_paid_usdc"`
	NetUnrealizedUSDC   decimal.Decimal `json:"net_unrealized_usdc"`
	Status              ValuationStatus `json:"status"`
}

// AgentNAV is the snapshot persisted to agent_nav_snapshots.
type AgentNAV struct {
	Time                 time.Time        `json:"time"`
	AgentID              string           `json:"agent_id"`
	NAVUSDC              decimal.Decimal  `json:"nav_usdc"`
	SpotValueUSDC        decimal.Decimal  `json:"spot_value_usdc"`
	PerpUnrealizedUSDC   decimal.Decimal  `json:"perp_unrealized_usdc"`
	FundingAccruedUSDC   decimal.Decimal  `json:"funding_accrued_usdc"`
	RealizedPnLUSDC      decimal.Decimal  `json:"realized_pnl_usdc"`
	CumulativeGasUSDC    decimal.Decimal  `json:"cumulative_gas_usdc"`
	OpenPositions        int              `json:"open_positions"`
	Positions            []BasisValuation `json:"positions"`
}

// History is the input the Engine needs from the agent.Store: cumulative
// realized numbers plus the open positions to value. Decoupled here so
// the engine doesn't import internal/agent (avoids cycle: agent → pnl
// for runtime hookup; pnl → agent for store would loop).
type History struct {
	OpenPositions     []types.BasisPosition
	RealizedPnLUSDC   decimal.Decimal // cumulative since agent inception
	CumulativeGasUSDC decimal.Decimal // gas + fees on every swap/order ever
}

// ValueAgent computes per-position valuations and the aggregate NAV.
// Errors only when an underlying RPC fails; missing venues are treated
// as a degraded (but successful) result.
func (e *Engine) ValueAgent(ctx context.Context, agentID string, hist History) (AgentNAV, error) {
	now := time.Now().UTC()
	out := AgentNAV{
		Time:              now,
		AgentID:           agentID,
		RealizedPnLUSDC:   hist.RealizedPnLUSDC,
		CumulativeGasUSDC: hist.CumulativeGasUSDC,
		OpenPositions:     len(hist.OpenPositions),
		Positions:         make([]BasisValuation, 0, len(hist.OpenPositions)),
	}

	// Pull HL positions once per call so we don't hammer the API per basis.
	// Map perp_symbol → HL position. nil map means "no perp data available".
	perpBySymbol := map[string]types.Position{}
	if e != nil && e.Perp != nil {
		positions, err := e.Perp.Positions(ctx)
		if err == nil {
			for _, p := range positions {
				perpBySymbol[p.Symbol] = p
			}
		}
		// silently ignore err — degraded valuation is fine
	}

	for _, p := range hist.OpenPositions {
		v, err := e.valueOne(ctx, p, perpBySymbol)
		if err != nil {
			return AgentNAV{}, fmt.Errorf("value basis %s: %w", p.Underlying, err)
		}
		out.Positions = append(out.Positions, v)
		out.SpotValueUSDC = out.SpotValueUSDC.Add(v.SpotValueUSDC)
		out.PerpUnrealizedUSDC = out.PerpUnrealizedUSDC.Add(v.PerpUnrealizedUSDC)
		out.FundingAccruedUSDC = out.FundingAccruedUSDC.Add(v.FundingAccruedUSDC)
	}

	// NAV = realized + unrealized + funding − gas + spot value held
	// We DON'T add SpotValueUSDC to NAV directly because it includes the
	// cost basis we already spent — that's not new equity. Instead NAV
	// reflects the marked value of the open positions (spot value −
	// cost basis = unrealized spot P&L) plus the perp unrealized.
	openUnrealized := decimal.Zero
	for _, v := range out.Positions {
		openUnrealized = openUnrealized.Add(v.NetUnrealizedUSDC)
	}
	out.NAVUSDC = hist.RealizedPnLUSDC.Add(openUnrealized)

	// Stable per-position ordering for predictable JSONB and CLI output.
	sort.Slice(out.Positions, func(i, j int) bool {
		return out.Positions[i].Underlying < out.Positions[j].Underlying
	})
	return out, nil
}

func (e *Engine) valueOne(ctx context.Context, p types.BasisPosition, perpBySymbol map[string]types.Position) (BasisValuation, error) {
	v := BasisValuation{
		BasisKey:   p.Underlying,
		Underlying: p.Underlying,
		OpenedAt:   p.OpenedAt,
		GasPaidUSDC: p.RealizedGas,
		FundingAccruedUSDC: p.RealizedFunding,
	}

	spotLeg, perpLeg, ok := splitLegs(p.Legs)
	if !ok {
		return v, errors.New("basis position must have spot + perp legs")
	}
	v.Chain = spotLeg.Asset.Chain
	v.SpotCostBasisUSDC = spotLeg.Qty // USDC committed
	v.PerpSize = perpLeg.Qty
	v.PerpEntryPrice = perpLeg.AvgPrice

	// Spot leg valuation: quote token → USDC at current price.
	// Spot quantity (in tokens) is approximated as the perp leg's size,
	// matching funding_arb_basic's delta-neutral 1:1 sizing. The strategy
	// always holds spot_qty ≈ perp_size; if they diverge in future
	// strategies we'll thread a real BaseQty through BasisLeg.
	v.SpotQty = perpLeg.Qty
	swapVenue := e.SwapVenueFor(spotLeg.Asset.Chain)
	gotSpotMark := false
	if swapVenue != nil && v.SpotQty.IsPositive() {
		usdc := types.Asset{Symbol: "USDC", Chain: spotLeg.Asset.Chain, Decimals: 6}
		q, err := swapVenue.Quote(ctx, types.QuoteRequest{
			InToken:  spotLeg.Asset,
			OutToken: usdc,
			Amount:   v.SpotQty,
			Mode:     types.QuoteExactIn,
		})
		if err == nil && q.OutAmount.IsPositive() {
			v.SpotValueUSDC = q.OutAmount
			if v.SpotQty.IsPositive() {
				v.SpotMarkUSDC = q.OutAmount.Div(v.SpotQty)
			}
			gotSpotMark = true
		}
	}
	if !gotSpotMark {
		// Degraded: assume spot is worth what we paid.
		v.SpotValueUSDC = v.SpotCostBasisUSDC
		if v.SpotQty.IsPositive() {
			v.SpotMarkUSDC = v.SpotCostBasisUSDC.Div(v.SpotQty)
		}
	}

	// Perp leg valuation: prefer HL's UnrealizedPnl (it accounts for
	// funding-paid-during-position correctly). Fall back to entry−mark
	// math if no HL data is available.
	gotPerp := false
	if hp, ok := perpBySymbol[perpLeg.Symbol]; ok {
		v.PerpMarkUSDC = markFromPosition(hp)
		v.PerpUnrealizedUSDC = hp.UnrealizedPx
		gotPerp = true
	}

	v.Status = decideStatus(gotSpotMark, gotPerp)

	v.NetUnrealizedUSDC = v.SpotValueUSDC.Sub(v.SpotCostBasisUSDC).
		Add(v.PerpUnrealizedUSDC).
		Add(v.FundingAccruedUSDC).
		Sub(v.GasPaidUSDC)
	return v, nil
}

// markFromPosition extracts the implied mark from a HL position:
// mark = entry + (unrealized / qty). Returns the entry price if qty is
// zero (defensive — shouldn't happen for an open position).
func markFromPosition(p types.Position) decimal.Decimal {
	if p.Qty.IsZero() {
		return p.EntryPrice
	}
	return p.EntryPrice.Add(p.UnrealizedPx.Div(p.Qty))
}

func splitLegs(legs []types.BasisLeg) (spot, perp types.BasisLeg, ok bool) {
	for _, l := range legs {
		switch l.Kind {
		case types.BasisLegSpot:
			spot = l
		case types.BasisLegPerp:
			perp = l
		}
	}
	return spot, perp, !spot.Asset.Chain.IsSpotChain() == false || perp.Symbol != ""
}

func decideStatus(gotSpot, gotPerp bool) ValuationStatus {
	switch {
	case gotSpot && gotPerp:
		return ValuationOK
	case gotSpot && !gotPerp:
		return ValuationDegradedPerp
	case !gotSpot && gotPerp:
		return ValuationDegradedSpot
	default:
		return ValuationDegradedBoth
	}
}
