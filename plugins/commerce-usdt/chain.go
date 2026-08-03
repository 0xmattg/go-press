package commerceusdt

import (
	"context"
	"math/big"
	"time"
)

// TokenSpec identifies the ERC-20 stablecoin being collected on a chain.
type TokenSpec struct {
	Symbol   string
	Contract string
	Decimals int
}

// Deposit is one confirmed incoming token transfer discovered on-chain.
type Deposit struct {
	TxHash      string
	LogIndex    uint
	From        string
	To          string   // the watched deposit address
	TokenAmount *big.Int // raw minor units (per TokenSpec.Decimals)
	BlockNumber uint64
	BlockTime   time.Time
}

// Chain abstracts a settlement network. The generic evmChain implements it for
// all EVM presets; a future TRC-20 chain would be a separate implementation.
// The payment flow (invoices, watcher, settlement) is chain-agnostic and only
// talks to this interface.
type Chain interface {
	ID() string
	Token() TokenSpec
	Confirmations() uint64
	// LatestBlock returns the current head height.
	LatestBlock(ctx context.Context) (uint64, error)
	// BlockTimestamp returns the chain timestamp of a block.
	BlockTimestamp(ctx context.Context, block uint64) (time.Time, error)
	// ScanTransfers returns token transfers to any of addrs within [from,to].
	ScanTransfers(ctx context.Context, addrs []string, from, to uint64) ([]Deposit, error)
	// DeriveAddress derives the index-th receiving address from the configured
	// watch-only account (HD, non-hardened).
	DeriveAddress(index uint32) (string, error)
	// PaymentURI builds a wallet-scannable URI for a QR code.
	PaymentURI(addr string, amount *big.Int) string
}

// evmNetwork is a (chain, network) tuple's on-chain constants.
type evmNetwork struct {
	ChainID  int64  // EIP-155 chain id, used in EIP-681 payment URIs
	Contract string // default USDT contract (operator may override)
}

// chainPreset describes a supported network family. The generic evmChain keeps
// the implementation reusable, but every new network still needs independently
// reviewed constants, confirmation policy, fixtures, and live acceptance tests.
// TRC-20 chains would add a non-EVM Kind + a separate Chain implementation.
type chainPreset struct {
	ID           string
	Kind         string // "evm"
	NameKey      string // i18n message id for the network display name
	TokenSymbol  string
	Decimals     int
	DefaultConfs uint64
	Networks     map[string]evmNetwork // "mainnet" | "testnet"
}

// chainPresets is the registry of supported chains. This release enables only
// Ethereum; commented examples are not supported presets.
var chainPresets = map[string]chainPreset{
	"ethereum": {
		ID: "ethereum", Kind: "evm", NameKey: "commerce-usdt.chain.ethereum",
		TokenSymbol: "USDT", Decimals: 6, DefaultConfs: 24,
		Networks: map[string]evmNetwork{
			"mainnet": {ChainID: 1, Contract: "0xdAC17F958D2ee523a2206206994597C13D831ec7"},
			// Sepolia has no canonical USDT; the operator supplies a test ERC-20.
			"testnet": {ChainID: 11155111, Contract: ""},
		},
	},
	// "bsc":     {ID:"bsc", Kind:"evm", NameKey:"commerce-usdt.chain.bsc", TokenSymbol:"USDT", Decimals:18, DefaultConfs:15,
	//            Networks: map[string]evmNetwork{"mainnet": {ChainID:56, Contract:"0x55d398326f99059fF775485246999027B3197955"}}},
	// "polygon": {ID:"polygon", Kind:"evm", NameKey:"commerce-usdt.chain.polygon", TokenSymbol:"USDT", Decimals:6, DefaultConfs:128,
	//            Networks: map[string]evmNetwork{"mainnet": {ChainID:137, Contract:"0xc2132D05D31c914a87C6611C10748AEb04B58e8F"}}},
}

// presetIDs returns the registry keys in a stable order (for the settings UI).
func presetIDs() []string {
	// Only ethereum in v1; keep deterministic ordering for when more are added.
	out := make([]string, 0, len(chainPresets))
	for _, id := range []string{"ethereum", "bsc", "polygon"} {
		if _, ok := chainPresets[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
