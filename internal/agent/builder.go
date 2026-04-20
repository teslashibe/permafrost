package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/exchange/hyperliquid"
	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/internal/swap"
	btswap "github.com/teslashibe/permafrost/internal/swap/bittensor"
	"github.com/teslashibe/permafrost/internal/swap/jupiter"
	"github.com/teslashibe/permafrost/internal/swap/oneinch"
	"github.com/teslashibe/permafrost/internal/wallet"
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
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
	infReg *inference.Registry,
	log *slog.Logger,
	opts BuildOptions,
) (Deps, error) {
	strat, err := BuildStrategy(a)
	if err != nil {
		return Deps{}, fmt.Errorf("strategy: %w", err)
	}
	// Resolve the inference provider for this agent. agent.Inference is
	// a "provider:model" string; we look up the provider name in the
	// global registry and surface both the resolved Provider and the
	// model id to strategies via Services.
	infProv, infModel, err := resolveInference(a.Inference, infReg)
	if err != nil {
		return Deps{}, fmt.Errorf("inference: %w", err)
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

	// Build per-chain SwapVenues. A chain is wired iff:
	//   1) the operator supplied the chain config (RPC URL etc.), AND
	//   2) the keystore has the appropriate signer.
	// Missing chains downgrade to paper-spot bookkeeping (the runtime
	// records the swap intent but doesn't broadcast).
	swaps := map[types.ChainID]swap.SwapVenue{}

	if opts.Solana.IsEnabled() {
		v, err := BuildSolanaSwapVenue(opts.Solana, keystore)
		switch {
		case err == nil:
			swaps[types.ChainSolana] = v
		case errors.Is(err, ErrNoSolanaSigner):
			log.Warn("solana swap disabled: no Solana signer in keystore",
				"agent_id", a.ID)
		default:
			return Deps{}, fmt.Errorf("solana swap venue: %w", err)
		}
	}

	if opts.Bittensor.IsEnabled() {
		v, err := BuildBittensorSwapVenue(opts.Bittensor, keystore)
		switch {
		case err == nil:
			swaps[types.ChainBittensor] = v
		case errors.Is(err, ErrNoBittensorSigner):
			log.Warn("bittensor swap disabled: no Bittensor signer in keystore",
				"agent_id", a.ID)
		default:
			return Deps{}, fmt.Errorf("bittensor swap venue: %w", err)
		}
	}

	for chainID, ec := range opts.EVM {
		if !ec.IsEnabled() {
			continue
		}
		v, err := BuildEVMSwapVenue(chainID, ec, keystore)
		switch {
		case err == nil:
			swaps[chainID] = v
		case errors.Is(err, wallet.ErrNoEVMSigner):
			log.Warn("evm swap disabled: no EVM-capable signer in keystore",
				"agent_id", a.ID, "chain", chainID)
		default:
			return Deps{}, fmt.Errorf("evm swap venue %s: %w", chainID, err)
		}
	}

	return Deps{
		Strategy:       strat,
		Perp:           venue,
		Swaps:          swaps,
		Inference:      infProv,
		InferenceModel: infModel,
		Registry:       reg,
		Risk:           BuildRiskEngine(a),
		Store:          store,
		Logger:         log.With("agent_id", a.ID, "network", network, "mode", a.Mode),
	}, nil
}

// resolveInference parses the "provider:model" string stored on the agent
// and looks the provider name up in the supplied registry. Returns
// (nil, "", nil) when the agent has no inference configured (empty
// string) — that's a normal state for strategies that don't need it.
// A configured-but-unresolvable provider is an error so the operator
// notices at agent-launch time instead of on the first decision tick.
//
// Spec for agent.Inference:
//
//	""                                  → no inference for this agent
//	"openrouter"                        → provider=openrouter, model=""
//	"openrouter:claude-sonnet-4.5"      → provider=openrouter, model="claude-sonnet-4.5"
//	"openrouter:vendor/model:variant"   → provider=openrouter, model="vendor/model:variant"
//	                                      (only the first ":" is the separator)
func resolveInference(spec string, reg *inference.Registry) (inference.Provider, string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, "", nil
	}
	provName, model, _ := strings.Cut(spec, ":")
	provName = strings.TrimSpace(provName)
	model = strings.TrimSpace(model)
	if provName == "" {
		return nil, "", fmt.Errorf("agent.inference %q has empty provider name", spec)
	}
	if reg == nil {
		// No registry was supplied (e.g. the operator didn't configure
		// any inference providers in config.yaml). Surface a clear
		// error rather than silently dropping the provider.
		return nil, "", fmt.Errorf("agent has inference=%q but no inference providers are configured in config.yaml", spec)
	}
	prov, err := reg.Get(provName)
	if err != nil {
		return nil, "", fmt.Errorf("provider %q (from agent.inference=%q): %w", provName, spec, err)
	}
	return prov, model, nil
}

