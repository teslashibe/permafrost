package vault

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestComputeNAV_HWMTakesMax(t *testing.T) {
	t0 := time.Now()
	cases := []struct {
		name    string
		in      NAVInputs
		wantNAV string
		wantHWM string
	}{
		{
			"new high",
			NAVInputs{Cash: d("1000"), PositionsValue: d("100"), PrevHWM: d("900")},
			"1100", "1100",
		},
		{
			"under high",
			NAVInputs{Cash: d("800"), PositionsValue: d("100"), PrevHWM: d("1100")},
			"900", "1100",
		},
		{
			"first ever",
			NAVInputs{Cash: d("500"), PositionsValue: d("0"), PrevHWM: d("0")},
			"500", "500",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeNAV(t0, 1, c.in)
			if !got.NAV.Equal(d(c.wantNAV)) {
				t.Errorf("NAV: got %s want %s", got.NAV, c.wantNAV)
			}
			if !got.HighWaterMark.Equal(d(c.wantHWM)) {
				t.Errorf("HWM: got %s want %s", got.HighWaterMark, c.wantHWM)
			}
		})
	}
}

func TestAvailableCapital(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		cash    string
		lockups []Lockup
		want    string
	}{
		{"no lockups", "1000", nil, "1000"},
		{
			"single active lockup",
			"1000",
			[]Lockup{{Amount: d("300"), UnlockAt: now.Add(time.Hour)}},
			"700",
		},
		{
			"expired lockup ignored",
			"1000",
			[]Lockup{{Amount: d("400"), UnlockAt: now.Add(-time.Hour)}},
			"1000",
		},
		{
			"oversubscribed clamps to zero",
			"1000",
			[]Lockup{
				{Amount: d("700"), UnlockAt: now.Add(time.Hour)},
				{Amount: d("500"), UnlockAt: now.Add(2 * time.Hour)},
			},
			"0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AvailableCapital(d(c.cash), c.lockups, now)
			if !got.Equal(d(c.want)) {
				t.Errorf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestSimpleReturn(t *testing.T) {
	cases := []struct {
		nav, dep, wit, want string
	}{
		{"1100", "1000", "0", "0.1"},     // +10%
		{"900", "1000", "0", "-0.1"},     // -10%
		{"1000", "0", "0", "0"},          // no contributions → 0
		{"500", "1000", "500", "0"},      // contributed 500, NAV=500 → 0% return
	}
	for _, c := range cases {
		got := SimpleReturn(NAVSnapshot{
			NAV:             d(c.nav),
			DepositTotal:    d(c.dep),
			WithdrawalTotal: d(c.wit),
		})
		if !got.Equal(d(c.want)) {
			t.Errorf("nav=%s dep=%s wit=%s: got %s want %s", c.nav, c.dep, c.wit, got, c.want)
		}
	}
}
