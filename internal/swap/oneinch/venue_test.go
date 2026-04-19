package oneinch

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// stubKeystoreSigner returns a wallet.EVMSigner backed by a deterministic
// secp256k1 key. Anvil account #0 — never used outside tests.
func stubKeystoreSigner(t *testing.T, chain types.ChainID) *wallet.EVMSigner {
	t.Helper()
	raw, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	hl, err := wallet.NewHyperliquidSignerFromPrivate(raw)
	if err != nil {
		t.Fatal(err)
	}
	es, err := wallet.NewEVMSigner(hl, chain)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// makeStubVenue stands up two HTTP stubs (1inch + RPC) and returns a
// Venue plus the captured request counters. The RPC stub is intentionally
// permissive — it just happily answers everything — so individual tests
// can focus on assertions about 1inch behaviour and the runtime ordering.
func makeStubVenue(t *testing.T, chain types.ChainID, allowance string) (*Venue, *atomic.Int32) {
	t.Helper()

	approveCalls := &atomic.Int32{}
	swapCalls := &atomic.Int32{}

	oneInch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/quote"):
			_ = json.NewEncoder(w).Encode(map[string]any{"dstAmount": "999000"})
		case strings.Contains(r.URL.Path, "/approve/allowance"):
			_ = json.NewEncoder(w).Encode(map[string]any{"allowance": allowance})
		case strings.Contains(r.URL.Path, "/approve/transaction"):
			approveCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"to":    "0x1111111254eeb25477b68fb85ed929f73a960582",
				"data":  "0x095ea7b3" + strings.Repeat("0", 64) + strings.Repeat("f", 64),
				"value": "0",
			})
		case strings.Contains(r.URL.Path, "/swap"):
			swapCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dstAmount": "999000",
				"tx": map[string]any{
					"from":  "0xfrom",
					"to":    "0x1111111254eeb25477b68fb85ed929f73a960582",
					"data":  "0xabcd",
					"value": "0",
				},
			})
		default:
			http.Error(w, "no route "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(oneInch.Close)

	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			ID     uint64 `json:"id"`
			Params []any  `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x2105"
		case "eth_blockNumber":
			result = "0x1"
		case "eth_getBalance":
			result = "0x16345785d8a0000" // 0.1 ETH
		case "eth_getTransactionCount":
			result = "0x0"
		case "eth_gasPrice":
			result = "0x3b9aca00" // 1 gwei
		case "eth_maxPriorityFeePerGas":
			result = "0x3b9aca00"
		case "eth_feeHistory":
			result = map[string]any{"baseFeePerGas": []string{"0x3b9aca00"}}
		case "eth_estimateGas":
			result = "0x30d40" // 200_000
		case "eth_sendRawTransaction":
			result = "0x" + strings.Repeat("ab", 32)
		case "eth_getTransactionReceipt":
			result = map[string]any{
				"transactionHash":   "0x" + strings.Repeat("ab", 32),
				"status":            "0x1",
				"blockNumber":       "0x1",
				"gasUsed":           "0x5208",
				"effectiveGasPrice": "0x3b9aca00",
			}
		default:
			http.Error(w, "stub: no method "+req.Method, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID, "result": result,
		})
	}))
	t.Cleanup(rpc.Close)

	signer := stubKeystoreSigner(t, chain)
	v, err := New(Config{
		OneInchAPIKey:  "test-key",
		OneInchBaseURL: oneInch.URL,
		RPCURL:         rpc.URL,
		Chain:          chain,
		PollTimeout:    5 * time.Second,
	}, signer)
	if err != nil {
		t.Fatal(err)
	}
	return v, approveCalls
}

func TestVenue_Swap_RunsApproveOnceWhenAllowanceIsZero(t *testing.T) {
	v, approveCalls := makeStubVenue(t, types.ChainBase, "0")

	usdc := types.Asset{Symbol: "USDC", Chain: types.ChainBase, Mint: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Decimals: 6}
	weth := types.Asset{Symbol: "WETH", Chain: types.ChainBase, Mint: "0x4200000000000000000000000000000000000006", Decimals: 18}

	q, err := v.Quote(context.Background(), types.QuoteRequest{
		InToken: usdc, OutToken: weth, Amount: decimal.NewFromInt(1), Mode: types.QuoteExactIn,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := v.Swap(context.Background(), q, 50)
	if err != nil {
		t.Fatal(err)
	}
	if tx == "" {
		t.Error("expected non-empty tx hash")
	}
	if got := approveCalls.Load(); got != 1 {
		t.Errorf("expected exactly 1 approve calldata fetch, got %d", got)
	}

	// Second swap on the same token MUST NOT re-approve — cache hit.
	if _, err := v.Swap(context.Background(), q, 50); err != nil {
		t.Fatal(err)
	}
	if got := approveCalls.Load(); got != 1 {
		t.Errorf("approve cache miss: got %d calldata fetches, want 1", got)
	}
}

func TestVenue_Swap_SkipsApproveWhenAllowanceSufficient(t *testing.T) {
	v, approveCalls := makeStubVenue(t, types.ChainAvalanche, "999999999999999999999999")

	usdc := types.Asset{Symbol: "USDC", Chain: types.ChainAvalanche, Mint: "0xb97ef9ef8734c71904d8002f8b6bc66dd9c48a6e", Decimals: 6}
	wavax := types.Asset{Symbol: "WAVAX", Chain: types.ChainAvalanche, Mint: "0xb31f66aa3c1e785363f0875a1b74e27b85fd66c7", Decimals: 18}

	q, err := v.Quote(context.Background(), types.QuoteRequest{
		InToken: usdc, OutToken: wavax, Amount: decimal.NewFromInt(1), Mode: types.QuoteExactIn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Swap(context.Background(), q, 0); err != nil {
		t.Fatal(err)
	}
	if got := approveCalls.Load(); got != 0 {
		t.Errorf("expected 0 approve fetches when allowance is sufficient, got %d", got)
	}
}

func TestVenue_Quote_RejectsNativeAsset(t *testing.T) {
	v, _ := makeStubVenue(t, types.ChainEthereum, "0")
	_, err := v.Quote(context.Background(), types.QuoteRequest{
		InToken:  types.Asset{Symbol: "ETH", Chain: types.ChainEthereum, Mint: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Decimals: 18},
		OutToken: types.Asset{Symbol: "USDC", Chain: types.ChainEthereum, Mint: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Decimals: 6},
		Amount:   decimal.NewFromInt(1),
	})
	if err == nil || !strings.Contains(err.Error(), "native") {
		t.Errorf("expected native-asset rejection, got %v", err)
	}
}

func TestVenue_Quote_RejectsExactOut(t *testing.T) {
	v, _ := makeStubVenue(t, types.ChainBase, "0")
	_, err := v.Quote(context.Background(), types.QuoteRequest{
		InToken:  types.Asset{Symbol: "USDC", Chain: types.ChainBase, Mint: "0x1", Decimals: 6},
		OutToken: types.Asset{Symbol: "WETH", Chain: types.ChainBase, Mint: "0x2", Decimals: 18},
		Amount:   decimal.NewFromInt(1),
		Mode:     types.QuoteExactOut,
	})
	if err == nil || !strings.Contains(err.Error(), "ExactOut") {
		t.Errorf("expected ExactOut rejection, got %v", err)
	}
}

func TestVenue_NameAndChain(t *testing.T) {
	v, _ := makeStubVenue(t, types.ChainBSC, "0")
	if v.Name() != VenueName {
		t.Errorf("name: %q", v.Name())
	}
	if v.Chain() != types.ChainBSC {
		t.Errorf("chain: %q", v.Chain())
	}
}

func TestBpsToPercent(t *testing.T) {
	tests := map[int]string{
		0:    "0.5",
		50:   "0.5",
		100:  "1",
		25:   "0.25",
		1000: "10",
	}
	for in, want := range tests {
		if got := bpsToPercent(in); got != want {
			t.Errorf("bpsToPercent(%d) = %q want %q", in, got, want)
		}
	}
}
