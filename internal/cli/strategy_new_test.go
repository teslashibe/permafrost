package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// Register noop so the TestRunStrategyNew_RejectsRegisteredName
	// test has a known-registered name to collide against. (Other PRs
	// in this chain register noop too; we add it explicitly here so
	// the test passes regardless of merge order.)
	_ "github.com/teslashibe/permafrost/strategies/noop"
)

// TestValidateStrategyName: snake_case is required; Permafrost's existing
// names match the rule. Surfacing the error here is much friendlier than
// a confusing Go package-name compile failure later.
func TestValidateStrategyName(t *testing.T) {
	good := []string{"noop", "dca_buy", "market_maker_basic", "x", "x1_2"}
	for _, n := range good {
		if err := validateStrategyName(n); err != nil {
			t.Errorf("expected %q to be valid, got %v", n, err)
		}
	}
	bad := []string{"", "_underscore_lead", "1numlead", "Has-Hyphen", "MixedCase", "with space", strings.Repeat("a", 65)}
	for _, n := range bad {
		if err := validateStrategyName(n); err == nil {
			t.Errorf("expected %q to fail validation", n)
		}
	}
}

// TestRunStrategyNew_ScaffoldsAndRegisters drives the full flow against a
// tmpdir that mimics a Permafrost repo layout (cmd/permafrost(d)/...).
// Asserts the strategy directory + files materialise and import lines
// land in both binaries' strategies.go.
func TestRunStrategyNew_ScaffoldsAndRegisters(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	mustWritePermafrostStub(t, dir, "cmd/permafrost/strategies.go")
	mustWritePermafrostStub(t, dir, "cmd/permafrostd/strategies.go")

	var out bytes.Buffer
	if err := runStrategyNew(&out, "scaffolded", "noop", false); err != nil {
		t.Fatalf("runStrategyNew: %v", err)
	}

	// Strategy package created with the three template files.
	for _, want := range []string{"strategy.go", "strategy_test.go", "README.md"} {
		path := filepath.Join(dir, "strategies", "scaffolded", want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s, missing: %v", path, err)
		}
	}

	// strategy.go contains the templated package + Name.
	body, _ := os.ReadFile(filepath.Join(dir, "strategies", "scaffolded", "strategy.go"))
	if !strings.Contains(string(body), `package scaffolded`) {
		t.Errorf("strategy.go missing package decl:\n%s", body)
	}
	if !strings.Contains(string(body), `const Name = "scaffolded"`) {
		t.Errorf("strategy.go missing Name constant:\n%s", body)
	}

	// Both import files updated with the blank import.
	for _, p := range []string{"cmd/permafrost/strategies.go", "cmd/permafrostd/strategies.go"} {
		body, _ := os.ReadFile(filepath.Join(dir, p))
		if !strings.Contains(string(body), `_ "github.com/teslashibe/permafrost/strategies/scaffolded"`) {
			t.Errorf("%s missing import:\n%s", p, body)
		}
	}

	// Output mentions the next-steps block and both registrations.
	got := out.String()
	for _, frag := range []string{
		"Created strategies/scaffolded/",
		"cmd/permafrost/strategies.go",
		"cmd/permafrostd/strategies.go",
		"Next steps:",
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("output missing %q:\n%s", frag, got)
		}
	}
}

// TestRunStrategyNew_PrivateUsesLocalFiles: --private routes the strategy
// to strategies/private/ AND the *_local.go (gitignored) import lists,
// creating those files if they don't yet exist.
func TestRunStrategyNew_PrivateUsesLocalFiles(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	// Note: no committed strategies.go; private mode shouldn't need them.

	var out bytes.Buffer
	if err := runStrategyNew(&out, "secret_thing", "noop", true); err != nil {
		t.Fatalf("runStrategyNew: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "strategies", "private", "secret_thing", "strategy.go")); err != nil {
		t.Errorf("private strategy not written: %v", err)
	}
	for _, p := range []string{"cmd/permafrost/strategies_local.go", "cmd/permafrostd/strategies_local.go"} {
		body, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			t.Errorf("%s not created: %v", p, err)
			continue
		}
		if !strings.Contains(string(body), "package main") {
			t.Errorf("%s missing package decl:\n%s", p, body)
		}
		if !strings.Contains(string(body), `_ "github.com/teslashibe/permafrost/strategies/private/secret_thing"`) {
			t.Errorf("%s missing import:\n%s", p, body)
		}
	}
}

// TestRunStrategyNew_RejectsRegisteredName: a name that's already in
// the registry (e.g. "noop", which the test process imports) must
// fail fast with a clear message — not silently overwrite anything.
func TestRunStrategyNew_RejectsRegisteredName(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	var out bytes.Buffer
	err := runStrategyNew(&out, "noop", "noop", false)
	if err == nil {
		t.Fatal("expected error for already-registered name")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error should mention 'already registered'; got %q", err)
	}
}

// TestRunStrategyNew_RejectsExistingDir: if the target directory
// already exists, refuse to write over it.
func TestRunStrategyNew_RejectsExistingDir(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	mustWritePermafrostStub(t, dir, "cmd/permafrost/strategies.go")
	mustWritePermafrostStub(t, dir, "cmd/permafrostd/strategies.go")
	if err := os.MkdirAll(filepath.Join(dir, "strategies", "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runStrategyNew(&out, "occupied", "noop", false)
	if err == nil {
		t.Fatal("expected error for existing directory")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists'; got %q", err)
	}
}

// TestAppendImport_Idempotent: calling appendImport twice with the
// same line writes the line once.
func TestAppendImport_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "strategies.go")
	mustWritePermafrostStub(t, dir, "strategies.go")

	line := `	_ "example.com/x"`
	if err := appendImport(importTarget{Path: path}, line); err != nil {
		t.Fatal(err)
	}
	if err := appendImport(importTarget{Path: path}, line); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if got := strings.Count(string(body), `"example.com/x"`); got != 1 {
		t.Errorf("import should appear exactly once after two appends; got %d in:\n%s", got, body)
	}
}

// TestRunStrategyNew_RejectsUnshippedTemplate: --template basis (etc)
// returns an explicit "not yet shipped" error rather than producing a
// half-baked stub.
func TestRunStrategyNew_RejectsUnshippedTemplate(t *testing.T) {
	dir := t.TempDir()
	mustChdir(t, dir)
	var out bytes.Buffer
	err := runStrategyNew(&out, "x", "basis", false)
	if err == nil {
		t.Fatal("expected error for unshipped template")
	}
	if !strings.Contains(err.Error(), "not shipped yet") {
		t.Errorf("error should mention 'not shipped yet'; got %q", err)
	}
}

// ─── helpers ───────────────────────────────────────────────────────────────

func mustChdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// mustWritePermafrostStub creates a minimal strategies.go file at
// dir/relpath that has a parseable import block. The scaffolder needs
// this to exist before it can append the new import line.
func mustWritePermafrostStub(t *testing.T, dir, relpath string) {
	t.Helper()
	full := filepath.Join(dir, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `package main

import (
)
`
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
