package googleidentity

import (
	"net/url"
	"strings"
)

type providerConfig struct {
	Enabled           bool
	ClientID          string
	ClientSecret      string
	HostedDomain      string
	HostedDomainValid bool
	RedirectURL       string
	AllowRegistration bool
}

func (p *Plugin) loadConfig() providerConfig {
	config := providerConfig{}
	if p == nil || p.options == nil {
		return config
	}
	config.Enabled = p.options.GetDefault(optEnabled, "0") == "1"
	config.ClientID = strings.TrimSpace(p.options.Get(optClientID))
	config.ClientSecret = strings.TrimSpace(p.options.Get(optClientSecret))
	config.HostedDomain, config.HostedDomainValid = normalizeHostedDomain(p.options.Get(optHostedDomain))
	config.AllowRegistration = p.options.GetDefault(optAutoRegister, "0") == "1"
	if p.siteURL != "" {
		config.RedirectURL = strings.TrimRight(strings.TrimSpace(p.siteURL), "/") + callbackPath
	}
	return config
}

func (c providerConfig) ready() bool {
	if !c.Enabled || c.ClientID == "" || c.ClientSecret == "" || !c.HostedDomainValid || len(c.ClientID) > 512 || len(c.ClientSecret) > 2048 || len(c.RedirectURL) > 2048 {
		return false
	}
	parsed, err := url.Parse(c.RedirectURL)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

func normalizeHostedDomain(raw string) (string, bool) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" {
		return "", true
	}
	if len(domain) > 253 || strings.ContainsAny(domain, "/:@ \\") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return "", false
	}
	return domain, true
}
