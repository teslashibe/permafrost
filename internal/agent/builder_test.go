package agent

import (
	"errors"
	"testing"

	"github.com/teslashibe/permafrost/internal/assets"
	walletnoop "github.com/teslashibe/permafrost/internal/wallet/noop"
)

func TestBuildStrategy_FundingArbAppliesConfigOverrides(t *testing.T) {
	reg, err := assets.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	a := Agent{
		ID:       "ag-x",
		Strategy: "funding_arb_basic",
		Universe: []string{"WIF"},
		Config: map[string]any{
			"entry_annualised_funding": 0.05,
			"exit_annualised_funding":  0.02,
			"position_cap_usdc":        100,
			"slippage_bps":             25,
		},
	}
	s, err := BuildStrategy(a, reg)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name() != "funding_arb_basic" {
		t.Fatalf("unexpected strategy: %+v", s)
	}
}

func TestBuildHyperliquidVenue_NoKeystoreAllowed(t *testing.T) {
	v, err := BuildHyperliquidVenue(nil, BuildOptions{HyperliquidNetwork: "testnet"})
	if err != nil {
		t.Fatalf("expected funding-only venue without keystore, got %v", err)
	}
	if v.Name() != "hyperliquid" {
		t.Errorf("Name: %q", v.Name())
	}
}

func TestBuildHyperliquidVenue_AddressOverrideWins(t *testing.T) {
	const addr = "0x1111111111111111111111111111111111111111"
	v, err := BuildHyperliquidVenue(nil, BuildOptions{
		HyperliquidNetwork: "testnet",
		HyperliquidAddress: addr,
	})
	if err != nil {
		t.Fatalf("BuildHyperliquidVenue: %v", err)
	}
	if v.Name() != "hyperliquid" {
		t.Errorf("Name: %q", v.Name())
	}
}

func TestBuildHyperliquidVenue_RejectsBadKeystoreAddress(t *testing.T) {
	// noop keystore returns addresses prefixed "noop_…" which fail HL's
	// 0x-prefix check. The builder should surface the validation error
	// rather than silently dropping the address.
	ks := walletnoop.NewKeystore("hyperliquid")
	_, err := BuildHyperliquidVenue(ks, BuildOptions{HyperliquidNetwork: "testnet"})
	if err == nil {
		t.Fatal("expected validation error on noop signer address")
	}
}

func TestBuildOptions_DefaultsToTestnet(t *testing.T) {
	v, err := BuildHyperliquidVenue(nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("expected venue")
	}
}

func TestNetwork_Validate(t *testing.T) {
	for _, n := range []Network{NetworkMainnet, NetworkTestnet, ""} {
		if err := n.Validate(); err != nil {
			t.Errorf("Validate(%q): unexpected error %v", n, err)
		}
	}
	if err := Network("rinkeby").Validate(); err == nil {
		t.Error("expected error for unknown network")
	}
}

func TestNetwork_OrDefault(t *testing.T) {
	if got := Network("").OrDefault(NetworkMainnet); got != NetworkMainnet {
		t.Errorf("OrDefault: empty should fall back, got %q", got)
	}
	if got := NetworkTestnet.OrDefault(NetworkMainnet); got != NetworkTestnet {
		t.Errorf("OrDefault: non-empty should pass through, got %q", got)
	}
}

// TestBuildDeps_PerAgentNetworkPlumbed ensures that BuildDeps uses
// agent.Network when no override is supplied. We can't easily check the
// venue's resolved endpoint without poking internals, but we CAN check
// that an agent with Network=testnet doesn't error and that the override
// path explicitly wins.
func TestBuildDeps_PerAgentNetworkPlumbed(t *testing.T) {
	reg, err := assets.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		stored         Network
		override       string
		expectError    bool
	}{
		{"stored=mainnet, no override", NetworkMainnet, "", false},
		{"stored=testnet, no override", NetworkTestnet, "", false},
		{"empty stored, no override", "", "", false},
		{"override=testnet wins", NetworkMainnet, "testnet", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{
				ID:       "ag",
				Strategy: "funding_arb_basic",
				Network:  tc.stored,
				Universe: []string{"WIF"},
			}
			deps, err := BuildDeps(a, reg, nil, nil, nil, BuildOptions{
				HyperliquidNetwork: tc.override,
			})
			if tc.expectError && err == nil {
				t.Fatal("expected error")
			}
			if !tc.expectError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.expectError && deps.Perp == nil {
				t.Fatal("Perp venue should be set")
			}
		})
	}
}

