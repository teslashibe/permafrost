package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/config"

	// Ensure noop is registered when the cli package's tests run, so the
	// strategies-registered check has at least one entry to find.
	_ "github.com/teslashibe/permafrost/strategies/noop"
)

// TestPrintDoctorResults_ExitCodeOnFailure verifies that any StatusFail
// produces an "preflight failed" error from the printer, regardless of how
// many warnings or successes are present alongside.
func TestPrintDoctorResults_ExitCodeOnFailure(t *testing.T) {
	results := []CheckResult{
		{Name: "go version", Status: StatusOK, Detail: "go1.25"},
		{Name: "docker", Status: StatusWarn, Detail: "not on PATH"},
		{Name: "database", Status: StatusFail, Detail: "localhost:5432", Err: errors.New("ping failed")},
	}
	var buf bytes.Buffer
	err := printDoctorResults(&buf, results, false)
	if err == nil {
		t.Fatal("expected error when any check fails")
	}
	if !strings.Contains(buf.String(), "Fix errors") {
		t.Errorf("expected 'Fix errors' guidance in output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "1 error(s)") {
		t.Errorf("expected '1 error(s)' in summary:\n%s", buf.String())
	}
}

// TestPrintDoctorResults_NoFailures returns nil when only warnings or all OK.
func TestPrintDoctorResults_NoFailures(t *testing.T) {
	results := []CheckResult{
		{Name: "go version", Status: StatusOK, Detail: "go1.25"},
		{Name: "docker", Status: StatusWarn, Detail: "not on PATH"},
	}
	var buf bytes.Buffer
	if err := printDoctorResults(&buf, results, false); err != nil {
		t.Fatalf("warnings should not produce an error: %v", err)
	}
}

// TestPrintDoctorResults_VerboseShowsErrors ensures --verbose surfaces
// the underlying error string under the failed check.
func TestPrintDoctorResults_VerboseShowsErrors(t *testing.T) {
	results := []CheckResult{
		{Name: "database", Status: StatusFail, Detail: "localhost:5432", Err: errors.New("connection refused")},
	}
	var buf bytes.Buffer
	_ = printDoctorResults(&buf, results, true)
	if !strings.Contains(buf.String(), "connection refused") {
		t.Errorf("verbose mode should include the underlying error:\n%s", buf.String())
	}
}

// TestRedactURL strips credentials from URLs in the doctor output. This is
// the soft-but-helpful guarantee that a user pasting their preflight log
// in an issue tracker doesn't leak DB passwords.
func TestRedactURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			in:   "postgres://permafrost:secret@localhost:5432/permafrost",
			want: "postgres://REDACTED@localhost:5432/permafrost",
		},
		{
			in:   "https://api.helius.com/?api-key=abc123",
			want: "https://api.helius.com/?api-key=abc123", // no @, nothing to redact
		},
		{
			in:   "http://localhost:8899",
			want: "http://localhost:8899",
		},
	}
	for _, tc := range cases {
		if got := redactURL(tc.in); got != tc.want {
			t.Errorf("redactURL(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestCheckGoVersion validates the version-string parser against a spread
// of real and synthetic Go version strings. We can't easily mock
// runtime.Version() so this exercises the parsing helpers indirectly via
// the atoiSafe + minor-extraction logic.
func TestAtoiSafe(t *testing.T) {
	cases := map[string]int{
		"25":   25,
		"0":    0,
		"":     0,
		"abc":  0,
		"25.5": 0, // dots aren't digits → returns 0
	}
	for in, want := range cases {
		if got := atoiSafe(in); got != want {
			t.Errorf("atoiSafe(%q) = %d want %d", in, got, want)
		}
	}
}

// TestCheckStrategiesRegistered reports the registered strategies. With
// `noop` registered (via cmd/permafrost/strategies.go in main builds, or
// directly imported in this test process), it must be StatusOK.
func TestCheckStrategiesRegistered(t *testing.T) {
	r := checkStrategiesRegistered()
	if r.Status != StatusOK {
		t.Errorf("expected StatusOK with noop registered; got %v (%s)", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "noop") {
		t.Errorf("expected detail to mention noop; got %q", r.Detail)
	}
}

// TestCheckSolanaRPC_Success uses an httptest server that responds like a
// healthy Solana node would to getSlot. Verifies the happy path including
// the slot number landing in the detail line.
func TestCheckSolanaRPC_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":281234567}`))
	}))
	defer srv.Close()

	g := &Globals{Config: &config.Config{Solana: config.SolanaConfig{RPCURL: srv.URL}}}
	r := checkSolanaRPC(context.Background(), g)
	if r.Status != StatusOK {
		t.Fatalf("expected StatusOK, got %v (%s); err=%v", r.Status, r.Detail, r.Err)
	}
	if !strings.Contains(r.Detail, "281234567") {
		t.Errorf("expected slot in detail; got %q", r.Detail)
	}
}

