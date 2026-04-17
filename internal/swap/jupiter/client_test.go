package jupiter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuote_BuildsURL(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"inputMint":            "USDC",
			"outputMint":           "WIF",
			"inAmount":             "1000000",
			"outAmount":             "5000000",
			"otherAmountThreshold": "4900000",
			"swapMode":             "ExactIn",
			"slippageBps":          50,
			"priceImpactPct":       "0.001",
			"routePlan":            []any{},
			"contextSlot":          12345,
			"timeTaken":            0.012,
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	resp, err := c.Quote(context.Background(), QuoteParams{
		InputMint: "USDC", OutputMint: "WIF", Amount: 1_000_000, SlippageBps: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "inputMint=USDC") || !strings.Contains(gotURL, "amount=1000000") {
		t.Errorf("URL: %s", gotURL)
	}
	if resp.OutAmount != "5000000" {
		t.Errorf("OutAmount: %s", resp.OutAmount)
	}
	if len(resp.RouteJSON) == 0 {
		t.Errorf("RouteJSON should be populated")
	}
}

func TestSwap_RequiresArgs(t *testing.T) {
	c := NewClient()
	if _, err := c.Swap(context.Background(), SwapParams{UserPublicKey: "x"}); err == nil {
		t.Fatal("expected error for missing quote")
	}
	if _, err := c.Swap(context.Background(), SwapParams{QuoteResponse: []byte("{}")}); err == nil {
		t.Fatal("expected error for missing user pubkey")
	}
}

func TestSwap_PostShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["userPublicKey"] != "user-1" {
			t.Errorf("userPublicKey: %v", got["userPublicKey"])
		}
		if _, ok := got["quoteResponse"]; !ok {
			t.Errorf("quoteResponse missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"swapTransaction":           "BASE64TX",
			"lastValidBlockHeight":      999,
			"prioritizationFeeLamports": 1000,
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	resp, err := c.Swap(context.Background(), SwapParams{
		QuoteResponse: []byte(`{"x":1}`),
		UserPublicKey: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SwapTransaction != "BASE64TX" {
		t.Errorf("SwapTransaction: %s", resp.SwapTransaction)
	}
}

func TestSwap_RejectsEmptyTransaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"swapTransaction": ""})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	_, err := c.Swap(context.Background(), SwapParams{
		QuoteResponse: []byte("{}"),
		UserPublicKey: "user",
	})
	if err == nil || !strings.Contains(err.Error(), "empty swapTransaction") {
		t.Fatalf("expected empty-tx error, got %v", err)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-api-key")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"inputMint":  "a", "outputMint": "b",
			"inAmount":  "1", "outAmount": "1", "otherAmountThreshold": "1",
			"swapMode": "ExactIn", "slippageBps": 50,
			"priceImpactPct": "0", "routePlan": []any{},
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithAPIKey("k-123"))
	if _, err := c.Quote(context.Background(), QuoteParams{
		InputMint: "a", OutputMint: "b", Amount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if got != "k-123" {
		t.Errorf("x-api-key: %s", got)
	}
}
