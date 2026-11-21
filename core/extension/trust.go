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

// The official market's public keys, base64, with their key IDs. Each set
// holds a primary key and a standby: if the primary is lost or leaks, the
// market switches to the standby and a later release replaces the pair. Keys
// are only ever rotated by releasing a new version of GoPress.
//
// Catalog keys sign the catalog the market serves; package keys countersign
// each reviewed package. The private keys never leave the market operator's
// machines. Test keys never go here: anyone holding a test private key could
// sign catalogs and packages. TestBuiltinKeysArePinned fails when a key here
// changes, so a change is always deliberate.
var (
	builtinCatalogKeys = []string{
		"T5+UAW5tNmH5l418ZPzNOc7iKTXHu1MPHBcY/h9QdEs=", // catalog-a, kid c43f20fbbb0fa6b8 (primary)
		"oqJsAwsafOqEEWl5Bge1JkG1h9tRG/TphKFg7C4KfIQ=", // catalog-b, kid 0007fc76614ef75d (standby)
	}
	builtinPackageKeys = []string{
		"Zxxsu3WMgKKPVdgQicggdO7RsDv1oV8M4KXspMkhug4=", // market-pkg-a, kid c3753561967aac02 (primary)
		"62jw/Cm37qgKWp7jSw7Afmwhq61Sc9dYExn0QGW5qVU=", // market-pkg-b, kid 1f277e92513c8b04 (standby)
	}
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

// UpdatesURL returns the market's update check endpoint, which answers the
// same protocol as the core update check (core/updatecheck) for the
// packages a site installed from it.
func (r TrustRoot) UpdatesURL() string {
	return r.Endpoint + "/updates/check"
}

// allowsLoopbackHTTP reports whether the market itself is a loopback HTTP
// server, in which case its download URLs may be too.
func (r TrustRoot) allowsLoopbackHTTP() bool {
	parsed, err := url.Parse(r.Endpoint)
	return err == nil && parsed.Scheme == "http"
}
