package cli

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/pkg/strategy"
)

// strategy new scaffolds a strategy package from an embedded template,
// updates the appropriate cmd/permafrost(d)/strategies{,_local}.go file
// with the import line, and prints next steps.
//
// The metaphor (per epic #30): adding a new penguin trader to the camp
// roster — name them, assign their training programme (template), they
// show up on the ice ready to play.

//go:embed templates/strategy/*/*.tmpl
var strategyTemplates embed.FS

func init() {
	// Hook into the existing `strategy` subcommand factory by appending
	// our own command at the cobra-tree-build time. The simplest way to
	// avoid editing strategy.go from a different chain branch is to
	// hijack the addCommandFactory path with our own group. So we
	// register a separate `strategy-new` command for now and the user
	// invokes it as `permafrost strategy new`. Cobra accepts both
	// `strategy-new` (top-level) and `strategy new` (nested) — we wire
	// the nested form by inserting into newStrategyCmd if reachable;
	// fall back to top-level if the strategy command doesn't expose a
	// way to extend.
	addCommandFactory(newStrategyNewTopLevelCmd)
}

// newStrategyNewTopLevelCmd registers `permafrost strategy-new <name>`.
// Cobra users typically prefer `permafrost strategy new <name>`; both
// work because we also wire it into the strategy subcommand below.
// Top-level command is the simpler entrypoint that doesn't require
// editing chain/02's strategy.go.
func newStrategyNewTopLevelCmd() *cobra.Command {
	return newStrategyNewCmd("strategy-new")
}