// BuildRiskEngine constructs a risk.Policy from the agent's persisted
// risk-limit overrides (agent.Config["risk"] keys). Sensible defaults
// fill in anything the operator omitted; passing zero everywhere
// effectively disables checks (cap=0, exposure=0, etc.) — explicit opt-in.
//
// Recognised keys under agent.Config["risk"] (all optional):
//
//	max_concurrent_positions: int
//	max_notional_per_leg:     decimal/string/number (USDC)
//	max_total_basis_exposure: decimal/string/number (USDC)
//	max_daily_loss:           decimal/string/number (USDC)
//	max_spot_slippage_bps:    int
//	max_drawdown:             decimal/string/number (fraction, e.g. 0.10 = 10%)
//
// The drawdown breaker is always installed; max_drawdown=0 disables it.
// The daily-loss breaker is always installed; max_daily_loss=0 disables it.
func BuildRiskEngine(a Agent) *risk.Policy {
	limits := types.RiskLimits{}
	maxDrawdown := decimal.Zero

	if rawAny, ok := a.Config["risk"]; ok {
		if rawMap, ok := rawAny.(map[string]any); ok {
			if v, ok := rawMap["max_concurrent_positions"]; ok {
				if i, err := intFromAny(v); err == nil {
					limits.MaxConcurrentPositions = i
				}
			}
			if v, ok := rawMap["max_notional_per_leg"]; ok {
				if d, err := decimalFromAny(v); err == nil {
					limits.MaxNotionalPerLeg = d
				}
			}
			if v, ok := rawMap["max_total_basis_exposure"]; ok {
				if d, err := decimalFromAny(v); err == nil {
					limits.MaxTotalBasisExposure = d
				}
			}
			if v, ok := rawMap["max_daily_loss"]; ok {
				if d, err := decimalFromAny(v); err == nil {
					limits.MaxDailyLoss = d
				}
			}
			if v, ok := rawMap["max_spot_slippage_bps"]; ok {
				if i, err := intFromAny(v); err == nil {
					limits.MaxSpotSlippageBps = i
				}
			}
			if v, ok := rawMap["max_drawdown"]; ok {
				if d, err := decimalFromAny(v); err == nil {
					maxDrawdown = d
				}
			}
		}
	}

	return risk.NewPolicy(limits,
		risk.MaxDrawdownBreaker{MaxFraction: maxDrawdown},
		risk.DailyLossBreaker{},
	)
}

// ErrNoSolanaSigner is returned by BuildSolanaSwapVenue when the
// supplied keystore doesn't contain a Solana key. The caller decides
// whether to degrade (paper-spot) or fail (live-mode required).
var ErrNoSolanaSigner = errors.New("agent: no Solana signer in keystore")

