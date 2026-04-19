package jupiter

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	solanago "github.com/gagliardetto/solana-go"
	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/chain/solana"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// SubmitMode controls how signed transactions are submitted on-chain.
type SubmitMode string

const (
	// SubmitJito sends signed transactions as a Jito bundle.
	SubmitJito SubmitMode = "jito"
	// SubmitRPC sends signed transactions via standard sendTransaction.
	SubmitRPC SubmitMode = "rpc"
)

// VenueName is the registered identifier of this SwapVenue.
const VenueName = "jupiter"

// Venue implements swap.SwapVenue against Jupiter on Solana, signing with
// the supplied wallet.Signer (must be a *wallet.SolanaSigner) and submitting
// via either Jito bundles or plain sendTransaction.
type Venue struct {
	jup        *Client
	rpc        *solana.Client
	jito       *solana.JitoClient
	signer     wallet.Signer
	mode       SubmitMode
	priorityFee uint64 // microLamports per CU; 0 disables
	pollTimeout time.Duration
}

// Config configures a Venue.
type Config struct {
	JupiterAPIKey       string        // optional
	JupiterBaseURL      string        // optional override
	RPCURL              string        // required (Helius, Triton, public, ...)
	JitoBundleURL       string        // required if Mode == SubmitJito
	Mode                SubmitMode    // defaults to SubmitJito
	PriorityFeeMicroLamports uint64   // optional priority-fee budget
	PollTimeout         time.Duration // confirmation poll budget; default 90s
}

// New constructs a Venue.
func New(cfg Config, signer wallet.Signer) (*Venue, error) {
	if signer == nil {
		return nil, errors.New("jupiter: signer is required")
	}
	if signer.Chain() != types.ChainSolana {
		return nil, fmt.Errorf("jupiter: signer chain must be solana, got %q", signer.Chain())
	}
	if cfg.RPCURL == "" {
		return nil, errors.New("jupiter: RPCURL is required")
	}
	mode := cfg.Mode
	if mode == "" {
		mode = SubmitJito
	}
	if mode == SubmitJito && cfg.JitoBundleURL == "" {
		return nil, errors.New("jupiter: JitoBundleURL is required when Mode=jito")
	}
	pollTO := cfg.PollTimeout
	if pollTO == 0 {
		pollTO = 90 * time.Second
	}

	v := &Venue{
		jup:        NewClient(WithBaseURL(cfg.JupiterBaseURL), WithAPIKey(cfg.JupiterAPIKey)),
		rpc:        solana.NewClient(cfg.RPCURL),
		signer:     signer,
		mode:       mode,
		priorityFee: cfg.PriorityFeeMicroLamports,
		pollTimeout: pollTO,
	}
	if mode == SubmitJito {
		v.jito = solana.NewJitoClient(cfg.JitoBundleURL)
	}
	return v, nil
}

// Compile-time check.
var _ swap.SwapVenue = (*Venue)(nil)

func (v *Venue) Name() string         { return VenueName }
func (v *Venue) Chain() types.ChainID { return types.ChainSolana }

// Quote translates types.QuoteRequest into a Jupiter quote and back.
func (v *Venue) Quote(ctx context.Context, req types.QuoteRequest) (types.Quote, error) {
	amount := scaleAmount(req.Amount, req.InToken.Decimals)
	if req.Mode == types.QuoteExactOut {
		amount = scaleAmount(req.Amount, req.OutToken.Decimals)
	}
	mode := "ExactIn"
	if req.Mode == types.QuoteExactOut {
		mode = "ExactOut"
	}
	q, err := v.jup.Quote(ctx, QuoteParams{
		InputMint:   req.InToken.Mint,
		OutputMint:  req.OutToken.Mint,
		Amount:      amount,
		SlippageBps: 50, // a sensible default; venue accepts caller override via Swap.slippageBps
		SwapMode:    mode,
	})
	if err != nil {
		return types.Quote{}, err
	}
	in := unscaleAmount(parseUint(q.InAmount), req.InToken.Decimals)
	out := unscaleAmount(parseUint(q.OutAmount), req.OutToken.Decimals)
	priceImpact, _ := decimal.NewFromString(strings.TrimSpace(q.PriceImpactPct))
	return types.Quote{
		InToken:     req.InToken,
		OutToken:    req.OutToken,
		InAmount:    in,
		OutAmount:   out,
		PriceImpact: priceImpact,
		Route:       VenueName,
		ExpiresAt:   time.Now().Add(20 * time.Second).UTC(),
		Raw:         q.RouteJSON,
	}, nil
}

// Swap requests a transaction from Jupiter, signs it, and submits via the
// configured route. Returns the on-chain signature.
func (v *Venue) Swap(ctx context.Context, q types.Quote, slippageBps int) (types.TxHash, error) {
	if time.Now().After(q.ExpiresAt) {
		return "", swap.ErrQuoteExpired
	}
	_ = slippageBps // slippage was baked into the quote on the Jupiter side

	sw, err := v.jup.Swap(ctx, SwapParams{
		QuoteResponse:                 q.Raw,
		UserPublicKey:                 v.signer.Address(),
		WrapAndUnwrapSOL:              true,
		DynamicComputeUnitLimit:       true,
		ComputeUnitPriceMicroLamports: priorityFeePtr(v.priorityFee),
	})
	if err != nil {
		return "", err
	}

	signed, err := signSerializedTransaction(sw.SwapTransaction, v.signer)
	if err != nil {
		return "", err
	}
	signedB64 := base64.StdEncoding.EncodeToString(signed)

	switch v.mode {
	case SubmitJito:
		bundleID, err := v.jito.SendBundle(ctx, []string{signedB64})
		if err != nil {
			return "", fmt.Errorf("jupiter: jito send: %w", err)
		}
		// Bundle UUID is informational; the tx signature is what we use to
		// poll. Recover it from the signed bytes.
		sig, err := firstSignature(signed)
		if err != nil {
			return "", err
		}
		_ = bundleID
		return types.TxHash(sig), nil
	case SubmitRPC:
		sig, err := v.rpc.SendTransaction(ctx, signedB64, false)
		if err != nil {
			return "", err
		}
		return types.TxHash(sig), nil
	default:
		return "", fmt.Errorf("jupiter: unknown submit mode %q", v.mode)
	}
}

