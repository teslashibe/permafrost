package bittensor

import (
	"encoding/binary"
	"testing"

	"github.com/shopspring/decimal"
)

func TestRAOToTAO(t *testing.T) {
	cases := []struct {
		name string
		rao  uint64
		want string
	}{
		{"1 TAO", 1_000_000_000, "1"},
		{"0.001 TAO", 1_000_000, "0.001"},
		{"0", 0, "0"},
		{"max uint64", 18_446_744_073_709_551_615, "18446744073.709551615"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RAOToTAO(tc.rao)
			if got.String() != tc.want {
				t.Errorf("RAOToTAO(%d): got %s, want %s", tc.rao, got, tc.want)
			}
		})
	}
}

func TestTAOToRAO(t *testing.T) {
	cases := []struct {
		name string
		tao  string
		want uint64
	}{
		{"1 TAO", "1", 1_000_000_000},
		{"0.001 TAO", "0.001", 1_000_000},
		{"0", "0", 0},
		{"truncates fractional RAO", "0.0000000001", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := decimal.NewFromString(tc.tao)
			got := TAOToRAO(d)
			if got != tc.want {
				t.Errorf("TAOToRAO(%s): got %d, want %d", tc.tao, got, tc.want)
			}
		})
	}
}

func TestEncodeU16Compact(t *testing.T) {
	// SCALE compact encoding: small values use single byte (n << 2).
	cases := []struct {
		n    uint16
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x04}},
		{63, []byte{0xfc}},                   // max single-byte
		{64, []byte{0x01, 0x01}},             // first two-byte
		{16383, []byte{0xfd, 0xff}},          // max two-byte (16383 << 2 | 0x01)
		{16384, []byte{0x02, 0x00, 0x01, 0}}, // first four-byte
	}
	for _, tc := range cases {
		got, err := encodeU16Compact(tc.n)
		if err != nil {
			t.Fatal(err)
		}
		if !equalBytes(got, tc.want) {
			t.Errorf("encodeU16Compact(%d): got %x, want %x", tc.n, got, tc.want)
		}
	}
}

func TestDecodeSimSwapResult(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		raw := make([]byte, 24)
		binary.LittleEndian.PutUint64(raw[0:8], 1_000_000_000)
		binary.LittleEndian.PutUint64(raw[8:16], 27_042_152_708)
		binary.LittleEndian.PutUint64(raw[16:24], 503_547)

		out, err := decodeSimSwapResult(raw, 1_000_000_000)
		if err != nil {
			t.Fatal(err)
		}
		if out.AmountIn != 1_000_000_000 {
			t.Errorf("AmountIn: got %d", out.AmountIn)
		}
		if out.AmountOut != 27_042_152_708 {
			t.Errorf("AmountOut: got %d", out.AmountOut)
		}
		if out.Fee != 503_547 {
			t.Errorf("Fee: got %d", out.Fee)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		_, err := decodeSimSwapResult([]byte{1, 2, 3}, 0)
		if err == nil {
			t.Fatal("expected error on truncated input")
		}
	})
}

func TestDecodeSS58_Alice(t *testing.T) {
	pub, err := decodeSS58("5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY")
	if err != nil {
		t.Fatal(err)
	}
	if len(pub) != 32 {
		t.Fatalf("expected 32-byte pubkey, got %d", len(pub))
	}
	expectedFirst := byte(0xd4)
	if pub[0] != expectedFirst {
		t.Errorf("Alice pubkey first byte: got 0x%x, want 0x%x", pub[0], expectedFirst)
	}
}

func TestDecodeSS58_RejectsEmpty(t *testing.T) {
	_, err := decodeSS58("")
	if err == nil {
		t.Fatal("expected error for empty SS58")
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNormaliseWSURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"wss://entrypoint-finney.opentensor.ai:443", "wss://entrypoint-finney.opentensor.ai:443"},
		{"https://entrypoint-finney.opentensor.ai:443/", "wss://entrypoint-finney.opentensor.ai:443"},
		{"http://localhost:9944", "ws://localhost:9944"},
	}
	for _, tc := range cases {
		got := normaliseWSURL(tc.in)
		if got != tc.want {
			t.Errorf("normaliseWSURL(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}
