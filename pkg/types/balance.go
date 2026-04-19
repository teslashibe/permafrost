package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// Balance is a free/locked breakdown of a single asset on a single venue.
type Balance struct {
	Asset  string          `json:"asset"`   // e.g. "USDC"
	Venue  string          `json:"venue"`   // venue name (hyperliquid, solana, ...)
	Free   decimal.Decimal `json:"free"`
	Locked decimal.Decimal `json:"locked"`
}

// Total returns Free + Locked.
func (b Balance) Total() decimal.Decimal { return b.Free.Add(b.Locked) }

// WalletBalance is a chain-bound balance held in a self-custodied wallet.
// Distinct from Balance because wallet balances have no concept of "locked"
// in the orderbook sense; everything held is freely transferable.
type WalletBalance struct {
	Chain     ChainID         `json:"chain"`
	Address   string          `json:"address"`
	Asset     Asset           `json:"asset"`
	Amount    decimal.Decimal `json:"amount"`
	UpdatedAt time.Time       `json:"updated_at"`
}
