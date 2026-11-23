// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/user"
)

// profileMember creates a member with a front-end session on the site and
// returns the account and its session cookie.
func profileMember(t *testing.T, e *Engine, name, role string) (*user.User, *http.Cookie) {
	t.Helper()
	account := &user.User{Username: name, DisplayName: name, Role: role, IsActive: true,
		AvatarURL: "https://lh3.googleusercontent.test/" + name}
	if err := e.Users.Create(account); err != nil {
		t.Fatal(err)
	}
	token, err := user.NewSessionManager(e.UserSessions, e.Users, time.Hour).Create(context.Background(), account.ID, user.SessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	return account, &http.Cookie{Name: user.PublicSessionCookie, Value: token.Token}
}

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: 90, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// postProfile submits the profile form as a browser on the site would.
func postProfile(t *testing.T, e *Engine, cookie *http.Cookie, origin string, fields map[string]string, avatar []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = form.WriteField(k, v)
	}
	if avatar != nil {
		part, err := form.CreateFormFile("avatar", "me.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(avatar)
	}
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, AccountProfilePath, &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", origin)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	e.Router.ServeHTTP(w, req)
	return w
}

func redirectQuery(t *testing.T, w *httptest.ResponseRecorder) (string, url.Values) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, body %q", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return loc.Path, loc.Query()
}

func TestPGMembersEditTheirProfile(t *testing.T) {
	e := packageSiteEngine(t, samplePackageDir(t, nil))
	const site = "http://example.com"
	member, cookie := profileMember(t, e, "matt", user.RoleSubscriber)
	fields := map[string]string{"display_name": "  Matt  G ", "bio": "I build themes.\n\nGo and CSS.", "return_to": "/profile?profile_error=bio"}

	// Not signed in, or from another site: nothing changes.
	if w := postProfile(t, e, nil, site, fields, nil); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Fatalf("signed out = %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := postProfile(t, e, cookie, "https://evil.test", fields, nil); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site = %d", w.Code)
	}
	// A role without profile.update is refused.
	e.RBAC.RegisterRole("restricted", "Restricted", 5, map[string]bool{"profile.read_own": true})
	_, restrictedCookie := profileMember(t, e, "kim", "restricted")
	if w := postProfile(t, e, restrictedCookie, site, fields, nil); w.Code != http.StatusForbidden {
		t.Fatalf("role without profile.update = %d", w.Code)
	}

	// Name, bio and avatar are saved; the old status parameter is replaced.
	path, q := redirectQuery(t, postProfile(t, e, cookie, site, fields, avatarPNG(t)))
	if path != "/profile" || q.Get("profile") != "saved" || q.Get("profile_error") != "" {
		t.Fatalf("saved redirect = %s %v", path, q)
	}
	saved, err := e.Users.FindByID(member.ID)
	if err != nil || saved.DisplayName != "Matt G" || !strings.HasPrefix(saved.AvatarURL, "/static/uploads/avatars/u") {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if bio, err := e.Users.ProfileBio(member.ID); err != nil || bio != "I build themes.\n\nGo and CSS." {
		t.Fatalf("bio = %q, %v", bio, err)
	}
	first := saved.AvatarURL
	w := httptest.NewRecorder()
	e.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, first, nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("avatar served %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes())); err != nil || cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("avatar %+v, %v", cfg, err)
	}

	// A link in the bio is refused and nothing changes.
	bad := map[string]string{"display_name": "Matt", "bio": "see https://spam.test", "return_to": "/profile"}
	if _, q := redirectQuery(t, postProfile(t, e, cookie, site, bad, nil)); q.Get("profile_error") != "link" {
		t.Fatalf("link = %v", q)
	}
	if again, _ := e.Users.FindByID(member.ID); again.DisplayName != "Matt G" {
		t.Fatalf("a refused form changed the name: %q", again.DisplayName)
	}
	// Something that is not an image is refused.
	if _, q := redirectQuery(t, postProfile(t, e, cookie, site, fields, []byte("<svg/>"))); q.Get("profile_error") != "avatar" {
		t.Fatalf("not an image = %v", q)
	}

	// A new avatar replaces the file of the previous one.
	redirectQuery(t, postProfile(t, e, cookie, site, fields, avatarPNG(t)))
	second, _ := e.Users.FindByID(member.ID)
	firstFile := filepath.Join(e.Config.CMS.UploadDir, "avatars", strings.TrimPrefix(first, "/static/uploads/avatars/"))
	if second.AvatarURL == first {
		t.Fatal("a new upload kept the old URL")
	}
	if _, err := os.Stat(firstFile); !os.IsNotExist(err) {
		t.Fatalf("the previous avatar file is still there: %v", err)
	}

	// Removing the avatar clears it and deletes the file.
	remove := map[string]string{"display_name": "Matt G", "bio": "", "remove_avatar": "1", "return_to": "/profile"}
	redirectQuery(t, postProfile(t, e, cookie, site, remove, nil))
	cleared, _ := e.Users.FindByID(member.ID)
	secondFile := filepath.Join(e.Config.CMS.UploadDir, "avatars", strings.TrimPrefix(second.AvatarURL, "/static/uploads/avatars/"))
	if cleared.AvatarURL != "" {
		t.Fatalf("avatar after removal = %q", cleared.AvatarURL)
	}
	if _, err := os.Stat(secondFile); !os.IsNotExist(err) {
		t.Fatalf("the removed avatar file is still there: %v", err)
	}
	if bio, _ := e.Users.ProfileBio(member.ID); bio != "" {
		t.Fatalf("bio after clearing = %q", bio)
	}

	// An external return_to falls back to the home page.
	evil := map[string]string{"display_name": "Matt", "return_to": "https://evil.test/"}
	if path, _ := redirectQuery(t, postProfile(t, e, cookie, site, evil, nil)); path != "/" {
		t.Fatalf("external return_to = %s", path)
	}
}
