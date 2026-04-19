package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/internal/store"
	"github.com/teslashibe/permafrost/pkg/strategy"
)

// doctor is the framework's preflight checker. It runs every check in
// sequence (does not short-circuit), prints colour-coded results, and
// exits 1 if any check failed (warnings don't fail the command).
//
// The metaphor (per epic #30): Skipper the husky doing a pre-game
// inspection of the camp. "All systems checked. World 1 is ready."

func init() { addCommandFactory(newDoctorCmd) }

func newDoctorCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Preflight check: verify Go, Docker, DB, keystore, inference providers, RPCs, strategies",
		Long: `Runs every preflight check in sequence and prints a green/yellow/red status
for each. Exits 0 if no errors; 1 if any check fails. Warnings don't fail.

Run this before 'permafrost agent start' to catch common misconfigurations
before they bite you on the first decision tick.

When an RPC check fails, try a different endpoint from https://chainlist.org/
(EVM) or your paid Solana provider (Helius/Triton).`,
		RunE: func(c *cobra.Command, _ []string) error {
			results := runDoctor(c.Context(), c, verbose)
			return printDoctorResults(c.OutOrStdout(), results, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show underlying error details for failures")
	return cmd
}

// CheckStatus is the result level for one preflight check.
type CheckStatus int

const (
	StatusOK   CheckStatus = iota // ✓ green
	StatusWarn                    // ⚠ yellow
	StatusFail                    // ✗ red
	StatusSkip                    // ─ grey (check intentionally skipped)
)

// CheckResult is one row in the doctor output.
type CheckResult struct {
	Name   string
	Status CheckStatus
	Detail string
	Err    error // populated on StatusFail; surfaced under --verbose
}

// httpClient is the doctor's HTTP client. Hard 5s ceiling so a hung TLS
// handshake (which a bare context.WithTimeout doesn't always preempt)
// cannot make the doctor wedge.
var httpClient = &http.Client{Timeout: 5 * time.Second}

// runDoctor executes every check. Order is deliberate (cheapest first so the
// operator sees fast feedback even on a slow connection).
func runDoctor(ctx context.Context, c *cobra.Command, _ bool) []CheckResult {
	g := FromContext(ctx)
	checks := []CheckResult{}

	// ── environment ───────────────────────────────────────────────
	checks = append(checks, checkGoVersion())
	checks = append(checks, checkDocker())

	if g == nil {
		// No globals = no config loaded; everything below depends on it.
		// Mark remaining checks as skipped with a clear reason.
		checks = append(checks, CheckResult{
			Name:   "config loaded",
			Status: StatusFail,
			Detail: "globals not initialised; cannot run config-dependent checks",
		})
		return checks
	}
	checks = append(checks, checkConfigLoaded(g))

	// ── data stores ───────────────────────────────────────────────
	checks = append(checks, checkDatabase(ctx, g))

	// ── keystore + secrets ────────────────────────────────────────
	checks = append(checks, checkKeystorePassphrase(g))
	checks = append(checks, checkKeystoreFile(c))

	// ── inference ─────────────────────────────────────────────────
	checks = append(checks, checkInferenceProviders(g)...)

	// ── chain RPCs ────────────────────────────────────────────────
	checks = append(checks, checkSolanaRPC(ctx, g))
	checks = append(checks, checkEVMRPCs(ctx, g)...)

	// ── strategies ────────────────────────────────────────────────
	checks = append(checks, checkStrategiesRegistered())

	return checks
}

// ─── individual checks ─────────────────────────────────────────────────────

func checkGoVersion() CheckResult {
	v := runtime.Version() // "go1.25.5"
	r := CheckResult{Name: "go version", Detail: v, Status: StatusOK}
	if !strings.HasPrefix(v, "go1.") {
		r.Status = StatusWarn
		r.Detail = v + " (unrecognised version string)"
		return r
	}
	rest := strings.TrimPrefix(v, "go1.")
	// "25.5" → minor "25"
	minor := rest
	if i := strings.Index(rest, "."); i >= 0 {
		minor = rest[:i]
	}
	if minorInt := atoiSafe(minor); minorInt < 25 {
		r.Status = StatusFail
		r.Detail = v + " (need Go 1.25+)"
		r.Err = fmt.Errorf("go version too old: %s", v)
	}
	return r
}

func checkDocker() CheckResult {
	r := CheckResult{Name: "docker", Detail: ""}
	path, err := exec.LookPath("docker")
	if err != nil {
		r.Status = StatusWarn
		r.Detail = "not on PATH (only required for `make up`)"
		return r
	}
	out, err := exec.Command(path, "version", "--format", "{{.Client.Version}}").CombinedOutput()
	if err != nil {
		r.Status = StatusWarn
		r.Detail = "found at " + path + " but `docker version` failed (daemon not running?)"
		r.Err = err
		return r
	}
	r.Status = StatusOK
	r.Detail = strings.TrimSpace(string(out))
	return r
}

func checkConfigLoaded(g *Globals) CheckResult {
	if g.Config == nil {
		return CheckResult{
			Name:   "config loaded",
			Status: StatusFail,
			Detail: "config is nil",
		}
	}
	return CheckResult{
		Name:   "config loaded",
		Status: StatusOK,
		Detail: fmt.Sprintf("env=%s", g.Config.Env),
	}
}

func checkDatabase(ctx context.Context, g *Globals) CheckResult {
	r := CheckResult{Name: "database reachable"}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := store.Open(dbCtx, g.Config.Database)
	if err != nil {
		r.Status = StatusFail
		r.Detail = redactURL(g.Config.Database.URL)
		r.Err = err
		return r
	}
	defer db.Close()
	r.Status = StatusOK
	r.Detail = redactURL(g.Config.Database.URL)
	return r
}

func checkKeystorePassphrase(g *Globals) CheckResult {
	r := CheckResult{Name: "keystore passphrase env"}
	envName := g.Config.Wallet.PassphraseEnv
	if envName == "" {
		envName = "PERMAFROST_KEYSTORE_PASSPHRASE"
	}
	if os.Getenv(envName) == "" {
		r.Status = StatusWarn
		r.Detail = envName + " not set (only required for keystore-using commands)"
		return r
	}
	r.Status = StatusOK
	r.Detail = envName + " set"
	return r
}

func checkKeystoreFile(c *cobra.Command) CheckResult {
	r := CheckResult{Name: "keystore file"}
	path, err := keystorePath(c)
	if err != nil {
		r.Status = StatusFail
		r.Detail = "cannot resolve path"
		r.Err = err
		return r
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			r.Status = StatusWarn
			r.Detail = path + " (not yet created — run `permafrost wallet generate` or `wallet import`)"
			return r
		}
		r.Status = StatusFail
		r.Detail = path
		r.Err = err
		return r
	}
	r.Status = StatusOK
	r.Detail = fmt.Sprintf("%s (%d bytes)", path, info.Size())
	return r
}

