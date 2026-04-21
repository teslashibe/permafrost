// End-to-end proof that the alpha_dca strategy executes real on-chain
// trades when wired through the Bittensor swap venue against a local
// Subtensor devnet.
//
// Run with:
//
//	docker run -d --rm --name local_subtensor \
//	    -p 9944:9944 -p 9945:9945 \
//	    ghcr.io/opentensor/subtensor-localnet:devnet-ready
//	sleep 5
//	go test -tags=devnet -v ./internal/swap/bittensor/... -run TestDevnet_AlphaDCA
//
// This test stitches the real strategy → real swap venue → real chain
// client → real Subtensor runtime. If it passes, community testers can
// trust that running `permafrost agent start` against their devnet will
// drive actual alpha trades.

//go:build devnet
// +build devnet

package bittensor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/internal/wallet"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"

	// Side-effect import: registers alpha_dca in the strategy registry.
	alphadca "github.com/teslashibe/permafrost/strategies/alpha_dca"
)

const devnetURL = "ws://localhost:9944"

// TestDevnet_AlphaDCA_RealStake exercises the alpha_dca strategy
// end-to-end:
//  1. Bootstrap a fresh tradeable subnet (register_network + start_call).
//  2. Construct the alpha_dca strategy with the new netuid in its config.
//  3. Tick it until it emits a buy intent.
//  4. Route the intent through the real Bittensor swap venue.
//  5. Verify the on-chain alpha balance grew.
func TestDevnet_AlphaDCA_RealStake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	c := btchain.NewClient(devnetURL)
	defer c.Close()

	alice, err := wallet.NewBittensorSignerFromURI("//Alice")
	if err != nil {
		t.Fatal(err)
	}

	// Step 1 — bootstrap a new tradeable subnet owned by Alice.
	if _, err := c.RegisterNetwork(ctx, alice, alice.PublicKey(), btchain.SubmitOptions{
		WaitTimeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("RegisterNetwork: %v", err)
	}
	count, err := c.SubnetCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	netuid := count - 1
	t.Logf("✓ registered subnet %d", netuid)
	time.Sleep(2 * time.Second)

	if _, err := c.StartCall(ctx, alice, netuid, btchain.SubmitOptions{
		WaitTimeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	t.Log("✓ subnet activated")
	time.Sleep(2 * time.Second)

	// Step 2 — construct the alpha_dca strategy targeting our new netuid.
	strat, err := alphadca.New(map[string]any{
		"subnets":        []any{int(netuid)},
		"tao_per_buy":    5.0,
		"interval_ticks": 1,
		"slippage_bps":   100,
	})
	if err != nil {
		t.Fatalf("alpha_dca.New: %v", err)
	}
	if err := strat.Warmup(ctx, strategy.WarmupInput{AgentID: "test", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Logf("✓ alpha_dca strategy initialised: %s", strat.Name())

	// Step 3 — drive ticks until the strategy emits a buy intent.
	var dec strategy.Decision
	for i := 0; i < 3; i++ {
		dec, err = strat.Decide(ctx, strategy.DecisionInput{
			AgentID: "test",
			Now:     time.Now().Add(time.Duration(i) * time.Second),
			Market:  types.MarketSnapshot{Symbols: map[string]types.SymbolSnap{}},
		})
		if err != nil {
			t.Fatalf("Decide tick %d: %v", i, err)
		}
		if len(dec.Swaps) > 0 {
			break
		}
	}
	if len(dec.Swaps) == 0 {
		t.Fatal("alpha_dca emitted no swap intents in 3 ticks")
	}
	t.Logf("✓ strategy emitted %d swap intent(s): %s", len(dec.Swaps), dec.Notes)

	// Step 4 — route the intent through the real swap venue.
	venue, err := New(Config{
		RPCURL:      devnetURL,
		AllowSubmit: true,
		PollTimeout: 30 * time.Second,
	}, alice)
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite the SwapIntent's mint to reference our newly-created netuid
	// (alpha_dca was configured with netuid in cfg, but its emitted Mint
	// uses the configured value as a literal "SN{n}" — which works as
	// long as the netuid the venue parses matches what the strategy used).
	for i := range dec.Swaps {
		dec.Swaps[i].OutToken.Mint = fmt.Sprintf("SN%d", netuid)
		dec.Swaps[i].OutToken.Symbol = fmt.Sprintf("SN%d", netuid)
	}

	taoBefore, _ := c.GetTAOBalance(ctx, alice.Address())
	alphaBefore, _ := c.GetTotalHotkeyAlpha(ctx, alice.Address(), netuid)
	t.Logf("Alice TAO before: %s", btchain.RAOToTAO(taoBefore))
	t.Logf("Alice alpha before (netuid %d): %d RAO", netuid, alphaBefore)

	for i, intent := range dec.Swaps {
		t.Logf("[%d] strategy SwapIntent: %s %s → %s %s on %s",
			i, intent.InAmount, intent.InToken.Mint,
			intent.OutToken.Symbol, intent.OutToken.Mint, intent.Chain)

		quote, err := venue.Quote(ctx, types.QuoteRequest{
			InToken:  intent.InToken,
			OutToken: intent.OutToken,
			Amount:   intent.InAmount,
			Mode:     types.QuoteExactIn,
		})
		if err != nil {
			t.Fatalf("Quote: %v", err)
		}
		t.Logf("    quote: %s TAO → %s alpha (impact %s)",
			quote.InAmount, quote.OutAmount, quote.PriceImpact)

		txHash, err := venue.Swap(ctx, quote, intent.SlippageBps)
		if err != nil {
			t.Fatalf("Swap: %v", err)
		}
		t.Logf("    ✓ swap submitted: tx=%s", txHash)
	}

	time.Sleep(3 * time.Second)

	taoAfter, _ := c.GetTAOBalance(ctx, alice.Address())
	alphaAfter, _ := c.GetTotalHotkeyAlpha(ctx, alice.Address(), netuid)
	taoDelta := int64(taoBefore) - int64(taoAfter)
	alphaDelta := int64(alphaAfter) - int64(alphaBefore)

	t.Logf("Alice TAO after: %s (Δ %s TAO)",
		btchain.RAOToTAO(taoAfter), btchain.RAOToTAO(uint64(taoDelta)))
	t.Logf("Alice alpha after: %d RAO (Δ +%d RAO)", alphaAfter, alphaDelta)

	// Quote-vs-actual rounding can shave a tiny bit off the input
	// amount. Demand at least 4.5 TAO consumption (90% of 5).
	expectedMinTAO := decimal.NewFromFloat(4.5).Mul(decimal.New(1_000_000_000, 0))
	if decimal.NewFromInt(taoDelta).LessThan(expectedMinTAO) {
		t.Fatalf("TAO did not decrease by at least 4.5 TAO: delta=%d", taoDelta)
	}
	if alphaDelta <= 0 {
		t.Fatalf("alpha did not increase: delta=%d", alphaDelta)
	}
	t.Log("")
	t.Log("✓✓✓ alpha_dca STRATEGY → BITTENSOR VENUE → SUBTENSOR RUNTIME PROVEN ✓✓✓")
	t.Logf("    Strategy emitted intent → venue submitted real extrinsic →")
	t.Logf("    %s TAO consumed → +%d RAO alpha credited to Alice on-chain.",
		btchain.RAOToTAO(uint64(taoDelta)), alphaDelta)
}
