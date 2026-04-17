package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// QuoteMode controls whether a quote fixes the input or output amount.
type QuoteMode string

const (
	QuoteExactIn  QuoteMode = "exact_in"
	QuoteExactOut QuoteMode = "exact_out"
)

// QuoteRequest asks a SwapVenue to price a swap.
type QuoteRequest struct {
	InToken  Asset
	OutToken Asset
	Amount   decimal.Decimal
	Mode     QuoteMode
}

// Quote is the venue's response to a QuoteRequest. Implementations may
// include opaque routing data in Raw which must be passed back unchanged
// when calling Swap.
type Quote struct {
	InToken      Asset           `json:"in_token"`
	OutToken     Asset           `json:"out_token"`
	InAmount     decimal.Decimal `json:"in_amount"`
	OutAmount    decimal.Decimal `json:"out_amount"`     // expected, before slippage
	PriceImpact  decimal.Decimal `json:"price_impact"`   // 0..1 fraction
	Route        string          `json:"route"`          // e.g. "Raydium → Orca"
	ExpiresAt    time.Time       `json:"expires_at"`
	Raw          []byte          `json:"raw,omitempty"`  // opaque payload returned to Swap
}

// SwapIntent is a Strategy-issued instruction to acquire or dispose of an
// on-chain asset. The agent runtime requests a Quote, validates it against
// risk, then submits the Swap and waits for confirmation. ClientID is set by
// the runtime for idempotency.
type SwapIntent struct {
	Chain        ChainID         `json:"chain"`
	InToken      Asset           `json:"in_token"`
	OutToken     Asset           `json:"out_token"`
	InAmount     decimal.Decimal `json:"in_amount"`
	MinOutAmount decimal.Decimal `json:"min_out_amount"`  // hard slippage floor
	SlippageBps  int             `json:"slippage_bps"`    // soft target slippage
	ClientID     string          `json:"client_id"`
	PositionID   string          `json:"position_id"`
	DecisionID   string          `json:"decision_id"`
	Slot         int             `json:"slot"`
	Tag          string          `json:"tag,omitempty"`
}

// TxHash is a chain-specific transaction identifier (signature on Solana,
// hex on EVM). Stored as string for cross-chain ergonomics.
type TxHash string

// SwapStatus is the lifecycle state of a submitted swap.
type SwapStatus string

const (
	SwapStatusPending   SwapStatus = "pending"
	SwapStatusConfirmed SwapStatus = "confirmed"
	SwapStatusFailed    SwapStatus = "failed"
)

// SwapResult reports the on-chain outcome of a submitted Swap.
type SwapResult struct {
	TxHash       TxHash          `json:"tx_hash"`
	Status       SwapStatus      `json:"status"`
	Slot         uint64          `json:"slot,omitempty"`     // Solana slot, if applicable
	BlockTime    time.Time       `json:"block_time"`
	InAmount     decimal.Decimal `json:"in_amount"`
	OutAmount    decimal.Decimal `json:"out_amount"`         // actual received
	GasNative    decimal.Decimal `json:"gas_native"`         // native units (lamports/wei)
	GasUSD       decimal.Decimal `json:"gas_usd"`            // priced at submission time
	Error        string          `json:"error,omitempty"`
}
