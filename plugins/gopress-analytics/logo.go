package gopressanalytics

import _ "embed"

//go:embed static/logo.svg
var adminCardLogoSVG string

// LogoSVG implements plugin.LogoProvider: the inline SVG shown on the admin
// plugin card. Core sanitizes the markup before rendering it.
func (p *Plugin) LogoSVG() string { return adminCardLogoSVG }
