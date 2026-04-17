package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/exchange/hyperliquid"
	"github.com/teslashibe/permafrost/internal/inference"
	"github.com/teslashibe/permafrost/internal/strategy"
	fab "github.com/teslashibe/permafrost/internal/strategy/funding_arb_basic"
	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// BuildDeps assembles dependencies for a single Agent's Runtime. It is the
// canonical wiring point: both the foreground `agent run` CLI and the
// supervisor inside `serve` use this so behaviour stays identical.
//
// Inputs:
//   - a: the Agent record (from the DB). a.Network drives the HL network
//     unless opts.HyperliquidNetwork overrides it.
//   - reg: the asset registry (typically assets.LoadEmbedded()).
//   - store: the agent Store (drives persistence and openBasis hydration).
//   - keystore: optional; if set and a Hyperliquid signer is present, its
//     address is wired into the HL Venue so per-account reads work.
//   - log: scoped slog logger.
//   - opts: BuildOptions overrides (rarely needed by the daemon).
//
// Returns Deps with Strategy + Perp Venue + Store + Logger populated. SwapVenue
// and Inference Provider are intentionally left nil for v1: paper-mode swaps
// are recorded directly by the runtime; live mode requires the HL signing
// follow-up before SwapVenue makes sense as a default.
func BuildDeps(
	a Agent,
	reg assets.Registry,
	store *Store,
	keystore wallet.Keystore,
	log *slog.Logger,
	opts BuildOptions,
) (Deps, error) {
	strat, err := BuildStrategy(a, reg)
	if err != nil {
		return Deps{}, fmt.Errorf("strategy: %w", err)
	}
	// Resolve network: explicit override wins, else fall back to what the
	// agent itself records, else default to mainnet (paper-safe by design).
	network := opts.HyperliquidNetwork
	if network == "" {
		network = string(a.Network.OrDefault(NetworkMainnet))
	}
	venue, err := BuildHyperliquidVenue(keystore, BuildOptions{
		HyperliquidNetwork: network,
		HyperliquidAddress: opts.HyperliquidAddress,
	})
	if err != nil {
		return Deps{}, fmt.Errorf("hyperliquid venue: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return Deps{
		Strategy: strat,
		Perp:     venue,
		Store:    store,
		Logger:   log.With("agent_id", a.ID, "network", network, "mode", a.Mode),
	}, nil
}

// BuildOptions are caller-supplied OVERRIDES of fields the Agent already
// carries on its DB record. An empty value means "use the agent's own
// stored value". Both fields default to non-overriding empty strings.
//
// HyperliquidNetwork override is mostly useful as an emergency switch
// ("force everything to testnet for safety") or for the foreground
// `agent run` command's --network flag. The daemon supervisor leaves it
// empty so each agent runs on its own configured network.
type BuildOptions struct {
	HyperliquidNetwork string // empty → use Agent.Network
	HyperliquidAddress string // empty → derive from keystore (or no address)
}

// BuildStrategy is the strategy-construction switch shared between the
// foreground CLI and the supervisor. Currently funding_arb_basic is the
// only strategy that needs the asset registry; everything else goes through
// the generic strategy.Constructor registry.
func BuildStrategy(a Agent, reg assets.Registry) (strategy.Strategy, error) {
	switch a.Strategy {
	case fab.Name:
		cfg := fab.Config{Universe: a.Universe}
		applyFundingArbConfig(a.Config, &cfg)
		return fab.New(cfg, reg, inference.Provider(nil))
	default:
		ctor, err := strategy.Get(a.Strategy)
		if err != nil {
			return nil, err
		}
		return ctor(a.Config)
	}
}

// BuildHyperliquidVenue assembles a Hyperliquid Venue. Address resolution:
// 1) opts.HyperliquidAddress if set, else 2) keystore's HL signer if any,
// else 3) leave empty (funding-only mode).
//
// Network defaults to mainnet here as a low-level safety: callers (BuildDeps)
// are expected to plumb the agent's stored network, but if invoked directly
// without a network the safer choice is real-data paper.
func BuildHyperliquidVenue(keystore wallet.Keystore, opts BuildOptions) (exchange.Venue, error) {
	addr := opts.HyperliquidAddress
	if addr == "" && keystore != nil {
		if s, err := keystore.Signer(types.ChainHyperliquid); err == nil {
			addr = s.Address()
		}
	}
	network := opts.HyperliquidNetwork
	if network == "" {
		network = string(NetworkMainnet)
	}
	return hyperliquid.New(hyperliquid.Config{
		Network: hyperliquid.Network(network),
		Address: addr,
	})
}

// applyFundingArbConfig copies known keys from agents.config (jsonb) into
// the typed funding_arb_basic Config struct. Unknown keys are ignored.
func applyFundingArbConfig(cfg map[string]any, out *fab.Config) {
	if v, ok := cfg["entry_annualised_funding"]; ok {
		if d, err := decimalFromAny(v); err == nil {
			out.EntryAnnualisedFunding = d
		}
	}
	if v, ok := cfg["exit_annualised_funding"]; ok {
		if d, err := decimalFromAny(v); err == nil {
			out.ExitAnnualisedFunding = d
		}
	}
	if v, ok := cfg["position_cap_usdc"]; ok {
		if d, err := decimalFromAny(v); err == nil {
			out.PositionCapUSDC = d
		}
	}
	if v, ok := cfg["slippage_bps"]; ok {
		if i, err := intFromAny(v); err == nil {
			out.SlippageBps = i
		}
	}
	if v, ok := cfg["use_llm_veto"]; ok {
		if b, ok := v.(bool); ok {
			out.UseLLMVeto = b
		}
	}
	if v, ok := cfg["veto_model"]; ok {
		if s, ok := v.(string); ok {
			out.VetoModel = s
		}
	}
}

func decimalFromAny(v any) (decimal.Decimal, error) {
	switch x := v.(type) {
	case float64:
		return decimal.NewFromFloat(x), nil
	case int:
		return decimal.NewFromInt(int64(x)), nil
	case int64:
		return decimal.NewFromInt(x), nil
	case string:
		return decimal.NewFromString(x)
	}
	return decimal.Zero, fmt.Errorf("agent: unrecognised numeric type %T", v)
}

func intFromAny(v any) (int, error) {
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	}
	return 0, fmt.Errorf("agent: unrecognised int type %T", v)
}

