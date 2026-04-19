// Package dca_buy implements a deterministic dollar-cost-averaging
// strategy: buy a fixed USDC amount of a configured spot asset every
// N hours, no shorting, no LLM. Demonstrates the SwapIntent-only path
// of the SAPI.
//
// In the arctic-theme universe (epic #30), DCA is one of the patient
// expedition routines — small, regular, builds the position slowly.
// Boulder the polar bear is the canonical operator for this strategy.
package dca_buy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Name is the registered identifier.
const Name = "dca_buy"

func init() { strategy.Register(Name, New) }

// Config controls DCA behaviour. All fields have safe defaults so an
// agent can be created with an empty config and run a sensible baseline.
type Config struct {
	// Asset is the spot symbol to buy (e.g. "SOL", "WIF").
	// Resolved against the embedded asset registry.
	Asset string

	// Chain is the chain to settle on (defaults to ChainSolana).
	Chain types.ChainID

	// USDCPerTick is the notional in USDC to buy each tick.
	USDCPerTick decimal.Decimal

	// IntervalSecs is the minimum gap between buys in seconds. Tick
	// schedule is set on the agent — this is an additional throttle so
	// a fast tick rate doesn't trigger faster purchases. Default 4h.
	IntervalSecs int

	// SlippageBps caps the per-swap slippage tolerance.
	SlippageBps int
}

// Defaults applies sensible defaults to zero-valued fields.
func (c *Config) Defaults() {
	if c.Asset == "" {
		c.Asset = "SOL"
	}
	if c.Chain == "" {
		c.Chain = types.ChainSolana
	}
	if c.USDCPerTick.IsZero() {
		c.USDCPerTick = decimal.NewFromInt(50)
	}
	if c.IntervalSecs == 0 {
		c.IntervalSecs = 4 * 60 * 60 // 4h
	}
	if c.SlippageBps == 0 {
		c.SlippageBps = 50
	}
}

// Validate sanity-checks the config.
func (c Config) Validate() error {
	if !c.USDCPerTick.IsPositive() {
		return errors.New("dca_buy: usdc_per_tick must be positive")
	}
	if c.IntervalSecs < 0 {
		return errors.New("dca_buy: interval_secs must be non-negative")
	}
	if c.SlippageBps < 0 || c.SlippageBps > 1000 {
		return errors.New("dca_buy: slippage_bps out of range (0..1000)")
	}
	return nil
}

// Strategy is the DCA implementation.
type Strategy struct {
	cfg Config
	// lastBuyAt is in-memory cooldown state. Persistent state lives in
	// the framework's BasisPositions; v2 will compute "last buy" from
	// there. For v1 the in-memory clock survives within a process.
	lastBuyAt time.Time
}

// New is the strategy.Constructor entry point.
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

// NewFromTypedConfig is the test-only direct constructor.
func NewFromTypedConfig(c Config) (*Strategy, error) {
	c.Defaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Strategy{cfg: c}, nil
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

// Warmup is a no-op for DCA — no per-agent services to wire.
func (s *Strategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }

// Decide emits at most one SwapIntent per tick: USDC → configured asset.
// Skips if we're inside the configured cooldown.
func (s *Strategy) Decide(_ context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	if !s.lastBuyAt.IsZero() && in.Now.Sub(s.lastBuyAt) < time.Duration(s.cfg.IntervalSecs)*time.Second {
		return strategy.Decision{Notes: fmt.Sprintf("cooldown: %s remaining",
			(time.Duration(s.cfg.IntervalSecs)*time.Second - in.Now.Sub(s.lastBuyAt)).Truncate(time.Second))}, nil
	}

	usdc, ok := assets.USDCAsset(s.cfg.Chain)
	if !ok {
		return strategy.Decision{Notes: fmt.Sprintf("dca_buy: chain %s has no USDC mapping", s.cfg.Chain)}, nil
	}
	// Out token: same chain, asset symbol from cfg. We don't know the
	// out-token mint here — that's the registry's job — but the runtime
	// fills the spot venue from cfg.Chain and the swap router resolves
	// the symbol on the configured DEX. Mark Decimals=0 so callers
	// know it's unset; Jupiter / 1inch resolve mint by symbol on the
	// chain anyway.
	out := types.Asset{Symbol: strings.ToUpper(s.cfg.Asset), Chain: s.cfg.Chain}

	intent := types.SwapIntent{
		Chain:       s.cfg.Chain,
		InToken:     usdc,
		OutToken:    out,
		InAmount:    s.cfg.USDCPerTick,
		SlippageBps: s.cfg.SlippageBps,
		BasisKey:    "dca:" + strings.ToUpper(s.cfg.Asset),
		Tag:         "dca_buy",
	}
	s.lastBuyAt = in.Now
	return strategy.Decision{
		Swaps: []types.SwapIntent{intent},
		Notes: fmt.Sprintf("dca buy: %s USDC → %s on %s", s.cfg.USDCPerTick, intent.OutToken.Symbol, s.cfg.Chain),
		Confidence: 1.0, // deterministic strategy; always confident in its own decision
	}, nil
}

// parseConfig pulls the typed Config out of the generic cfg map.
func parseConfig(in map[string]any) (Config, error) {
	var out Config
	if v, ok := in["asset"]; ok {
		if s, ok := v.(string); ok {
			out.Asset = s
		} else {
			return out, fmt.Errorf("dca_buy: asset must be a string, got %T", v)
		}
	}
	if v, ok := in["chain"]; ok {
		if s, ok := v.(string); ok {
			out.Chain = types.ChainID(s)
		} else {
			return out, fmt.Errorf("dca_buy: chain must be a string, got %T", v)
		}
	}
	if v, ok := in["usdc_per_tick"]; ok {
		d, err := decimalFromAny(v)
		if err != nil {
			return out, fmt.Errorf("dca_buy: usdc_per_tick: %w", err)
		}
		out.USDCPerTick = d
	}
	if v, ok := in["interval_secs"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("dca_buy: interval_secs: %w", err)
		}
		out.IntervalSecs = n
	}
	if v, ok := in["slippage_bps"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("dca_buy: slippage_bps: %w", err)
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
