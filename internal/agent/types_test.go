package agent

import "testing"

// TestParseMode_TyposRejected is the regression guard for the
// "papre falls through to live execution" footgun. The runtime
// only branches paper when Mode == ModePaper, so any typo that
// passes the input boundary becomes live. ParseMode is the
// boundary; this test asserts it actually rejects the typo set.
func TestParseMode_TyposRejected(t *testing.T) {
	// Each of these must error — none of them should silently fall
	// through to either paper or live.
	cases := []string{
		"papre", "papper", "papre ", "PAPER", "Paper",
		"livv", "Live", "LIVE", "live ", " live",
		"halted", "stopped", "running",
		"yes", "no", "true", "false", "1", "0",
		"\nlive", "live\n",
	}
	for _, in := range cases {
		_, err := ParseMode(in)
		if err == nil {
			t.Errorf("ParseMode(%q): expected error, got nil — typo footgun is open", in)
		}
	}
}

// TestParseMode_HappyPath sanity-checks the two valid inputs and
// the empty-string-defaults-to-paper safety convention.
func TestParseMode_HappyPath(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
	}{
		{"", ModePaper},
		{"paper", ModePaper},
		{"live", ModeLive},
	}
	for _, tc := range cases {
		got, err := ParseMode(tc.in)
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestModeValidate covers the receiver-method form. Empty is invalid
// here (callers wanting "default to paper" should use ParseMode).
func TestModeValidate(t *testing.T) {
	if err := ModePaper.Validate(); err != nil {
		t.Errorf("ModePaper.Validate(): %v", err)
	}
	if err := ModeLive.Validate(); err != nil {
		t.Errorf("ModeLive.Validate(): %v", err)
	}
	if err := Mode("").Validate(); err == nil {
		t.Errorf("Mode(\"\").Validate(): expected error")
	}
	if err := Mode("papre").Validate(); err == nil {
		t.Errorf("Mode(\"papre\").Validate(): expected error")
	}
}
