package funding_arb_basic

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/internal/inference/mock"
	"github.com/teslashibe/permafrost/internal/strategy"
	"github.com/teslashibe/permafrost/internal/types"
)

func loadRegistry(t *testing.T) assets.Registry {
	t.Helper()
	r, err := assets.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// fundingSymbol builds a SymbolSnap with a fixed annualised funding rate.
// The rate is per-hour, so an annual ann = rate * 24*365.
func fundingSymbol(symbol string, hourlyRate decimal.Decimal) types.SymbolSnap {
	return types.SymbolSnap{
		Funding: types.FundingRate{
			Symbol:   symbol,
			Rate:     hourlyRate,
			Interval: time.Hour,
		},
	}
}

func TestNew_ValidatesConfig(t *testing.T) {
	r := loadRegistry(t)
	if _, err := New(Config{
		EntryAnnualisedFunding: d("0.1"),
		ExitAnnualisedFunding:  d("0.2"),
		PositionCapUSDC:        d("100"),
	}, r, nil); err == nil {
		t.Errorf("entry < exit should error")
	}
	if _, err := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("-1"),
	}, r, nil); err == nil {
		t.Errorf("negative PositionCapUSDC should error")
	}
	if _, err := New(Config{}, r, nil); err != nil {
		t.Errorf("empty config should succeed via Defaults, got %v", err)
	}
}

func TestDecide_OpensTopFundingCandidate(t *testing.T) {
	r := loadRegistry(t)
	s, err := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	in := strategy.DecisionInput{
		AgentID: "ag",
		Now:     time.Now().UTC(),
		Market: types.MarketSnapshot{
			Time: time.Now().UTC(),
			Symbols: map[string]types.SymbolSnap{
				"WIF":  fundingSymbol("WIF", d("0.0001")),  // ann ≈ 0.876
				"BONK": fundingSymbol("BONK", d("0.00001")), // ann ≈ 0.0876, below threshold
			},
		},
	}
	dec, err := s.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Swaps) != 1 || len(dec.Orders) != 1 {
		t.Fatalf("expected exactly one swap+order pair, got swaps=%d orders=%d", len(dec.Swaps), len(dec.Orders))
	}
	if dec.Orders[0].Symbol != "WIF" || dec.Swaps[0].OutToken.Symbol != "WIF" {
		t.Errorf("expected WIF (highest ann funding), got swap=%+v order=%+v", dec.Swaps[0].OutToken, dec.Orders[0])
	}
	if dec.Orders[0].Side != types.SideSell {
		t.Errorf("perp leg should be sell, got %s", dec.Orders[0].Side)
	}
}

func TestDecide_ClosesWhenFundingFalls(t *testing.T) {
	r := loadRegistry(t)
	s, _ := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, r, nil)
	wif, _ := r.Get("WIF")
	open := types.BasisPosition{
		ID:         "p1",
		AgentID:    "ag",
		Underlying: "WIF",
		State:      types.BasisStateOpen,
		Legs: []types.BasisLeg{
			{Kind: types.BasisLegSpot, Asset: wif.AsAsset(), Qty: d("100")},
			{Kind: types.BasisLegPerp, Symbol: "WIF", Qty: d("100"), AvgPrice: d("1")},
		},
	}
	in := strategy.DecisionInput{
		AgentID:        "ag",
		Now:            time.Now().UTC(),
		BasisPositions: []types.BasisPosition{open},
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"WIF": fundingSymbol("WIF", d("0.000005")), // ann ≈ 0.0438, below exit
			},
		},
	}
	dec, err := s.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Orders) != 1 || dec.Orders[0].Side != types.SideBuy || !dec.Orders[0].ReduceOnly {
		t.Errorf("expected reduce-only buy to close short, got %+v", dec.Orders)
	}
	if len(dec.Swaps) != 1 || dec.Swaps[0].OutToken.Symbol != "USDC" {
		t.Errorf("expected swap WIF→USDC, got %+v", dec.Swaps)
	}
}

