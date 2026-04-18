//go:build live

// Live testnet probes. Run with:
//
//	go test -tags=live -v ./internal/chain/evm/... -run Probe
//
// These tests hit real public RPC endpoints on EVM testnets and verify
// that our JSON-RPC client + chain registry round-trip cleanly. They do
// NOT send transactions (that requires a funded faucet wallet); the
// signer roundtrip is exercised by the `ProbeSignerSendsTinyTx` test
// below, which is gated on a separately-set env var so it's opt-in.
package evm

import (
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// rpcURLForTestnet picks the RPC URL from env or a sensible default.
// Operators can override with PERMAFROST_TEST_RPC_<NAME>.
func rpcURLForTestnet(name string) string {
	envKey := "PERMAFROST_TEST_RPC_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	switch name {
	case "sepolia":
		return "https://ethereum-sepolia-rpc.publicnode.com"
	case "base-sepolia":
		return "https://sepolia.base.org"
	case "fuji":
		return "https://api.avax-test.network/ext/bc/C/rpc"
	case "bsc-testnet":
		return "https://bsc-testnet-rpc.publicnode.com"
	}
	return ""
}

// ProbeTestnets_RPCRoundtrip walks every testnet and asserts that the
// chain id reported by the RPC matches what our static registry expects.
// This is the simplest, most useful "the wiring works" check.
func TestProbeTestnets_RPCRoundtrip(t *testing.T) {
	for name, chain := range Testnets {
		name, chain := name, chain
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			url := rpcURLForTestnet(name)
			if url == "" {
				t.Skipf("no RPC URL for %s; set PERMAFROST_TEST_RPC_%s",
					name, strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
			}
			c := NewClient(url, chain)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			id, err := c.ChainID(ctx)
			if err != nil {
				t.Fatalf("eth_chainId failed: %v", err)
			}
			if id != chain.NumericID {
				t.Errorf("chain id mismatch: rpc=%d registry=%d", id, chain.NumericID)
			}
			bn, err := c.BlockNumber(ctx)
			if err != nil {
				t.Fatalf("eth_blockNumber failed: %v", err)
			}
			if bn == 0 {
				t.Error("block number is zero — RPC stale?")
			}
			t.Logf("✓ %s (chain id %d, head block %d)", chain.DisplayName, id, bn)
		})
	}
}

// ProbeTestnets_SignerSendsTinyTx is a more involved check: it derives
// the wallet address from PERMAFROST_TEST_PRIVATE_KEY (32-byte hex),
// reports the native balance on each testnet, and — if PERMAFROST_TEST_BROADCAST=1
// — sends a 0-value self-transfer to verify end-to-end signing+broadcast.
//
// To run: fund the test address from a faucet (we log the address) and
// then re-run with PERMAFROST_TEST_BROADCAST=1.
func TestProbeTestnets_SignerSendsTinyTx(t *testing.T) {
	keyHex := os.Getenv("PERMAFROST_TEST_PRIVATE_KEY")
	if keyHex == "" {
		t.Skip("set PERMAFROST_TEST_PRIVATE_KEY (32-byte hex) to run this probe")
	}
	keyHex = strings.TrimPrefix(keyHex, "0x")
	raw, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("bad PERMAFROST_TEST_PRIVATE_KEY: %v", err)
	}
	priv, err := ethcrypto.ToECDSA(raw)
	if err != nil {
		t.Fatal(err)
	}
	addr := AddressFromPrivateKey(priv)
	broadcast := os.Getenv("PERMAFROST_TEST_BROADCAST") == "1"

	for name, chain := range Testnets {
		name, chain := name, chain
		t.Run(name, func(t *testing.T) {
			url := rpcURLForTestnet(name)
			if url == "" {
				t.Skipf("no RPC URL for %s", name)
			}
			c := NewClient(url, chain)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			bal, err := c.GetBalance(ctx, addr)
			if err != nil {
				t.Fatalf("balance: %v", err)
			}
			t.Logf("addr=%s balance=%s wei (%s)", addr, bal, chain.Native)

			if !broadcast {
				t.Skip("set PERMAFROST_TEST_BROADCAST=1 to actually send a tx")
			}
			if bal.Sign() == 0 {
				t.Skipf("address has 0 %s on %s; fund it from a faucet first", chain.Native, name)
			}

			// 0-value self-transfer. Tiny gas, easy to confirm.
			req := TxRequest{From: addr, To: addr, Data: "0x", Value: nil}
			r, hash, err := SendAndWait(ctx, c, req, priv, 90*time.Second)
			if err != nil {
				t.Fatalf("send/wait: %v", err)
			}
			if !r.IsSuccess() {
				t.Fatalf("tx %s reverted: status=%s", hash, r.Status)
			}
			t.Logf("✓ confirmed: %s", chain.ExplorerURL(hash))
		})
	}
}
