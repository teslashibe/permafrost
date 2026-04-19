package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// Position is a per-symbol position on a single venue (typically Hyperliquid).
type Position struct {
	Venue        string          `json:"venue"`
	Symbol       string          `json:"symbol"`
	Qty          decimal.Decimal `json:"qty"`            // signed: positive = long, negative = short
	EntryPrice   decimal.Decimal `json:"entry_price"`    // VWAP entry
	MarkPrice    decimal.Decimal `json:"mark_price"`
	LiqPrice     decimal.Decimal `json:"liq_price"`
	Leverage     decimal.Decimal `json:"leverage"`
	Margin       decimal.Decimal `json:"margin"`
	UnrealizedPx decimal.Decimal `json:"upnl"`
	RealizedPx   decimal.Decimal `json:"rpnl"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// IsLong reports whether the position is net long.
func (p Position) IsLong() bool { return p.Qty.IsPositive() }

// IsShort reports whether the position is net short.
func (p Position) IsShort() bool { return p.Qty.IsNegative() }

// IsFlat reports whether the position is closed.
func (p Position) IsFlat() bool { return p.Qty.IsZero() }

// BasisPositionState models the lifecycle of a paired (spot + perp) position.
type BasisPositionState string

const (
	BasisStateOpening BasisPositionState = "opening"
	BasisStateOpen    BasisPositionState = "open"
	BasisStateClosing BasisPositionState = "closing"
	BasisStateBroken  BasisPositionState = "broken"
	BasisStateClosed  BasisPositionState = "closed"
)

// BasisLeg is one side of a basis position. Kind disambiguates spot vs perp.
type BasisLeg struct {
	Kind     BasisLegKind    `json:"kind"`
	Asset    Asset           `json:"asset"`
	Symbol   string          `json:"symbol"`              // perp symbol (perp leg) or "" (spot leg)
	Qty      decimal.Decimal `json:"qty"`                 // base-asset units, unsigned
	AvgPrice decimal.Decimal `json:"avg_price"`           // entry VWAP in USDC
}

// BasisLegKind labels a leg as spot (long, on-chain) or perp (short, venue).
type BasisLegKind string

const (
	BasisLegSpot BasisLegKind = "spot"
	BasisLegPerp BasisLegKind = "perp"
)

// BasisPosition is a logical paired position spanning two symbols.
// PnL only makes sense at the pair level. The agent runtime persists these
// to the strategy_positions table; per-leg detail lives in the orders, fills,
// and swaps tables linked by PositionID.
type BasisPosition struct {
	ID               string             `json:"id"`
	AgentID          string             `json:"agent_id"`
	Underlying       string             `json:"underlying"`        // canonical symbol, e.g. "WIF"
	State            BasisPositionState `json:"state"`
	Legs             []BasisLeg         `json:"legs"`
	OpenedAt         time.Time          `json:"opened_at"`
	ClosedAt         *time.Time         `json:"closed_at,omitempty"`
	RealizedFunding  decimal.Decimal    `json:"realized_funding"`
	RealizedBasisPnL decimal.Decimal    `json:"realized_basis_pnl"`
	RealizedFees     decimal.Decimal    `json:"realized_fees"`
	RealizedGas      decimal.Decimal    `json:"realized_gas"`
}

// NetPnL returns realized funding minus fees and gas plus realized basis PnL.
func (b BasisPosition) NetPnL() decimal.Decimal {
	return b.RealizedFunding.
		Add(b.RealizedBasisPnL).
		Sub(b.RealizedFees).
		Sub(b.RealizedGas)
}
