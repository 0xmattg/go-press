package commerceusdt

import (
	"encoding/hex"
	"testing"

	bip32 "github.com/tyler-smith/go-bip32"
)

// canonicalSeed is the BIP-39 seed for the well-known all-"abandon" test mnemonic
// ("abandon abandon abandon abandon abandon abandon abandon abandon abandon
// abandon abandon about", empty passphrase). Its m/44'/60'/0'/0/0 address is the
// canonical MetaMask account #0 — a strong external anchor for the derivation.
const canonicalSeed = "5eb00bbddcf069084889a8ab9155568165f5c453ccb85e70811aaed6f6da5fc19a5ac40b389cd370d086206dec8aa6c43daea6690f20ad3d8d48b2d2ce9e38e4"
const canonicalAddr0 = "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"

// accountXpub derives the m/44'/60'/0'/0 watch-only xpub from the canonical seed,
// which is exactly what an operator would paste into the settings page.
func accountXpub(t *testing.T) string {
	t.Helper()
	seed, err := hex.DecodeString(canonicalSeed)
	if err != nil {
		t.Fatal(err)
	}
	node, err := bip32.NewMasterKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	const h = uint32(0x80000000)
	for _, idx := range []uint32{44 + h, 60 + h, 0 + h, 0} { // m/44'/60'/0'/0
		node, err = node.NewChildKey(idx)
		if err != nil {
			t.Fatal(err)
		}
	}
	return node.PublicKey().B58Serialize()
}

func TestDeriveEVMAddressCanonicalVector(t *testing.T) {
	got, err := deriveEVMAddress(accountXpub(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalAddr0 {
		t.Fatalf("derive index 0 = %s, want canonical %s", got, canonicalAddr0)
	}
}

func TestDeriveEVMAddressDeterministicAndUnique(t *testing.T) {
	xpub := accountXpub(t)
	a0, _ := deriveEVMAddress(xpub, 0)
	a0again, _ := deriveEVMAddress(xpub, 0)
	a1, _ := deriveEVMAddress(xpub, 1)
	if a0 != a0again {
		t.Error("same index must derive the same address")
	}
	if a0 == a1 {
		t.Error("different indices must derive different addresses")
	}
	if len(a0) != 42 || a0[:2] != "0x" {
		t.Errorf("unexpected address format: %s", a0)
	}
}

func TestDeriveEVMAddressRejectsBadInput(t *testing.T) {
	if _, err := deriveEVMAddress("not-an-xpub", 0); err == nil {
		t.Error("expected an error for an invalid xpub")
	}
	if _, err := deriveEVMAddress(accountXpub(t), 0x80000000); err == nil {
		t.Error("expected an error for a hardened index")
	}
	if validXpub("garbage") {
		t.Error("validXpub should reject garbage")
	}
	if !validXpub(accountXpub(t)) {
		t.Error("validXpub should accept a real xpub")
	}
}
