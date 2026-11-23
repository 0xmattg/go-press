// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/user"
)

// profileTestApp lets members with role "subscriber" edit and serves bios.
type profileTestApp struct {
	dataTestApp
	bioCalls int
}

func (a *profileTestApp) CanPublicUser(c *gin.Context, resource, action string) bool {
	account := user.CurrentUser(c)
	return account != nil && resource == "profile" && action == "update" && account.Role == user.RoleSubscriber
}

func (a *profileTestApp) ProfileBio(userID uint) (string, error) {
	a.bioCalls++
	return "Bio of " + map[uint]string{7: "matt"}[userID], nil
}

func accountProfileFunc(t *testing.T, app App) func(interface{}) *user.AccountProfileView {
	t.Helper()
	b := &BaseTheme{}
	b.InitBase(app, "", "", nil)
	fn, ok := b.BaseFuncMap()["accountProfile"].(func(interface{}) *user.AccountProfileView)
	if !ok {
		t.Fatalf("accountProfile has type %T", b.BaseFuncMap()["accountProfile"])
	}
	return fn
}

func profileContext(target string, account *user.User) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", target, nil)
	if account != nil {
		c.Set(user.CtxKeyPublicUser, account)
	}
	return c
}

func TestAccountProfileGivesTheFormItsData(t *testing.T) {
	app := &profileTestApp{}
	profile := accountProfileFunc(t, app)

	if got := profile(profileContext("/profile", nil)); got != nil {
		t.Fatalf("visitor = %+v", got)
	}
	matt := &user.User{ID: 7, Username: "matt", DisplayName: "Matt", AvatarURL: "/static/uploads/avatars/u7-a.png", Role: user.RoleSubscriber, IsActive: true}
	c := profileContext("/profile?profile=saved", matt)
	got := profile(map[string]interface{}{"Ctx": c})
	want := user.AccountProfileView{
		FormURL: "/account/profile", CanEdit: true, DisplayName: "Matt", AvatarURL: "/static/uploads/avatars/u7-a.png", Bio: "Bio of matt",
		Saved: true, MaxDisplayName: 50, MaxBio: 160, MaxAvatarMB: 5, AvatarAccept: "image/jpeg,image/png,image/gif",
	}
	if got == nil || *got != want {
		t.Fatalf("profile = %+v, want %+v", got, want)
	}
	// Computed once per request.
	if again := profile(c); again != got || app.bioCalls != 1 {
		t.Fatalf("second call: same %v, bio calls %d", again == got, app.bioCalls)
	}

	// Errors: a known code is kept, an unknown one reads as "failed".
	if got := profile(profileContext("/profile?profile_error=link", matt)); got.Error != "link" || got.Saved {
		t.Fatalf("link error = %+v", got)
	}
	if got := profile(profileContext("/profile?profile_error=%3Cscript%3E", matt)); got.Error != "failed" {
		t.Fatalf("unknown error = %+v", got)
	}

	// A role without profile.update sees the data but may not edit; an
	// inactive account gets nothing; a blank name falls back to the username.
	editor := &user.User{ID: 8, Username: "eve", Role: user.RoleEditor, IsActive: true}
	if got := profile(profileContext("/profile", editor)); got == nil || got.CanEdit || got.DisplayName != "eve" {
		t.Fatalf("role without profile.update = %+v", got)
	}
	if got := profile(profileContext("/profile", &user.User{ID: 9, Username: "off", IsActive: false})); got != nil {
		t.Fatalf("inactive account = %+v", got)
	}

	// Without the optional interfaces: no editing and no bio, but no panic.
	plain := accountProfileFunc(t, &dataTestApp{})
	if got := plain(profileContext("/profile", matt)); got == nil || got.CanEdit || got.Bio != "" {
		t.Fatalf("plain app = %+v", got)
	}
	if got := CommonFuncMap()["accountProfile"].(func(interface{}) *user.AccountProfileView)(c); got != nil {
		t.Fatalf("default accountProfile = %+v", got)
	}
}

// Theme packages are checked with TemplateCheckFuncs: templates that use
// the account functions must parse there too.
func TestPackageCheckKnowsTheAccountFunctions(t *testing.T) {
	funcs := TemplateCheckFuncs()
	for _, name := range []string{"accountProfile", "accountActions", "userBadges"} {
		if funcs[name] == nil {
			t.Errorf("the package checker lacks %s", name)
		}
	}
}
