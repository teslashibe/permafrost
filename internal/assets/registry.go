// Package assets owns the curated asset registry that maps a token to its
// Hyperliquid perp symbol and on-chain spot mint(s). It is the source of
// truth for what basis-style (delta-neutral) strategies may trade.
//
// The registry lives in registry.yaml. It is embedded into the binary so a
// fresh deployment can boot without an external file; operators may also
// supply a custom path via Load(path).
package assets

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/teslashibe/permafrost/pkg/types"
)

// Asset is one fully-resolved registry entry.
type Asset struct {
	Symbol     string
	Perp       *PerpRef
	Spot       SpotRef
	HedgeRatio float64
	Notes      string
}

// PerpRef points at a Hyperliquid perp symbol. nil for assets that are
// quote-only (e.g. USDC).
type PerpRef struct {
	Venue  string
	Symbol string
}

// SpotRef points at a chain-bound mint.
type SpotRef struct {
	Chain    types.ChainID
	Mint     string
	Decimals int32
}

// Tradable reports whether the asset has both legs (perp + spot) configured
// and is therefore eligible for basis trading.
func (a Asset) Tradable() bool {
	return a.Perp != nil && a.Spot.Mint != ""
}

// AsAsset returns the Spot leg as a types.Asset for use with SwapVenue.
func (a Asset) AsAsset() types.Asset {
	return types.Asset{
		Symbol:   a.Symbol,
		Chain:    a.Spot.Chain,
		Mint:     a.Spot.Mint,
		Decimals: a.Spot.Decimals,
	}
}

// Registry holds the parsed asset list. Safe to share by value (read-only).
type Registry struct {
	Version int
	Defaults Defaults
	Assets  []Asset

	bySymbol map[string]Asset
}

// Defaults are global defaults applied when an asset entry omits a field.
type Defaults struct {
	HedgeRatio float64
}

// Get returns the asset for a symbol, or false if not present.
func (r Registry) Get(symbol string) (Asset, bool) {
	a, ok := r.bySymbol[strings.ToUpper(symbol)]
	return a, ok
}

// Tradable returns the subset of assets that have both perp and spot legs
// configured (the universe a basis-style strategy may consider).
func (r Registry) Tradable() []Asset {
	out := make([]Asset, 0, len(r.Assets))
	for _, a := range r.Assets {
		if a.Tradable() {
			out = append(out, a)
		}
	}
	return out
}

// Symbols returns all symbols (sorted).
func (r Registry) Symbols() []string {
	out := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		out = append(out, a.Symbol)
	}
	sort.Strings(out)
	return out
}

// ─── loading ────────────────────────────────────────────────────────────────

// rawFile mirrors the YAML schema 1:1.
type rawFile struct {
	Version  int      `yaml:"version"`
	Defaults rawDefaults `yaml:"defaults"`
	Assets   []rawAsset  `yaml:"assets"`
}

type rawDefaults struct {
	HedgeRatio float64 `yaml:"hedge_ratio"`
}

type rawAsset struct {
	Symbol     string  `yaml:"symbol"`
	HedgeRatio float64 `yaml:"hedge_ratio"`
	Notes      string  `yaml:"notes"`
	Perp       *struct {
		Venue  string `yaml:"venue"`
		Symbol string `yaml:"symbol"`
	} `yaml:"perp,omitempty"`
	Spot struct {
		Chain    string `yaml:"chain"`
		Mint     string `yaml:"mint"`
		Decimals int32  `yaml:"decimals"`
	} `yaml:"spot"`
}

// Embedded copy of the curated registry shipped with the binary.
//
//go:generate go run ./internal/assets/cmd/embed
//
//revive:disable-next-line:line-length-limit
var Embedded = mustEmbed()

// LoadEmbedded returns the embedded registry.
func LoadEmbedded() (Registry, error) { return Embedded, nil }

// Load reads a registry from the given filesystem path.
func Load(path string) (Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Registry{}, fmt.Errorf("assets: open %s: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a registry from any YAML reader.
func Parse(r io.Reader) (Registry, error) {
	var raw rawFile
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return Registry{}, fmt.Errorf("assets: parse: %w", err)
	}
	return validate(raw)
}

func validate(raw rawFile) (Registry, error) {
	if raw.Version != 1 {
		return Registry{}, fmt.Errorf("assets: unsupported version %d (want 1)", raw.Version)
	}
	out := Registry{
		Version: raw.Version,
		Defaults: Defaults{HedgeRatio: raw.Defaults.HedgeRatio},
		bySymbol: make(map[string]Asset, len(raw.Assets)),
	}
	if out.Defaults.HedgeRatio == 0 {
		out.Defaults.HedgeRatio = 1.0
	}
	for i, ra := range raw.Assets {
		if ra.Symbol == "" {
			return Registry{}, fmt.Errorf("assets[%d]: missing symbol", i)
		}
		key := strings.ToUpper(ra.Symbol)
		if _, dup := out.bySymbol[key]; dup {
			return Registry{}, fmt.Errorf("assets[%d]: duplicate symbol %q", i, ra.Symbol)
		}
		if ra.Spot.Mint == "" {
			return Registry{}, fmt.Errorf("assets[%s]: missing spot.mint", ra.Symbol)
		}
		if ra.Spot.Chain == "" {
			return Registry{}, fmt.Errorf("assets[%s]: missing spot.chain", ra.Symbol)
		}
		if ra.Spot.Decimals < 0 || ra.Spot.Decimals > 30 {
			return Registry{}, fmt.Errorf("assets[%s]: spot.decimals out of range: %d", ra.Symbol, ra.Spot.Decimals)
		}
		hr := ra.HedgeRatio
		if hr == 0 {
			hr = out.Defaults.HedgeRatio
		}
		var perp *PerpRef
		if ra.Perp != nil {
			if ra.Perp.Venue == "" || ra.Perp.Symbol == "" {
				return Registry{}, fmt.Errorf("assets[%s]: perp requires venue + symbol", ra.Symbol)
			}
			perp = &PerpRef{Venue: ra.Perp.Venue, Symbol: ra.Perp.Symbol}
		}
		a := Asset{
			Symbol:     ra.Symbol,
			Perp:       perp,
			Spot:       SpotRef{Chain: types.ChainID(ra.Spot.Chain), Mint: ra.Spot.Mint, Decimals: ra.Spot.Decimals},
			HedgeRatio: hr,
			Notes:      ra.Notes,
		}
		out.Assets = append(out.Assets, a)
		out.bySymbol[key] = a
	}
	if len(out.Assets) == 0 {
		return Registry{}, errors.New("assets: registry is empty")
	}
	return out, nil
}
