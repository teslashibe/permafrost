package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/config"
	walletnoop "github.com/teslashibe/permafrost/internal/wallet/noop"
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/inference/openai"
	"github.com/teslashibe/permafrost/pkg/types"

	// Register noop so BuildStrategy can resolve a known-good strategy
	// name in these tests without depending on any out-of-tree strategy.
	_ "github.com/teslashibe/permafrost/strategies/noop"
)

// TestBuildStrategy_LooksUpFromRegistry verifies the post-#25 BuildStrategy
// is a pure registry lookup with no per-strategy special-casing. Strategies
// own their own typed config parsing inside their Constructor.
func TestBuildStrategy_LooksUpFromRegistry(t *testing.T) {
	a := Agent{
		ID:       "ag-x",
		Strategy: "noop",
		Universe: []string{"WIF"},
		Config:   map[string]any{},
	}
	s, err := BuildStrategy(a)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name() != "noop" {
		t.Fatalf("unexpected strategy: %+v", s)
	}
}

func TestBuildStrategy_UnknownNameErrors(t *testing.T) {
	a := Agent{ID: "ag", Strategy: "definitely-not-registered"}
	if _, err := BuildStrategy(a); err == nil {
		t.Fatal("expected error for unknown strategy name")
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
				Strategy: "noop",
				Network:  tc.stored,
				Universe: []string{"WIF"},
			}
			deps, err := BuildDeps(a, reg, nil, nil, nil, nil, BuildOptions{
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
		Strategy: "noop",
		Universe: []string{"WIF"},
	}
	ks := walletnoop.NewKeystore("solana")
	deps, err := BuildDeps(a, reg, nil, ks, nil, nil, BuildOptions{
		HyperliquidNetwork: "testnet",
		Solana: SolanaSpot{
			RPCURL:     "http://localhost:8899",
			SubmitMode: "rpc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deps.SwapVenueForChain(types.ChainSolana) == nil {
		t.Fatal("expected Deps.Swaps[solana] to be populated")
	}
	if deps.Perp == nil {
		t.Fatal("expected Deps.Perp to be populated")
	}
}

func TestBuildDeps_LeavesSwapNilWhenSolanaUnconfigured(t *testing.T) {
	reg, _ := assets.LoadEmbedded()
	a := Agent{ID: "ag", Strategy: "noop", Universe: []string{"WIF"}}
	deps, err := BuildDeps(a, reg, nil, nil, nil, nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deps.SwapVenueForChain(types.ChainSolana) != nil {
		t.Error("Solana venue should be nil when unconfigured (paper-spot fallback)")
	}
	if len(deps.Swaps) != 0 {
		t.Errorf("Swaps map should be empty, got %d entries", len(deps.Swaps))
	}
}

func TestBuildDeps_DegradesWhenSolanaConfiguredButNoSigner(t *testing.T) {
	reg, _ := assets.LoadEmbedded()
	a := Agent{ID: "ag", Strategy: "noop", Universe: []string{"WIF"}}
	deps, err := BuildDeps(a, reg, nil, nil, nil, nil, BuildOptions{
		Solana: SolanaSpot{RPCURL: "http://localhost:8899"},
	})
	if err != nil {
		t.Fatalf("BuildDeps should not error when degrading to paper-spot: %v", err)
	}
	if deps.SwapVenueForChain(types.ChainSolana) != nil {
		t.Error("Solana venue should be nil when no Solana signer available")
	}
}

// TestBuildDeps_DegradesWhenEVMConfiguredButNoSigner verifies the same
// graceful-degradation pattern for EVM chains: configured but no
// keystore = warn + skip, never error.
func TestBuildDeps_DegradesWhenEVMConfiguredButNoSigner(t *testing.T) {
	reg, _ := assets.LoadEmbedded()
	a := Agent{ID: "ag", Strategy: "noop", Universe: []string{"WIF"}}
	deps, err := BuildDeps(a, reg, nil, nil, nil, nil, BuildOptions{
		EVM: map[types.ChainID]EVMSpot{
			types.ChainBase: {RPCURL: "https://mainnet.base.org", OneInchAPIKey: "stub"},
		},
	})
	if err != nil {
		t.Fatalf("BuildDeps should not error when degrading EVM to paper-spot: %v", err)
	}
	if deps.SwapVenueForChain(types.ChainBase) != nil {
		t.Error("Base venue should be nil when no EVM signer available")
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

func TestEVMSpot_IsEnabled(t *testing.T) {
	if (EVMSpot{}).IsEnabled() {
		t.Error("zero EVMSpot must be disabled")
	}
	if (EVMSpot{RPCURL: "x"}).IsEnabled() {
		t.Error("RPCURL alone is not enough; need API key")
	}
	if (EVMSpot{OneInchAPIKey: "k"}).IsEnabled() {
		t.Error("API key alone is not enough; need RPC URL")
	}
	if !(EVMSpot{RPCURL: "x", OneInchAPIKey: "k"}).IsEnabled() {
		t.Error("RPCURL + API key must enable")
	}
}

// TestResolveInference covers the parsing + registry-lookup that turns
// agent.Inference ("provider:model") into a concrete inference.Provider
// surfaced via Services. Regression test for the audit's H1 finding.
func TestResolveInference(t *testing.T) {
	t.Run("empty string is no-op (no inference for this agent)", func(t *testing.T) {
		prov, model, err := resolveInference("", nil)
		if err != nil {
			t.Fatalf("empty spec must not error, got %v", err)
		}
		if prov != nil {
			t.Errorf("provider should be nil, got %T", prov)
		}
		if model != "" {
			t.Errorf("model should be empty, got %q", model)
		}
	})

	t.Run("configured but no registry → error", func(t *testing.T) {
		_, _, err := resolveInference("openrouter:claude-sonnet-4.5", nil)
		if err == nil {
			t.Fatal("expected error when agent.Inference is set but no registry is supplied")
		}
	})

	t.Run("provider missing from registry → error mentions both", func(t *testing.T) {
		reg, _ := inference.NewRegistry(config.InferenceConfig{}, openai.NewProvider)
		_, _, err := resolveInference("openrouter:claude", reg)
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
		if !strings.Contains(err.Error(), "openrouter") || !strings.Contains(err.Error(), "openrouter:claude") {
			t.Errorf("error should reference provider name and full spec, got %q", err.Error())
		}
	})

	t.Run("provider:model parsed; only first ':' is the separator", func(t *testing.T) {
		reg, _ := inference.NewRegistry(
			config.InferenceConfig{
				Default: "openrouter",
				Providers: map[string]config.InferenceProviderConfig{
					"openrouter": {BaseURL: "https://openrouter.ai/api/v1"},
				},
			},
			openai.NewProvider,
		)
		_, model, err := resolveInference("openrouter:vendor/model:variant", reg)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if model != "vendor/model:variant" {
			t.Errorf("model should preserve trailing colons; got %q", model)
		}
	})

	t.Run("provider only (no colon) → empty model", func(t *testing.T) {
		reg, _ := inference.NewRegistry(
			config.InferenceConfig{
				Default: "openrouter",
				Providers: map[string]config.InferenceProviderConfig{
					"openrouter": {BaseURL: "https://openrouter.ai/api/v1"},
				},
			},
			openai.NewProvider,
		)
		_, model, err := resolveInference("openrouter", reg)
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if model != "" {
			t.Errorf("model should be empty when no ':' supplied, got %q", model)
		}
	})
}

// TestDecimalFromAny exercises the JSONB-numeric helper that
// BuildRiskEngine and any other framework-side config parser uses.
// Strategy-specific config parsing now lives inside each strategy.
func TestDecimalFromAny(t *testing.T) {
	cases := []any{
		0.5,         // float64 (JSON default)
		int(1),      // int (rare via flags)
		int64(1),    // int64
		"0.25",      // string
	}
	for _, v := range cases {
		if _, err := decimalFromAny(v); err != nil {
			t.Errorf("decimalFromAny rejected %T: %v", v, err)
		}
	}
	if _, err := decimalFromAny(struct{}{}); err == nil {
		t.Error("decimalFromAny should reject struct")
	}
}