func checkInferenceProviders(g *Globals) []CheckResult {
	if len(g.Config.Inference.Providers) == 0 {
		return []CheckResult{{
			Name:   "inference providers",
			Status: StatusWarn,
			Detail: "none configured (only required for LLM-using strategies)",
		}}
	}
	out := make([]CheckResult, 0, len(g.Config.Inference.Providers))
	// Sort provider names for deterministic output ordering.
	names := make([]string, 0, len(g.Config.Inference.Providers))
	for n := range g.Config.Inference.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := g.Config.Inference.Providers[name]
		r := CheckResult{Name: "inference: " + name}
		if p.BaseURL == "" {
			r.Status = StatusFail
			r.Detail = "base_url is required"
			r.Err = errors.New("base_url is empty")
			out = append(out, r)
			continue
		}
		// Don't actually call the provider — just verify config plausibility.
		// A real round-trip needs `permafrost inference test --provider <name>`.
		hasKey := p.APIKey != "" || (p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) != "")
		if p.APIKeyEnv != "" && !hasKey {
			r.Status = StatusWarn
			r.Detail = fmt.Sprintf("base_url=%s but %s not set (provider may reject auth)", p.BaseURL, p.APIKeyEnv)
		} else {
			r.Status = StatusOK
			r.Detail = "base_url=" + p.BaseURL
		}
		out = append(out, r)
	}
	return out
}

