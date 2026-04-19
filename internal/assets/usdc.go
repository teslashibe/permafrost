package assets

import "github.com/teslashibe/permafrost/pkg/types"

// USDCMintFor returns the canonical USDC mint / contract address for a
// supported chain, plus the canonical decimals (6 on every supported
// chain today). Returns (address, decimals=6, true) when the chain is
// known; ("", 0, false) otherwise.
//
// Hard-coded sources (verified at the time of writing):
//   - Solana   — Circle's native USDC mint
//   - Ethereum — https://www.circle.com/usdc-multichain/ethereum
//   - Base     — Circle's native USDC on Base (NOT USDbC, which is bridged)
//   - Avalanche — Circle's native USDC on Avalanche C-Chain
//   - BSC      — Binance-Peg USDC (BSC has no native USDC; pegged is the
//               de-facto liquid one)
//
// Lives in internal/assets so framework code (the killswitch's
// LiquidateSpot path) can use it without importing a strategy.
// Strategies that need the same mapping should call USDCMintFor too
// rather than maintaining their own copy.
func USDCMintFor(chain types.ChainID) (mint string, decimals int32, ok bool) {
	switch chain {
	case types.ChainSolana:
		return "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", 6, true
	case types.ChainEthereum:
		return "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 6, true
	case types.ChainBase:
		return "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", 6, true
	case types.ChainAvalanche:
		return "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E", 6, true
	case types.ChainBSC:
		return "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", 6, true
	}
	return "", 0, false
}

// USDCAsset returns a populated types.Asset for USDC on the given chain.
// Caller-friendly wrapper around USDCMintFor; returns false when the
// chain isn't supported.
func USDCAsset(chain types.ChainID) (types.Asset, bool) {
	mint, dec, ok := USDCMintFor(chain)
	if !ok {
		return types.Asset{}, false
	}
	return types.Asset{Symbol: "USDC", Chain: chain, Mint: mint, Decimals: dec}, true
}
