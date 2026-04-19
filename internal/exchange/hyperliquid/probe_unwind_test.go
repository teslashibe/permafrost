//go:build live

package hyperliquid

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// TestLive_UnwindAllPositions closes every open position on HL testnet
// for the configured signer. Useful as a "panic button" after a half-open
// run leaves residual short exposure. Skips unless PERMAFROST_HL_TEST_KEY
// is set.
func TestLive_UnwindAllPositions(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	positions, err := v.Positions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("found %d open positions on testnet", len(positions))

	for _, p := range positions {
		if p.IsFlat() {
			continue
		}
		side := types.SideSell
		size := p.Qty
		if p.IsShort() {
			side = types.SideBuy
			size = p.Qty.Abs()
		}
		// Use a market-ish IOC limit far through the book to flatten.
		// 5% past mark to guarantee execution.
		rates, err := v.FundingRates(ctx, []string{p.Symbol})
		if err != nil || len(rates) == 0 || !rates[0].MarkPrice.IsPositive() {
			t.Errorf("no mark for %s; skipping", p.Symbol)
			continue
		}
		mark := rates[0].MarkPrice
		bump := decimalFromString("1.05")
		drop := decimalFromString("0.95")
		var limit = mark.Mul(drop)
		if side == types.SideBuy {
			limit = mark.Mul(bump)
		}
		intent := types.OrderIntent{
			Venue:      VenueName,
			Symbol:     p.Symbol,
			Side:       side,
			Type:       types.OrderTypeLimit,
			TIF:        types.TIFIOC,
			Size:       size,
			Price:      limit,
			ReduceOnly: true,
		}
		ack, err := v.Place(ctx, intent)
		if err != nil {
			t.Errorf("close %s: %v", p.Symbol, err)
			continue
		}
		t.Logf("closed %s qty=%s ack=%+v", p.Symbol, size, ack)
	}

	// Re-check.
	positions, _ = v.Positions(ctx)
	for _, p := range positions {
		if !p.IsFlat() {
			t.Errorf("residual position: %s qty=%s", p.Symbol, p.Qty)
		}
	}
}