func checkSolanaRPC(ctx context.Context, g *Globals) CheckResult {
	r := CheckResult{Name: "solana RPC"}
	if g.Config.Solana.RPCURL == "" {
		r.Status = StatusSkip
		r.Detail = "not configured"
		return r
	}
	// Use getSlot rather than getHealth: getHealth is restricted on most
	// commercial providers (Helius, Triton); getSlot is part of the
	// universally-exposed RPC surface and returns a uint64 we can show.
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"getSlot"}`)
	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rpcCtx, http.MethodPost, g.Config.Solana.RPCURL, body)
	if err != nil {
		r.Status = StatusFail
		r.Detail = redactURL(g.Config.Solana.RPCURL)
		r.Err = err
		return r
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		r.Status = StatusFail
		r.Detail = redactURL(g.Config.Solana.RPCURL) + " (network error)"
		r.Err = err
		return r
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusTooManyRequests {
		r.Status = StatusWarn
		r.Detail = redactURL(g.Config.Solana.RPCURL) + " (rate-limited; try a paid provider like Helius/Triton)"
		return r
	}
	if resp.StatusCode != http.StatusOK {
		r.Status = StatusFail
		r.Detail = fmt.Sprintf("%s (HTTP %d)", redactURL(g.Config.Solana.RPCURL), resp.StatusCode)
		r.Err = fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200))
		return r
	}
	// getSlot returns a number in result; presence indicates a working RPC.
	var rpcResp struct {
		Result *uint64 `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		r.Status = StatusFail
		r.Detail = redactURL(g.Config.Solana.RPCURL) + " (unparseable response)"
		r.Err = err
		return r
	}
	if rpcResp.Error != nil {
		r.Status = StatusFail
		r.Detail = redactURL(g.Config.Solana.RPCURL) + " (rpc error: " + rpcResp.Error.Message + ")"
		r.Err = errors.New(rpcResp.Error.Message)
		return r
	}
	if rpcResp.Result == nil {
		r.Status = StatusWarn
		r.Detail = redactURL(g.Config.Solana.RPCURL) + " (no result; method may be restricted)"
		return r
	}
	r.Status = StatusOK
	r.Detail = fmt.Sprintf("%s (slot %d)", redactURL(g.Config.Solana.RPCURL), *rpcResp.Result)
	return r
}

func checkEVMRPCs(ctx context.Context, g *Globals) []CheckResult {
	if len(g.Config.EVM.Chains) == 0 {
		return []CheckResult{{
			Name:   "evm RPCs",
			Status: StatusSkip,
			Detail: "no chains configured",
		}}
	}
	names := make([]string, 0, len(g.Config.EVM.Chains))
	for n := range g.Config.EVM.Chains {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]CheckResult, 0, len(names))
	for _, name := range names {
		ch := g.Config.EVM.Chains[name]
		r := CheckResult{Name: "evm RPC: " + name}
		if ch.RPCURL == "" {
			r.Status = StatusFail
			r.Detail = "rpc_url is empty (try chainlist.org for a fresh one)"
			r.Err = errors.New("rpc_url empty")
			out = append(out, r)
			continue
		}
		// eth_blockNumber is a cheap, universally supported call.
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`)
		rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, err := http.NewRequestWithContext(rpcCtx, http.MethodPost, ch.RPCURL, body)
		if err != nil {
			cancel()
			r.Status = StatusFail
			r.Detail = redactURL(ch.RPCURL)
			r.Err = err
			out = append(out, r)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		cancel()
		if err != nil {
			r.Status = StatusFail
			r.Detail = redactURL(ch.RPCURL) + " (network error; try a different RPC from chainlist.org)"
			r.Err = err
			out = append(out, r)
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			r.Status = StatusWarn
			r.Detail = redactURL(ch.RPCURL) + " (rate-limited; try a different RPC from chainlist.org)"
			out = append(out, r)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			r.Status = StatusFail
			r.Detail = fmt.Sprintf("%s (HTTP %d)", redactURL(ch.RPCURL), resp.StatusCode)
			r.Err = fmt.Errorf("http %d: %s", resp.StatusCode, string(respBody))
			out = append(out, r)
			continue
		}
		// Parse the block number for a friendly detail line.
		var rpcResp struct {
			Result string `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &rpcResp)
		if rpcResp.Error != nil {
			r.Status = StatusFail
			r.Detail = redactURL(ch.RPCURL) + " (rpc error: " + rpcResp.Error.Message + ")"
			r.Err = errors.New(rpcResp.Error.Message)
			out = append(out, r)
			continue
		}
		r.Status = StatusOK
		r.Detail = fmt.Sprintf("%s (block %s)", redactURL(ch.RPCURL), rpcResp.Result)
		out = append(out, r)
	}
	return out
}

