// Package evm wraps the JSON-RPC, signing, and ERC-20 calldata pieces
// that Permafrost needs to settle spot legs on EVM chains via DEX
// aggregators (1inch, 0x, etc.).
//
// Design rule: this package speaks raw JSON-RPC and produces hex-encoded
// calldata only. It does NOT pull in go-ethereum's full client/abi
// machinery — calldata for the few ERC-20 selectors we use is hand-built
// in erc20.go. This keeps the dependency surface small and matches the
// philosophy of internal/chain/solana.
package evm

import (
	"fmt"
	"strings"

	"github.com/teslashibe/permafrost/pkg/types"
)

// Chain captures the static metadata for one EVM chain.
type Chain struct {
	ID          types.ChainID // permafrost-internal name (e.g. "base")
	NumericID   uint64        // EIP-155 chain id (e.g. 8453 for Base)
	Native      string        // native gas token ticker (e.g. "ETH", "BNB")
	NativeDec   int32         // native token decimals (always 18 for the chains we support today)
	Explorer    string        // human-readable tx explorer (with %s for the hash)
	GasModel    GasModel      // EIP-1559 vs legacy
	IsTestnet   bool          // true for Sepolia/Fuji/etc.
	DisplayName string        // for logs and UI ("Base mainnet")
}

// GasModel selects the transaction format used when sending raw txs.
type GasModel string

const (
	// GasModelEIP1559 uses (maxFeePerGas, maxPriorityFeePerGas) — the
	// post-London fee market. ETH mainnet, Base, Sepolia, Base Sepolia.
	GasModelEIP1559 GasModel = "eip1559"
	// GasModelLegacy uses gasPrice. AVAX C-chain and BSC still prefer
	// legacy (BSC supports 1559 but most RPCs and wallets default to
	// legacy; sticking with legacy avoids surprises on those chains).
	GasModelLegacy GasModel = "legacy"
)

// Chains is the registry of supported EVM chains. Mainnets first, then
// the canonical testnet for each.
var Chains = map[types.ChainID]Chain{
	types.ChainEthereum: {
		ID: types.ChainEthereum, NumericID: 1, Native: "ETH", NativeDec: 18,
		Explorer: "https://etherscan.io/tx/%s",
		GasModel: GasModelEIP1559, DisplayName: "Ethereum mainnet",
	},
	types.ChainBase: {
		ID: types.ChainBase, NumericID: 8453, Native: "ETH", NativeDec: 18,
		Explorer: "https://basescan.org/tx/%s",
		GasModel: GasModelEIP1559, DisplayName: "Base mainnet",
	},
	types.ChainAvalanche: {
		ID: types.ChainAvalanche, NumericID: 43114, Native: "AVAX", NativeDec: 18,
		Explorer: "https://snowtrace.io/tx/%s",
		GasModel: GasModelLegacy, DisplayName: "Avalanche C-Chain",
	},
	types.ChainBSC: {
		ID: types.ChainBSC, NumericID: 56, Native: "BNB", NativeDec: 18,
		Explorer: "https://bscscan.com/tx/%s",
		GasModel: GasModelLegacy, DisplayName: "BNB Smart Chain",
	},
}

// Testnets is a separate map so production code can't accidentally swap
// in a testnet by typoing a config value. The signer + RPC + ERC-20
// pieces work identically on testnets — only the aggregator (1inch) is
// mainnet-only, which is why we test signing/RPC on testnets and
// aggregator e2e on mainnet with hard caps.
var Testnets = map[string]Chain{
	"sepolia": {
		ID: "sepolia", NumericID: 11155111, Native: "ETH", NativeDec: 18,
		Explorer: "https://sepolia.etherscan.io/tx/%s",
		GasModel: GasModelEIP1559, IsTestnet: true, DisplayName: "Ethereum Sepolia",
	},
	"base-sepolia": {
		ID: "base-sepolia", NumericID: 84532, Native: "ETH", NativeDec: 18,
		Explorer: "https://sepolia.basescan.org/tx/%s",
		GasModel: GasModelEIP1559, IsTestnet: true, DisplayName: "Base Sepolia",
	},
	"fuji": {
		ID: "fuji", NumericID: 43113, Native: "AVAX", NativeDec: 18,
		Explorer: "https://testnet.snowtrace.io/tx/%s",
		GasModel: GasModelLegacy, IsTestnet: true, DisplayName: "Avalanche Fuji",
	},
	"bsc-testnet": {
		ID: "bsc-testnet", NumericID: 97, Native: "BNB", NativeDec: 18,
		Explorer: "https://testnet.bscscan.com/tx/%s",
		GasModel: GasModelLegacy, IsTestnet: true, DisplayName: "BSC testnet",
	},
}

// LookupChain returns the Chain for an internal ChainID. It accepts both
// canonical mainnet names and the testnet shorthand keys above.
func LookupChain(id types.ChainID) (Chain, error) {
	if c, ok := Chains[id]; ok {
		return c, nil
	}
	if c, ok := Testnets[strings.ToLower(string(id))]; ok {
		return c, nil
	}
	return Chain{}, fmt.Errorf("evm: unknown chain %q", id)
}

// ExplorerURL formats a tx URL for the chain. Empty if no explorer is
// configured.
func (c Chain) ExplorerURL(txHash string) string {
	if c.Explorer == "" {
		return ""
	}
	return fmt.Sprintf(c.Explorer, txHash)
}