// BuildSolanaSwapVenue constructs a Jupiter-backed SwapVenue from
// SolanaSpot config + a keystore that holds a Solana signer.
func BuildSolanaSwapVenue(cfg SolanaSpot, keystore wallet.Keystore) (swap.SwapVenue, error) {
	if !cfg.IsEnabled() {
		return nil, errors.New("agent: SolanaSpot is not configured")
	}
	if keystore == nil {
		return nil, ErrNoSolanaSigner
	}
	signer, err := keystore.Signer(types.ChainSolana)
	if err != nil {
		return nil, ErrNoSolanaSigner
	}
	mode := jupiter.SubmitMode(cfg.SubmitMode)
	if mode == "" {
		mode = jupiter.SubmitJito
	}
	return jupiter.New(jupiter.Config{
		RPCURL:                   cfg.RPCURL,
		JupiterAPIKey:            cfg.JupiterAPIKey,
		JupiterBaseURL:           cfg.JupiterBaseURL,
		Mode:                     mode,
		JitoBundleURL:            cfg.JitoBundleURL,
		PriorityFeeMicroLamports: cfg.PriorityFeeMicroLamports,
		PollTimeout:              time.Duration(cfg.ConfirmationTimeoutSecs) * time.Second,
	}, signer)
}

// BuildOptions are caller-supplied OVERRIDES of fields the Agent already
// carries on its DB record. An empty value means "use the agent's own
// stored value". Both Hyperliquid fields default to non-overriding
// empty strings.
//
// HyperliquidNetwork override is mostly useful as an emergency switch
// ("force everything to testnet for safety") or for the foreground
// `agent run` command's --network flag. The daemon supervisor leaves it
// empty so each agent runs on its own configured network.
//
// Solana enables the live spot leg on Solana via Jupiter.
// EVM enables live spot legs on EVM chains via 1inch — keyed by
// ChainEthereum / ChainBase / ChainAvalanche / ChainBSC.
//
// When a chain's config is missing or its IsEnabled() is false the
// Runtime falls back to paper-spot for that chain (records intent,
// never quotes/broadcasts).
type BuildOptions struct {
	HyperliquidNetwork string // empty → use Agent.Network
	HyperliquidAddress string // empty → derive from keystore (or no address)
	Solana             SolanaSpot
	EVM                map[types.ChainID]EVMSpot
	Bittensor          BittensorSpot
}

// SolanaSpot captures everything the Jupiter SwapVenue needs to settle
// real spot legs on Solana. Mirrors config.SolanaConfig but lives in the
// agent package so the builder doesn't import the cli/config layer.
type SolanaSpot struct {
	RPCURL                   string
	JupiterBaseURL           string
	JupiterAPIKey            string
	SubmitMode               string // "jito" | "rpc"; "" => jito
	JitoBundleURL            string
	PriorityFeeMicroLamports uint64
	ConfirmationTimeoutSecs  int
}

// IsEnabled reports whether the operator has provided enough config for
// the SwapVenue to be constructed. RPCURL is the bare minimum.
func (s SolanaSpot) IsEnabled() bool { return s.RPCURL != "" }

// EVMSpot captures one EVM chain's swap configuration: an RPC endpoint
// + 1inch API key. The signer is sourced from the keystore (the same
// secp256k1 key as Hyperliquid).
type EVMSpot struct {
	RPCURL                  string
	OneInchAPIKey           string
	OneInchBaseURL          string // optional override (proxy)
	DefaultSlippageBps      int    // default slippage if SwapIntent doesn't set one (0 ⇒ 50)
	ConfirmationTimeoutSecs int    // 0 ⇒ 90s
}

// IsEnabled reports whether the operator has provided enough config for
// the EVM SwapVenue to be constructed.
func (e EVMSpot) IsEnabled() bool { return e.RPCURL != "" && e.OneInchAPIKey != "" }

// BuildEVMSwapVenue constructs a 1inch-backed SwapVenue for one EVM
// chain. Returns wallet.ErrNoEVMSigner if the keystore has no
// EVM-capable key.
func BuildEVMSwapVenue(chain types.ChainID, cfg EVMSpot, keystore wallet.Keystore) (swap.SwapVenue, error) {
	if !cfg.IsEnabled() {
		return nil, fmt.Errorf("agent: EVMSpot for %s is not configured", chain)
	}
	signer, err := wallet.EVMSignerFromKeystore(keystore, chain)
	if err != nil {
		return nil, err
	}
	return oneinch.New(oneinch.Config{
		OneInchAPIKey:   cfg.OneInchAPIKey,
		OneInchBaseURL:  cfg.OneInchBaseURL,
		RPCURL:          cfg.RPCURL,
		Chain:           chain,
		DefaultSlippage: cfg.DefaultSlippageBps,
		PollTimeout:     time.Duration(cfg.ConfirmationTimeoutSecs) * time.Second,
	}, signer)
}

