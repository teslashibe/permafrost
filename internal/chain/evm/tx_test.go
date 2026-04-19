package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/teslashibe/permafrost/pkg/types"
)

// stubKey returns a deterministic test private key. Never used outside
// tests — DO NOT reuse for real funds.
func stubKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	raw, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	priv, err := ethcrypto.ToECDSA(raw)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestSignAndSend_EIP1559_BroadcastsValidRawTx(t *testing.T) {
	var capturedRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
			ID     uint64 `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "eth_sendRawTransaction":
			capturedRaw = req.Params[0].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": "0x" + strings.Repeat("ab", 32),
			})
		default:
			http.Error(w, "stub: unexpected "+req.Method, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, Chain{ID: types.ChainBase, NumericID: 8453, GasModel: GasModelEIP1559})
	priv := stubKey(t)
	addr := AddressFromPrivateKey(priv)

	nonce := uint64(0)
	req := TxRequest{
		From:                 addr,
		To:                   "0x000000000000000000000000000000000000dead",
		Data:                 "0x",
		Value:                big.NewInt(0),
		GasLimit:             21000,
		Nonce:                &nonce,
		MaxFeePerGasWei:      big.NewInt(2_000_000_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
	}
	hash, err := SignAndSend(context.Background(), c, req, priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "0x") || len(hash) != 66 {
		t.Errorf("hash looks invalid: %q", hash)
	}
	// EIP-1559 envelope starts with 0x02.
	if !strings.HasPrefix(strings.ToLower(capturedRaw), "0x02") {
		t.Errorf("expected EIP-1559 (0x02) raw tx, got prefix %q", capturedRaw[:8])
	}
}

func TestSignAndSend_Legacy_BroadcastsValidRawTx(t *testing.T) {
	var capturedRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
			ID     uint64 `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "eth_sendRawTransaction" {
			capturedRaw = req.Params[0].(string)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": "0x" + strings.Repeat("cd", 32),
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, Chain{ID: types.ChainBSC, NumericID: 56, GasModel: GasModelLegacy})
	priv := stubKey(t)
	addr := AddressFromPrivateKey(priv)

	nonce := uint64(0)
	req := TxRequest{
		From:        addr,
		To:          "0x000000000000000000000000000000000000beef",
		Data:        "0x",
		Value:       big.NewInt(0),
		GasLimit:    21000,
		Nonce:       &nonce,
		GasPriceWei: big.NewInt(3_000_000_000),
	}
	if _, err := SignAndSend(context.Background(), c, req, priv); err != nil {
		t.Fatal(err)
	}
	// Legacy txs do NOT start with 0x02 — they start with the RLP list prefix (0xf8/0xf9/0xeb/...).
	if strings.HasPrefix(strings.ToLower(capturedRaw), "0x02") {
		t.Errorf("expected legacy tx (no 0x02 prefix), got %q", capturedRaw[:8])
	}
}

func TestAddressFromPrivateKey_Stable(t *testing.T) {
	priv := stubKey(t)
	addr := AddressFromPrivateKey(priv)
	// Anvil's well-known account #0 derived from this key. Used to
	// catch any accidental change in our derivation path.
	want := "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	if !strings.EqualFold(addr, want) {
		t.Errorf("address: got %s want %s", addr, want)
	}
}
