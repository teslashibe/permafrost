package oneinch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubServer fakes the 1inch v6 API.
func stubServer(t *testing.T, routes map[string]func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match by path prefix because path includes the chainId segment.
		for prefix, h := range routes {
			if strings.Contains(r.URL.Path, prefix) {
				h(w, r)
				return
			}
		}
		http.Error(w, "no route for "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestClient_Quote_Happy(t *testing.T) {
	srv := stubServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/quote": func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("missing/incorrect auth header: %q", got)
			}
			if r.URL.Query().Get("amount") != "1000000" {
				t.Errorf("amount param: %q", r.URL.Query().Get("amount"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dstAmount": "999500",
			})
		},
	})

	c := NewClient(8453, "test-key", WithBaseURL(srv.URL))
	q, err := c.Quote(context.Background(), QuoteParams{
		Src:    "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
		Dst:    "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2",
		Amount: "1000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.DstAmount != "999500" {
		t.Errorf("DstAmount: %q", q.DstAmount)
	}
}

func TestClient_Swap_Happy(t *testing.T) {
	srv := stubServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/swap": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("from") == "" {
				t.Error("from param missing")
			}
			if r.URL.Query().Get("slippage") != "0.5" {
				t.Errorf("slippage default: %q", r.URL.Query().Get("slippage"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dstAmount": "999500",
				"tx": map[string]any{
					"from":  "0xfrom",
					"to":    "0x1111111254eeb25477b68fb85ed929f73a960582", // 1inch router
					"data":  "0xabcd",
					"value": "0",
					"gas":   200000,
				},
			})
		},
	})

	c := NewClient(8453, "k", WithBaseURL(srv.URL))
	resp, err := c.Swap(context.Background(), SwapParams{
		Src:    "0xa",
		Dst:    "0xb",
		Amount: "1000000",
		From:   "0xfrom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Tx.To == "" || resp.Tx.Data == "" {
		t.Errorf("missing tx fields in response: %+v", resp.Tx)
	}
}

func TestClient_Allowance_AndApproveTx(t *testing.T) {
	srv := stubServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/approve/allowance": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"allowance": "0"})
		},
		"/approve/transaction": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"to": "0x1111111254eeb25477b68fb85ed929f73a960582",
				"data": "0x095ea7b3" +
					"0000000000000000000000001111111254eeb25477b68fb85ed929f73a960582" +
					"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				"value": "0",
			})
		},
	})

	c := NewClient(8453, "k", WithBaseURL(srv.URL))
	allow, err := c.Allowance(context.Background(), "0xtok", "0xowner")
	if err != nil {
		t.Fatal(err)
	}
	if allow != "0" {
		t.Errorf("allowance: %q", allow)
	}
	tx, err := c.ApproveTx(context.Background(), "0xtok", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tx.Data, "0x095ea7b3") {
		t.Errorf("approve calldata should start with 0x095ea7b3, got %q", tx.Data[:12])
	}
}

func TestClient_APIErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"description": "Insufficient liquidity",
			"statusCode":  400,
		})
	}))
	defer srv.Close()
	c := NewClient(8453, "k", WithBaseURL(srv.URL))
	_, err := c.Quote(context.Background(), QuoteParams{Src: "x", Dst: "y", Amount: "1"})
	if err == nil || !strings.Contains(err.Error(), "Insufficient liquidity") {
		t.Errorf("expected error to surface, got %v", err)
	}
}