func checkStrategiesRegistered() CheckResult {
	r := CheckResult{Name: "strategies registered"}
	names := strategy.List()
	if len(names) == 0 {
		r.Status = StatusFail
		r.Detail = "no strategies registered (cmd/permafrost(d)/strategies.go missing imports?)"
		r.Err = errors.New("registry empty")
		return r
	}
	r.Status = StatusOK
	r.Detail = strings.Join(names, ", ")
	return r
}

// ─── output ────────────────────────────────────────────────────────────────

func printDoctorResults(w io.Writer, results []CheckResult, verbose bool) error {
	fmt.Fprintln(w, "Permafrost preflight ───────────────────────────────────────────")
	var oks, warns, fails, skips int
	for _, r := range results {
		fmt.Fprintf(w, "  %s  %-26s %s\n", statusGlyph(r.Status), r.Name, r.Detail)
		if verbose && r.Err != nil {
			fmt.Fprintf(w, "       └─ %v\n", r.Err)
		}
		switch r.Status {
		case StatusOK:
			oks++
		case StatusWarn:
			warns++
		case StatusFail:
			fails++
		case StatusSkip:
			skips++
		}
	}
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────────────")
	parts := []string{}
	if oks > 0 {
		parts = append(parts, fmt.Sprintf("%d ok", oks))
	}
	if warns > 0 {
		parts = append(parts, fmt.Sprintf("%d warning(s)", warns))
	}
	if fails > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", fails))
	}
	if skips > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skips))
	}
	summary := strings.Join(parts, ", ")
	if fails > 0 {
		fmt.Fprintf(w, "%s. Fix errors before `permafrost agent start`.\n", summary)
		if !verbose {
			fmt.Fprintln(w, "Re-run with --verbose to see underlying errors.")
		}
		return errors.New("preflight failed")
	}
	if warns > 0 {
		fmt.Fprintf(w, "%s. You're good to go (warnings indicate optional features).\n", summary)
		return nil
	}
	fmt.Fprintf(w, "%s. You're good to go.\n", summary)
	return nil
}

func statusGlyph(s CheckStatus) string {
	// ANSI colour codes work in most terminals; they degrade to literal text
	// in non-tty contexts (CI logs, file redirection) which is fine.
	switch s {
	case StatusOK:
		return "\033[32m✓\033[0m"
	case StatusWarn:
		return "\033[33m⚠\033[0m"
	case StatusFail:
		return "\033[31m✗\033[0m"
	case StatusSkip:
		return "─"
	}
	return "?"
}

// ─── helpers ───────────────────────────────────────────────────────────────

// redactURL strips username:password from a URL so the doctor doesn't leak
// credentials in its output. Returns the original on any parse difficulty.
func redactURL(raw string) string {
	// We deliberately don't use net/url here because Postgres URLs sometimes
	// contain query-string secrets too; a simple textual scrub is robust.
	if i := strings.Index(raw, "@"); i > 0 {
		// Find scheme://user:pass@host pattern
		if j := strings.Index(raw, "://"); j >= 0 && j < i {
			return raw[:j+3] + "REDACTED@" + raw[i+1:]
		}
	}
	return raw
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func atoiSafe(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
