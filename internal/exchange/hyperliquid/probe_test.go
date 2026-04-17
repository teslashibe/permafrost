//go:build live

// Live integration test against the real Hyperliquid REST API. Run with:
//
//	go test -tags=live ./internal/exchange/hyperliquid/... -v -run TestLive
//
// Excluded from the default build to avoid hitting the network in CI.
package hyperliquid

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestLive_FundingRates_Testnet(t *testing.T) {
	v, err := New(Config{Network: NetworkTestnet})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rates, err := v.FundingRates(ctx, nil)
	if err != nil {
		t.Fatalf("FundingRates testnet: %v", err)
	}
	if len(rates) == 0 {
		t.Fatal("expected at least one funding rate from testnet")
	}
	t.Logf("got %d funding rates from hyperliquid testnet", len(rates))
}

// TestLive_FundingRates_Mainnet_TopAnnualised prints the highest-annualised
// funding rates on mainnet — useful to know whether default thresholds will
// trigger entries during a paper run.
func TestLive_FundingRates_Mainnet_TopAnnualised(t *testing.T) {
	v, err := New(Config{Network: NetworkMainnet})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rates, err := v.FundingRates(ctx, nil)
	if err != nil {
		t.Fatalf("FundingRates mainnet: %v", err)
	}
	sort.Slice(rates, func(i, j int) bool {
		return rates[i].Annualised().Abs().GreaterThan(rates[j].Annualised().Abs())
	})
	t.Logf("top 10 |annualised funding| on mainnet (out of %d):", len(rates))
	for i, r := range rates {
		if i >= 10 {
			break
		}
		t.Logf("  %-12s rate/h=%-15s ann=%s", r.Symbol, r.Rate, r.Annualised())
	}
}

// TestLive_FundingRates_Universe prints the funding for our registry
// universe so we know what the strategy would actually see.
func TestLive_FundingRates_Universe(t *testing.T) {
	v, err := New(Config{Network: NetworkMainnet})
	if err != nil {
		t.Fatal(err)
	}
	universe := []string{"WIF", "BONK", "POPCAT", "PNUT", "GOAT", "FARTCOIN", "MOODENG", "CHILLGUY"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rates, err := v.FundingRates(ctx, universe)
	if err != nil {
		t.Fatalf("FundingRates: %v", err)
	}
	t.Logf("funding for permafrost universe (mainnet, %d hits):", len(rates))
	for _, r := range rates {
		t.Logf("  %-10s rate/h=%-15s ann=%s", r.Symbol, r.Rate, r.Annualised())
	}
}
