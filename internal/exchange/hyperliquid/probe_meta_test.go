//go:build live

package hyperliquid

import (
	"context"
	"testing"
	"time"
)

// TestLive_TestnetMarkPrice prints WIF mark price on testnet so we can
// size live orders to clear the $10 min-notional rule.
func TestLive_TestnetMarkPrice(t *testing.T) {
	v, err := New(Config{Network: NetworkTestnet})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.refreshMeta(ctx, true); err != nil {
		t.Fatal(err)
	}
	v.metaMu.Lock()
	defer v.metaMu.Unlock()
	for coin, i := range v.metaIndex {
		if coin != "WIF" && coin != "BTC" && coin != "ETH" {
			continue
		}
		t.Logf("%-6s mark=%s mid=%s funding/h=%s",
			coin, v.metaSnapshot.Ctxs[i].MarkPx, v.metaSnapshot.Ctxs[i].MidPx, v.metaSnapshot.Ctxs[i].Funding)
	}
}
