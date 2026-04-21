// Package alpha_yield implements a price-stability + low-volatility
// yield-proxy strategy for Bittensor subnet alpha tokens.
//
// "Yield" on Bittensor alpha tokens is the rate at which holding alpha
// accrues additional alpha through staking emissions. Without direct
// access to on-chain Emission storage from inside the strategy, we use
// a proxy: rank subnets by the inverse of recent volatility — stable,
// liquid pools tend to be where emissions actually accumulate value
// rather than getting eroded by churn.
//
// This is a deliberately conservative stand-in until the framework
// surfaces emission data via Services. The strategy still aims at the
// same goal (yield-stable subnets) and is forkable.
package alpha_yield

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

const Name = "alpha_yield"

func init() { strategy.Register(Name, New) }

// Config controls yield-farming behaviour.
type Config struct {
	// Universe is the set of netuids to evaluate.
	Universe []uint16

	// TopK is the number of subnets to hold.
	TopK int

	// RebalanceTicks is how often to re-evaluate and rebalance.
	RebalanceTicks int

	// MinYieldDelta is the minimum yield improvement (or stability
	// improvement, in proxy mode) to trigger a rotation.
	MinYieldDelta float64

	// VolatilityWindow is the number of ticks over which to measure
	// price stability for the yield proxy.
	VolatilityWindow int

	// TAOPerPosition is the TAO amount allocated per subnet.
	TAOPerPosition decimal.Decimal

	// SlippageBps caps per-swap slippage tolerance.
	SlippageBps int
}

func (c *Config) Defaults() {
	if len(c.Universe) == 0 {
		c.Universe = make([]uint16, 64)
		for i := range c.Universe {
			c.Universe[i] = uint16(i + 1)
		}
	}
	if c.TopK == 0 {
		c.TopK = 3
	}
	if c.RebalanceTicks == 0 {
		c.RebalanceTicks = 50
	}
	if c.MinYieldDelta == 0 {
		c.MinYieldDelta = 0.05
	}
	if c.VolatilityWindow == 0 {
		c.VolatilityWindow = 30
	}
	if c.TAOPerPosition.IsZero() {
		c.TAOPerPosition = decimal.NewFromFloat(10.0)
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 100
	}
}

type position struct {
	alphaHeld  decimal.Decimal
	entryPrice decimal.Decimal
}

type Strategy struct {
	cfg       Config
	tickCount int
	history   map[uint16][]decimal.Decimal // price history for volatility calc
	held      map[uint16]position
}

