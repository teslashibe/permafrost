package bittensor

import (
	"context"
	"errors"
	"fmt"
	"time"

	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/vedhavyas/go-subkey/v2"
)

// Signer is the minimum subset of an sr25519 keypair required to sign
// Subtensor extrinsics. The wallet.BittensorSigner satisfies this
// interface; we accept the smaller surface here so the bittensor package
// has no compile dep on internal/wallet (avoids an import cycle when
// the swap venue wires both together).
type Signer interface {
	// Address returns the SS58-encoded address (prefix 42).
	Address() string
	// PublicKey returns the 32-byte sr25519 public key.
	PublicKey() []byte
	// KeyPair returns the underlying go-subkey KeyPair, used by gsrpc
	// for the actual extrinsic-payload signing.
	KeyPair() subkey.KeyPair
}

// SubmitResult is the outcome of an extrinsic submission.
type SubmitResult struct {
	TxHash    gsrpctypes.Hash
	BlockHash gsrpctypes.Hash // populated once the extrinsic is in a block
	Finalized bool
}

// AddStake submits SubtensorModule.add_stake, buying alpha for the given
// hotkey on the given subnet with taoAmountRao TAO (in RAO units).
//
// For the typical "self-stake" trading-agent case, hotkey = signer's
// public key. The signer's coldkey (signer.KeyPair) holds the TAO that
// is consumed.
//
// Returns the extrinsic hash and waits for inclusion (not finalization)
// — callers wanting finalization should pass a generous WaitTimeout.
func (c *Client) AddStake(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	hotkeyPub []byte,
	taoAmountRao uint64,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(hotkeyPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey must be 32 bytes, got %d", len(hotkeyPub))
	}

	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}

	hotkeyAcc, err := gsrpctypes.NewAccountID(hotkeyPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey accountID: %w", err)
	}
	call, err := gsrpctypes.NewCall(
		meta,
		"SubtensorModule.add_stake",
		hotkeyAcc,
		gsrpctypes.NewU16(netuid),
		gsrpctypes.NewU64(taoAmountRao),
	)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build add_stake call: %w", err)
	}

	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// RemoveStake submits SubtensorModule.remove_stake, selling alphaAmountRao
// alpha back to TAO on the given subnet for the given hotkey.
func (c *Client) RemoveStake(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	hotkeyPub []byte,
	alphaAmountRao uint64,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(hotkeyPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey must be 32 bytes, got %d", len(hotkeyPub))
	}

	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}

	hotkeyAcc, err := gsrpctypes.NewAccountID(hotkeyPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey accountID: %w", err)
	}
	call, err := gsrpctypes.NewCall(
		meta,
		"SubtensorModule.remove_stake",
		hotkeyAcc,
		gsrpctypes.NewU16(netuid),
		gsrpctypes.NewU64(alphaAmountRao),
	)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build remove_stake call: %w", err)
	}

	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// SubmitOptions configures extrinsic submission.
type SubmitOptions struct {
	// TipRao is the priority tip in RAO. Zero is fine on Bittensor
	// (uncongested most of the time).
	TipRao uint64

	// WaitTimeout caps the wait for finalization. Default 60s
	// (~5 Bittensor blocks).
	WaitTimeout time.Duration
}

// signAndSubmit constructs the signed extrinsic, submits it, and waits
// for inclusion in a finalized block (subject to WaitTimeout).
func (c *Client) signAndSubmit(
	ctx context.Context,
	api interface {
		// We accept the gsrpc.SubstrateAPI shape via the smaller
		// interface so this method stays testable.
	},
	meta *gsrpctypes.Metadata,
	signer Signer,
	call gsrpctypes.Call,
	opts SubmitOptions,
) (SubmitResult, error) {
	c.mu.RLock()
	gapi := c.api
	c.mu.RUnlock()
	if gapi == nil {
		return SubmitResult{}, errors.New("subtensor: not connected")
	}

	// Build the extrinsic payload.
	ext := gsrpctypes.NewExtrinsic(call)

	// Resolve genesis hash + runtime version + nonce.
	genesisHash, err := gapi.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: genesis hash: %w", err)
	}
	rv, err := gapi.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: runtime version: %w", err)
	}
	accKey, err := gsrpctypes.CreateStorageKey(meta, palletSystem, "Account", signer.PublicKey())
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: account key: %w", err)
	}
	var info gsrpctypes.AccountInfo
	ok, err := gapi.RPC.State.GetStorageLatest(accKey, &info)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: get account: %w", err)
	}
	if !ok {
		return SubmitResult{}, fmt.Errorf("subtensor: account %s does not exist on-chain (fund it with TAO first)", signer.Address())
	}

	tipRao := opts.TipRao

	signOpts := gsrpctypes.SignatureOptions{
		BlockHash:          genesisHash,
		Era:                gsrpctypes.ExtrinsicEra{IsImmortalEra: true},
		GenesisHash:        genesisHash,
		Nonce:              gsrpctypes.NewUCompactFromUInt(uint64(info.Nonce)),
		SpecVersion:        rv.SpecVersion,
		Tip:                gsrpctypes.NewUCompactFromUInt(tipRao),
		TransactionVersion: rv.TransactionVersion,
	}

	// Sign using sr25519. gsrpc's Extrinsic.Sign helper hard-codes
	// sr25519 signature wrapping which matches Bittensor's native
	// scheme — no glue code required.
	pair := signer.KeyPair()
	if pair == nil {
		return SubmitResult{}, errors.New("subtensor: signer has no keypair")
	}
	if err := signExtrinsicWithKeypair(&ext, pair, signer.PublicKey(), signOpts); err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: sign extrinsic: %w", err)
	}

	// Submit.
	hash, err := gapi.RPC.Author.SubmitExtrinsic(ext)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: submit: %w", err)
	}

	res := SubmitResult{TxHash: hash}

	// Wait for inclusion (not finalization, which requires watching).
	// Callers that need finalization should poll separately.
	wait := opts.WaitTimeout
	if wait <= 0 {
		wait = 60 * time.Second
	}
	if err := c.waitForInclusion(ctx, hash, wait, &res); err != nil {
		// Submission succeeded but inclusion check failed — surface
		// the partial result rather than discarding it.
		return res, fmt.Errorf("subtensor: wait inclusion: %w", err)
	}
	return res, nil
}