// TestCheckSolanaRPC_NotConfigured returns StatusSkip when no RPC URL is
// set — the doctor should never make an HTTP call against an empty URL.
func TestCheckSolanaRPC_NotConfigured(t *testing.T) {
	g := &Globals{Config: &config.Config{}}
	r := checkSolanaRPC(context.Background(), g)
	if r.Status != StatusSkip {
		t.Errorf("expected StatusSkip when RPCURL is empty; got %v", r.Status)
	}
}

// TestCheckSolanaRPC_RateLimited turns 429 into a warning, not a fail.
// Operators on free public RPCs hit this constantly; warning + a nudge
// to chainlist/Helius is the right UX.
func TestCheckSolanaRPC_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	g := &Globals{Config: &config.Config{Solana: config.SolanaConfig{RPCURL: srv.URL}}}
	r := checkSolanaRPC(context.Background(), g)
	if r.Status != StatusWarn {
		t.Fatalf("429 should produce StatusWarn, got %v (%s)", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "rate-limited") {
		t.Errorf("warning detail should mention rate-limited; got %q", r.Detail)
	}
}

// TestCheckSolanaRPC_RPCError surfaces a JSON-RPC error from the node
// as a failure with the underlying message in Err.
func TestCheckSolanaRPC_RPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`))
	}))
	defer srv.Close()

	g := &Globals{Config: &config.Config{Solana: config.SolanaConfig{RPCURL: srv.URL}}}
	r := checkSolanaRPC(context.Background(), g)
	if r.Status != StatusFail {
		t.Fatalf("rpc error should produce StatusFail, got %v", r.Status)
	}
	if r.Err == nil || !strings.Contains(r.Err.Error(), "Method not found") {
		t.Errorf("expected underlying error to mention Method not found; got %v", r.Err)
	}
}

// TestCheckEVMRPCs_HappyAndRateLimitMix verifies the per-chain check
// handles a mix of healthy + rate-limited backends and reports both
// correctly without aborting the whole batch.
func TestCheckEVMRPCs_HappyAndRateLimitMix(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x16a4c8d"}`))
	}))
	defer healthy.Close()
	rateLimited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer rateLimited.Close()

	g := &Globals{Config: &config.Config{
		EVM: config.EVMConfig{
			Chains: map[string]config.EVMChainConfig{
				"base":     {RPCURL: healthy.URL},
				"ethereum": {RPCURL: rateLimited.URL},
			},
		},
	}}
	results := checkEVMRPCs(context.Background(), g)
	if len(results) != 2 {
		t.Fatalf("expected one row per chain; got %d", len(results))
	}
	statusByName := map[string]CheckStatus{}
	for _, r := range results {
		statusByName[r.Name] = r.Status
	}
	if statusByName["evm RPC: base"] != StatusOK {
		t.Errorf("base should be OK, got %v", statusByName["evm RPC: base"])
	}
	if statusByName["evm RPC: ethereum"] != StatusWarn {
		t.Errorf("ethereum (429) should be Warn, got %v", statusByName["evm RPC: ethereum"])
	}
}

// TestCheckEVMRPCs_NoChainsConfigured returns a single Skip row when
// no EVM chains are configured. Common state for noop-only operators.
func TestCheckEVMRPCs_NoChainsConfigured(t *testing.T) {
	g := &Globals{Config: &config.Config{}}
	results := checkEVMRPCs(context.Background(), g)
	if len(results) != 1 || results[0].Status != StatusSkip {
		t.Errorf("expected single StatusSkip row; got %+v", results)
	}
}

