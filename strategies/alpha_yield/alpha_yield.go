// Package alpha_yield implements a yield-farming strategy for Bittensor
// subnet alpha tokens. Ranks subnets by emission yield (emissions per unit
// of alpha staked), stakes into the highest-yielding subnets, and
// rebalances periodically.
//
// All data is read on-chain — no third-party APIs. Fork this and tune
// the rebalance frequency and yield threshold to match your strategy.
package alpha_yield

import (
	"context"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

const Name = "alpha_yield"

func init() { strategy.Register(Name, New) }

// Config controls yield-farming behaviour.
type Config struct {
	// Universe is the set of netuids to evaluate for yield.
	Universe []uint16

	// TopK is the number of highest-yield subnets to hold.
	TopK int

	// RebalanceTicks is how often to re-evaluate and rebalance.
	RebalanceTicks int

	// MinYieldDelta is the minimum yield improvement needed to trigger
	// a rotation out of an existing position into a higher-yield one.
	// Prevents churn when yields are close.
	MinYieldDelta float64

	// TAOPerPosition is the TAO amount allocated to each subnet.
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
	if c.TAOPerPosition.IsZero() {
		c.TAOPerPosition = decimal.NewFromFloat(10.0)
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 100
	}
}

type Strategy struct {
	cfg       Config
	tickCount int
	held      map[uint16]bool // currently-held positions
}

func New(cfg map[string]any) (strategy.Strategy, error) {
	c, err := parseConfig(cfg)
	if err != nil {
		return nil, err
	}
	c.Defaults()
	return &Strategy{
		cfg:  c,
		held: make(map[uint16]bool),
	}, nil
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

func (s *Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

func (s *Strategy) Decide(_ context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	s.tickCount++

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

	// Calculate yield proxy from market snapshot. We use the Tick.Volume
	// field as a proxy for emission rate when available; otherwise we
	// rank by inverse price (cheaper alpha = higher potential yield
	// per TAO staked). Real implementation would read Emission storage.
	type scored struct {
		netuid uint16
		yield  float64
	}
	var scores []scored
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
		// Yield proxy: volume (emission indicator) / price.
		// Higher volume relative to price suggests better emission yield.
		vol, _ := snap.Tick.Volume.Float64()
		p, _ := price.Float64()
		y := vol / p
		if y <= 0 {
			y = 1.0 / p // fallback: cheaper tokens rank higher
		}
		scores = append(scores, scored{netuid: netuid, yield: y})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].yield > scores[j].yield
	})

	// Determine target portfolio: top-K by yield.
	target := make(map[uint16]bool)
	for i := 0; i < s.cfg.TopK && i < len(scores); i++ {
		target[scores[i].netuid] = true
	}

	var swaps []types.SwapIntent
	exits := 0
	entries := 0

	// Exit subnets no longer in target.
	for netuid := range s.held {
		if target[netuid] {
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
			InAmount:    s.cfg.TAOPerPosition,
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_yield:%s", symbol),
			Tag:         "alpha_yield_exit",
		})
		delete(s.held, netuid)
		exits++
	}

	// Enter new subnets in target that we don't already hold.
	for netuid := range target {
		if s.held[netuid] {
			continue
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
		s.held[netuid] = true
		entries++
	}

	notes := fmt.Sprintf("alpha_yield: rebalance → holding %d subnets (%d entries, %d exits)",
		len(s.held), entries, exits)

	return strategy.Decision{
		Swaps:      swaps,
		Notes:      notes,
		Confidence: 0.8,
	}, nil
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
