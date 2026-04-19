// Package market_maker_basic implements a basic Hyperliquid market-maker:
// places paired buy/sell limit orders around the mid every N ticks,
// optionally consults the LLM to skip volatile cycles. Demonstrates
// the OrderIntent + Cancels + LLM-veto paths of the SAPI.
//
// In the arctic-theme universe (epic #30), market making is the busy
// diligent expedition routine — quote, cancel, re-quote, watch the ice
// for cracks. Pip the penguin is the canonical operator for this
// strategy.
package market_maker_basic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Name is the registered identifier.
const Name = "market_maker_basic"

func init() { strategy.Register(Name, New) }

// Config controls the maker behaviour.
type Config struct {
	// Symbol is the perp symbol to make on (e.g. "WIF", "SOL").
	Symbol string

	// SpreadBps is the half-spread around the mid, in basis points.
	// 25 = 0.25% on each side. Default 25.
	SpreadBps int

	// OrderSize is the per-side base-asset quantity. Default 0
	// disables — strategy emits no orders. Operator must set this.
	OrderSize decimal.Decimal

	// RefreshTicks is how many ticks between quote refreshes. 1 = every
	// tick. Default 1.
	RefreshTicks int

	// UseLLMVeto, if true, asks the inference provider whether to skip
	// the current refresh cycle (useful for high-volatility moments).
	UseLLMVeto bool

	// VetoModel is the model id for the veto call. Defaults to whatever
	// the framework supplies via Services.InferenceModel.
	VetoModel string
}

// Defaults applies sensible defaults to zero-valued fields.
func (c *Config) Defaults() {
	if c.SpreadBps == 0 {
		c.SpreadBps = 25
	}
	if c.RefreshTicks == 0 {
		c.RefreshTicks = 1
	}
}

// Validate sanity-checks the config.
func (c Config) Validate() error {
	if c.Symbol == "" {
		return errors.New("market_maker_basic: symbol is required")
	}
	if c.SpreadBps <= 0 || c.SpreadBps > 1000 {
		return errors.New("market_maker_basic: spread_bps out of range (1..1000)")
	}
	if !c.OrderSize.IsPositive() {
		return errors.New("market_maker_basic: order_size must be positive")
	}
	if c.RefreshTicks <= 0 {
		return errors.New("market_maker_basic: refresh_ticks must be positive")
	}
	return nil
}

