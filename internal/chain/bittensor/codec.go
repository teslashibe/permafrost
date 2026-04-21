package bittensor

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/vedhavyas/go-subkey/v2"
)

// decodeSS58 decodes an SS58-encoded address into the 32-byte raw account
// id. We delegate to vedhavyas/go-subkey/v2 which is the canonical Go
// implementation matching the subkey CLI.
func decodeSS58(ss58 string) ([]byte, error) {
	if ss58 == "" {
		return nil, errors.New("ss58: empty address")
	}
	prefix, pub, err := subkey.SS58Decode(ss58)
	if err != nil {
		return nil, fmt.Errorf("ss58 decode: %w", err)
	}
	if prefix != SS58Prefix && prefix != 0 {
		// Accept generic Substrate (42) or Polkadot (0); refuse others
		// so callers don't accidentally feed a non-Bittensor address.
		return nil, fmt.Errorf("ss58: unexpected network prefix %d (want 42 or 0)", prefix)
	}
	if len(pub) != 32 {
		return nil, fmt.Errorf("ss58: expected 32-byte payload, got %d", len(pub))
	}
	return pub, nil
}

// encodeU16Compact encodes a u16 as SCALE Compact<u16>. Used as the
// netuid component of storage keys.
//
// SCALE compact rules for values < 2^6: single byte (value << 2).
// 2^6..2^14: two bytes (value << 2 | 0x01).
// We hand-encode here to avoid pulling another dep for one call site.
func encodeU16Compact(n uint16) ([]byte, error) {
	switch {
	case n < 1<<6:
		return []byte{byte(n) << 2}, nil
	case n < 1<<14:
		v := uint16(n)<<2 | 0x01
		out := make([]byte, 2)
		binary.LittleEndian.PutUint16(out, v)
		return out, nil
	default:
		// 2^14..2^16
		v := uint32(n)<<2 | 0x02
		out := make([]byte, 4)
		binary.LittleEndian.PutUint32(out, v)
		return out, nil
	}
}

// decodeSimSwapResult decodes the SCALE-encoded result of swap_simSwap*
// runtime calls. The Subtensor runtime returns a struct of the form:
//
//	{ amount_paid_in: u64, amount_paid_out: u64, amount_paid_fee: u64 }
//
// All three are little-endian u64 values, totalling 24 bytes. The RPC
// wraps the result in a Vec<u8>; gsrpc returns it as []byte.
//
// If the response is unexpectedly short we surface a clear error rather
// than silently returning zeros — silent zeros caused real production
// pain in the previous draft.
func decodeSimSwapResult(raw []byte, fallbackIn uint64) (SimulatedSwapResult, error) {
	if len(raw) < 24 {
		return SimulatedSwapResult{}, fmt.Errorf("subtensor: simSwap result truncated (got %d bytes, need 24)", len(raw))
	}
	return SimulatedSwapResult{
		AmountIn:  binary.LittleEndian.Uint64(raw[0:8]),
		AmountOut: binary.LittleEndian.Uint64(raw[8:16]),
		Fee:       binary.LittleEndian.Uint64(raw[16:24]),
	}, nil
}
