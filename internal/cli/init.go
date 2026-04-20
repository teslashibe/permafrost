package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// init wires the `permafrost init` command into the cobra registry.
//
// The wizard walks a first-time operator from "git clone" to a working
// configuration in 60 seconds. End state: config.yaml exists, a keystore
// passphrase is generated and saved to a sourceable env file, and the
// operator is told the next 3 commands to type. Deliberately does NOT
// touch the database — that requires `make up` first.

func init() { addCommandFactory(newInitCmd) }

func newInitCmd() *cobra.Command {
	opts := initOptions{Theme: "arctic"}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactive setup wizard: config + keystore passphrase + first-agent template",
		Long: `Walks a first-time operator through Permafrost setup in ~60 seconds.

Idempotent: re-running on an already-configured repo detects existing
files and offers to skip / append rather than overwriting. Safe to run
multiple times.

The arctic theme uses character vocabulary (Captain Pole the polar bear,
penguin traders, etc.). Pass --theme plain for the no-vocabulary version.

Use --non-interactive for scripted installs; defaults are applied
silently and any existing files are left alone.`,
		RunE: func(c *cobra.Command, _ []string) error {
			var p prompter = newStdioPrompter(c.InOrStdin(), c.OutOrStdout())
			if opts.NonInteractive {
				p = nonInteractivePrompter{}
			}
			return runInit(c.OutOrStdout(), p, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.NonInteractive, "non-interactive", false, "skip all prompts; accept defaults silently (for CI / scripts)")
	cmd.Flags().StringVar(&opts.Theme, "theme", "arctic", "vocabulary theme: arctic (Captain Pole, penguins, …) or plain")
	cmd.Flags().StringVar(&opts.ConfigPath, "config-out", "", "where to write config.yaml (default: ./config.yaml)")
	cmd.Flags().StringVar(&opts.KeystorePath, "keystore-out", "", "where to put the keystore (default: ~/.permafrost/keystore.json)")
	cmd.Flags().StringVar(&opts.EnvOut, "env-out", "", "where to write the generated passphrase env file (default: ~/.permafrost/env)")
	return cmd
}

// initOptions are the wizard's flags. Each one has a sensible default;
// non-interactive mode applies all of them silently.
type initOptions struct {
	NonInteractive bool
	Theme          string
	ConfigPath     string
	KeystorePath   string
	EnvOut         string
}

// runInit is the wizard body. Split out from the cobra cmd so tests can
// drive it with a scripted prompter and capture output for assertions.
func runInit(out io.Writer, p prompter, opts initOptions) error {
	v := vocab(opts.Theme)
	fmt.Fprintln(out, v.banner())

	// ── 1. Captain's callsign ─────────────────────────────────────
	callsign, err := p.Ask(v.callsignPrompt(), v.defaultCallsign())
	if err != nil {
		return err
	}

	// ── 2. Resolve target paths ───────────────────────────────────
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath, err = p.Ask("Where should I keep your config?", "./config.yaml")
		if err != nil {
			return err
		}
	}
	keystorePath := opts.KeystorePath
	if keystorePath == "" {
		def, _ := defaultKeystorePath()
		keystorePath, err = p.Ask("Where should I keep your keystore?", def)
		if err != nil {
			return err
		}
	}
	envPath := opts.EnvOut
	if envPath == "" {
		def, _ := defaultEnvPath()
		envPath, err = p.Ask("Where should I save the generated passphrase env file?", def)
		if err != nil {
			return err
		}
	}

	// ── 3. Config file ────────────────────────────────────────────
	wroteConfig, err := writeConfigIfAbsent(configPath, p)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// ── 4. Keystore passphrase ────────────────────────────────────
	wrotePassphrase, passphraseValue, err := generatePassphraseIfAbsent(envPath, p)
	if err != nil {
		return fmt.Errorf("passphrase: %w", err)
	}

	// ── 5. Inference (optional) ───────────────────────────────────
	wantInf, err := p.AskYN(v.inferencePrompt(), false)
	if err != nil {
		return err
	}
	infChoice := ""
	if wantInf {
		infChoice, err = p.AskChoice(
			"Pick a provider (you can edit config.yaml later):",
			[]string{"openrouter", "openai", "groq", "ollama", "skip"},
			0,
		)
		if err != nil {
			return err
		}
		if infChoice != "skip" {
			fmt.Fprintf(out, "  ℹ Make sure %s_API_KEY is set in your env (or edit config.yaml inference.providers.%s.api_key_env).\n",
				strings.ToUpper(infChoice), infChoice)
		}
	}

	// ── 6. EVM chains: nudge to chainlist.org ─────────────────────
	fmt.Fprintf(out, "\n  ℹ EVM RPCs: pick fresh ones from https://chainlist.org/ when you want to trade EVM-chain spot legs.\n")
	fmt.Fprintf(out, "    Add them under evm.chains.<name>.rpc_url in %s.\n", configPath)

	// ── 7. Done ───────────────────────────────────────────────────
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, v.done())
	if wroteConfig {
		fmt.Fprintf(out, "  ✓ wrote %s\n", configPath)
	} else {
		fmt.Fprintf(out, "  ─ kept existing %s (re-run with rm if you want a fresh template)\n", configPath)
	}
	if wrotePassphrase {
		fmt.Fprintf(out, "  ✓ generated keystore passphrase, saved to %s (mode 0600)\n", envPath)
		fmt.Fprintf(out, "    Read it with: grep PERMAFROST_KEYSTORE_PASSPHRASE %s\n", envPath)
		fmt.Fprintf(out, "    BACK IT UP NOW. If you lose it, the keystore is unrecoverable.\n")
		// Reference passphraseValue so unused-var lint is happy, but
		// deliberately do NOT print it to stdout: terminal scrollback,
		// shell history, screen-share tools, and wrapping loggers all
		// capture that stream. Operators read it from the 0600 env file.
		_ = passphraseValue
	} else {
		fmt.Fprintf(out, "  ─ kept existing passphrase env at %s\n", envPath)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, v.nextSteps(envPath, callsign))
	return nil
}

