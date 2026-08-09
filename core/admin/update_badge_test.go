package admin

import (
	"bytes"
	"html/template"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/updatecheck"
	"github.com/0xmattg/go-press/core/user"
)

func TestAdminSidebarUpdateBadgeUsesOfficialGitHubReleases(t *testing.T) {
	handler := NewHandler(
		&Service{rbac: user.NewRBAC()},
		content.NewRegistry(),
		filepath.Join("templates"),
	)
	layout := filepath.Join("templates", "layouts", "admin.tmpl")
	tmpl := template.Must(template.New("").Funcs(handler.funcMap).ParseFiles(layout))
	tmpl = template.Must(tmpl.Parse(`{{define "content"}}{{end}}`))
	var output bytes.Buffer
	err := tmpl.ExecuteTemplate(&output, "admin", map[string]interface{}{
		"Title":          "Dashboard",
		"SiteName":       "Example",
		"AdminLanguage":  "en",
		"CurrentRole":    user.RoleSuperAdmin,
		"GoPressVersion": "0.6.48",
		"CoreUpdate": updatecheck.Status{
			Kind:          updatecheck.KindCore,
			Slug:          "gopress",
			LatestVersion: "0.6.52",
			Severity:      "normal",
			ReleaseURL:    updatecheck.OfficialReleasesURL,
			HasUpdate:     true,
		},
	})
	if err != nil {
		t.Fatalf("execute admin layout: %v", err)
	}
	body := output.String()
	if !strings.Contains(body, `href="https://github.com/0xmattg/go-press/releases"`) {
		t.Fatalf("badge does not link to official GitHub Releases: %s", body)
	}
	if !strings.Contains(body, "Update v0.6.52") || !strings.Contains(body, `rel="noopener noreferrer"`) {
		t.Fatalf("badge copy or safe external-link attributes missing: %s", body)
	}
}
