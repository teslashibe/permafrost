package solana

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRPCMock(t *testing.T, h func(method string, params json.RawMessage) (any, error)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		result, err := h(req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]any{"code": -32000, "message": err.Error()},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID, "result": result,
		})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), srv
}

func TestGetBalance(t *testing.T) {
	c, _ := newRPCMock(t, func(method string, _ json.RawMessage) (any, error) {
		if method != "getBalance" {
			t.Errorf("method: %s", method)
		}
		return map[string]any{"value": 1234567890}, nil
	})
	got, err := c.GetBalance(context.Background(), "addr")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1234567890 {
		t.Errorf("balance: %d", got)
	}
}

func TestGetTokenAccountsByOwner(t *testing.T) {
	c, _ := newRPCMock(t, func(method string, _ json.RawMessage) (any, error) {
		if method != "getTokenAccountsByOwner" {
			t.Errorf("method: %s", method)
		}
		return map[string]any{
			"value": []any{
				map[string]any{
					"pubkey": "tokenAcct1",
					"account": map[string]any{
						"data": map[string]any{
							"parsed": map[string]any{
								"info": map[string]any{
									"mint":  "mintA",
									"owner": "ownerA",
									"tokenAmount": map[string]any{
										"amount": "100", "decimals": 6, "uiAmountString": "0.000100",
									},
								},
							},
						},
					},
				},
			},
		}, nil
	})
	got, err := c.GetTokenAccountsByOwner(context.Background(), "ownerA", "mintA")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Mint != "mintA" || got[0].Balance.Decimals != 6 {
		t.Errorf("got: %+v", got)
	}
}

func TestSendTransactionAndStatuses(t *testing.T) {
	c, _ := newRPCMock(t, func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "sendTransaction":
			return "sig1", nil
		case "getSignatureStatuses":
			return map[string]any{
				"value": []any{
					map[string]any{
						"slot":               321,
						"confirmationStatus": "confirmed",
					},
				},
			}, nil
		}
		return nil, nil
	})

	sig, err := c.SendTransaction(context.Background(), "BASE64TX==", true)
	if err != nil {
		t.Fatal(err)
	}
	if sig != "sig1" {
		t.Errorf("sig: %q", sig)
	}

	statuses, err := c.GetSignatureStatuses(context.Background(), []string{"sig1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].ConfirmationStatus != "confirmed" {
		t.Errorf("status: %+v", statuses)
	}
}

func TestGetLatestBlockhash(t *testing.T) {
	c, _ := newRPCMock(t, func(method string, _ json.RawMessage) (any, error) {
		return map[string]any{
			"value": map[string]any{
				"blockhash":            "bh-xyz",
				"lastValidBlockHeight": 555,
			},
		}, nil
	})
	bh, lvbh, err := c.GetLatestBlockhash(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bh != "bh-xyz" || lvbh != 555 {
		t.Errorf("got: %s/%d", bh, lvbh)
	}
}

func TestRPCError(t *testing.T) {
	c, _ := newRPCMock(t, func(_ string, _ json.RawMessage) (any, error) {
		return nil, http.ErrServerClosed
	})
	if _, err := c.GetBalance(context.Background(), "addr"); err == nil {
		t.Fatal("expected error")
	}
}
