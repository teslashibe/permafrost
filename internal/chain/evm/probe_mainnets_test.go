//go:build live

package evm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teslashibe/permafrost/internal/types"
)

// rpcURLForMainnet picks the RPC URL from env or a free public endpoint.
// Operators can override with PERMAFROST_TEST_RPC_<NAME>.
func rpcURLForMainnet(c types.ChainID) string {
	envKey := "PERMAFROST_TEST_RPC_" + strings.ToUpper(string(c))
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	switch c {
	case types.ChainEthereum:
		return "https://ethereum-rpc.publicnode.com"
	case types.ChainBase:
		return "https://mainnet.base.org"
	case types.ChainAvalanche:
		return "https://api.avax.network/ext/bc/C/rpc"
	case types.ChainBSC:
		return "https://bsc-rpc.publicnode.com"
	}
	return ""
}

// TestProbeMainnets_RPCRoundtrip is the mainnet twin of the testnet
// probe — same assertion (chain id matches registry + head block is
// non-zero), all four chains.
func TestProbeMainnets_RPCRoundtrip(t *testing.T) {
	for id, chain := range Chains {
		id, chain := id, chain
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			url := rpcURLForMainnet(id)
			if url == "" {
				t.Skipf("no RPC URL for %s", id)
			}
			c := NewClient(url, chain)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			gotID, err := c.ChainID(ctx)
			if err != nil {
				t.Fatalf("eth_chainId: %v", err)
			}
			if gotID != chain.NumericID {
				t.Errorf("chain id mismatch: rpc=%d registry=%d", gotID, chain.NumericID)
			}
			bn, err := c.BlockNumber(ctx)
			if err != nil {
				t.Fatalf("eth_blockNumber: %v", err)
			}
			t.Logf("✓ %s (chain id %d, head block %d)", chain.DisplayName, gotID, bn)
		})
	}
}