// newStrategyNewCmd builds the actual cobra command. Use is parameterised
// so the same builder can be wired in both as a top-level alias and as
// a `strategy new` subcommand if/when the strategy command is extended
// from this PR.
func newStrategyNewCmd(use string) *cobra.Command {
	var (
		template string
		private  bool
	)
	cmd := &cobra.Command{
		Use:     use + " <name>",
		Aliases: []string{"new"},
		Short:   "Scaffold a new strategy package and register it in the build",
		Long: `Generate a strategy package from an embedded template, update the
cmd/permafrost(d)/strategies{,_local}.go import lists, and print next
steps.

  permafrost strategy-new my_strategy
  permafrost strategy-new my_secret --private
  permafrost strategy-new my_basis --template basis     # planned (not yet shipped)

Templates:
  noop   (default) — minimal stub satisfying the SAPI; copy this and
                     fill in Decide() with your logic.
  basis  TODO       — paired SwapIntent + OrderIntent skeleton (see
                     strategies/private/funding_arb_basic for a complete
                     example until the template lands).
  maker  TODO       — perp-only OrderIntent skeleton with optional LLM
                     veto path (see strategies/market_maker_basic).
  dca    TODO       — SwapIntent-only DCA skeleton (see strategies/dca_buy).

The strategy is registered in BOTH binaries so it's runnable AND
backtest-able. With --private, the directory and import lines go to
their gitignored counterparts.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runStrategyNew(c.OutOrStdout(), args[0], template, private)
		},
	}
	cmd.Flags().StringVar(&template, "template", "noop", "template to use (noop | basis | maker | dca)")
	cmd.Flags().BoolVar(&private, "private", false, "scaffold under strategies/private/ and register in *_local.go (gitignored)")
	return cmd
}

// runStrategyNew is the body, separated so tests can call it directly
// against a tmpdir.
func runStrategyNew(out interface{ Write([]byte) (int, error) }, name, tpl string, private bool) error {
	if err := validateStrategyName(name); err != nil {
		return err
	}
	// Registry collision check FIRST — fail fast before we touch the
	// filesystem so a refused-because-collision name doesn't leave a
	// stray (empty-then-rolled-back) directory.
	if _, err := strategy.Get(name); err == nil {
		return fmt.Errorf("strategy %q is already registered (collision with a built-in or another local strategy)", name)
	}
	if tpl != "noop" {
		// basis/maker/dca templates are TODO: ship as a small follow-up
		// once we have time to ship them as real templates rather than
		// half-finished placeholders.
		return fmt.Errorf("template %q is not shipped yet (only 'noop' available in v1; see strategies/{dca_buy,market_maker_basic,private/funding_arb_basic} for full examples)", tpl)
	}

	// Resolve target paths.
	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	subdir := filepath.Join("strategies", name)
	if private {
		subdir = filepath.Join("strategies", "private", name)
	}
	targetDir := filepath.Join(repoRoot, subdir)

	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("strategy directory already exists at %s", targetDir)
	}

	// Render the template files.
	data := templateData{
		Name:        name,
		PackageName: name, // snake_case validates against Go package rules
		Path:        subdir,
	}
	if err := renderStrategyTemplates(tpl, targetDir, data); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	// Update the import files (for both binaries).
	files := []importTarget{
		{Path: filepath.Join("cmd", "permafrost", "strategies.go"), Local: false},
		{Path: filepath.Join("cmd", "permafrostd", "strategies.go"), Local: false},
	}
	if private {
		files = []importTarget{
			{Path: filepath.Join("cmd", "permafrost", "strategies_local.go"), Local: true},
			{Path: filepath.Join("cmd", "permafrostd", "strategies_local.go"), Local: true},
		}
	}
	importLine := fmt.Sprintf(`	_ "github.com/teslashibe/permafrost/%s"`, strings.ReplaceAll(subdir, string(filepath.Separator), "/"))
	for _, f := range files {
		if err := appendImport(f, importLine); err != nil {
			return fmt.Errorf("update %s: %w", f.Path, err)
		}
	}

	// Report.
	fmt.Fprintf(out, "✓ Created %s/ (template=%s)\n", subdir, tpl)
	for _, f := range files {
		fmt.Fprintf(out, "✓ Registered in %s\n", f.Path)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  go build ./...")
	fmt.Fprintf(out, "  permafrost agent create --strategy %s --perp hyperliquid --alloc 100\n", name)
	fmt.Fprintln(out, "  permafrost agent run <id>      # foreground iteration; SIGINT to stop")
	return nil
}

// validateStrategyName enforces snake_case. Permafrost's existing
// registered names (noop, dca_buy, market_maker_basic, funding_arb_basic)
// all match this pattern. Restricting here surfaces a clear error
// instead of a confusing Go package-name compile error later.
var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateStrategyName(name string) error {
	if !snakeCase.MatchString(name) {
		return fmt.Errorf("strategy name %q must be lowercase snake_case (e.g. my_strategy)", name)
	}
	if len(name) > 64 {
		return fmt.Errorf("strategy name too long (max 64 chars): %q", name)
	}
	return nil
}

// templateData is the {{.Name}} / {{.PackageName}} substitution context
// passed into the embedded text/template files.
type templateData struct {
	Name        string // snake_case identifier
	PackageName string // Go package name (same as Name; explicit for clarity)
	Path        string // strategies/<name> or strategies/private/<name>
}

// renderStrategyTemplates walks the embedded templates/strategy/<tpl>/
// directory and writes each file to targetDir, stripping the .tmpl
// suffix and substituting templateData.
func renderStrategyTemplates(tpl, targetDir string, data templateData) error {
	root := "templates/strategy/" + tpl
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(strategyTemplates, root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		body, err := strategyTemplates.ReadFile(path)
		if err != nil {
			return err
		}
		t, err := template.New(path).Parse(string(body))
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		var rendered bytes.Buffer
		if err := t.Execute(&rendered, data); err != nil {
			return fmt.Errorf("execute %s: %w", path, err)
		}
		// Strip the .tmpl suffix so we end up with strategy.go, not
		// strategy.go.tmpl.
		name := strings.TrimSuffix(filepath.Base(path), ".tmpl")
		return os.WriteFile(filepath.Join(targetDir, name), rendered.Bytes(), 0o644)
	})
}

// importTarget locates a file we should append a blank import to.
// Local=true means the file lives in a gitignored *_local.go and may
// not exist yet (we create it with a package main + import header).
type importTarget struct {
	Path  string
	Local bool
}

// appendImport adds a blank-import line to a strategies(_local).go file.
// Idempotent: if the line is already present, it's a no-op. If the
// file doesn't exist (only valid for Local=true), it's created with
// the right header. Parent directory is created if missing for the
// Local case (committed files' parents are guaranteed to exist).
func appendImport(t importTarget, importLine string) error {
	body, err := os.ReadFile(t.Path)
	if errors.Is(err, fs.ErrNotExist) {
		if !t.Local {
			return fmt.Errorf("expected %s to exist (committed file)", t.Path)
		}
		// Local *_local.go files are operator-managed and may not yet
		// exist on a fresh repo. Create the parent (cmd/permafrost(d))
		// just in case the test/sandbox environment is unusual.
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			return err
		}
		body = []byte(`package main

// This file is gitignored. Add private strategy blank-imports here.
// Each entry's init() runs at binary startup and registers the
// strategy with the framework registry.

import (
)
`)
	} else if err != nil {
		return err
	}

	if bytes.Contains(body, []byte(importLine)) {
		return nil // idempotent
	}

	// Insert the line before the closing ")" of the import block.
	closeIdx := bytes.LastIndex(body, []byte("\n)"))
	if closeIdx < 0 {
		return fmt.Errorf("could not find import block close in %s; please add manually", t.Path)
	}
	updated := append(append(append([]byte{}, body[:closeIdx]...), '\n'), importLine...)
	updated = append(updated, body[closeIdx:]...)

	return os.WriteFile(t.Path, updated, 0o644)
}