// ─── prompter ──────────────────────────────────────────────────────────────

// prompter is a tiny abstraction over os.Stdin/Stdout so tests can feed
// scripted responses. We intentionally hand-roll this rather than pulling
// in a third-party prompt library — fewer deps, easier to mock, fully
// scriptable from CI without a TTY.
type prompter interface {
	Ask(prompt, defaultValue string) (string, error)
	AskYN(prompt string, defaultYes bool) (bool, error)
	AskChoice(prompt string, choices []string, defaultIdx int) (string, error)
}

// stdioPrompter reads single-line answers from stdin, writes prompts to
// stdout. Empty input accepts the default. Trailing whitespace stripped.
type stdioPrompter struct {
	r *bufio.Reader
	w io.Writer
}

func newStdioPrompter(in io.Reader, out io.Writer) *stdioPrompter {
	return &stdioPrompter{r: bufio.NewReader(in), w: out}
}

func (s *stdioPrompter) Ask(prompt, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(s.w, "  %s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(s.w, "  %s: ", prompt)
	}
	line, err := s.r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func (s *stdioPrompter) AskYN(prompt string, defaultYes bool) (bool, error) {
	def := "y"
	if !defaultYes {
		def = "n"
	}
	resp, err := s.Ask(prompt+" [y/n]", def)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(resp) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func (s *stdioPrompter) AskChoice(prompt string, choices []string, defaultIdx int) (string, error) {
	if defaultIdx < 0 || defaultIdx >= len(choices) {
		defaultIdx = 0
	}
	def := choices[defaultIdx]
	for {
		resp, err := s.Ask(prompt+" ("+strings.Join(choices, " / ")+")", def)
		if err != nil {
			return "", err
		}
		for _, c := range choices {
			if strings.EqualFold(resp, c) {
				return c, nil
			}
		}
		fmt.Fprintf(s.w, "  please choose one of: %s\n", strings.Join(choices, ", "))
	}
}

// nonInteractivePrompter always returns the default. Used by
// `permafrost init --non-interactive` for scripted installs.
type nonInteractivePrompter struct{}

func (nonInteractivePrompter) Ask(_ string, def string) (string, error) { return def, nil }
func (nonInteractivePrompter) AskYN(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}
func (nonInteractivePrompter) AskChoice(_ string, choices []string, defaultIdx int) (string, error) {
	if defaultIdx < 0 || defaultIdx >= len(choices) {
		defaultIdx = 0
	}
	return choices[defaultIdx], nil
}

// ─── side-effecting helpers ────────────────────────────────────────────────

// writeConfigIfAbsent writes a starter config.yaml if the path doesn't
// exist. Returns (wrote, err) — wrote=false means we kept what was there.
// We deliberately do NOT prompt to overwrite; idempotency means
// non-destructive on re-run.
func writeConfigIfAbsent(path string, _ prompter) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	// Use the example as the starter. We don't embed it because it
	// already lives at config.example.yaml in the repo and operators
	// can find it. Just point the user at it instead of duplicating.
	exampleSrc := "config.example.yaml"
	body, err := os.ReadFile(exampleSrc)
	if err != nil {
		// Repo layout doesn't have it (running from elsewhere). Write a
		// minimal stub so the daemon at least boots.
		body = []byte(minimalConfigYAML)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// generatePassphraseIfAbsent creates a 32-byte hex passphrase and writes
// it to envPath in a sourceable form. Returns (wrote, value, err);
// wrote=false means a passphrase was already present and we kept it.
func generatePassphraseIfAbsent(envPath string, _ prompter) (bool, string, error) {
	if _, err := os.Stat(envPath); err == nil {
		return false, "", nil
	} else if !os.IsNotExist(err) {
		return false, "", fmt.Errorf("stat %s: %w", envPath, err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return false, "", fmt.Errorf("random: %w", err)
	}
	passphrase := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		return false, "", err
	}
	body := fmt.Sprintf(`# Permafrost keystore passphrase. Source this in your shell:
#   set -a; source %s; set +a
# Or in CI / Docker, mount as an env file.
PERMAFROST_KEYSTORE_PASSPHRASE=%s
`, envPath, passphrase)
	// 0600 — readable only by the owner. The directory is 0700.
	if err := os.WriteFile(envPath, []byte(body), 0o600); err != nil {
		return false, "", err
	}
	return true, passphrase, nil
}

func defaultKeystorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".permafrost", "keystore.json"), nil
}

func defaultEnvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".permafrost", "env"), nil
}