func New(cfg map[string]any) (strategy.Strategy, error) {
	c, err := parseConfig(cfg)
	if err != nil {
		return nil, err
	}
	c.Defaults()
	return &Strategy{
		cfg:     c,
		history: make(map[uint16][]decimal.Decimal),
		held:    make(map[uint16]position),
	}, nil
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

func (s *Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

func (s *Strategy) Decide(_ context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	s.tickCount++

	// Always update history every tick.
	for _, netuid := range s.cfg.Universe {
		symbol := fmt.Sprintf("SN%d/TAO", netuid)
		snap, ok := in.Market.Symbols[symbol]
		if !ok {
			continue
		}
		price := snap.Tick.Mid()
		if price.IsZero() {
			continue
		}
		hist := s.history[netuid]
		hist = append(hist, price)
		if len(hist) > s.cfg.VolatilityWindow {
			hist = hist[len(hist)-s.cfg.VolatilityWindow:]
		}
		s.history[netuid] = hist
	}

	// Rebalance only on cadence.
	if s.tickCount%s.cfg.RebalanceTicks != 0 && len(s.held) > 0 {
		return strategy.Decision{
			Notes: fmt.Sprintf("alpha_yield: holding %d subnets, %d ticks until rebalance",
				len(s.held), s.cfg.RebalanceTicks-(s.tickCount%s.cfg.RebalanceTicks)),
		}, nil
	}

	taoAsset := types.Asset{
		Symbol: "TAO",
		Chain:  types.ChainBittensor,
		Mint:   "TAO",
	}

	// Score subnets by yield proxy = 1 / (1 + relative_volatility).
	// Lower volatility ⇒ higher score ⇒ better candidate for stable
	// emissions accumulation.
	type scored struct {
		netuid uint16
		score  float64
	}
	var scores []scored
	for _, netuid := range s.cfg.Universe {
		hist := s.history[netuid]
		// relativeStdDev needs ≥ 2 prices to compute a return series. We
		// previously gated on ≥ 5 here, but that conflicted with operator
		// configs that set volatility_window < 5: history would never grow
		// long enough to score, so the strategy entered nothing forever.
		// Use the actual lower bound of the math instead.
		if len(hist) < 2 {
			continue
		}
		vol := relativeStdDev(hist)
		if math.IsNaN(vol) || math.IsInf(vol, 0) {
			continue
		}
		scores = append(scores, scored{netuid: netuid, score: 1.0 / (1.0 + vol)})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Build target portfolio: top-K.
	target := make(map[uint16]bool)
	for i := 0; i < s.cfg.TopK && i < len(scores); i++ {
		target[scores[i].netuid] = true
	}

	var swaps []types.SwapIntent
	exits, entries := 0, 0

	// Exit subnets no longer in target.
	for netuid, pos := range s.held {
		if target[netuid] {
			continue
		}
		if pos.alphaHeld.IsZero() {
			delete(s.held, netuid)
			continue
		}
		symbol := fmt.Sprintf("SN%d", netuid)
		swaps = append(swaps, types.SwapIntent{
			Chain: types.ChainBittensor,
			InToken: types.Asset{
				Symbol: symbol,
				Chain:  types.ChainBittensor,
				Mint:   symbol,
			},
			OutToken:    taoAsset,
			InAmount:    pos.alphaHeld,
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_yield:%s", symbol),
			Tag:         "alpha_yield_exit",
		})
		delete(s.held, netuid)
		exits++
	}

	// Enter subnets in target that we don't already hold.
	for netuid := range target {
		if _, ok := s.held[netuid]; ok {
			continue
		}
		entryPrice := decimal.Zero
		if hist := s.history[netuid]; len(hist) > 0 {
			entryPrice = hist[len(hist)-1]
		}
		var estimatedAlpha decimal.Decimal
		if !entryPrice.IsZero() {
			estimatedAlpha = s.cfg.TAOPerPosition.Div(entryPrice)
		}

		symbol := fmt.Sprintf("SN%d", netuid)
		swaps = append(swaps, types.SwapIntent{
			Chain:   types.ChainBittensor,
			InToken: taoAsset,
			OutToken: types.Asset{
				Symbol: symbol,
				Chain:  types.ChainBittensor,
				Mint:   symbol,
			},
			InAmount:    s.cfg.TAOPerPosition,
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_yield:%s", symbol),
			Tag:         "alpha_yield_enter",
		})
		s.held[netuid] = position{
			alphaHeld:  estimatedAlpha,
			entryPrice: entryPrice,
		}
		entries++
	}

	notes := fmt.Sprintf("alpha_yield: rebalance → holding %d subnets (%d entries, %d exits)",
		len(s.held), entries, exits)

	return strategy.Decision{
		Swaps:      swaps,
		Notes:      notes,
		Confidence: 0.7,
	}, nil
}

// relativeStdDev returns the standard deviation of price returns
// expressed as a fraction of the mean. Returns NaN for empty inputs.
func relativeStdDev(prices []decimal.Decimal) float64 {
	if len(prices) < 2 {
		return math.NaN()
	}
	rs := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		prev, _ := prices[i-1].Float64()
		cur, _ := prices[i].Float64()
		if prev == 0 {
			continue
		}
		rs = append(rs, (cur-prev)/prev)
	}
	if len(rs) == 0 {
		return math.NaN()
	}
	var sum float64
	for _, r := range rs {
		sum += r
	}
	mean := sum / float64(len(rs))
	var ss float64
	for _, r := range rs {
		d := r - mean
		ss += d * d
	}
	variance := ss / float64(len(rs))
	return math.Sqrt(variance)
}

func parseConfig(in map[string]any) (Config, error) {
	var out Config
	if v, ok := in["universe"]; ok {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				n, err := intFromAny(item)
				if err != nil {
					return out, fmt.Errorf("alpha_yield: universe item: %w", err)
				}
				out.Universe = append(out.Universe, uint16(n))
			}
		}
	}
	if v, ok := in["top_k"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.TopK = n
		}
	}
	if v, ok := in["rebalance_ticks"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.RebalanceTicks = n
		}
	}
	if v, ok := in["min_yield_delta"]; ok {
		if f, ok := v.(float64); ok {
			out.MinYieldDelta = f
		}
	}
	if v, ok := in["volatility_window"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.VolatilityWindow = n
		}
	}
	if v, ok := in["tao_per_position"]; ok {
		d, err := decimalFromAny(v)
		if err == nil {
			out.TAOPerPosition = d
		}
	}
	if v, ok := in["slippage_bps"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.SlippageBps = n
		}
	}
	return out, nil
}

func decimalFromAny(v any) (decimal.Decimal, error) {
	switch x := v.(type) {
	case decimal.Decimal:
		return x, nil
	case float64:
		return decimal.NewFromFloat(x), nil
	case int:
		return decimal.NewFromInt(int64(x)), nil
	case int64:
		return decimal.NewFromInt(x), nil
	case string:
		return decimal.NewFromString(x)
	default:
		return decimal.Decimal{}, fmt.Errorf("unsupported type %T", v)
	}
}

func intFromAny(v any) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		return int(x), nil
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}
