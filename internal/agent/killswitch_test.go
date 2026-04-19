package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// silentLogger discards all logger output during tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestCancelOpenOrders_HappyPath: venue exposes 3 open orders; killswitch
// cancels each one and Cancellations() reflects all 3 ids.
func TestCancelOpenOrders_HappyPath(t *testing.T) {
	venue := exchangenoop.New()
	venue.SetOpenOrders([]exchange.OpenOrder{
		{ID: "o1", Symbol: "WIF", Side: types.SideSell},
		{ID: "o2", Symbol: "BONK", Side: types.SideSell},
		{ID: "o3", Symbol: "POPCAT", Side: types.SideBuy},
	})

	if err := cancelOpenOrders(context.Background(), venue, silentLogger()); err != nil {
		t.Fatalf("cancelOpenOrders: %v", err)
	}
	got := venue.Cancellations()
	if len(got) != 3 {
		t.Fatalf("expected 3 cancels, got %d (%v)", len(got), got)
	}
	want := map[types.OrderID]bool{"o1": true, "o2": true, "o3": true}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected cancel %q", id)
		}
	}
}

// TestCancelOpenOrders_NoOpenOrders: empty list returns nil cleanly,
// no cancels issued, no error.
func TestCancelOpenOrders_NoOpenOrders(t *testing.T) {
	venue := exchangenoop.New()
	if err := cancelOpenOrders(context.Background(), venue, silentLogger()); err != nil {
		t.Fatalf("expected nil err with no open orders; got %v", err)
	}
	if got := len(venue.Cancellations()); got != 0 {
		t.Errorf("expected 0 cancels; got %d", got)
	}
}

// failingCancelVenue wraps the noop venue and returns an error on Cancel
// for one specific id, to test that one failure doesn't abort the rest.
type failingCancelVenue struct {
	*exchangenoop.Venue
	failID types.OrderID
}

func (f *failingCancelVenue) Cancel(ctx context.Context, id types.OrderID) error {
	if id == f.failID {
		return errors.New("simulated venue failure")
	}
	return f.Venue.Cancel(ctx, id)
}

// TestCancelOpenOrders_OneFailureContinues: a single cancel failure
// surfaces an error from cancelOpenOrders but doesn't prevent the
// other cancels from issuing.
func TestCancelOpenOrders_OneFailureContinues(t *testing.T) {
	inner := exchangenoop.New()
	inner.SetOpenOrders([]exchange.OpenOrder{
		{ID: "o1", Symbol: "WIF"},
		{ID: "o2", Symbol: "BONK"}, // this one fails
		{ID: "o3", Symbol: "POPCAT"},
	})
	venue := &failingCancelVenue{Venue: inner, failID: "o2"}

	err := cancelOpenOrders(context.Background(), venue, silentLogger())
	if err == nil {
		t.Fatal("expected error to surface from one failed cancel")
	}
	got := inner.Cancellations()
	if len(got) != 2 {
		t.Errorf("expected 2 successful cancels (o1, o3); got %d (%v)", len(got), got)
	}
}

// stubPositionVenue returns scripted positions, lets us assert what
// flatten places without simulating a real venue.
type stubPositionVenue struct {
	*exchangenoop.Venue
	positions []types.Position
}

func (s *stubPositionVenue) Positions(_ context.Context) ([]types.Position, error) {
	return s.positions, nil
}

// TestFlattenPositions_ShortAndLong: a short-position triggers a
// reduce-only buy; a long-position triggers a reduce-only sell. Flat
// positions are skipped.
func TestFlattenPositions_ShortAndLong(t *testing.T) {
	inner := exchangenoop.New()
	venue := &stubPositionVenue{Venue: inner, positions: []types.Position{
		{Venue: "noop", Symbol: "WIF", Qty: decimal.NewFromInt(-100)},  // short
		{Venue: "noop", Symbol: "BONK", Qty: decimal.NewFromInt(50)},   // long
		{Venue: "noop", Symbol: "POPCAT", Qty: decimal.NewFromInt(0)},  // flat
	}}

	if err := flattenPositions(context.Background(), "ag-test", venue, silentLogger()); err != nil {
		t.Fatalf("flattenPositions: %v", err)
	}
	placed := inner.Placed()
	if len(placed) != 2 {
		t.Fatalf("expected 2 places (skip flat), got %d (%+v)", len(placed), placed)
	}
	for _, p := range placed {
		if !p.ReduceOnly {
			t.Errorf("flatten orders must be reduce-only; got %+v", p)
		}
		if p.Type != types.OrderTypeMarket {
			t.Errorf("flatten orders must be market; got %+v", p)
		}
	}

	// WIF is a short → buy back (qty becomes positive 100)
	// BONK is a long → sell (qty 50)
	bySymbol := map[string]types.OrderIntent{}
	for _, p := range placed {
		bySymbol[p.Symbol] = p
	}
	if got := bySymbol["WIF"]; got.Side != types.SideBuy || !got.Size.Equal(decimal.NewFromInt(100)) {
		t.Errorf("WIF short → expected buy size=100, got %+v", got)
	}
	if got := bySymbol["BONK"]; got.Side != types.SideSell || !got.Size.Equal(decimal.NewFromInt(50)) {
		t.Errorf("BONK long → expected sell size=50, got %+v", got)
	}
}

// TestKillSwitchOptions_DefaultIsConservative: DefaultKillSwitchOptions
// closes shorts but does not auto-liquidate spot. Documented behaviour;
// regression-guard against a future change that flips this default.
func TestKillSwitchOptions_DefaultIsConservative(t *testing.T) {
	opts := DefaultKillSwitchOptions("test")
	if !opts.CloseShorts {
		t.Errorf("default should CloseShorts; got %+v", opts)
	}
	if opts.LiquidateSpot {
		t.Errorf("default must NOT auto-LiquidateSpot; got %+v", opts)
	}
	if opts.Reason != "test" {
		t.Errorf("Reason: got %q want test", opts.Reason)
	}
}

// TestRuntime_Trip_StopsAndHalts: invoking Trip on a running runtime
// stops the loop and (when a Store is wired) sets persistent status to
// halted. Regression guard for the operator-visible behaviour.
func TestRuntime_Trip_StopsAndHalts(t *testing.T) {
	venue := exchangenoop.New()
	a := Agent{ID: "ag-test", Strategy: "noop", Mode: ModePaper, TickSecs: 1}
	r := NewRuntime(a, Deps{
		Strategy: stubStrategyForKill{},
		Perp:     venue,
		Logger:   silentLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !r.IsRunning() {
		t.Fatal("expected running")
	}

	tripCtx, tripCancel := context.WithTimeout(context.Background(), time.Second)
	defer tripCancel()
	if err := r.Trip(tripCtx, DefaultKillSwitchOptions("test")); err != nil {
		t.Errorf("Trip returned err: %v", err)
	}
	if r.IsRunning() {
		t.Error("expected stopped after Trip")
	}
}

// stubStrategyForKill is a minimal Strategy used only by
// TestRuntime_Trip_StopsAndHalts. (Avoids an import-cycle with the
// noop strategy package.)
type stubStrategyForKill struct{}

func (stubStrategyForKill) Name() string { return "noop" }
func (stubStrategyForKill) Warmup(context.Context, strategy.WarmupInput) error {
	return nil
}
func (stubStrategyForKill) Decide(context.Context, strategy.DecisionInput) (strategy.Decision, error) {
	return strategy.Decision{}, nil
}
