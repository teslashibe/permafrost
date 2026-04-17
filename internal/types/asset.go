// Package types holds the shared trading-domain types used across packages
// (strategy, exchange, swap, risk). Types here must be free of import cycles:
// they may not import any internal package other than each other.
package types

import "fmt"

// ChainID identifies a settlement venue or chain.
type ChainID string

const (
	ChainHyperliquid ChainID = "hyperliquid"
	ChainSolana      ChainID = "solana"
)

// Asset is a chain-bound token. For perpetual symbols traded on Hyperliquid,
// Mint holds the perp symbol (e.g. "WIF-PERP") and Decimals holds the on-venue
// quote decimals.
type Asset struct {
	Symbol   string  `json:"symbol"`   // human-readable: USDC, WIF, BONK
	Chain    ChainID `json:"chain"`    // settlement venue
	Mint     string  `json:"mint"`     // chain-specific identifier (mint, contract, perp symbol)
	Decimals int32   `json:"decimals"` // on-chain decimals
}

// String renders Asset as "<symbol>@<chain>".
func (a Asset) String() string {
	if a.Symbol == "" {
		return fmt.Sprintf("?@%s", a.Chain)
	}
	return fmt.Sprintf("%s@%s", a.Symbol, a.Chain)
}

// Equal reports whether two assets refer to the same token on the same chain.
// Symbol is informational; identity is determined by (Chain, Mint).
func (a Asset) Equal(b Asset) bool {
	return a.Chain == b.Chain && a.Mint == b.Mint
}