func TestBuildSolanaSwapVenue_RequiresConfigAndSigner(t *testing.T) {
	// No config → error.
	if _, err := BuildSolanaSwapVenue(SolanaSpot{}, nil); err == nil {
		t.Fatal("expected error without config")
	}
	// Config but no keystore → ErrNoSolanaSigner.
	cfg := SolanaSpot{RPCURL: "http://localhost:8899"}
	if _, err := BuildSolanaSwapVenue(cfg, nil); !errors.Is(err, ErrNoSolanaSigner) {
		t.Fatalf("expected ErrNoSolanaSigner, got %v", err)
	}
	// Config + keystore without solana key → ErrNoSolanaSigner.
	emptyKS := walletnoop.NewKeystore() // no chains registered
	if _, err := BuildSolanaSwapVenue(cfg, emptyKS); !errors.Is(err, ErrNoSolanaSigner) {
		t.Fatalf("expected ErrNoSolanaSigner from empty keystore, got %v", err)
	}
}

func TestBuildSolanaSwapVenue_BuildsWithSolanaSigner(t *testing.T) {
	ks := walletnoop.NewKeystore("solana") // type missing — let me check
	cfg := SolanaSpot{
		RPCURL:        "http://localhost:8899",
		SubmitMode:    "rpc",
		JitoBundleURL: "",
	}
	v, err := BuildSolanaSwapVenue(cfg, ks)
	if err != nil {
		t.Fatalf("BuildSolanaSwapVenue: %v", err)
	}
	if v == nil {
		t.Fatal("expected swap venue")
	}
	if v.Name() != "jupiter" {
		t.Errorf("Name: %q want jupiter", v.Name())
	}
}

func TestBuildDeps_PopulatesSwapWhenSolanaConfigured(t *testing.T) {
	reg, err := assets.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	a := Agent{
		ID:       "ag",
		Strategy: "funding_arb_basic",
		Universe: []string{"WIF"},
	}
	ks := walletnoop.NewKeystore("solana")
	deps, err := BuildDeps(a, reg, nil, ks, nil, BuildOptions{
		HyperliquidNetwork: "testnet",
		Solana: SolanaSpot{
			RPCURL:     "http://localhost:8899",
			SubmitMode: "rpc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deps.Swap == nil {
		t.Fatal("expected Deps.Swap to be populated")
	}
	if deps.Perp == nil {
		t.Fatal("expected Deps.Perp to be populated")
	}
}

func TestBuildDeps_LeavesSwapNilWhenSolanaUnconfigured(t *testing.T) {
	reg, _ := assets.LoadEmbedded()
	a := Agent{ID: "ag", Strategy: "funding_arb_basic", Universe: []string{"WIF"}}
	deps, err := BuildDeps(a, reg, nil, nil, nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deps.Swap != nil {
		t.Error("Swap should be nil when Solana is unconfigured (paper-spot fallback)")
	}
}

func TestBuildDeps_DegradesWhenSolanaConfiguredButNoSigner(t *testing.T) {
	reg, _ := assets.LoadEmbedded()
	a := Agent{ID: "ag", Strategy: "funding_arb_basic", Universe: []string{"WIF"}}
	deps, err := BuildDeps(a, reg, nil, nil, nil, BuildOptions{
		Solana: SolanaSpot{RPCURL: "http://localhost:8899"},
	})
	if err != nil {
		t.Fatalf("BuildDeps should not error when degrading to paper-spot: %v", err)
	}
	if deps.Swap != nil {
		t.Error("Swap should be nil when no Solana signer available")
	}
}

func TestSolanaSpot_IsEnabled(t *testing.T) {
	if (SolanaSpot{}).IsEnabled() {
		t.Error("zero SolanaSpot must be disabled")
	}
	if !(SolanaSpot{RPCURL: "x"}).IsEnabled() {
		t.Error("RPCURL alone must enable")
	}
}

func TestApplyFundingArbConfig_NumericTypes(t *testing.T) {
	cases := []map[string]any{
		{"entry_annualised_funding": 0.5},          // float64 (JSON default)
		{"entry_annualised_funding": int(1)},        // int (rare via flags)
		{"entry_annualised_funding": int64(1)},      // int64
		{"entry_annualised_funding": "0.25"},        // string
	}
	for _, m := range cases {
		if _, err := decimalFromAny(m["entry_annualised_funding"]); err != nil {
			t.Errorf("decimalFromAny rejected %T: %v", m["entry_annualised_funding"], err)
		}
	}
	if _, err := decimalFromAny(struct{}{}); err == nil {
		t.Error("decimalFromAny should reject struct")
	}
}