// WaitConfirm polls getSignatureStatuses until the tx reaches confirmed
// status (or fails or the deadline is reached).
func (v *Venue) WaitConfirm(ctx context.Context, tx types.TxHash) (types.SwapResult, error) {
	deadline := time.Now().Add(v.pollTimeout)
	tick := time.NewTicker(800 * time.Millisecond)
	defer tick.Stop()

	for {
		statuses, err := v.rpc.GetSignatureStatuses(ctx, []string{string(tx)}, true)
		if err == nil && len(statuses) == 1 && statuses[0] != nil {
			s := statuses[0]
			if len(s.Err) > 0 && string(s.Err) != "null" {
				return types.SwapResult{
					TxHash: tx, Status: types.SwapStatusFailed, Slot: s.Slot,
					Error: string(s.Err),
				}, nil
			}
			switch s.ConfirmationStatus {
			case "confirmed", "finalized":
				return types.SwapResult{
					TxHash: tx, Status: types.SwapStatusConfirmed, Slot: s.Slot,
					BlockTime: time.Now().UTC(),
				}, nil
			}
		}
		if time.Now().After(deadline) {
			return types.SwapResult{TxHash: tx, Status: types.SwapStatusPending}, nil
		}
		select {
		case <-ctx.Done():
			return types.SwapResult{}, ctx.Err()
		case <-tick.C:
		}
	}
}

// Balance returns the wallet balance of the requested asset. SOL (Mint=="So11111111111111111111111111111111111111112")
// is fetched via getBalance; SPL tokens via getTokenAccountsByOwner.
func (v *Venue) Balance(ctx context.Context, asset types.Asset) (decimal.Decimal, error) {
	if asset.Chain != types.ChainSolana {
		return decimal.Zero, fmt.Errorf("jupiter: asset chain %q is not solana", asset.Chain)
	}
	if isWrappedSOL(asset.Mint) || asset.Mint == "" {
		lamports, err := v.rpc.GetBalance(ctx, v.signer.Address())
		if err != nil {
			return decimal.Zero, err
		}
		return unscaleAmount(lamports, 9), nil
	}
	accts, err := v.rpc.GetTokenAccountsByOwner(ctx, v.signer.Address(), asset.Mint)
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, a := range accts {
		amt, _ := decimal.NewFromString(a.Balance.Amount)
		total = total.Add(amt)
	}
	return unscaleAmountDec(total, asset.Decimals), nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

const wrappedSOL = "So11111111111111111111111111111111111111112"

func isWrappedSOL(mint string) bool { return mint == wrappedSOL }

// scaleAmount converts a decimal amount to base units using the asset's
// declared decimals. e.g. 1.5 USDC with decimals=6 -> 1_500_000.
func scaleAmount(amount decimal.Decimal, decimals int32) uint64 {
	if decimals < 0 {
		decimals = 0
	}
	scaled := amount.Shift(decimals).Truncate(0)
	if scaled.IsNegative() {
		return 0
	}
	return scaled.BigInt().Uint64()
}

// unscaleAmount converts base units back to a fractional amount.
func unscaleAmount(units uint64, decimals int32) decimal.Decimal {
	return decimal.NewFromUint64(units).Shift(-decimals)
}

func unscaleAmountDec(d decimal.Decimal, decimals int32) decimal.Decimal {
	return d.Shift(-decimals)
}

func parseUint(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
}

func priorityFeePtr(fee uint64) *uint64 {
	if fee == 0 {
		return nil
	}
	v := fee
	return &v
}

// signSerializedTransaction parses the base64 transaction returned by
// Jupiter, signs the embedded message with the provided signer, and returns
// the fully-serialized transaction bytes ready for submission.
func signSerializedTransaction(b64 string, signer wallet.Signer) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("jupiter: base64 decode tx: %w", err)
	}
	tx, err := solanago.TransactionFromBytes(raw)
	if err != nil {
		return nil, fmt.Errorf("jupiter: parse tx: %w", err)
	}
	msgBytes, err := tx.Message.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("jupiter: marshal message: %w", err)
	}
	sig, err := signer.Sign(context.Background(), msgBytes)
	if err != nil {
		return nil, fmt.Errorf("jupiter: sign: %w", err)
	}
	if len(sig) != 64 {
		return nil, fmt.Errorf("jupiter: expected 64-byte ed25519 signature, got %d", len(sig))
	}
	if len(tx.Signatures) == 0 {
		return nil, fmt.Errorf("jupiter: tx has no signature slots")
	}
	var s solanago.Signature
	copy(s[:], sig)
	tx.Signatures[0] = s
	return tx.MarshalBinary()
}

// firstSignature extracts the first signature from a serialized transaction
// and returns it as a base58 string (Solana's signature display format).
func firstSignature(serialised []byte) (string, error) {
	tx, err := solanago.TransactionFromBytes(serialised)
	if err != nil {
		return "", err
	}
	if len(tx.Signatures) == 0 {
		return "", errors.New("no signatures")
	}
	return tx.Signatures[0].String(), nil
}
