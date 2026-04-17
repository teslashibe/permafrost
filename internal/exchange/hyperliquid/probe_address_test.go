//go:build live

package hyperliquid

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/teslashibe/permafrost/internal/wallet"
)

// TestLive_DeriveAddress_FromEnv pulls a hex key from
// PERMAFROST_HL_TEST_KEY and prints the derived EVM address +
// clearinghouseState (balances). For local sanity-checking only.
func TestLive_DeriveAddress_FromEnv(t *testing.T) {
	keyHex := os.Getenv("PERMAFROST_HL_TEST_KEY")
	if keyHex == "" {
		t.Skip("PERMAFROST_HL_TEST_KEY not set")
	}
	s, err := wallet.NewHyperliquidSignerFromHex(keyHex)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("derived address: %s", s.Address())

	// Now hit testnet for the account state.
	v, err := New(Config{Network: NetworkTestnet, Address: s.Address()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bals, err := v.Balances(ctx)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	for _, b := range bals {
		t.Logf("balance: asset=%s free=%s locked=%s", b.Asset, b.Free, b.Locked)
	}
}
