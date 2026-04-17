package risk_test

import (
	"context"
	"testing"

	"github.com/teslashibe/permafrost/internal/risk/noop"
	"github.com/teslashibe/permafrost/internal/types"
)

func TestNoopEngineAllows(t *testing.T) {
	e := noop.New()
	if v := e.PreTrade(context.Background(), "agent", types.OrderIntent{}, types.PortfolioSnapshot{}); v.IsBlock() {
		t.Errorf("PreTrade should allow, got %+v", v)
	}
	if v := e.Portfolio(context.Background(), types.PortfolioSnapshot{}); v.IsBlock() {
		t.Errorf("Portfolio should allow, got %+v", v)
	}
}

func TestVerdictKinds(t *testing.T) {
	cases := []struct {
		v       types.Verdict
		isBlock bool
	}{
		{types.Verdict{Kind: types.VerdictAllow}, false},
		{types.Verdict{Kind: types.VerdictWarn}, false},
		{types.Verdict{Kind: types.VerdictBlock}, true},
	}
	for _, c := range cases {
		if got := c.v.IsBlock(); got != c.isBlock {
			t.Errorf("%+v IsBlock=%v want %v", c.v, got, c.isBlock)
		}
	}
}