// Loader is a small helper used by the supervisor inside `serve` to start
// every running agent on daemon boot. It is safe to call multiple times.
//
// BuildOpts is an emergency-override hatch — leave it zero in normal
// operation so each agent uses the network stored on its own record.
// Setting BuildOpts.HyperliquidNetwork forces every supervised agent to
// that network regardless of their stored value (useful for "panic
// switch all agents to testnet" scenarios).
type Loader struct {
	Store      *Store
	Registry   assets.Registry
	Keystore   wallet.Keystore
	Logger     *slog.Logger
	BuildOpts  BuildOptions
	Supervisor *Supervisor
}

// LoadAndStartRunning queries every agent with status='running' and starts
// each one via the supervisor. Errors are logged; one bad agent does not
// stop the rest. Returns the number of agents successfully started.
func (l *Loader) LoadAndStartRunning(ctx context.Context) (int, error) {
	if l.Store == nil {
		return 0, errors.New("agent: loader Store is required")
	}
	if l.Supervisor == nil {
		return 0, errors.New("agent: loader Supervisor is required")
	}
	agents, err := l.Store.List(ctx)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, a := range agents {
		if a.Status != StatusRunning {
			continue
		}
		// Re-fetch so we get the full record (Universe, Config, etc.).
		full, err := l.Store.Get(ctx, a.ID)
		if err != nil {
			l.Logger.Warn("loader: get agent failed", "agent_id", a.ID, "err", err)
			continue
		}
		deps, err := BuildDeps(full, l.Registry, l.Store, l.Keystore, l.Logger, l.BuildOpts)
		if err != nil {
			l.Logger.Error("loader: build deps failed", "agent_id", full.ID, "err", err)
			continue
		}
		rt := NewRuntime(full, deps)
		l.Supervisor.Register(full.ID, rt)
		if err := l.Supervisor.Start(ctx, full.ID); err != nil {
			l.Logger.Error("loader: start failed", "agent_id", full.ID, "err", err)
			continue
		}
		// Persistent status confirms "this agent is supposed to be running".
		// We re-set it on every boot so an admin who manually toggled it back
		// to 'running' (instead of using `agent start`) gets resumed too.
		_ = l.Store.SetStatus(ctx, full.ID, StatusRunning)
		l.Logger.Info("loader: started agent",
			"agent_id", full.ID, "strategy", full.Strategy,
			"mode", full.Mode, "tick_secs", full.TickSecs)
		started++
	}
	return started, nil
}