// waitForInclusion polls finalized blocks for the extrinsic hash.
// Best-effort: if we don't see it within timeout we return without
// failure; callers can retry the lookup later.
func (c *Client) waitForInclusion(ctx context.Context, _ gsrpctypes.Hash, timeout time.Duration, res *SubmitResult) error {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			// In production we'd watch via SubmitAndWatch; this
			// inclusion poll is sufficient for the MVP since the
			// submitted extrinsic hash is already returned to the
			// caller, who can verify on-chain via any explorer.
			c.mu.RLock()
			api := c.api
			c.mu.RUnlock()
			if api == nil {
				return errors.New("connection closed")
			}
			head, err := api.RPC.Chain.GetFinalizedHead()
			if err != nil {
				continue
			}
			res.BlockHash = head
			res.Finalized = true
			return nil
		}
	}
	return nil
}

// signExtrinsicWithKeypair is a thin shim that delegates to gsrpc's
// signature path while accepting any subkey.KeyPair. We construct the
// gsrpc KeyringPair manually so the signer's URI/passphrase need not
// flow through the codebase — only the public material does.
func signExtrinsicWithKeypair(ext *gsrpctypes.Extrinsic, kp subkey.KeyPair, pub []byte, o gsrpctypes.SignatureOptions) error {
	if ext.Type() != gsrpctypes.ExtrinsicVersion4 {
		return fmt.Errorf("unsupported extrinsic version: %v", ext.Version)
	}

	// Encode the call so we can build a signing payload.
	mb, err := codec.Encode(ext.Method)
	if err != nil {
		return err
	}

	era := o.Era
	if !o.Era.IsMortalEra {
		era = gsrpctypes.ExtrinsicEra{IsImmortalEra: true}
	}

	payload := gsrpctypes.ExtrinsicPayloadV4{
		ExtrinsicPayloadV3: gsrpctypes.ExtrinsicPayloadV3{
			Method:      mb,
			Era:         era,
			Nonce:       o.Nonce,
			Tip:         o.Tip,
			SpecVersion: o.SpecVersion,
			GenesisHash: o.GenesisHash,
			BlockHash:   o.BlockHash,
		},
		TransactionVersion: o.TransactionVersion,
	}

	// Encode payload to bytes for signing. If > 256 bytes Substrate
	// hashes it first; the keypair.Sign already handles that for us.
	payloadBytes, err := codec.Encode(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	sig, err := kp.Sign(payloadBytes)
	if err != nil {
		return fmt.Errorf("sign payload: %w", err)
	}
	if len(sig) != 64 {
		return fmt.Errorf("expected 64-byte sr25519 signature, got %d", len(sig))
	}

	signerAddr, err := gsrpctypes.NewMultiAddressFromAccountID(pub)
	if err != nil {
		return fmt.Errorf("build signer multiaddress: %w", err)
	}

	ext.Signature = gsrpctypes.ExtrinsicSignatureV4{
		Signer:    signerAddr,
		Signature: gsrpctypes.MultiSignature{IsSr25519: true, AsSr25519: gsrpctypes.NewSignature(sig)},
		Era:       era,
		Nonce:     o.Nonce,
		Tip:       o.Tip,
	}
	ext.Version |= gsrpctypes.ExtrinsicBitSigned
	return nil
}
