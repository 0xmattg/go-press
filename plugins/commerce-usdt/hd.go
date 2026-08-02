package commerceusdt

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	bip32 "github.com/tyler-smith/go-bip32"
)

// deriveEVMAddress derives the index-th non-hardened receiving address from a
// watch-only extended public key (xpub).
//
// The xpub MUST be the external-chain account node, e.g. m/44'/60'/0'/0, so that
// per-order addresses are m/44'/60'/0'/0/index. Only the public key is used —
// the server never holds spend authority; sweeping collected funds is an
// out-of-band operations concern handled with a separately-secured key.
//
// BIP-32 child derivation is delegated to a well-tested library; the EVM address
// is keccak256(uncompressed_pubkey)[12:], EIP-55 checksummed.
func deriveEVMAddress(xpub string, index uint32) (string, error) {
	if index >= 0x80000000 {
		return "", errors.New("usdt: hardened index unsupported for public derivation")
	}
	key, err := bip32.B58Deserialize(xpub)
	if err != nil {
		return "", fmt.Errorf("usdt: invalid xpub: %w", err)
	}
	if key.IsPrivate {
		return "", errors.New("usdt: refusing an extended PRIVATE key; provide a watch-only xpub")
	}
	child, err := key.NewChildKey(index) // non-hardened public (CKDpub)
	if err != nil {
		return "", fmt.Errorf("usdt: derive index %d: %w", index, err)
	}
	pub, err := crypto.DecompressPubkey(child.Key) // 33-byte compressed secp256k1
	if err != nil {
		return "", fmt.Errorf("usdt: decompress derived pubkey: %w", err)
	}
	return crypto.PubkeyToAddress(*pub).Hex(), nil // 0x… EIP-55 checksummed
}

// validXpub reports whether xpub parses as a watch-only extended public key and
// can derive its first address. Used to gate checkout availability.
func validXpub(xpub string) bool {
	_, err := deriveEVMAddress(xpub, 0)
	return err == nil
}
