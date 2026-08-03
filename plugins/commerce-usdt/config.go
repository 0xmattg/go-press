package commerceusdt

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
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
	minWindowMins     = 5
	maxWindowMins     = 24 * 60
	maxConfirmations  = 10_000
	minRateScaled     = 100_000    // 0.1 USDT per USD
	maxRateScaled     = 10_000_000 // 10 USDT per USD
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
	return p.loadConfigWith(nil)
}

func (p *Plugin) loadConfigWith(overlay map[string]string) config {
	get := func(key string) string {
		if value, ok := overlay[key]; ok {
			return value
		}
		if p == nil || p.options == nil {
			return ""
		}
		return p.options.Get(key)
	}
	c := config{
		ChainID: "ethereum", Network: defaultNetwork,
		WindowMinutes: defaultWindowMins, RateScaled: rateScale, DustTolerance: big.NewInt(0),
	}
	c.Enabled = strings.TrimSpace(get(optEnabled)) == "1"
	if v := strings.TrimSpace(get(optChain)); v != "" {
		c.ChainID = v
	}
	if v := strings.TrimSpace(get(optNetwork)); v != "" {
		c.Network = v
	}
	c.RPCURL = strings.TrimSpace(get(optRPCURL))
	c.Xpub = strings.TrimSpace(get(optReceiveXpub))
	rateText := strings.TrimSpace(get(optUSDRate))
	if rateText == "" {
		rateText = "1.00"
	}
	if rate, err := parseRateScaledStrict(rateText); err == nil {
		c.RateScaled = rate
	} else {
		c.RateScaled = 0
	}
	if windowText := strings.TrimSpace(get(optWindowMinutes)); windowText != "" {
		if v, err := strconv.Atoi(windowText); err == nil {
			c.WindowMinutes = v
		} else {
			c.WindowMinutes = 0
		}
	}
	dustText := strings.TrimSpace(get(optDustTolerance))
	if dustText == "" {
		dustText = "0"
	}
	if v, ok := new(big.Int).SetString(dustText, 10); ok && v.Sign() >= 0 {
		c.DustTolerance = v
	} else {
		c.DustTolerance = nil
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
	if v := strings.TrimSpace(get(optTokenContract)); v != "" {
		c.TokenContract = v // operator override (required on testnets with no canonical USDT)
	}
	if confirmationsText := strings.TrimSpace(get(optConfirmations)); confirmationsText != "" {
		if v, err := strconv.ParseUint(confirmationsText, 10, 64); err == nil {
			c.Confirmations = v
		} else {
			c.Confirmations = 0
		}
	}
	return c
}

// ready reports whether checkout can start a USDT payment.
func (c config) ready() bool {
	return c.Enabled && c.resolved &&
		validRPCURL(c.RPCURL) == nil && common.IsHexAddress(c.TokenContract) && common.HexToAddress(c.TokenContract) != (common.Address{}) &&
		c.contractAllowed() &&
		c.RateScaled >= minRateScaled && c.RateScaled <= maxRateScaled &&
		c.DustTolerance != nil && c.DustTolerance.Sign() >= 0 &&
		c.Confirmations > 0 && c.Confirmations <= maxConfirmations &&
		c.WindowMinutes >= minWindowMins && c.WindowMinutes <= maxWindowMins &&
		validXpub(c.Xpub)
}

func (c config) contractAllowed() bool {
	// A preset with a canonical contract (currently Ethereum mainnet) must not
	// silently accept a look-alike token. Test networks intentionally have no
	// canonical USDT address and therefore require an operator-supplied contract.
	return c.net.Contract == "" || strings.EqualFold(c.TokenContract, c.net.Contract)
}

func (c config) networkKey() string {
	if !c.resolved || !common.IsHexAddress(c.TokenContract) {
		return ""
	}
	return fmt.Sprintf("evm:%d:%s", c.net.ChainID, strings.ToLower(common.HexToAddress(c.TokenContract).Hex()))
}

func validRPCURL(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("RPC URL must be an absolute http(s) URL")
	}
	if u.User != nil {
		return errors.New("RPC URL must not contain user-info credentials")
	}
	return nil
}

func (p *Plugin) mergedSetting(settings map[string]string, key, fallback string) string {
	if value, ok := settings[key]; ok {
		return strings.TrimSpace(value)
	}
	if p != nil && p.options != nil {
		if value := strings.TrimSpace(p.options.Get(key)); value != "" {
			return value
		}
	}
	return fallback
}

// ValidateSettings implements plugin.SettingsValidateProvider. It validates the
// merged next configuration before Core persists any option, and refuses to
// disable or switch settlement identity while invoices still require watching.
func (p *Plugin) ValidateSettings(settings map[string]string) error {
	cfg := p.loadConfigWith(settings)
	if enabled := strings.TrimSpace(settings[optEnabled]); enabled != "" && enabled != "0" && enabled != "1" {
		return errors.New("USDT enabled must be 0 or 1")
	}
	if !cfg.resolved {
		return errors.New("unsupported USDT chain or network")
	}
	if err := validRPCURL(cfg.RPCURL); err != nil && (cfg.Enabled || cfg.RPCURL != "") {
		return err
	}
	if cfg.TokenContract != "" && (!common.IsHexAddress(cfg.TokenContract) || common.HexToAddress(cfg.TokenContract) == (common.Address{})) {
		return errors.New("USDT contract must be a non-zero EVM address")
	}
	if !cfg.contractAllowed() {
		return errors.New("the selected mainnet requires its canonical USDT contract")
	}
	if cfg.Xpub != "" && !validXpub(cfg.Xpub) {
		return errors.New("receiving key must be a valid watch-only xpub")
	}
	confirmations, err := strconv.ParseUint(p.mergedSetting(settings, optConfirmations, strconv.FormatUint(cfg.preset.DefaultConfs, 10)), 10, 64)
	if err != nil || confirmations == 0 || confirmations > maxConfirmations {
		return fmt.Errorf("confirmations must be between 1 and %d", maxConfirmations)
	}
	window, err := strconv.Atoi(p.mergedSetting(settings, optWindowMinutes, strconv.Itoa(defaultWindowMins)))
	if err != nil || window < minWindowMins || window > maxWindowMins {
		return fmt.Errorf("payment window must be between %d and %d minutes", minWindowMins, maxWindowMins)
	}
	rateText := p.mergedSetting(settings, optUSDRate, "1.00")
	rate, err := parseRateScaledStrict(rateText)
	if err != nil || rate < minRateScaled || rate > maxRateScaled {
		return fmt.Errorf("USDT rate must be between 0.1 and 10 with at most 6 decimal places")
	}
	dust, ok := new(big.Int).SetString(p.mergedSetting(settings, optDustTolerance, "0"), 10)
	if !ok || dust.Sign() < 0 {
		return errors.New("USDT dust tolerance must be non-negative")
	}

	watchable, err := p.watchableInvoiceCount(time.Now().UTC())
	if err != nil {
		return fmt.Errorf("cannot verify watched USDT invoices: %w", err)
	}
	current := p.loadConfig()
	if watchable > 0 && (!cfg.Enabled || current.networkKey() == "" || cfg.networkKey() != current.networkKey()) {
		return errors.New("cannot disable USDT or switch network/contract while invoices are still being watched")
	}
	if !cfg.Enabled {
		return nil
	}
	if !cfg.ready() {
		return errors.New("USDT configuration is incomplete or outside the supported limits")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.buildChain(cfg).VerifyConfiguration(ctx); err != nil {
		return fmt.Errorf("USDT RPC verification failed: %w", err)
	}
	return nil
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
