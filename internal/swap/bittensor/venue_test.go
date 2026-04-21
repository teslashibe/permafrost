package bittensor

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/wallet"
	"github.com/teslashibe/permafrost/pkg/types"
)

func newTestSigner(t *testing.T) *wallet.BittensorSigner {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	s, err := wallet.NewBittensorSignerFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNew_RequiresSigner(t *testing.T) {
	_, err := New(Config{RPCURL: "wss://example.com"}, nil)
	if err == nil {
		t.Fatal("expected error for nil signer")
	}
}

func TestNew_RequiresRPCURL(t *testing.T) {
	_, err := New(Config{}, newTestSigner(t))
	if err == nil {
		t.Fatal("expected error for empty RPCURL")
	}
}

func TestNew_RequiresBittensorChain(t *testing.T) {
	_, err := New(Config{RPCURL: "wss://x"}, &wrongChainSigner{})
	if err == nil {
		t.Fatal("expected error for non-bittensor signer")
	}
}

func TestParseSubnetMint(t *testing.T) {
	cases := []struct {
		mint   string
		ok     bool
		netuid uint16
	}{
		{"SN8", true, 8},
		{"SN1", true, 1},
		{"SN128", true, 128},
		{"SN0", true, 0},
		{"sn8", false, 0}, // case-sensitive
		{"S8", false, 0},
		{"SN", false, 0},
		{"SNA", false, 0},
		{"", false, 0},
	}
	for _, tc := range cases {
		got, err := parseSubnetMint(tc.mint)
		if (err == nil) != tc.ok {
			t.Errorf("parseSubnetMint(%q): err=%v, want ok=%v", tc.mint, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.netuid {
			t.Errorf("parseSubnetMint(%q): got %d, want %d", tc.mint, got, tc.netuid)
		}
	}
}

func TestParseSubnetMints(t *testing.T) {
	cases := []struct {
		in, out string
		netuid  uint16
		isBuy   bool
		ok      bool
	}{
		{"TAO", "SN8", 8, true, true},   // buy
		{"SN8", "TAO", 8, false, true},  // sell
		{"SN3", "SN8", 0, false, false}, // not a TAO pair
		{"USDC", "SN8", 0, false, false},
	}
	for _, tc := range cases {
		netuid, isBuy, err := parseSubnetMints(tc.in, tc.out)
		if (err == nil) != tc.ok {
			t.Errorf("parseSubnetMints(%q,%q): err=%v, want ok=%v", tc.in, tc.out, err, tc.ok)
			continue
		}
		if !tc.ok {
			continue
		}
		if netuid != tc.netuid || isBuy != tc.isBuy {
			t.Errorf("parseSubnetMints(%q,%q): got (%d, %v), want (%d, %v)",
				tc.in, tc.out, netuid, isBuy, tc.netuid, tc.isBuy)
		}
	}
}

func TestParseFeedSymbol(t *testing.T) {
	cases := []struct {
		sym    string
		netuid uint16
		ok     bool
	}{
		{"SN8/TAO", 8, true},
		{"SN1/TAO", 1, true},
		{"SN128/TAO", 128, true},
		{"BTC", 0, false},
		{"SN8", 0, false},
		{"sn8/TAO", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseFeedSymbol(tc.sym)
		if ok != tc.ok || got != tc.netuid {
			t.Errorf("parseFeedSymbol(%q): got (%d,%v), want (%d,%v)",
				tc.sym, got, ok, tc.netuid, tc.ok)
		}
	}
}

func TestSwap_GuardRail(t *testing.T) {
	v, err := New(Config{
		RPCURL:      "wss://entrypoint-finney.opentensor.ai:443",
		AllowSubmit: false,
	}, newTestSigner(t))
	if err != nil {
		t.Fatal(err)
	}

	// Construct a quote that hasn't expired. The actual on-chain
	// data isn't relevant — we just want to verify Swap refuses.
	q := types.Quote{
		InToken:   types.Asset{Mint: "TAO", Chain: types.ChainBittensor},
		OutToken:  types.Asset{Mint: "SN8", Chain: types.ChainBittensor},
		InAmount:  decimal.NewFromFloat(1.0),
		OutAmount: decimal.NewFromFloat(27.0),
		ExpiresAt: time.Now().Add(time.Minute),
	}
	_, err = v.Swap(context.Background(), q, 100)
	if err != ErrSubmitDisabled {
		t.Fatalf("expected ErrSubmitDisabled, got %v", err)
	}
}

// wrongChainSigner returns a non-bittensor chain to test rejection.
type wrongChainSigner struct{}

func (wrongChainSigner) Address() string { return "x" }
func (wrongChainSigner) Chain() types.ChainID {
	return types.ChainSolana
}
func (wrongChainSigner) Sign(_ context.Context, _ []byte) ([]byte, error) {
	return nil, nil
}
