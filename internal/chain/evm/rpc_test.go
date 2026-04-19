package evm

import (
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

// stubRPC starts an httptest server that pretends to be an EVM JSON-RPC
// node. Each request is matched against the supplied responses by RPC
// method; the first match wins. Unmatched requests return an error.
func stubRPC(t *testing.T, responses map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			ID     uint64 `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		v, ok := responses[req.Method]
		if !ok {
			http.Error(w, "stub: no response for "+req.Method, http.StatusInternalServerError)
			return
		}
		out := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": v}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_ChainIDAndBalance(t *testing.T) {
	srv := stubRPC(t, map[string]any{
		"eth_chainId":    "0x2105",                                                                  // 8453
		"eth_getBalance": "0x16345785d8a0000",                                                       // 0.1 ETH
		"eth_blockNumber": "0xabcd",                                                                 //
		"eth_getTransactionCount": "0x7",                                                            //
	})
	c := NewClient(srv.URL, Chain{ID: types.ChainBase, NumericID: 8453, GasModel: GasModelEIP1559})

	id, err := c.ChainID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != 8453 {
		t.Errorf("chain id: got %d want 8453", id)
	}

	bal, err := c.GetBalance(context.Background(), "0x000000000000000000000000000000000000dead")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := new(big.Int).SetString("100000000000000000", 10) // 0.1 ETH in wei
	if bal.Cmp(want) != 0 {
		t.Errorf("balance: got %s want %s", bal, want)
	}

	bn, err := c.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bn != 0xabcd {
		t.Errorf("blocknumber: got %d want %d", bn, 0xabcd)
	}

	nonce, err := c.GetTransactionCount(context.Background(), "0xdeadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 7 {
		t.Errorf("nonce: got %d want 7", nonce)
	}
}

func TestClient_RPCErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"insufficient funds"}}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, Chain{ID: types.ChainBase})

	_, err := c.GetBalance(context.Background(), "0x0")
	if err == nil || !strings.Contains(err.Error(), "insufficient funds") {
		t.Errorf("expected RPC error to surface, got %v", err)
	}
}

func TestLookupChain_KnownAndUnknown(t *testing.T) {
	c, err := LookupChain(types.ChainBase)
	if err != nil {
		t.Fatal(err)
	}
	if c.NumericID != 8453 {
		t.Errorf("base id: got %d want 8453", c.NumericID)
	}
	if _, err := LookupChain("nonsense"); err == nil {
		t.Error("expected error for unknown chain")
	}
	// testnet shorthand also resolves
	if _, err := LookupChain("base-sepolia"); err != nil {
		t.Errorf("base-sepolia should resolve: %v", err)
	}
}

func TestExplorerURL(t *testing.T) {
	c, _ := LookupChain(types.ChainBase)
	url := c.ExplorerURL("0xabc")
	if !strings.Contains(url, "basescan.org") || !strings.Contains(url, "0xabc") {
		t.Errorf("explorer url: %q", url)
	}
}
