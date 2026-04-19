package funding_arb_basic

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Strategy is the deterministic funding-arb strategy.
type Strategy struct {
	cfg       Config
	registry  assets.Registry
	inference inference.Provider // optional; nil disables LLM veto
	clock     func() time.Time
}

// New constructs the strategy. registry is required (the trade universe);
// inf may be nil if the operator opts out of the LLM veto.
func New(cfg Config, registry assets.Registry, inf inference.Provider) (*Strategy, error) {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if registry.Version == 0 || len(registry.Assets) == 0 {
		return nil, fmt.Errorf("funding_arb_basic: empty asset registry")
	}
	return &Strategy{
		cfg:       cfg,
		registry:  registry,
		inference: inf,
		clock:     func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetClock overrides the clock function. For tests only.
func (s *Strategy) SetClock(fn func() time.Time) { s.clock = fn }

// Compile-time check.
var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

// Warmup is a no-op for v1; future versions may pre-fetch funding history.
func (s *Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

// Decide produces opens for high-funding candidates and closes for any open
// positions where funding has fallen below the exit threshold.
func (s *Strategy) Decide(ctx context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	dec := strategy.Decision{}
	openSymbols := openSymbolSet(in.BasisPositions)

	// Closes first — these don't compete for capital.
	for _, p := range in.BasisPositions {
		if !shouldClose(p, in.Market, s.cfg) {
			continue
		}
		closeSwap, closeOrder := closeIntents(p, in.AgentID)
		dec.Swaps = append(dec.Swaps, closeSwap)
		dec.Orders = append(dec.Orders, closeOrder)
	}

	// Opens — rank by annualised funding, filter, ask LLM veto if enabled.
	candidates := s.rank(in)
	for _, c := range candidates {
		if openSymbols[strings.ToUpper(c.Symbol)] {
			continue
		}
		if s.cfg.UseLLMVeto && s.inference != nil {
			veto, reason, err := s.askVeto(ctx, c)
			if err != nil {
				dec.Notes = appendNote(dec.Notes, fmt.Sprintf("veto error for %s: %v", c.Symbol, err))
				continue
			}
			if veto {
				dec.Notes = appendNote(dec.Notes, fmt.Sprintf("vetoed %s: %s", c.Symbol, reason))
				continue
			}
		}
		spotIntent, perpIntent, err := openIntents(c, s.cfg, in.AgentID)
		if err != nil {
			dec.Notes = appendNote(dec.Notes, fmt.Sprintf("skip %s: %v", c.Symbol, err))
			continue
		}
		dec.Swaps = append(dec.Swaps, spotIntent)
		dec.Orders = append(dec.Orders, perpIntent)
		dec.Notes = appendNote(dec.Notes, fmt.Sprintf(
			"opening %s @ funding %s/yr (mark=%s size=%s)",
			c.Symbol, c.Annualised, c.Funding.MarkPrice, perpIntent.Size))
	}

	dec.Confidence = candidateConfidence(candidates)
	return dec, nil
}

// candidate is one ranked tradable asset.
type candidate struct {
	Symbol     string
	Funding    types.FundingRate
	Annualised decimal.Decimal
	Asset      assets.Asset
}

func (s *Strategy) rank(in strategy.DecisionInput) []candidate {
	universe := s.cfg.Universe
	if len(universe) == 0 {
		for _, a := range s.registry.Tradable() {
			universe = append(universe, a.Symbol)
		}
	}
	out := make([]candidate, 0, len(universe))
	for _, sym := range universe {
		a, ok := s.registry.Get(sym)
		if !ok || !a.Tradable() {
			continue
		}
		snap, ok := in.Market.Symbols[a.Perp.Symbol]
		if !ok {
			continue
		}
		ann := snap.Funding.Annualised()
		if ann.LessThan(s.cfg.EntryAnnualisedFunding) {
			continue
		}
		out = append(out, candidate{
			Symbol:     a.Symbol,
			Funding:    snap.Funding,
			Annualised: ann,
			Asset:      a,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Annualised.GreaterThan(out[j].Annualised) })
	return out
}

func shouldClose(p types.BasisPosition, snap types.MarketSnapshot, cfg Config) bool {
	if p.State != types.BasisStateOpen {
		return false
	}
	for _, leg := range p.Legs {
		if leg.Kind != types.BasisLegPerp {
			continue
		}
		s, ok := snap.Symbols[leg.Symbol]
		if !ok {
			return false
		}
		ann := s.Funding.Annualised()
		return ann.LessThan(cfg.ExitAnnualisedFunding)
	}
	return false
}

func openSymbolSet(positions []types.BasisPosition) map[string]bool {
	out := make(map[string]bool, len(positions))
	for _, p := range positions {
		if p.State == types.BasisStateOpen || p.State == types.BasisStateOpening {
			out[strings.ToUpper(p.Underlying)] = true
		}
	}
	return out
}

// openIntents builds the spot-buy + perp-short pair for a candidate.
//
// The spot leg trades USDC for the spot mint and is naturally USDC-sized
// (Jupiter takes InAmount in input-token units, so USDC works directly).
//
// The perp leg needs base-asset units (e.g. WIF tokens), not USDC notional.
// We translate using the venue-published mark price:
//
//	perp.Size = position_cap_usdc / mark_price
//
// If the mark is unknown (zero) we cannot size safely and refuse the open
// — callers will see "skip <SYMBOL>: no mark price" in Decision.Notes.
func openIntents(c candidate, cfg Config, agentID string) (types.SwapIntent, types.OrderIntent, error) {
	mark := c.Funding.MarkPrice
	if !mark.IsPositive() {
		return types.SwapIntent{}, types.OrderIntent{},
			fmt.Errorf("no mark price for %s — venue cache miss?", c.Symbol)
	}
	perpSize := cfg.PositionCapUSDC.Div(mark)

	usdc := types.Asset{Symbol: "USDC", Chain: c.Asset.Spot.Chain, Mint: usdcMintFor(c.Asset.Spot.Chain), Decimals: 6}
	// BasisKey is the registry asset symbol — uniquely identifies the
	// (perp_symbol, spot_chain) pair so multi-chain entries (ETH-BASE vs
	// ETH-MAINNET) reconcile correctly.
	basisKey := c.Symbol
	swap := types.SwapIntent{
		Chain:       c.Asset.Spot.Chain,
		InToken:     usdc,
		OutToken:    c.Asset.AsAsset(),
		InAmount:    cfg.PositionCapUSDC,
		SlippageBps: cfg.SlippageBps,
		BasisKey:    basisKey,
		Tag:         "open:" + c.Symbol,
	}
	perp := types.OrderIntent{
		Venue:      c.Asset.Perp.Venue,
		Symbol:     c.Asset.Perp.Symbol,
		Side:       types.SideSell,
		Type:       types.OrderTypeLimit, // resting maker far from book; live tests use a price near mark
		Price:      mark,                 // strategy submits at mark; venue rounds size to szDecimals
		Size:       perpSize,
		TIF:        types.TIFGTC,
		ReduceOnly: false,
		BasisKey:   basisKey,
		Tag:        "open:" + c.Symbol,
	}
	_ = agentID
	return swap, perp, nil
}

// closeIntents builds the unwind pair for an open basis position.
func closeIntents(p types.BasisPosition, agentID string) (types.SwapIntent, types.OrderIntent) {
	var spotLeg, perpLeg types.BasisLeg
	for _, l := range p.Legs {
		switch l.Kind {
		case types.BasisLegSpot:
			spotLeg = l
		case types.BasisLegPerp:
			perpLeg = l
		}
	}
	usdc := types.Asset{Symbol: "USDC", Chain: spotLeg.Asset.Chain, Mint: usdcMintFor(spotLeg.Asset.Chain), Decimals: 6}
	swap := types.SwapIntent{
		Chain:    spotLeg.Asset.Chain,
		InToken:  spotLeg.Asset,
		OutToken: usdc,
		InAmount: spotLeg.Qty,
		BasisKey: p.Underlying,
		Tag:      "close:" + p.Underlying,
	}
	perp := types.OrderIntent{
		Venue:      "hyperliquid",
		Symbol:     perpLeg.Symbol,
		Side:       types.SideBuy, // closing a short → buy back
		Type:       types.OrderTypeMarket,
		BasisKey:   p.Underlying,
		Size:       perpLeg.Qty,
		ReduceOnly: true,
		Tag:        "close:" + p.Underlying,
	}
	_ = agentID
	return swap, perp
}

// usdcMintFor returns the canonical USDC mint/contract for a chain.
// Hard-coded for MVP; future versions can drive this off the registry.
//
// Sources:
//   - Solana:    Circle's native USDC mint
//   - Ethereum:  https://www.circle.com/usdc-multichain/ethereum
//   - Base:      Circle's native USDC on Base (NOT USDbC, which is bridged)
//   - Avalanche: Circle's native USDC on Avalanche C-Chain
//   - BSC:       Binance-Peg USDC (no native USDC; pegged is the de-facto liquid one)
//
// All EVM USDC contracts are 6-decimal; Solana USDC is also 6-decimal.
func usdcMintFor(chain types.ChainID) string {
	switch chain {
	case types.ChainSolana:
		return "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	case types.ChainEthereum:
		return "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	case types.ChainBase:
		return "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	case types.ChainAvalanche:
		return "0xB97EF9Ef8734C71904D8002F8b6Bc66Dd9c48a6E"
	case types.ChainBSC:
		return "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"
	default:
		return ""
	}
}

func appendNote(existing, line string) string {
	if existing == "" {
		return line
	}
	return existing + "\n" + line
}

// candidateConfidence is a simple monotonic mapping: more candidates → higher
// confidence, capped at 1.0.
func candidateConfidence(c []candidate) float64 {
	switch n := len(c); {
	case n == 0:
		return 0
	case n >= 5:
		return 1.0
	default:
		return float64(n) / 5.0
	}
}