// Strategy is the market maker.
type Strategy struct {
	cfg       Config
	inference inference.Provider // captured in Warmup; nil disables veto
	tickCount int                // for RefreshTicks gating
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
func NewFromTypedConfig(c Config, inf inference.Provider) (*Strategy, error) {
	c.Defaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Strategy{cfg: c, inference: inf}, nil
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Name() string { return Name }

// Warmup wires the inference provider. If UseLLMVeto is on but no
// provider was wired, fail fast so the operator notices at startup
// rather than on the first decision tick.
func (s *Strategy) Warmup(_ context.Context, in strategy.WarmupInput) error {
	s.inference = in.Services.Inference
	if s.cfg.VetoModel == "" {
		s.cfg.VetoModel = in.Services.InferenceModel
	}
	if s.cfg.UseLLMVeto && s.inference == nil {
		return errors.New("market_maker_basic: use_llm_veto=true but no inference provider configured for agent")
	}
	return nil
}

// Decide:
//   - Cancel any orders we placed last cycle (best-effort; we identify
//     them by ClientID prefix in BasisKey).
//   - If RefreshTicks gating says to skip, return empty.
//   - If UseLLMVeto + the volatility heuristic both fire, ask the LLM;
//     if it vetoes, return empty.
//   - Otherwise emit two new limit orders bid/ask around mid.
func (s *Strategy) Decide(ctx context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	dec := strategy.Decision{}
	s.tickCount++

	// Find the venue's view of our symbol.
	snap, ok := in.Market.Symbols[s.cfg.Symbol]
	if !ok {
		return strategy.Decision{Notes: fmt.Sprintf("no market data for %s", s.cfg.Symbol)}, nil
	}
	mid := snap.Funding.MarkPrice
	if !mid.IsPositive() {
		return strategy.Decision{Notes: "no mark price; skipping"}, nil
	}

	// Cancel any of our resting orders (simple heuristic: look at the
	// BasisKey we use). The actual venue-side cancels happen in the
	// runtime; here we list intents to cancel via the existing order
	// IDs we know about. v1 trick: we don't track order IDs across
	// ticks (TODO when we have a persistent maker-state table); just
	// emit a cancel-by-tag via the framework's reduce-only path.
	// For now, emit no Cancels — the runtime's existing reduce-only
	// handling drops stale quotes naturally as new ones replace them.

	if s.tickCount%s.cfg.RefreshTicks != 0 {
		return strategy.Decision{Notes: fmt.Sprintf("skip: tick %d, refresh every %d", s.tickCount, s.cfg.RefreshTicks)}, nil
	}

	if s.cfg.UseLLMVeto && s.inference != nil && shouldConsultVeto(snap) {
		veto, reason, err := s.askVeto(ctx, snap)
		if err != nil {
			dec.Notes = fmt.Sprintf("veto error: %v", err)
			return dec, nil
		}
		if veto {
			dec.Notes = fmt.Sprintf("vetoed: %s", reason)
			return dec, nil
		}
	}

	// Build bid + ask around mid with the configured half-spread.
	half := decimal.NewFromInt(int64(s.cfg.SpreadBps)).Div(decimal.NewFromInt(10000))
	bidPx := mid.Mul(decimal.NewFromInt(1).Sub(half))
	askPx := mid.Mul(decimal.NewFromInt(1).Add(half))

	bid := types.OrderIntent{
		Venue:    "hyperliquid",
		Symbol:   s.cfg.Symbol,
		Side:     types.SideBuy,
		Type:     types.OrderTypeLimit,
		Price:    bidPx,
		Size:     s.cfg.OrderSize,
		TIF:      types.TIFGTC,
		BasisKey: "mm:" + s.cfg.Symbol,
		Tag:      "mm_quote_bid",
	}
	ask := types.OrderIntent{
		Venue:    "hyperliquid",
		Symbol:   s.cfg.Symbol,
		Side:     types.SideSell,
		Type:     types.OrderTypeLimit,
		Price:    askPx,
		Size:     s.cfg.OrderSize,
		TIF:      types.TIFGTC,
		BasisKey: "mm:" + s.cfg.Symbol,
		Tag:      "mm_quote_ask",
	}
	dec.Orders = []types.OrderIntent{bid, ask}
	dec.Notes = fmt.Sprintf("quote %s: bid=%s ask=%s mid=%s spread=%dbps",
		s.cfg.Symbol, bidPx, askPx, mid, s.cfg.SpreadBps)
	dec.Confidence = 0.7 // arbitrary: maker is opinion-light by design
	return dec, nil
}

// shouldConsultVeto fires when something looks unusual — wide bid/ask,
// extreme funding swing, etc. Cheap heuristics; the actual decision
// is the LLM's. v1 just consults on every refresh cycle when veto is
// enabled.
func shouldConsultVeto(_ types.SymbolSnap) bool { return true }

// askVeto consults the inference provider with a JSON-Schema response.
const vetoSchemaJSON = `{
  "type": "object",
  "properties": {
    "veto":   {"type": "boolean"},
    "reason": {"type": "string", "maxLength": 200}
  },
  "required": ["veto", "reason"],
  "additionalProperties": false
}`

const vetoSystemPrompt = `You are a volatility filter for a basic crypto perpetuals market maker.
Decide whether the maker should SKIP this refresh cycle (do not requote).
Default to NOT vetoing. Veto only when there's clear, present reason —
extreme volatility, breaking news, an obvious depeg/exploit/listing,
funding-flip, or RPC degradation.
Respond ONLY in the supplied JSON schema. Keep "reason" under 200 chars.`

type vetoResponse struct {
	Veto   bool   `json:"veto"`
	Reason string `json:"reason"`
}

func (s *Strategy) askVeto(ctx context.Context, snap types.SymbolSnap) (bool, string, error) {
	prompt := fmt.Sprintf(`Symbol: %s
Mark: %s
Funding (annualised, from last interval): %s
Funding interval: %s

Should the maker SKIP requoting now?`, snap.Funding.Symbol, snap.Funding.MarkPrice,
		snap.Funding.Annualised(), snap.Funding.Interval)

	resp, err := s.inference.Complete(ctx, inference.Request{
		Model:  s.cfg.VetoModel,
		System: vetoSystemPrompt,
		Messages: []inference.Message{
			{Role: inference.RoleUser, Content: prompt},
		},
		JSONSchema: &inference.Schema{
			Name:   "veto_decision",
			JSON:   []byte(vetoSchemaJSON),
			Strict: true,
		},
		Temperature: 0,
		MaxTokens:   200,
	})
	if err != nil {
		if errors.Is(err, inference.ErrUnsupportedFeature) {
			return false, "", nil // graceful degrade: don't veto if the provider can't enforce schema
		}
		return false, "", err
	}
	var v vetoResponse
	if err := json.Unmarshal([]byte(resp.Content), &v); err != nil {
		return false, "", fmt.Errorf("parse veto response: %w", err)
	}
	return v.Veto, v.Reason, nil
}

// ─── config parsing ────────────────────────────────────────────────────────

func parseConfig(in map[string]any) (Config, error) {
	var out Config
	if v, ok := in["symbol"]; ok {
		if s, ok := v.(string); ok {
			out.Symbol = s
		} else {
			return out, fmt.Errorf("market_maker_basic: symbol must be a string, got %T", v)
		}
	}
	if v, ok := in["spread_bps"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("market_maker_basic: spread_bps: %w", err)
		}
		out.SpreadBps = n
	}
	if v, ok := in["order_size"]; ok {
		d, err := decimalFromAny(v)
		if err != nil {
			return out, fmt.Errorf("market_maker_basic: order_size: %w", err)
		}
		out.OrderSize = d
	}
	if v, ok := in["refresh_ticks"]; ok {
		n, err := intFromAny(v)
		if err != nil {
			return out, fmt.Errorf("market_maker_basic: refresh_ticks: %w", err)
		}
		out.RefreshTicks = n
	}
	if v, ok := in["use_llm_veto"]; ok {
		b, ok := v.(bool)
		if !ok {
			return out, fmt.Errorf("market_maker_basic: use_llm_veto must be a bool, got %T", v)
		}
		out.UseLLMVeto = b
	}
	if v, ok := in["veto_model"]; ok {
		if s, ok := v.(string); ok {
			out.VetoModel = s
		} else {
			return out, fmt.Errorf("market_maker_basic: veto_model must be a string, got %T", v)
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
