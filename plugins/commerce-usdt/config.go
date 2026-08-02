package commerceusdt

import (
	"math/big"
	"strconv"
	"strings"
)

// Option keys (namespaced plugin_<slug>_*, matching the admin settings pipeline).
const (
	optEnabled       = "plugin_commerce-usdt_enabled"
	optChain         = "plugin_commerce-usdt_chain"
	optNetwork       = "plugin_commerce-usdt_network"
	optRPCURL        = "plugin_commerce-usdt_rpc_url"
	optTokenContract = "plugin_commerce-usdt_token_contract"
	optReceiveXpub   = "plugin_commerce-usdt_receive_xpub"
	optConfirmations = "plugin_commerce-usdt_confirmations"
	optWindowMinutes = "plugin_commerce-usdt_window_minutes"
	optUSDRate       = "plugin_commerce-usdt_usd_rate"
	optDustTolerance = "plugin_commerce-usdt_dust_tolerance"
)

const (
	defaultNetwork    = "mainnet"
	defaultWindowMins = 30
)

// config is the resolved gateway configuration from stored options.
type config struct {
	Enabled       bool
	ChainID       string // preset id, e.g. "ethereum"
	Network       string // "mainnet" | "testnet"
	RPCURL        string
	TokenContract string
	Xpub          string
	Confirmations uint64
	WindowMinutes int
	RateScaled    int64    // USD->token rate, fixed-point (rateScale)
	DustTolerance *big.Int // token minor units

	preset   chainPreset
	net      evmNetwork
	decimals int
	resolved bool // preset + network resolved
}

func (p *Plugin) loadConfig() config {
	c := config{
		ChainID: "ethereum", Network: defaultNetwork,
		WindowMinutes: defaultWindowMins, RateScaled: rateScale, DustTolerance: big.NewInt(0),
	}
	if p == nil || p.options == nil {
		return c
	}
	c.Enabled = p.options.GetDefault(optEnabled, "0") == "1"
	if v := strings.TrimSpace(p.options.Get(optChain)); v != "" {
		c.ChainID = v
	}
	if v := strings.TrimSpace(p.options.Get(optNetwork)); v != "" {
		c.Network = v
	}
	c.RPCURL = strings.TrimSpace(p.options.Get(optRPCURL))
	c.Xpub = strings.TrimSpace(p.options.Get(optReceiveXpub))
	c.RateScaled = parseRateScaled(p.options.GetDefault(optUSDRate, "1.00"))
	if v, err := strconv.Atoi(strings.TrimSpace(p.options.Get(optWindowMinutes))); err == nil && v > 0 {
		c.WindowMinutes = v
	}
	if v, ok := new(big.Int).SetString(strings.TrimSpace(p.options.GetDefault(optDustTolerance, "0")), 10); ok && v.Sign() >= 0 {
		c.DustTolerance = v
	}

	// Resolve the preset + network constants.
	preset, ok := chainPresets[c.ChainID]
	if ok {
		net, okNet := preset.Networks[c.Network]
		if okNet {
			c.preset, c.net, c.decimals, c.resolved = preset, net, preset.Decimals, true
			c.Confirmations = preset.DefaultConfs
			c.TokenContract = net.Contract // preset default (may be overridden below)
		}
	}
	if v := strings.TrimSpace(p.options.Get(optTokenContract)); v != "" {
		c.TokenContract = v // operator override (required on testnets with no canonical USDT)
	}
	if v, err := strconv.ParseUint(strings.TrimSpace(p.options.Get(optConfirmations)), 10, 64); err == nil && v > 0 {
		c.Confirmations = v
	}
	return c
}

// ready reports whether checkout can start a USDT payment.
func (c config) ready() bool {
	return c.Enabled && c.resolved &&
		c.RPCURL != "" && c.TokenContract != "" && c.RateScaled > 0 &&
		validXpub(c.Xpub)
}

// buildChain constructs the active Chain from a resolved, ready config.
func (p *Plugin) buildChain(c config) *evmChain {
	confs := c.Confirmations
	if confs == 0 {
		confs = c.preset.DefaultConfs
	}
	return &evmChain{
		id:      c.ChainID,
		token:   TokenSpec{Symbol: c.preset.TokenSymbol, Contract: c.TokenContract, Decimals: c.decimals},
		chainID: c.net.ChainID,
		confs:   confs,
		rpcURL:  c.RPCURL,
		xpub:    c.Xpub,
		http:    p.httpClient,
	}
}
