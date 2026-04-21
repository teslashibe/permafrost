// Package alpha_dca implements a dollar-cost-averaging strategy for
// Bittensor subnet alpha tokens: buy a fixed TAO amount of alpha every
// N ticks. Simplest possible entry point for Bittensor subnet trading.
//
// Fork this strategy and tune the config to match your thesis on which
// subnets will accrue value. No LLM, no external signals — pure DCA.
package alpha_dca

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

const Name = "alpha_dca"

func init() { strategy.Register(Name, New) }

// Config controls DCA behaviour for alpha tokens.
type Config struct {
	// Subnets is the list of netuids to buy into (e.g. [8, 3, 19]).
	Subnets []uint16

	// TAOPerBuy is the amount of TAO to spend per subnet per buy tick.
	TAOPerBuy decimal.Decimal

	// IntervalTicks is the minimum number of ticks between buys.
	// With a 30s agent tick, IntervalTicks=10 → buy every 5 minutes.
	IntervalTicks int

	// SlippageBps caps the per-swap slippage tolerance.
	SlippageBps int
}

func (c *Config) Defaults() {
	if len(c.Subnets) == 0 {
		c.Subnets = []uint16{8, 3, 19}
	}
	if c.TAOPerBuy.IsZero() {
		c.TAOPerBuy = decimal.NewFromFloat(1.0)
	}
	if c.IntervalTicks == 0 {
		c.IntervalTicks = 10
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 100 // 1% — AMM slippage can be significant
	}
}

func (c Config) Validate() error {
	if len(c.Subnets) == 0 {
		return errors.New("alpha_dca: subnets must not be empty")
	}
	if !c.TAOPerBuy.IsPositive() {
		return errors.New("alpha_dca: tao_per_buy must be positive")
	}
	if c.IntervalTicks < 1 {
		return errors.New("alpha_dca: interval_ticks must be >= 1")
	}
	return nil
}

type Strategy struct {
	cfg       Config
	tickCount int
	lastBuyAt time.Time
}

func New(cfg map[string]any) (strategy.Strategy, error) {
	c, err := parseConfig(cfg)
	if err != nil {
		return nil, err
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Strategy{cfg: c}, nil
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

func (s *Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

func (s *Strategy) Decide(_ context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	s.tickCount++

	if s.tickCount%s.cfg.IntervalTicks != 0 {
		remaining := s.cfg.IntervalTicks - (s.tickCount % s.cfg.IntervalTicks)
		return strategy.Decision{
			Notes: fmt.Sprintf("alpha_dca: waiting %d more ticks", remaining),
		}, nil
	}

	taoAsset := types.Asset{
		Symbol: "TAO",
		Chain:  types.ChainBittensor,
		Mint:   "TAO",
	}

	var swaps []types.SwapIntent
	for _, netuid := range s.cfg.Subnets {
		symbol := fmt.Sprintf("SN%d", netuid)
		alphaAsset := types.Asset{
			Symbol: symbol,
			Chain:  types.ChainBittensor,
			Mint:   symbol,
		}

		swaps = append(swaps, types.SwapIntent{
			Chain:       types.ChainBittensor,
			InToken:     taoAsset,
			OutToken:    alphaAsset,
			InAmount:    s.cfg.TAOPerBuy,
			SlippageBps: s.cfg.SlippageBps,
			BasisKey:    fmt.Sprintf("alpha_dca:%s", symbol),
			Tag:         "alpha_dca",
		})
	}

	s.lastBuyAt = in.Now
	subnetsStr := make([]string, len(s.cfg.Subnets))
	for i, n := range s.cfg.Subnets {
		subnetsStr[i] = fmt.Sprintf("SN%d", n)
	}

	return strategy.Decision{
		Swaps: swaps,
		Notes: fmt.Sprintf("alpha_dca: buying %s TAO each of [%s]",
			s.cfg.TAOPerBuy, strings.Join(subnetsStr, ", ")),
		Confidence: 1.0,
	}, nil
}

func parseConfig(in map[string]any) (Config, error) {
	var out Config
	if v, ok := in["subnets"]; ok {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				n, err := intFromAny(item)
				if err != nil {
					return out, fmt.Errorf("alpha_dca: subnets item: %w", err)
				}
				out.Subnets = append(out.Subnets, uint16(n))
			}
		default:
			return out, fmt.Errorf("alpha_dca: subnets must be a list, got %T", v)
		}
	}
	if v, ok := in["tao_per_buy"]; ok {
		d, err := decimalFromAny(v)
		if err != nil {
			return out, fmt.Errorf("alpha_dca: tao_per_buy: %w", err)
		}
		out.TAOPerBuy = d
	}
	if v, ok := in["interval_ticks"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("alpha_dca: interval_ticks: %w", err)
		}
		out.IntervalTicks = n
	}
	if v, ok := in["slippage_bps"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("alpha_dca: slippage_bps: %w", err)
		}
		out.SlippageBps = n
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
