package assets

import (
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

func TestEmbedded_Loads(t *testing.T) {
	r, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Assets) == 0 {
		t.Fatal("embedded registry is empty")
	}
	if got, _ := r.Get("USDC"); got.Symbol != "USDC" {
		t.Errorf("USDC missing or mis-loaded: %+v", got)
	}
	wif, ok := r.Get("WIF")
	if !ok || wif.Perp == nil || wif.Perp.Symbol != "WIF" {
		t.Errorf("WIF missing or perp not populated: %+v", wif)
	}
	if !wif.Tradable() {
		t.Errorf("WIF should be tradable")
	}
	usdc, _ := r.Get("USDC")
	if usdc.Tradable() {
		t.Errorf("USDC should not be tradable (no perp)")
	}
}

func TestEmbedded_TradableList(t *testing.T) {
	r, _ := LoadEmbedded()
	got := r.Tradable()
	if len(got) < 5 {
		t.Errorf("expected at least 5 tradable assets, got %d", len(got))
	}
	for _, a := range got {
		if a.Perp == nil || a.Spot.Mint == "" {
			t.Errorf("Tradable returned non-tradable: %+v", a)
		}
	}
}

func TestEmbedded_AsAsset(t *testing.T) {
	r, _ := LoadEmbedded()
	wif, _ := r.Get("WIF")
	a := wif.AsAsset()
	if a.Chain != types.ChainSolana {
		t.Errorf("AsAsset chain: %q", a.Chain)
	}
	if a.Mint == "" || a.Decimals == 0 {
		t.Errorf("AsAsset incomplete: %+v", a)
	}
}

func TestParse_Validations(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			"version unsupported",
			`version: 99
defaults: {}
assets: []`,
			"unsupported version",
		},
		{
			"missing symbol",
			`version: 1
assets:
  - spot: {chain: solana, mint: x, decimals: 6}`,
			"missing symbol",
		},
		{
			"missing mint",
			`version: 1
assets:
  - symbol: WIF
    spot: {chain: solana, mint: "", decimals: 6}`,
			"spot.mint",
		},
		{
			"missing chain",
			`version: 1
assets:
  - symbol: WIF
    spot: {chain: "", mint: x, decimals: 6}`,
			"spot.chain",
		},
		{
			"decimals out of range",
			`version: 1
assets:
  - symbol: WIF
    spot: {chain: solana, mint: x, decimals: 99}`,
			"out of range",
		},
		{
			"perp missing parts",
			`version: 1
assets:
  - symbol: WIF
    perp: {venue: hyperliquid, symbol: ""}
    spot: {chain: solana, mint: x, decimals: 6}`,
			"perp requires",
		},
		{
			"empty registry",
			`version: 1
defaults: {}
assets: []`,
			"empty",
		},
		{
			"duplicate symbol",
			`version: 1
assets:
  - symbol: WIF
    spot: {chain: solana, mint: a, decimals: 6}
  - symbol: wif
    spot: {chain: solana, mint: b, decimals: 6}`,
			"duplicate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestParse_HedgeRatioDefault(t *testing.T) {
	r, err := Parse(strings.NewReader(`version: 1
defaults:
  hedge_ratio: 1.25
assets:
  - symbol: WIF
    perp: {venue: hyperliquid, symbol: WIF}
    spot: {chain: solana, mint: x, decimals: 6}
  - symbol: BONK
    hedge_ratio: 0.5
    spot: {chain: solana, mint: y, decimals: 5}
`))
	if err != nil {
		t.Fatal(err)
	}
	wif, _ := r.Get("WIF")
	if wif.HedgeRatio != 1.25 {
		t.Errorf("WIF hedge_ratio default: %v", wif.HedgeRatio)
	}
	bonk, _ := r.Get("BONK")
	if bonk.HedgeRatio != 0.5 {
		t.Errorf("BONK hedge_ratio override: %v", bonk.HedgeRatio)
	}
}
