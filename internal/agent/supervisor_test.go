package agent

import (
	"context"
	"testing"

	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/pkg/strategy"
)

type stubStrategy struct{}

func (stubStrategy) Name() string                                          { return "stub" }
func (stubStrategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }
func (stubStrategy) Decide(_ context.Context, _ strategy.DecisionInput) (strategy.Decision, error) {
	return strategy.Decision{}, nil
}

func TestSupervisor_RegisterReplacesOld(t *testing.T) {
	sup := NewSupervisor(nil)
	a := Agent{ID: "x", Strategy: "stub", Mode: ModePaper}
	first := NewRuntime(a, Deps{Strategy: stubStrategy{}})
	second := NewRuntime(a, Deps{Strategy: stubStrategy{}})
	sup.Register("x", first)
	sup.Register("x", second) // should replace cleanly
	got, err := sup.Get("x")
	if err != nil || got != second {
		t.Errorf("expected second runtime, got=%v err=%v", got == second, err)
	}
}

func TestSupervisor_TripAll_Empty(t *testing.T) {
	sup := NewSupervisor(nil)
	if err := sup.TripAll(context.Background(), DefaultKillSwitchOptions("x")); err != nil {
		t.Errorf("trip all on empty supervisor should succeed, got %v", err)
	}
}

func TestSupervisor_StartStopGet(t *testing.T) {
	sup := NewSupervisor(nil)
	a := Agent{ID: "x", Strategy: "stub", Mode: ModePaper, TickSecs: 1}
	r := NewRuntime(a, Deps{Strategy: stubStrategy{}})
	sup.Register("x", r)

	if err := sup.Start(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if !r.IsRunning() {
		t.Fatal("expected running")
	}
	if err := sup.Stop(context.Background(), "x", "test"); err != nil {
		t.Fatal(err)
	}
	if r.IsRunning() {
		t.Fatal("expected stopped")
	}
}

func TestSupervisor_TripAll_FlattensAcrossAgents(t *testing.T) {
	sup := NewSupervisor(nil)
	for _, id := range []string{"a", "b"} {
		a := Agent{ID: id, Strategy: "stub", Mode: ModePaper}
		r := NewRuntime(a, Deps{Strategy: stubStrategy{}, Perp: exchangenoop.New()})
		sup.Register(id, r)
	}
	if err := sup.TripAll(context.Background(), DefaultKillSwitchOptions("test")); err != nil {
		t.Errorf("trip all failed: %v", err)
	}
}
