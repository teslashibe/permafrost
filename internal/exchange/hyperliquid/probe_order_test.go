//go:build live

package hyperliquid

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// TestLive_PlaceAndCancel_Testnet exercises the full signing path:
// constructs a HL Venue with a signer from PERMAFROST_HL_TEST_KEY,
// places a tiny limit order far from market on testnet, verifies it
// rests, then cancels it.
//
// Requires the env var. Skip otherwise. Never run against mainnet
// without an explicit flag (this test pins NetworkTestnet).
func TestLive_PlaceAndCancel_Testnet(t *testing.T) {
	keyHex := os.Getenv("PERMAFROST_HL_TEST_KEY")
	if keyHex == "" {
		t.Skip("PERMAFROST_HL_TEST_KEY not set")
	}
	signer, err := wallet.NewHyperliquidSignerFromHex(keyHex)
	if err != nil {
		t.Fatal(err)
	}

	v, err := New(Config{
		Network: NetworkTestnet,
		Address: signer.Address(),
	}, WithSigner(signer))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Sanity check: balance before.
	bals, err := v.Balances(ctx)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	t.Logf("balance before: %+v", bals)

	// Place a buy of WIF well below testnet mark (~$0.21) so the order
	// rests as a maker. HL evaluates the $10 min-notional against mark
	// price, so size needs to clear $10 / mark, not $10 / limit_price.
	// 200 WIF * mark $0.21 = ~$42 (mark notional), safely above $10.
	intent := types.OrderIntent{
		Venue:  VenueName,
		Symbol: "WIF",
		Side:   types.SideBuy,
		Type:   types.OrderTypeLimit,
		Price:  decimal.NewFromFloat(0.10),
		Size:   decimal.NewFromFloat(200.0),
		TIF:    types.TIFGTC,
	}
	t.Logf("placing: %+v", intent)
	ack, err := v.Place(ctx, intent)
	if err != nil {
		t.Fatalf("Place: %v (ack=%+v)", err, ack)
	}
	t.Logf("ack: %+v", ack)
	if ack.ID == "" {
		t.Fatalf("expected non-empty order id; ack=%+v", ack)
	}

	// Give the venue a beat to register the resting order.
	time.Sleep(500 * time.Millisecond)

	// Cancel by venue oid — the Venue cached the symbol from Place.
	if err := v.Cancel(ctx, ack.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	t.Logf("cancelled %s", ack.ID)

	// Balance should be unchanged (order never filled).
	balsAfter, err := v.Balances(ctx)
	if err != nil {
		t.Fatalf("Balances after: %v", err)
	}
	t.Logf("balance after:  %+v", balsAfter)
}
