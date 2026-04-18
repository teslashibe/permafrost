// Package types holds the shared trading-domain types used across packages
// (strategy, exchange, swap, risk). Types here must be free of import cycles:
// they may not import any internal package other than each other.
package types

import "fmt"

// ChainID identifies a settlement venue or chain.
//
// Conventions:
//   - lowercase short name
//   - perp venues are still ChainIDs (Hyperliquid)
//   - EVM chain ids match the human-readable name (ethereum/base/avalanche/bsc),
//     not the numeric chain id; the numeric id lives in chain/evm/chains.go.
type ChainID string

const (
	ChainHyperliquid ChainID = "hyperliquid"
	ChainSolana      ChainID = "solana"

	// EVM chains supported for spot legs via DEX aggregators.
	ChainEthereum  ChainID = "ethereum"
	ChainBase      ChainID = "base"
	ChainAvalanche ChainID = "avalanche"
	ChainBSC       ChainID = "bsc"
)

// IsEVM reports whether the chain settles via the EVM execution model
// (so the EVM signer + JSON-RPC stack apply).
func (c ChainID) IsEVM() bool {
	switch c {
	case ChainEthereum, ChainBase, ChainAvalanche, ChainBSC:
		return true
	}
	return false
}

// IsSpotChain reports whether the chain hosts spot tokens that the
// strategy can route through (i.e. has a SwapVenue adapter).
func (c ChainID) IsSpotChain() bool {
	return c == ChainSolana || c.IsEVM()
}

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
