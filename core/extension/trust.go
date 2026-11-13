// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/pkg/sign"
)

// DefaultMarketEndpoint is the base URL of the official extension market.
// The catalog is served at <base>/catalog. A [market] section in config.toml
// replaces it, together with the trusted keys.
const DefaultMarketEndpoint = "https://gopress.xyz/api/market/v1"

// maxTrustedKeys caps each key set, built in or configured. Two keys cover a
// rotation; a few more leave room for a self-hosted market's own policy.
const maxTrustedKeys = 4

// The official market's public keys, base64. Keys are rotated by releasing a
// new version of GoPress.
//
// Both lists stay empty until the release keys are created; while they are
// empty the official market is disabled and only a self-hosted market
// configured in config.toml can be used. Test keys never go here: anyone
// holding a test private key could sign catalogs and packages.
var (
	builtinCatalogKeys = []string{}
	builtinPackageKeys = []string{}
)

// Market modes, as reported by TrustRoot.Mode.
const (
	MarketDisabled = "disabled"
	MarketOfficial = "official"
	MarketCustom   = "custom"
)

// TrustRoot is the market a site talks to and the keys it trusts: catalog
// keys sign the catalog, package keys countersign packages. The two sets
// never share a key.
type TrustRoot struct {
	// Endpoint is the market's base URL, without a trailing slash.
	Endpoint    string
	CatalogKeys sign.KeySet
	PackageKeys sign.KeySet
	// Custom reports a self-hosted market configured in config.toml.
	Custom bool
}

// NewTrustRoot builds the trust root from the [market] section: the official
// market when the section is empty, otherwise the configured market. A
// section that sets only some of endpoint, catalog_keys and package_keys is
// an error, as are malformed keys or an endpoint that is not HTTPS.
func NewTrustRoot(cfg config.MarketConfig) (TrustRoot, error) {
	if cfg.IsZero() {
		return newTrustRoot(DefaultMarketEndpoint, builtinCatalogKeys, builtinPackageKeys, false)
	}
	var missing []string
	if strings.TrimSpace(cfg.Endpoint) == "" {
		missing = append(missing, "endpoint")
	}
	if len(cfg.CatalogKeys) == 0 {
		missing = append(missing, "catalog_keys")
	}
	if len(cfg.PackageKeys) == 0 {
		missing = append(missing, "package_keys")
	}
	if len(missing) > 0 {
		return TrustRoot{}, fmt.Errorf("market: set endpoint, catalog_keys and package_keys together, or none of them; missing %s", strings.Join(missing, ", "))
	}
	return newTrustRoot(cfg.Endpoint, cfg.CatalogKeys, cfg.PackageKeys, true)
}

func newTrustRoot(endpoint string, catalogKeys, packageKeys []string, custom bool) (TrustRoot, error) {
	base, err := normalizeEndpoint(endpoint)
	if err != nil {
		return TrustRoot{}, err
	}
	catalog, err := parseKeys("catalog_keys", catalogKeys)
	if err != nil {
		return TrustRoot{}, err
	}
	packages, err := parseKeys("package_keys", packageKeys)
	if err != nil {
		return TrustRoot{}, err
	}
	for kid := range catalog {
		if _, shared := packages[kid]; shared {
			return TrustRoot{}, fmt.Errorf("market: key %s is listed in both catalog_keys and package_keys; use separate keys", kid)
		}
	}
	return TrustRoot{Endpoint: base, CatalogKeys: catalog, PackageKeys: packages, Custom: custom}, nil
}

func parseKeys(field string, encoded []string) (sign.KeySet, error) {
	if len(encoded) > maxTrustedKeys {
		return nil, fmt.Errorf("market: %s lists %d keys, at most %d are allowed", field, len(encoded), maxTrustedKeys)
	}
	set := make(sign.KeySet, len(encoded))
	for _, value := range encoded {
		key, err := sign.ParsePublicKey(value)
		if err != nil {
			return nil, fmt.Errorf("market: %s: %w", field, err)
		}
		if _, dup := set[key.KID()]; dup {
			return nil, fmt.Errorf("market: %s lists key %s twice", field, key.KID())
		}
		set[key.KID()] = key
	}
	return set, nil
}

// normalizeEndpoint checks a market base URL: HTTPS, or HTTP on a loopback
// host for local development; no credentials, query or fragment.
func normalizeEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("market: endpoint %q is not an absolute URL", raw)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", fmt.Errorf("market: endpoint %q must not contain credentials, a query or a fragment", raw)
	}
	if !allowedScheme(parsed) {
		return "", fmt.Errorf("market: endpoint %q must use https (http only for a loopback host)", raw)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

// allowedScheme reports whether u is HTTPS, or HTTP on a loopback host.
func allowedScheme(u *url.URL) bool {
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHost(u.Hostname())
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Enabled reports whether the site can use a market: it needs both catalog
// and package keys.
func (r TrustRoot) Enabled() bool {
	return len(r.CatalogKeys) > 0 && len(r.PackageKeys) > 0
}

// Mode returns MarketDisabled, MarketOfficial or MarketCustom.
func (r TrustRoot) Mode() string {
	switch {
	case !r.Enabled():
		return MarketDisabled
	case r.Custom:
		return MarketCustom
	default:
		return MarketOfficial
	}
}

// Host returns the market's host name, for display and logs.
func (r TrustRoot) Host() string {
	parsed, err := url.Parse(r.Endpoint)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// catalogURL is where the catalog is fetched. schema=1 asks for the catalog
// format this version understands.
func (r TrustRoot) catalogURL() string {
	return r.Endpoint + "/catalog?schema=1"
}

// allowsLoopbackHTTP reports whether the market itself is a loopback HTTP
// server, in which case its download URLs may be too.
func (r TrustRoot) allowsLoopbackHTTP() bool {
	parsed, err := url.Parse(r.Endpoint)
	return err == nil && parsed.Scheme == "http"
}
