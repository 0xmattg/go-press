package metamaskidentity

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const defaultChainID = 1

type providerConfig struct {
	Enabled           bool
	ChainID           int
	ChainIDHex        string
	ChainIDValid      bool
	AllowRegistration bool
	Scheme            string
	Origin            string
	Domain            string
	URI               string
	SiteURLValid      bool
}

func (p *Plugin) loadConfig() providerConfig {
	config := providerConfig{ChainID: defaultChainID, ChainIDValid: true}
	if p == nil || p.options == nil {
		return config
	}
	config.Enabled = p.options.GetDefault(optEnabled, "0") == "1"
	config.AllowRegistration = p.options.GetDefault(optAutoRegister, "0") == "1"
	chainRaw := strings.TrimSpace(p.options.GetDefault(optChainID, strconv.Itoa(defaultChainID)))
	chainID, err := strconv.Atoi(chainRaw)
	if err != nil || chainID <= 0 || chainID > 2147483647 {
		config.ChainIDValid = false
	} else {
		config.ChainID = chainID
		config.ChainIDHex = fmt.Sprintf("0x%x", chainID)
	}

	parsed, err := url.Parse(strings.TrimSpace(p.siteURL))
	if err != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return config
	}
	config.Scheme = strings.ToLower(parsed.Scheme)
	config.Domain = strings.ToLower(parsed.Host)
	config.Origin = config.Scheme + "://" + config.Domain
	config.URI = config.Origin
	config.SiteURLValid = true
	return config
}

func (c providerConfig) ready() bool {
	return c.Enabled && c.ChainIDValid && c.SiteURLValid
}
