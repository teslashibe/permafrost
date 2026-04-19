package hyperliquid

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

const testAddr = "0x1111111111111111111111111111111111111111"

// newMockServer builds an httptest.Server that routes /info requests to the
// supplied handler. Other paths return 404.
func newMockServer(t *testing.T, h func(req infoRequest) (any, int)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req infoRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		resp, status := h(req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux)
}

func newTestVenue(t *testing.T, srv *httptest.Server) *Venue {
	t.Helper()
	v, err := New(Config{
		Network:      NetworkTestnet,
		Address:      testAddr,
		RESTOverride: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(Config{}); err != nil {
		t.Errorf("empty Address should be allowed (funding-only mode): %v", err)
	}
	if _, err := New(Config{Address: "0xZZZ"}); err == nil {
		t.Error("malformed address should error")
	}
	if _, err := New(Config{Address: testAddr, Network: "rinkeby"}); err == nil {
		t.Error("unknown network should error")
	}

	// Without an address, per-account reads must fail with ErrAddressRequired.
	v, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Positions(context.Background()); !errors.Is(err, ErrAddressRequired) {
		t.Errorf("Positions: expected ErrAddressRequired, got %v", err)
	}
	if _, err := v.Balances(context.Background()); !errors.Is(err, ErrAddressRequired) {
		t.Errorf("Balances: expected ErrAddressRequired, got %v", err)
	}
}

func TestVenuePositions(t *testing.T) {
	srv := newMockServer(t, func(req infoRequest) (any, int) {
		if req.Type != "clearinghouseState" || req.User != testAddr {
			t.Errorf("unexpected request: %+v", req)
		}
		return map[string]any{
			"assetPositions": []map[string]any{
				{
					"position": map[string]any{
						"coin": "WIF", "szi": "-100", "entryPx": "1.5",
						"liquidationPx": "3.0", "marginUsed": "50",
						"unrealizedPnl": "-2.5",
						"leverage":      map[string]any{"type": "cross", "value": 5},
					},
					"type": "oneWay",
				},
				// flat positions are filtered out
				{"position": map[string]any{"coin": "ETH", "szi": "0", "entryPx": "0", "liquidationPx": "0", "marginUsed": "0", "unrealizedPnl": "0", "leverage": map[string]any{"type": "cross", "value": 1}}},
			},
			"crossMarginSummary": map[string]any{"accountValue": "1000"},
			"withdrawable":       "950",
			"time":               int64(1_700_000_000_000),
		}, 200
	})
	defer srv.Close()
	v := newTestVenue(t, srv)

	ps, err := v.Positions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("expected 1 non-flat position, got %d: %+v", len(ps), ps)
	}
	if ps[0].Symbol != "WIF" {
		t.Errorf("Symbol: got %q", ps[0].Symbol)
	}
	if !ps[0].IsShort() {
		t.Errorf("IsShort: expected true, qty=%s", ps[0].Qty)
	}
}

func TestVenueBalances(t *testing.T) {
	srv := newMockServer(t, func(req infoRequest) (any, int) {
		return map[string]any{
			"assetPositions":     []any{},
			"crossMarginSummary": map[string]any{"totalMarginUsed": "150"},
			"withdrawable":       "850",
			"time":               int64(0),
		}, 200
	})
	defer srv.Close()
	v := newTestVenue(t, srv)

	bs, err := v.Balances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 {
		t.Fatalf("expected 1 balance, got %d", len(bs))
	}
	if bs[0].Asset != "USDC" || bs[0].Total().String() != "1000" {
		t.Errorf("balance: got %+v", bs[0])
	}
}

func TestVenueFundingRates(t *testing.T) {
	srv := newMockServer(t, func(req infoRequest) (any, int) {
		if req.Type != "metaAndAssetCtxs" {
			t.Errorf("expected metaAndAssetCtxs, got %s", req.Type)
		}
		// HL returns a JSON array [meta, ctxs]
		raw := []any{
			map[string]any{
				"universe": []map[string]any{
					{"name": "WIF", "szDecimals": 4, "maxLeverage": 50},
					{"name": "BTC", "szDecimals": 5, "maxLeverage": 50},
				},
			},
			[]map[string]any{
				{"funding": "0.0001", "markPx": "1.5"},
				{"funding": "0.00005", "markPx": "60000"},
			},
		}
		return raw, 200
	})
	defer srv.Close()
	v := newTestVenue(t, srv)

	all, err := v.FundingRates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rates, got %d: %+v", len(all), all)
	}
	if all[0].Symbol != "BTC" || all[1].Symbol != "WIF" {
		t.Errorf("expected sort order BTC,WIF; got %+v", all)
	}
	if all[1].Rate.String() != "0.0001" {
		t.Errorf("WIF rate: got %s", all[1].Rate)
	}
	if all[1].Interval == 0 {
		t.Errorf("Interval: should default to 1h")
	}

	// Filtered by symbol
	one, err := v.FundingRates(context.Background(), []string{"WIF"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Symbol != "WIF" {
		t.Errorf("filtered: got %+v", one)
	}
}

func TestVenuePlace_RequiresSigner(t *testing.T) {
	v, _ := New(Config{Address: testAddr})
	_, err := v.Place(context.Background(), types.OrderIntent{})
	if !errors.Is(err, ErrSignerRequired) {
		t.Fatalf("expected ErrSignerRequired, got %v", err)
	}
	if err := v.Cancel(context.Background(), types.OrderID("x")); !errors.Is(err, ErrSignerRequired) {
		t.Fatalf("expected ErrSignerRequired, got %v", err)
	}
}

func TestEndpointsFor(t *testing.T) {
	mainnet, _ := EndpointsFor(NetworkMainnet)
	if !strings.Contains(mainnet.REST, "api.hyperliquid.xyz") {
		t.Errorf("mainnet REST: %s", mainnet.REST)
	}
	testnet, _ := EndpointsFor(NetworkTestnet)
	if !strings.Contains(testnet.REST, "testnet") {
		t.Errorf("testnet REST: %s", testnet.REST)
	}
	def, _ := EndpointsFor("")
	if def != testnet {
		t.Errorf("default should be testnet")
	}
}