const minimalConfigYAML = `# Permafrost minimal config (generated by 'permafrost init').
# See config.example.yaml in the repo for the full annotated reference.
env: dev
server:
  bind: 127.0.0.1:8080
logging:
  level: info
database:
  url: postgres://permafrost:permafrost@localhost:5432/permafrost?sslmode=disable
wallet:
  passphrase_env: PERMAFROST_KEYSTORE_PASSPHRASE
`

// ─── vocabulary ────────────────────────────────────────────────────────────

// vocabulary swaps the wizard's text between the arctic theme and plain
// English. The arctic theme uses Captain Pole the polar bear, penguin
// traders, etc. — see epic #30 for the full character mapping. Plain
// theme exists for users who'd rather see "agent" / "operator".
type vocabulary struct {
	arctic bool
}

func vocab(theme string) vocabulary {
	return vocabulary{arctic: theme != "plain"}
}

func (v vocabulary) banner() string {
	if v.arctic {
		return "❄ ❄   Permafrost camp setup\n──────"
	}
	return "Permafrost setup\n────────────────"
}

func (v vocabulary) callsignPrompt() string {
	if v.arctic {
		return "Captain's callsign (just for display)"
	}
	return "Operator name (just for display)"
}

func (v vocabulary) defaultCallsign() string {
	if v.arctic {
		return "Captain Pole"
	}
	return "Operator"
}

func (v vocabulary) inferencePrompt() string {
	if v.arctic {
		return "Want to set up an LLM provider so your narwhal advisors can speak?"
	}
	return "Configure an LLM provider for inference-using strategies?"
}

func (v vocabulary) done() string {
	if v.arctic {
		return "✓ Camp set up. The expedition is ready to launch."
	}
	return "✓ Setup complete."
}

func (v vocabulary) nextSteps(envPath, _ string) string {
	source := fmt.Sprintf("source %s", envPath)
	if v.arctic {
		return fmt.Sprintf(`Next steps:

  set -a; %s; set +a    # load the keystore passphrase into your shell
  make up                                         # bring up Postgres + permafrostd
  permafrost doctor                               # confirm everything is wired
  permafrost agent create --strategy noop --perp hyperliquid --alloc 1000
  permafrost agent start <id>                     # mark runnable; supervisor picks up

Or for one-shot foreground iteration: 'permafrost agent run <id>'.`, source)
	}
	return fmt.Sprintf(`Next steps:

  set -a; %s; set +a
  make up
  permafrost doctor
  permafrost agent create --strategy noop --perp hyperliquid --alloc 1000
  permafrost agent start <id>`, source)
}
