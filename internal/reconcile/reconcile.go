// Package reconcile compares the agent's persisted view of the world
// (strategy_positions, swap/order ledgers) against what the venues
// actually report on chain or via API. Drift is the early-warning signal
// for half-open positions, missed fills, and bookkeeping bugs — the
// kind of issue that bleeds money quietly until someone notices.
//
// Two reconciliation modes:
//
//   - Per-agent: for ONE agent, walk its open BasisPositions and verify
//     each leg against its venue. Reports drift per leg.
//   - Global: walk every running agent and produce a unified report.
//
// Drift severity is split into three buckets so callers (CLI, future
// alerting) can decide what to escalate:
//
//   - Info    — mark moved, balance changed within tolerance, etc.
//   - Warning — quantity mismatch within configurable tolerance.
//   - Critical — leg missing entirely (we think we're hedged but aren't).
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/types"
)

// Severity is how loud the operator should hear about a discrepancy.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Drift is one discrepancy between book and venue.
type Drift struct {
	AgentID    string          `json:"agent_id"`
	BasisKey   string          `json:"basis_key"`
	Underlying string          `json:"underlying"`
	Leg        string          `json:"leg"`        // "spot" | "perp"
	Field      string          `json:"field"`      // "qty" | "exists" | "side"
	Expected   decimal.Decimal `json:"expected"`
	Actual     decimal.Decimal `json:"actual"`
	Diff       decimal.Decimal `json:"diff"`
	Severity   Severity        `json:"severity"`
	Note       string          `json:"note"`
}

// Report is the result of one reconciliation pass.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	AgentsCount int       `json:"agents_count"`
	OpenBasis   int       `json:"open_basis"`
	OK          int       `json:"ok"`         // basis positions with no drift
	Drifts      []Drift   `json:"drifts"`
}

// HasIssues reports whether anything more severe than Info was found.
func (r Report) HasIssues() bool {
	for _, d := range r.Drifts {
		if d.Severity != SeverityInfo {
			return true
		}
	}
	return false
}

// Engine performs reconciliation. Both venues are optional; missing
// venues yield "Could not verify <leg>" Info entries rather than errors.
type Engine struct {
	Perp  exchange.Venue
	Swaps map[types.ChainID]swap.SwapVenue
	// QtyToleranceBps is the relative tolerance below which a quantity
	// drift is considered a Warning (not Critical). Default 25 bps.
	// Anything matching exactly within this band is silently OK.
	QtyToleranceBps int
}

// New constructs an Engine with the standard tolerance.
func New(perp exchange.Venue, swaps map[types.ChainID]swap.SwapVenue) *Engine {
	return &Engine{Perp: perp, Swaps: swaps, QtyToleranceBps: 25}
}

// Reconcile walks the open basis positions of one agent and verifies
// each leg against its venue. Returns the drift list (which may be
// empty). Errors only on hard RPC failures; a missing venue is degraded
// silently into "could not verify" entries.
func (e *Engine) Reconcile(ctx context.Context, agentID string, open []types.BasisPosition) (Report, error) {
	r := Report{
		GeneratedAt: time.Now().UTC(),
		AgentsCount: 1,
		OpenBasis:   len(open),
	}

	// Pull HL positions once so we don't query per-leg.
	perpBySymbol := map[string]types.Position{}
	perpAvailable := false
	if e != nil && e.Perp != nil {
		positions, err := e.Perp.Positions(ctx)
		if err == nil {
			perpAvailable = true
			for _, p := range positions {
				perpBySymbol[p.Symbol] = p
			}
		}
	}

	for _, p := range open {
		drifts := e.reconcileOne(ctx, agentID, p, perpBySymbol, perpAvailable)
		// "OK" means no warnings or criticals — Info entries (e.g.
		// "no venue configured; could not verify") are status updates
		// rather than problems.
		if !hasIssue(drifts) {
			r.OK++
		}
		r.Drifts = append(r.Drifts, drifts...)
	}
	return r, nil
}

func hasIssue(drifts []Drift) bool {
	for _, d := range drifts {
		if d.Severity != SeverityInfo {
			return true
		}
	}
	return false
}

