package evm

import (
	"math/big"
	"strings"
	"testing"
)

// Vectors below were generated against go-ethereum's
// abi.Pack to make sure our hand-rolled calldata matches the
// canonical encoding bit-for-bit.

func TestEncodeApprove_KnownVector(t *testing.T) {
	// approve(0x111000000000000000000000000000000000aaa, MaxUint256)
	got := EncodeApprove("0x111000000000000000000000000000000000aaaa", MaxUint256)
	want := "0x095ea7b3" +
		"000000000000000000000000111000000000000000000000000000000000aaaa" +
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if !strings.EqualFold(got, want) {
		t.Errorf("approve calldata mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestEncodeApprove_FixedAmount(t *testing.T) {
	// approve(spender, 100_000_000) — 100 USDC at 6 decimals
	got := EncodeApprove("0x000000000000000000000000000000000000dEaD", big.NewInt(100_000_000))
	want := "0x095ea7b3" +
		"000000000000000000000000000000000000000000000000000000000000dead" +
		"0000000000000000000000000000000000000000000000000000000005f5e100"
	if !strings.EqualFold(got, want) {
		t.Errorf("approve(amount) calldata mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestEncodeBalanceOf(t *testing.T) {
	got := EncodeBalanceOf("0x0000000000000000000000000000000000000001")
	want := "0x70a082310000000000000000000000000000000000000000000000000000000000000001"
	if !strings.EqualFold(got, want) {
		t.Errorf("balanceOf calldata mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestEncodeAllowance(t *testing.T) {
	got := EncodeAllowance(
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002")
	want := "0xdd62ed3e" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"0000000000000000000000000000000000000000000000000000000000000002"
	if !strings.EqualFold(got, want) {
		t.Errorf("allowance calldata mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestEncodeTransfer(t *testing.T) {
	got := EncodeTransfer("0x000000000000000000000000000000000000bEEF", big.NewInt(1))
	want := "0xa9059cbb" +
		"000000000000000000000000000000000000000000000000000000000000beef" +
		"0000000000000000000000000000000000000000000000000000000000000001"
	if !strings.EqualFold(got, want) {
		t.Errorf("transfer calldata mismatch\n got=%s\nwant=%s", got, want)
	}
}

func TestPadAddress_StripsPrefix_Lowercases(t *testing.T) {
	got := padAddress("0xABCDEFabcdef")
	want := "0000000000000000000000000000000000000000000000000000abcdefabcdef"
	if got != want {
		t.Errorf("padAddress\n got=%s\nwant=%s", got, want)
	}
	if len(got) != 64 {
		t.Errorf("padAddress should always be 64 hex chars, got %d", len(got))
	}
}

func TestPadUint256_Negative_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on negative uint256")
		}
	}()
	_ = padUint256(big.NewInt(-1))
}
