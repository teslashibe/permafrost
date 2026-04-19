package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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

