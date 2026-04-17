package risk

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/types"
)

// Policy is the concrete production Engine. It checks hard limits at the
// pre-trade boundary and walks a configurable set of circuit breakers at
// portfolio level.
type Policy struct {
	limits   types.RiskLimits
	breakers []CircuitBreaker
}

// NewPolicy constructs a Policy. Callers may register additional breakers
// after construction with WithBreaker.
func NewPolicy(limits types.RiskLimits, breakers ...CircuitBreaker) *Policy {
	return &Policy{limits: limits, breakers: append([]CircuitBreaker(nil), breakers...)}
}

// WithBreaker appends a circuit breaker.
func (p *Policy) WithBreaker(b CircuitBreaker) *Policy {
	p.breakers = append(p.breakers, b)
	return p
}

// Compile-time check.
var _ Engine = (*Policy)(nil)

// PreTrade evaluates a single intent against the configured hard limits.
// Returns the first blocking verdict; if every check passes, returns Allow.
func (p *Policy) PreTrade(_ context.Context, _ string, intent any, snap types.PortfolioSnapshot) types.Verdict {
	switch v := intent.(type) {
	case types.OrderIntent:
		return p.checkOrder(v, snap)
	case types.SwapIntent:
		return p.checkSwap(v, snap)
	}
	return types.Verdict{Kind: types.VerdictAllow, Reason: "unknown_intent_type", Detail: "policy passes through unknown intents"}
}

func (p *Policy) checkOrder(o types.OrderIntent, snap types.PortfolioSnapshot) types.Verdict {
	// Per-leg notional ceiling
	if !p.limits.MaxNotionalPerLeg.IsZero() && o.Notional().GreaterThan(p.limits.MaxNotionalPerLeg) {
		return block("max_notional_per_leg",
			fmt.Sprintf("order notional %s > limit %s", o.Notional(), p.limits.MaxNotionalPerLeg))
	}
	// Total exposure ceiling
	if !p.limits.MaxTotalBasisExposure.IsZero() {
		nextExposure := snap.TotalBasisExposure().Add(o.Notional())
		if nextExposure.GreaterThan(p.limits.MaxTotalBasisExposure) {
			return block("max_total_exposure",
				fmt.Sprintf("after-order exposure %s > limit %s", nextExposure, p.limits.MaxTotalBasisExposure))
		}
	}
	// Concurrent positions ceiling (count distinct underlyings)
	if p.limits.MaxConcurrentPositions > 0 && len(snap.OpenBasis) >= p.limits.MaxConcurrentPositions {
		return block("max_concurrent_positions",
			fmt.Sprintf("open=%d limit=%d", len(snap.OpenBasis), p.limits.MaxConcurrentPositions))
	}
	return allow()
}

func (p *Policy) checkSwap(s types.SwapIntent, _ types.PortfolioSnapshot) types.Verdict {
	if p.limits.MaxSpotSlippageBps > 0 && s.SlippageBps > p.limits.MaxSpotSlippageBps {
		return block("max_spot_slippage_bps",
			fmt.Sprintf("requested %dbps > limit %dbps", s.SlippageBps, p.limits.MaxSpotSlippageBps))
	}
	if !p.limits.MaxNotionalPerLeg.IsZero() {
		// for swaps we approximate notional as in_amount (in USDC the input is USDC)
		notional := s.InAmount
		if notional.GreaterThan(p.limits.MaxNotionalPerLeg) {
			return block("max_notional_per_leg",
				fmt.Sprintf("swap in_amount %s > limit %s", notional, p.limits.MaxNotionalPerLeg))
		}
	}
	return allow()
}

// Portfolio walks the configured circuit breakers; any block stops the
// agent immediately. Warnings are returned as the first non-allow result so
// the runtime can log/metric them.
func (p *Policy) Portfolio(_ context.Context, snap types.PortfolioSnapshot) types.Verdict {
	dd := drawdown(snap)
	for _, b := range p.breakers {
		v := b.Check(snap, p.limits, dd)
		if v.Kind != types.VerdictAllow {
			return v
		}
	}
	return allow()
}

// drawdown computes (HWM - NAV) / HWM. Zero if HWM is zero.
func drawdown(s types.PortfolioSnapshot) decimal.Decimal {
	if s.HighWaterMark.IsZero() {
		return decimal.Zero
	}
	return s.HighWaterMark.Sub(s.NAV).Div(s.HighWaterMark)
}

func allow() types.Verdict {
	return types.Verdict{Kind: types.VerdictAllow}
}

func block(reason, detail string) types.Verdict {
	return types.Verdict{Kind: types.VerdictBlock, Reason: reason, Detail: detail}
}

func warn(reason, detail string) types.Verdict {
	return types.Verdict{Kind: types.VerdictWarn, Reason: reason, Detail: detail}
}