// BittensorSpot captures the Subtensor RPC config needed by the
// Bittensor SwapVenue.
type BittensorSpot struct {
	RPCURL string
}

// IsEnabled reports whether the operator has provided enough config for
// the Bittensor SwapVenue.
func (b BittensorSpot) IsEnabled() bool { return b.RPCURL != "" }

// ErrNoBittensorSigner is returned when the keystore has no Bittensor key.
var ErrNoBittensorSigner = errors.New("agent: no Bittensor signer in keystore")

// BuildBittensorSwapVenue constructs a Bittensor SwapVenue backed by the
// on-chain AMM. Requires a Bittensor signer in the keystore.
func BuildBittensorSwapVenue(cfg BittensorSpot, keystore wallet.Keystore) (swap.SwapVenue, error) {
	if !cfg.IsEnabled() {
		return nil, errors.New("agent: BittensorSpot is not configured")
	}
	if keystore == nil {
		return nil, ErrNoBittensorSigner
	}
	signer, err := keystore.Signer(types.ChainBittensor)
	if err != nil {
		return nil, ErrNoBittensorSigner
	}
	return btswap.New(btswap.Config{
		RPCURL: cfg.RPCURL,
	}, signer)
}

// BuildStrategy constructs the strategy by name from the registry. It is a
// pure registry lookup with no special-casing per strategy: each strategy
// owns its own typed config parsing inside its Constructor and pulls
// framework services (asset registry, inference, etc.) from WarmupInput
// later. Adding a new strategy never requires editing this function.
func BuildStrategy(a Agent) (strategy.Strategy, error) {
	ctor, err := strategy.Get(a.Strategy)
	if err != nil {
		return nil, err
	}
	return ctor(a.Config)
}

// decimalFromAny converts a JSONB-decoded numeric value into a decimal.
// Used by BuildRiskEngine and any other framework-side config parsing.
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

// intFromAny converts a JSONB-decoded numeric value into an int.
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

// BuildHyperliquidVenue assembles a Hyperliquid Venue. Address resolution:
// 1) opts.HyperliquidAddress if set, else 2) keystore's HL signer if any,
// else 3) leave empty (funding-only mode).
//
// If the keystore yields a *wallet.HyperliquidSigner the venue is wired
// for signed writes (Place / Cancel) too — otherwise it is read-only.
//
// Network defaults to mainnet here as a low-level safety: callers (BuildDeps)
// are expected to plumb the agent's stored network, but if invoked directly
// without a network the safer choice is real-data paper.
func BuildHyperliquidVenue(keystore wallet.Keystore, opts BuildOptions) (exchange.Venue, error) {
	addr := opts.HyperliquidAddress
	var hlSigner wallet.Signer
	if keystore != nil {
		if s, err := keystore.Signer(types.ChainHyperliquid); err == nil {
			hlSigner = s
			if addr == "" {
				addr = s.Address()
			}
		}
	}
	network := opts.HyperliquidNetwork
	if network == "" {
		network = string(NetworkMainnet)
	}
	venueOpts := []hyperliquid.Option{}
	if hlSigner != nil {
		venueOpts = append(venueOpts, hyperliquid.WithSigner(hlSigner))
	}
	return hyperliquid.New(hyperliquid.Config{
		Network: hyperliquid.Network(network),
		Address: addr,
	}, venueOpts...)
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
	Inference  *inference.Registry
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
		deps, err := BuildDeps(full, l.Registry, l.Store, l.Keystore, l.Inference, l.Logger, l.BuildOpts)
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

