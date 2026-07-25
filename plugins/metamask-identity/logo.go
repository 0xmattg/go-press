package metamaskidentity

// LogoSVG implements plugin.LogoProvider: the inline SVG shown on the admin
// plugin card, read from the already-embedded static assets. Core sanitizes the
// markup before rendering it.
func (p *Plugin) LogoSVG() string {
	data, err := publicFiles.ReadFile("static/logo.svg")
	if err != nil {
		return ""
	}
	return string(data)
}
