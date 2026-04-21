// Package alpha_momentum implements a momentum-rotation strategy for
// Bittensor subnet alpha tokens.
//
// Tracks rolling price changes across subnets in the universe, rotates
// into the top-K by momentum, exits positions where momentum flips
// negative. Pure on-chain price-action — no external signals.
//
// Position sizing notes:
//   - Entries use a fixed TAO notional per position (TAOPerPosition).
//   - Exits sell the alpha amount we estimate we hold, computed from
//     the entry tao notional / entry price. This is approximate; for
//     a perfect close-out the runtime should query the venue's actual
//     alpha balance — wired in a follow-up via SpotBalances enrichment.
package alpha_momentum

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

const Name = "alpha_momentum"

func init() { strategy.Register(Name, New) }

// Config controls momentum behaviour.
type Config struct {
	// Universe is the set of netuids to track for momentum.
	Universe []uint16

	// WindowTicks is the rolling window length for momentum calculation.
	WindowTicks int

	// TopK is the number of subnets to hold at any time.
	TopK int

	// ExitThreshold is the momentum value below which positions are exited.
	ExitThreshold float64

	// TAOPerPosition is the TAO amount allocated to each subnet position.
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
	if c.WindowTicks == 0 {
		c.WindowTicks = 30
	}
	if c.TopK == 0 {
		c.TopK = 5
	}
	if c.ExitThreshold == 0 {
		c.ExitThreshold = -0.02
	}
	if c.TAOPerPosition.IsZero() {
		c.TAOPerPosition = decimal.NewFromFloat(5.0)
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 100
	}
}

type scored struct {
	netuid   uint16
	momentum float64
}

// position records what we hold for a subnet so we can size exits correctly.
type position struct {
	alphaHeld  decimal.Decimal // estimated alpha amount, in alpha units
	entryPrice decimal.Decimal // TAO per alpha at entry
}

type Strategy struct {
	cfg     Config
	history map[uint16][]decimal.Decimal // netuid → price history ring buffer
	held    map[uint16]position          // currently-held positions
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
	taoAsset := types.Asset{
		Symbol: "TAO",
		Chain:  types.ChainBittensor,
		Mint:   "TAO",
	}

	// Record current prices from market snapshot.
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
		if len(hist) > s.cfg.WindowTicks {
			hist = hist[len(hist)-s.cfg.WindowTicks:]
		}
		s.history[netuid] = hist
	}

	// Calculate momentum for each subnet with enough history.
	var scores []scored
	for _, netuid := range s.cfg.Universe {
		hist := s.history[netuid]
		if len(hist) < 2 {
			continue
		}
		first := hist[0]
		last := hist[len(hist)-1]
		if first.IsZero() {
			continue
		}
		mom, _ := last.Sub(first).Div(first).Float64()
		scores = append(scores, scored{netuid: netuid, momentum: mom})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].momentum > scores[j].momentum
	})

	var swaps []types.SwapIntent
	exits, entries := 0, 0

	// Exit positions where momentum has flipped below threshold.
	for netuid, pos := range s.held {
		mom := findMomentum(scores, netuid)
		if mom >= s.cfg.ExitThreshold {
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
			InAmount:    pos.alphaHeld, // sized in alpha units
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_momentum:%s", symbol),
			Tag:         "alpha_momentum_exit",
		})
		delete(s.held, netuid)
		exits++
	}

	// Enter top-K subnets with positive momentum that we don't already hold.
	for _, sc := range scores {
		if len(s.held) >= s.cfg.TopK {
			break
		}
		if _, alreadyHeld := s.held[sc.netuid]; alreadyHeld || sc.momentum <= 0 {
			continue
		}
		// Look up current price to estimate alpha-out for sizing
		// the eventual exit.
		entryPrice := decimal.Zero
		if hist := s.history[sc.netuid]; len(hist) > 0 {
			entryPrice = hist[len(hist)-1]
		}
		var estimatedAlpha decimal.Decimal
		if !entryPrice.IsZero() {
			estimatedAlpha = s.cfg.TAOPerPosition.Div(entryPrice)
		}

		symbol := fmt.Sprintf("SN%d", sc.netuid)
		swaps = append(swaps, types.SwapIntent{
			Chain:   types.ChainBittensor,
			InToken: taoAsset,
			OutToken: types.Asset{
				Symbol: symbol,
				Chain:  types.ChainBittensor,
				Mint:   symbol,
			},
			InAmount:    s.cfg.TAOPerPosition, // sized in TAO for entries
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_momentum:%s", symbol),
			Tag:         "alpha_momentum_enter",
		})
		s.held[sc.netuid] = position{
			alphaHeld:  estimatedAlpha,
			entryPrice: entryPrice,
		}
		entries++
	}

	notes := fmt.Sprintf("alpha_momentum: holding %d subnets, %d entries, %d exits",
		len(s.held), entries, exits)

	return strategy.Decision{
		Swaps:      swaps,
		Notes:      notes,
		Confidence: 0.7,
	}, nil
}

func findMomentum(scores []scored, netuid uint16) float64 {
	for _, s := range scores {
		if s.netuid == netuid {
			return s.momentum
		}
	}
	return math.Inf(-1)
}

func parseConfig(in map[string]any) (Config, error) {
	var out Config
	if v, ok := in["universe"]; ok {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				n, err := intFromAny(item)
				if err != nil {
					return out, fmt.Errorf("alpha_momentum: universe item: %w", err)
				}
				out.Universe = append(out.Universe, uint16(n))
			}
		}
	}
	if v, ok := in["window_ticks"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.WindowTicks = n
		}
	}
	if v, ok := in["top_k"]; ok {
		n, err := intFromAny(v)
		if err == nil {
			out.TopK = n
		}
	}
	if v, ok := in["exit_threshold"]; ok {
		if f, ok := v.(float64); ok {
			out.ExitThreshold = f
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