func TestDecide_DoesNotReopenAlreadyOpenSymbol(t *testing.T) {
	r := loadRegistry(t)
	s, _ := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, r, nil)
	in := strategy.DecisionInput{
		AgentID: "ag",
		Now:     time.Now().UTC(),
		BasisPositions: []types.BasisPosition{{
			Underlying: "WIF",
			State:      types.BasisStateOpen,
			Legs: []types.BasisLeg{
				{Kind: types.BasisLegPerp, Symbol: "WIF", Qty: d("100"), AvgPrice: d("1")},
			},
		}},
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"WIF": fundingSymbol("WIF", d("0.0005")), // ann very high → would open if not already
			},
		},
	}
	dec, _ := s.Decide(context.Background(), in)
	for _, o := range dec.Orders {
		if o.Symbol == "WIF" && !o.ReduceOnly {
			t.Errorf("should not open second WIF basis: %+v", o)
		}
	}
}

func TestDecide_UnderEntryThresholdEmitsNothing(t *testing.T) {
	r := loadRegistry(t)
	s, _ := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, r, nil)
	in := strategy.DecisionInput{
		AgentID: "ag",
		Now:     time.Now().UTC(),
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"WIF": fundingSymbol("WIF", d("0.00001")), // ann ≈ 0.0876
			},
		},
	}
	dec, _ := s.Decide(context.Background(), in)
	if dec.HasIntents() {
		t.Errorf("under threshold should produce no intents, got %+v", dec)
	}
}

func TestDecide_LLMVetoBlocksOpen(t *testing.T) {
	r := loadRegistry(t)
	mp := mock.New(mock.WithResponse(inference.Response{
		Content:  `{"veto": true, "reason": "token unlock in 24h"}`,
		Provider: "mock", Model: "mock",
	}))
	s, _ := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
		UseLLMVeto:             true,
		VetoModel:              "mock",
	}, r, mp)
	in := strategy.DecisionInput{
		AgentID: "ag",
		Now:     time.Now().UTC(),
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"WIF": fundingSymbol("WIF", d("0.0001")),
			},
		},
	}
	dec, _ := s.Decide(context.Background(), in)
	if len(dec.Swaps) != 0 || len(dec.Orders) != 0 {
		t.Errorf("LLM veto should block open, got %+v", dec)
	}
	if !strings.Contains(dec.Notes, "vetoed WIF") {
		t.Errorf("expected veto note, got %q", dec.Notes)
	}
}

func TestDecide_LLMUnsupportedDoesNotVeto(t *testing.T) {
	r := loadRegistry(t)
	mp := mock.New(mock.WithResponse(inference.Response{Content: ""}))
	// Inject an inference provider that always returns ErrUnsupportedFeature
	failing := &failingProvider{err: inference.ErrUnsupportedFeature}
	_ = mp

	s, _ := New(Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
		UseLLMVeto:             true,
		VetoModel:              "mock",
	}, r, failing)
	in := strategy.DecisionInput{
		AgentID: "ag",
		Now:     time.Now().UTC(),
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{"WIF": fundingSymbol("WIF", d("0.0001"))},
		},
	}
	dec, _ := s.Decide(context.Background(), in)
	if len(dec.Swaps) != 1 || len(dec.Orders) != 1 {
		t.Errorf("ErrUnsupportedFeature should NOT veto, got swaps=%d orders=%d", len(dec.Swaps), len(dec.Orders))
	}
}

type failingProvider struct{ err error }

func (failingProvider) Name() string { return "failing" }
func (p *failingProvider) Complete(_ context.Context, _ inference.Request) (inference.Response, error) {
	return inference.Response{}, p.err
}
func (failingProvider) Embed(_ context.Context, _ inference.EmbedRequest) (inference.EmbedResponse, error) {
	return inference.EmbedResponse{}, nil
}
