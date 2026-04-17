package solana

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJito_SendBundle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Method != "sendBundle" {
			t.Errorf("method: %s", req.Method)
		}
		txs, _ := req.Params[0].([]any)
		if len(txs) != 2 {
			t.Errorf("expected 2 txs, got %d", len(txs))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "bundle-uuid-xyz"})
	}))
	defer srv.Close()

	c := NewJitoClient(srv.URL)
	uuid, err := c.SendBundle(context.Background(), []string{"tx1", "tx2"})
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "bundle-uuid-xyz" {
		t.Errorf("uuid: %s", uuid)
	}
}

func TestJito_BundleSizeLimits(t *testing.T) {
	c := NewJitoClient("http://example")
	if _, err := c.SendBundle(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty bundle")
	}
	if _, err := c.SendBundle(context.Background(), []string{"a", "b", "c", "d", "e", "f"}); err == nil {
		t.Fatal("expected error for >5 bundle")
	}
}