func (e *Engine) reconcileOne(ctx context.Context, agentID string, p types.BasisPosition, perpBySymbol map[string]types.Position, perpAvailable bool) []Drift {
	var drifts []Drift
	spotLeg, perpLeg, ok := splitLegs(p.Legs)
	if !ok {
		return []Drift{{
			AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
			Leg: "both", Field: "exists", Severity: SeverityCritical,
			Note: "basis position has malformed legs JSON",
		}}
	}

	// ─── PERP leg ─────────────────────────────────────────────────────
	if !perpAvailable {
		drifts = append(drifts, Drift{
			AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
			Leg: "perp", Field: "exists", Severity: SeverityInfo,
			Note: "no Perp venue configured; could not verify",
		})
	} else {
		hp, exists := perpBySymbol[perpLeg.Symbol]
		switch {
		case !exists:
			drifts = append(drifts, Drift{
				AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
				Leg: "perp", Field: "exists", Severity: SeverityCritical,
				Expected: perpLeg.Qty.Neg(), Actual: decimal.Zero,
				Note: "DB says short " + perpLeg.Qty.String() + " " + perpLeg.Symbol +
					" — venue reports no position",
			})
		default:
			// We hold a SHORT, so HL reports negative szi.
			expected := perpLeg.Qty.Neg()
			actual := hp.Qty
			diff := actual.Sub(expected).Abs()
			rel := relativeBps(diff, expected.Abs())
			switch {
			case isOppositeSide(expected, actual):
				drifts = append(drifts, Drift{
					AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
					Leg: "perp", Field: "side", Severity: SeverityCritical,
					Expected: expected, Actual: actual, Diff: diff,
					Note: "venue reports opposite side — strategy may have flipped position",
				})
			case e.QtyToleranceBps > 0 && rel > e.QtyToleranceBps:
				drifts = append(drifts, Drift{
					AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
					Leg: "perp", Field: "qty",
					Severity: severityForRel(rel),
					Expected: expected, Actual: actual, Diff: diff,
					Note: fmt.Sprintf("perp size mismatch: %dbps vs %dbps tolerance", rel, e.QtyToleranceBps),
				})
			}
		}
	}

	// ─── SPOT leg ─────────────────────────────────────────────────────
	swapVenue := e.swapVenueFor(spotLeg.Asset.Chain)
	if swapVenue == nil {
		drifts = append(drifts, Drift{
			AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
			Leg: "spot", Field: "exists", Severity: SeverityInfo,
			Note: "no SwapVenue configured for chain " + string(spotLeg.Asset.Chain) +
				"; could not verify",
		})
		return drifts
	}
	bal, err := swapVenue.Balance(ctx, spotLeg.Asset)
	if err != nil {
		drifts = append(drifts, Drift{
			AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
			Leg: "spot", Field: "exists", Severity: SeverityWarning,
			Note: "balance query failed: " + err.Error(),
		})
		return drifts
	}
	// Expected spot quantity (in tokens) ≈ perp size (delta-neutral 1:1).
	// This is the same approximation pnl.go uses; we'll thread real
	// BaseQty through BasisLeg in a follow-up.
	expected := perpLeg.Qty
	switch {
	case bal.IsZero() && expected.IsPositive():
		drifts = append(drifts, Drift{
			AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
			Leg: "spot", Field: "exists", Severity: SeverityCritical,
			Expected: expected, Actual: bal,
			Note: "DB says we hold " + expected.String() + " " + spotLeg.Asset.Symbol +
				" on " + string(spotLeg.Asset.Chain) + " — wallet reports zero",
		})
	default:
		diff := bal.Sub(expected).Abs()
		rel := relativeBps(diff, expected)
		// Wallet > expected is fine (the agent might share a wallet
		// with manual deposits). We only warn when wallet < expected.
		if bal.LessThan(expected) && e.QtyToleranceBps > 0 && rel > e.QtyToleranceBps {
			drifts = append(drifts, Drift{
				AgentID: agentID, BasisKey: p.Underlying, Underlying: p.Underlying,
				Leg: "spot", Field: "qty",
				Severity: severityForRel(rel),
				Expected: expected, Actual: bal, Diff: diff,
				Note: fmt.Sprintf("wallet %s < expected %s on %s (%dbps under)",
					bal, expected, spotLeg.Asset.Chain, rel),
			})
		}
	}
	return drifts
}

func (e *Engine) swapVenueFor(c types.ChainID) swap.SwapVenue {
	if e == nil || e.Swaps == nil {
		return nil
	}
	return e.Swaps[c]
}

// ─── helpers ────────────────────────────────────────────────────────────────

// ErrNoBasis is returned when the caller asked to reconcile a specific
// basis that doesn't exist in the open set.
var ErrNoBasis = errors.New("reconcile: no such basis")

func splitLegs(legs []types.BasisLeg) (spot, perp types.BasisLeg, ok bool) {
	gotSpot, gotPerp := false, false
	for _, l := range legs {
		switch l.Kind {
		case types.BasisLegSpot:
			spot = l
			gotSpot = true
		case types.BasisLegPerp:
			perp = l
			gotPerp = true
		}
	}
	return spot, perp, gotSpot && gotPerp
}

// relativeBps returns the absolute difference as a basis-point fraction
// of expected. Returns 0 when expected is zero (avoids div-by-zero;
// callers should special-case missing-position via the exists check).
func relativeBps(diff, expected decimal.Decimal) int {
	if expected.IsZero() {
		return 0
	}
	bps := diff.Mul(decimal.NewFromInt(10_000)).Div(expected.Abs())
	rounded := bps.Round(0)
	return int(rounded.IntPart())
}

// severityForRel maps a relative-bps drift to a severity. Anything
// above 100bps (1%) is Critical; the rest is Warning.
func severityForRel(rel int) Severity {
	if rel > 100 {
		return SeverityCritical
	}
	return SeverityWarning
}

// isOppositeSide reports whether expected and actual have opposite signs
// (and neither is zero). A flipped position is way worse than a sized
// drift — the agent has effectively no hedge.
func isOppositeSide(expected, actual decimal.Decimal) bool {
	if expected.IsZero() || actual.IsZero() {
		return false
	}
	return expected.Sign() != actual.Sign()
}

// ChainName returns a human-readable name for log lines.
func ChainName(c types.ChainID) string { return strings.ToUpper(string(c)) }
