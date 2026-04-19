package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedPrompter is a test prompter that returns canned responses in
// order. AskYN reads "y"/"n" responses; AskChoice reads literal choice
// strings. If responses run out, it returns the prompted default — so
// half-scripted tests behave gracefully.
type scriptedPrompter struct {
	answers []string
	idx     int
}

func (s *scriptedPrompter) next(def string) string {
	if s.idx >= len(s.answers) {
		return def
	}
	v := s.answers[s.idx]
	s.idx++
	if v == "" {
		return def
	}
	return v
}

func (s *scriptedPrompter) Ask(_, def string) (string, error) {
	return s.next(def), nil
}

func (s *scriptedPrompter) AskYN(_ string, defaultYes bool) (bool, error) {
	def := "n"
	if defaultYes {
		def = "y"
	}
	v := s.next(def)
	return strings.EqualFold(v, "y") || strings.EqualFold(v, "yes"), nil
}

func (s *scriptedPrompter) AskChoice(_ string, choices []string, defaultIdx int) (string, error) {
	def := choices[defaultIdx]
	v := s.next(def)
	for _, c := range choices {
		if strings.EqualFold(v, c) {
			return c, nil
		}
	}
	return def, nil
}

// TestRunInit_FreshHappyPath drives the wizard end-to-end against a
// fresh tmpdir and asserts: config + env files materialised, output
// includes the next-steps block, exit was clean.
func TestRunInit_FreshHappyPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	keystorePath := filepath.Join(dir, "keystore.json")
	envPath := filepath.Join(dir, "env")

	p := &scriptedPrompter{answers: []string{
		"Captain Pole",   // callsign
		cfgPath,          // config path
		keystorePath,     // keystore path
		envPath,          // env path
		"n",              // skip inference setup
	}}
	var out bytes.Buffer
	err := runInit(&out, p, initOptions{Theme: "arctic"})
	if err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config not written: %v", err)
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("env file not written: %v", err)
	}
	body, _ := os.ReadFile(envPath)
	if !strings.Contains(string(body), "PERMAFROST_KEYSTORE_PASSPHRASE=") {
		t.Errorf("env file missing passphrase var; got:\n%s", body)
	}

	got := out.String()
	for _, frag := range []string{
		"Camp set up",            // arctic done message
		"Next steps:",            // next-steps header
		"permafrost doctor",      // doctor in next steps
		"chainlist.org",          // EVM nudge
	} {
		if !strings.Contains(got, frag) {
			t.Errorf("output missing %q:\n%s", frag, got)
		}
	}
}

// TestRunInit_Idempotent re-runs the wizard against an existing
// installation and asserts files aren't overwritten and the user is
// told what was kept.
func TestRunInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	envPath := filepath.Join(dir, "env")

	// Pre-seed both files with sentinel content.
	if err := os.WriteFile(cfgPath, []byte("# pre-existing config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("# pre-existing env\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	p := &scriptedPrompter{answers: []string{
		"",       // callsign default
		cfgPath,  // config path (exists)
		filepath.Join(dir, "keystore.json"),
		envPath,  // env path (exists)
		"n",      // skip inference
	}}
	var out bytes.Buffer
	if err := runInit(&out, p, initOptions{Theme: "arctic"}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	// Files should be untouched.
	if body, _ := os.ReadFile(cfgPath); string(body) != "# pre-existing config\n" {
		t.Errorf("config was overwritten; got %q", body)
	}
	if body, _ := os.ReadFile(envPath); string(body) != "# pre-existing env\n" {
		t.Errorf("env was overwritten; got %q", body)
	}

	// Output should announce the keeps.
	got := out.String()
	if !strings.Contains(got, "kept existing") {
		t.Errorf("output should mention 'kept existing':\n%s", got)
	}
}

// TestRunInit_NonInteractive_PlainTheme verifies the --non-interactive
// + --theme plain combination produces a working setup with no
// prompts and no arctic vocabulary.
func TestRunInit_NonInteractive_PlainTheme(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	envPath := filepath.Join(dir, "env")

	var out bytes.Buffer
	err := runInit(&out, nonInteractivePrompter{}, initOptions{
		NonInteractive: true,
		Theme:          "plain",
		ConfigPath:     cfgPath,
		KeystorePath:   filepath.Join(dir, "keystore.json"),
		EnvOut:         envPath,
	})
	if err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config not written: %v", err)
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Errorf("env not written: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Captain Pole") {
		t.Errorf("plain theme should not contain arctic vocab; got:\n%s", got)
	}
	if !strings.Contains(got, "Setup complete") {
		t.Errorf("plain theme done message missing:\n%s", got)
	}
}

// TestGeneratePassphrase_ProducesUniqueValues sanity-checks that two
// successive generations land on different passphrases. (Also exercises
// the "wrote" path of generatePassphraseIfAbsent.)
func TestGeneratePassphrase_ProducesUniqueValues(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "env-a")
	b := filepath.Join(dir, "env-b")

	_, valA, err := generatePassphraseIfAbsent(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, valB, err := generatePassphraseIfAbsent(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if valA == "" || valB == "" {
		t.Fatal("expected non-empty passphrases")
	}
	if valA == valB {
		t.Errorf("two successive passphrases collided (vanishingly improbable; check rand source)")
	}
	// File permissions should be 0600 — passphrase is a secret.
	info, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("env file permission = %o, want 0600", perm)
	}
}

// TestGeneratePassphrase_KeptIfExists verifies the second call returns
// wrote=false and an empty value (the wizard treats this as "keep
// what's there"). Critical for idempotency.
func TestGeneratePassphrase_KeptIfExists(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	if _, _, err := generatePassphraseIfAbsent(envPath, nil); err != nil {
		t.Fatal(err)
	}
	wrote, val, err := generatePassphraseIfAbsent(envPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Errorf("second call should not have written")
	}
	if val != "" {
		t.Errorf("kept-path should return empty value, got %q", val)
	}
}

// TestVocabSwap verifies the arctic↔plain vocabulary toggle flips every
// user-facing string. Cheap regression guard against a future contributor
// adding an arctic-flavoured prompt that isn't gated behind v.arctic.
func TestVocabSwap(t *testing.T) {
	arctic := vocab("arctic")
	plain := vocab("plain")
	pairs := []struct {
		arcticVal string
		plainVal  string
	}{
		{arctic.banner(), plain.banner()},
		{arctic.callsignPrompt(), plain.callsignPrompt()},
		{arctic.defaultCallsign(), plain.defaultCallsign()},
		{arctic.inferencePrompt(), plain.inferencePrompt()},
		{arctic.done(), plain.done()},
	}
	for _, p := range pairs {
		if p.arcticVal == p.plainVal {
			t.Errorf("arctic and plain vocab agree (%q); should differ", p.arcticVal)
		}
	}
}

// TestStdioPrompter_DefaultsOnEmptyInput exercises the actual stdin path
// to make sure pressing Enter accepts the default cleanly.
func TestStdioPrompter_DefaultsOnEmptyInput(t *testing.T) {
	in := strings.NewReader("\n\n\n")
	var out bytes.Buffer
	p := newStdioPrompter(in, &out)

	got, err := p.Ask("ignored", "default-value")
	if err != nil && !errors.Is(err, nil) {
		t.Fatal(err)
	}
	if got != "default-value" {
		t.Errorf("Ask: got %q, want default-value", got)
	}

	yn, _ := p.AskYN("ignored", true)
	if !yn {
		t.Errorf("AskYN with defaultYes=true and empty input should return true")
	}
}
