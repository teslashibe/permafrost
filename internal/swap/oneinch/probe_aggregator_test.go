//go:build live

// Live mainnet aggregator probes. Run with:
//
//	export ONEINCH_API_KEY=...        # required (free key from portal.1inch.dev)
//	export PERMAFROST_TEST_PRIVATE_KEY=...  # 32-byte hex; address must hold ~$5 USDC on Base
//	export PERMAFROST_TEST_BROADCAST=1      # opt-in to actually swap
//	go test -tags=live -v ./internal/swap/oneinch/... -run Probe
//
// Why only Base? Cheapest gas of the four chains by 100x — a $5 round
// trip on Base costs pennies; on Ethereum mainnet it would cost more
// than the trade.
//
// Without PERMAFROST_TEST_BROADCAST, the probe only fetches a quote +
// allowance — useful for verifying the API key + signer pair without
// touching real funds.
package oneinch

import (
	"context"
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/chain/evm"
	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

const (
	usdcBase = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	wethBase = "0x4200000000000000000000000000000000000006"
)

func TestProbeAggregator_BaseUSDCToWETH(t *testing.T) {
	apiKey := os.Getenv("ONEINCH_API_KEY")
	if apiKey == "" {
		t.Skip("set ONEINCH_API_KEY to run the aggregator probe")
	}
	keyHex := os.Getenv("PERMAFROST_TEST_PRIVATE_KEY")
	if keyHex == "" {
		t.Skip("set PERMAFROST_TEST_PRIVATE_KEY (32-byte hex)")
	}
	rpcURL := os.Getenv("PERMAFROST_TEST_RPC_BASE")
	if rpcURL == "" {
		rpcURL = "https://mainnet.base.org" // public default; rate-limited
	}

	keyHex = strings.TrimPrefix(keyHex, "0x")
	raw, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatal(err)
	}
	hl, err := wallet.NewHyperliquidSignerFromPrivate(raw)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := wallet.NewEVMSigner(hl, types.ChainBase)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	v, err := New(Config{
		OneInchAPIKey: apiKey, RPCURL: rpcURL, Chain: types.ChainBase,
		PollTimeout: 60 * time.Second,
	}, signer)
	if err != nil {
		t.Fatal(err)
	}

	usdc := types.Asset{Symbol: "USDC", Chain: types.ChainBase, Mint: usdcBase, Decimals: 6}
	weth := types.Asset{Symbol: "WETH", Chain: types.ChainBase, Mint: wethBase, Decimals: 18}

	// Always-safe step 1: report current balance.
	chain, _ := evm.LookupChain(types.ChainBase)
	rpc := evm.NewClient(rpcURL, chain)
	usdcTok := evm.NewERC20Token(rpc, usdcBase)
	bal, err := usdcTok.BalanceOf(ctx, signer.Address())
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	t.Logf("addr=%s usdc=%s (base units, 6dp)", signer.Address(), bal)

	// Always-safe step 2: fetch a quote for $1 USDC → WETH (read-only).
	q, err := v.Quote(ctx, types.QuoteRequest{
		InToken: usdc, OutToken: weth, Amount: decimal.NewFromInt(1), Mode: types.QuoteExactIn,
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	t.Logf("quote: 1 USDC → %s WETH", q.OutAmount.StringFixed(8))

	if os.Getenv("PERMAFROST_TEST_BROADCAST") != "1" {
		t.Skip("set PERMAFROST_TEST_BROADCAST=1 to execute the swap (uses ~$1 of USDC)")
	}

	// HARD CAP: refuse to swap more than $5 worth.
	const capUSDC = 5
	tradeUSDC := big.NewInt(int64(1_000_000)) // 1 USDC base units (6dp)
	if bal.Cmp(big.NewInt(int64(capUSDC*1_000_000))) < 0 {
		t.Skipf("address has < %d USDC; fund it first", capUSDC)
	}

	q1, err := v.Quote(ctx, types.QuoteRequest{
		InToken: usdc, OutToken: weth, Amount: decimal.NewFromBigInt(tradeUSDC, -6), Mode: types.QuoteExactIn,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := v.Swap(ctx, q1, 100) // 1.0% slippage tolerance for safety
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	t.Logf("swap submitted: %s", chain.ExplorerURL(string(tx)))

	res, err := v.WaitConfirm(ctx, tx)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if res.Status != types.SwapStatusConfirmed {
		t.Fatalf("swap failed: status=%s", res.Status)
	}
	t.Logf("✓ confirmed: gas_native=%s", res.GasNative)
}
