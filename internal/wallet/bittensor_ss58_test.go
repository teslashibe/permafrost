package wallet

import (
	"encoding/hex"
	"testing"
)

// TestSS58_AlicePublicKey verifies our SS58 encoder produces the canonical
// generic-Substrate address (network prefix 42) for Alice's well-known
// sr25519 public key. The canonical reference comes from subkey,
// polkadot.js, and every other Substrate tooling — so this is the
// cross-tool compatibility check.
//
// Alice's public key (sr25519): 0xd43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d
// Expected SS58 address (prefix 42): 5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY
//
// Source: https://docs.substrate.io/reference/glossary/#well-known-keys
// (and every Substrate test fixture in existence)
func TestSS58_AlicePublicKey(t *testing.T) {
	pubHex := "d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	expected := "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"

	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ss58Encode(pub, byte(bittensorSS58Prefix))
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("SS58 mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}

// TestSS58_BobPublicKey verifies a second well-known address.
// Bob: 0x8eaf04151687736326c9fea17e25fc5287613693c912909cb226aa4794f26a48
// Expected: 5FHneW46xGXgs5mUiveU4sbTyGBzmstUspZC92UhjJM694ty
func TestSS58_BobPublicKey(t *testing.T) {
	pubHex := "8eaf04151687736326c9fea17e25fc5287613693c912909cb226aa4794f26a48"
	expected := "5FHneW46xGXgs5mUiveU4sbTyGBzmstUspZC92UhjJM694ty"

	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ss58Encode(pub, byte(bittensorSS58Prefix))
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("SS58 mismatch:\n  got:      %s\n  expected: %s", got, expected)
	}
}
