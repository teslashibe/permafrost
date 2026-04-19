package assets

import (
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

// TestUSDCMintFor_KnownChains: every chain we currently claim USDC support
// for returns a populated mint and decimals=6. Regression guard against a
// future entry being added with the wrong decimals or address format.
func TestUSDCMintFor_KnownChains(t *testing.T) {
	cases := []struct {
		chain     types.ChainID
		hasPrefix string // "0x" for EVM; "" for Solana base58
	}{
		{types.ChainSolana, ""},
		{types.ChainEthereum, "0x"},
		{types.ChainBase, "0x"},
		{types.ChainAvalanche, "0x"},
		{types.ChainBSC, "0x"},
	}
	for _, tc := range cases {
		t.Run(string(tc.chain), func(t *testing.T) {
			mint, dec, ok := USDCMintFor(tc.chain)
			if !ok {
				t.Fatalf("expected ok for %s", tc.chain)
			}
			if mint == "" {
				t.Errorf("empty mint for %s", tc.chain)
			}
			if tc.hasPrefix != "" && !strings.HasPrefix(mint, tc.hasPrefix) {
				t.Errorf("%s mint should start with %q; got %q", tc.chain, tc.hasPrefix, mint)
			}
			if dec != 6 {
				t.Errorf("%s decimals: got %d want 6", tc.chain, dec)
			}
		})
	}
}

// TestUSDCMintFor_UnknownChain: an unknown chain returns ok=false and
// callers should treat the leg as un-liquidatable.
func TestUSDCMintFor_UnknownChain(t *testing.T) {
	if _, _, ok := USDCMintFor(types.ChainID("imaginary-chain")); ok {
		t.Error("expected ok=false for unknown chain")
	}
}

// TestUSDCAsset_RoundTrip: USDCAsset returns a populated Asset for
// supported chains, with the right symbol + decimals.
func TestUSDCAsset_RoundTrip(t *testing.T) {
	for _, chain := range []types.ChainID{types.ChainSolana, types.ChainBase} {
		a, ok := USDCAsset(chain)
		if !ok {
			t.Fatalf("USDCAsset(%s): expected ok=true", chain)
		}
		if a.Symbol != "USDC" {
			t.Errorf("symbol: got %q want USDC", a.Symbol)
		}
		if a.Chain != chain {
			t.Errorf("chain: got %q want %q", a.Chain, chain)
		}
		if a.Decimals != 6 {
			t.Errorf("decimals: got %d want 6", a.Decimals)
		}
	}
}
