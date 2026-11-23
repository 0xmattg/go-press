// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"  Matt  ":     "Matt",
		"Matt \t  Gao": "Matt Gao",
		"林 · Lin":      "林 · Lin",
		strings.Repeat("名", MaxDisplayNameRunes): strings.Repeat("名", MaxDisplayNameRunes),
	} {
		got, err := NormalizeDisplayName(in)
		if err != nil || got != want {
			t.Errorf("NormalizeDisplayName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]error{
		"":    ErrProfileDisplayName,
		"   ": ErrProfileDisplayName,
		strings.Repeat("a", MaxDisplayNameRunes+1): ErrProfileDisplayName,
		"Matt\x00":            ErrProfileDisplayName,
		"Matt\u202e":          ErrProfileDisplayName, // reads backwards
		"Visit www.spam.test": ErrProfileLink,
		"https://spam.test":   ErrProfileLink,
		"HTTP://SPAM.TEST":    ErrProfileLink,
		"ftp://files.test":    ErrProfileLink,
		"Matt (http:spam)":    ErrProfileLink,
	} {
		_, err := NormalizeDisplayName(in)
		if want == nil {
			continue
		}
		if !errors.Is(err, want) {
			t.Errorf("NormalizeDisplayName(%q) error = %v, want %v", in, err, want)
		}
	}
}

func TestNormalizeBio(t *testing.T) {
	got, err := NormalizeBio("\r\n  I build   themes.\r\n\r\n\r\n\r\nGo and  CSS.  \n")
	if err != nil || got != "I build themes.\n\nGo and CSS." {
		t.Fatalf("bio = %q, %v", got, err)
	}
	if got, err := NormalizeBio("   "); err != nil || got != "" {
		t.Fatalf("empty bio = %q, %v", got, err)
	}
	if _, err := NormalizeBio(strings.Repeat("字", MaxBioRunes)); err != nil {
		t.Fatalf("a bio of the maximum length: %v", err)
	}
	for in, want := range map[string]error{
		strings.Repeat("字", MaxBioRunes+1):       ErrProfileBio,
		"tab\tinside is fine but bell \a is not": ErrProfileBio,
		"see https://example.com":                ErrProfileLink,
		"see WWW.example.com":                    ErrProfileLink,
		"mail me: http:me":                       ErrProfileLink,
	} {
		if _, err := NormalizeBio(in); !errors.Is(err, want) {
			t.Errorf("NormalizeBio(%q) error = %v, want %v", in, err, want)
		}
	}
	var profileErr *ProfileError
	if _, err := NormalizeBio("https://x.test"); !errors.As(err, &profileErr) || profileErr.Code != "link" {
		t.Fatalf("error code = %v", err)
	}
}

func TestUpdateProfileStoresNameBioAndAvatar(t *testing.T) {
	db, repo, _ := publicSessionTestStores(t)
	if err := db.Exec(`CREATE TABLE gp_user_meta (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		meta_key TEXT NOT NULL,
		meta_value TEXT
	)`).Error; err != nil {
		t.Fatal(err)
	}
	avatar := "/static/uploads/avatars/u1-abc.png"
	if err := repo.UpdateProfile(1, ProfileUpdate{DisplayName: "Matt", Bio: "Themes and Go.", AvatarURL: &avatar}); err != nil {
		t.Fatal(err)
	}
	account, err := repo.FindByID(1)
	if err != nil || account.DisplayName != "Matt" || account.AvatarURL != avatar {
		t.Fatalf("account = %+v, %v", account, err)
	}
	if bio, err := repo.ProfileBio(1); err != nil || bio != "Themes and Go." {
		t.Fatalf("bio = %q, %v", bio, err)
	}

	// No avatar given: unchanged. An empty bio removes it.
	if err := repo.UpdateProfile(1, ProfileUpdate{DisplayName: "Matt G", Bio: ""}); err != nil {
		t.Fatal(err)
	}
	account, _ = repo.FindByID(1)
	if account.DisplayName != "Matt G" || account.AvatarURL != avatar {
		t.Fatalf("account after the second update = %+v", account)
	}
	if bio, err := repo.ProfileBio(1); err != nil || bio != "" {
		t.Fatalf("bio after removal = %q, %v", bio, err)
	}
	if err := repo.UpdateProfile(99, ProfileUpdate{DisplayName: "Nobody"}); err == nil {
		t.Fatal("updated a user that does not exist")
	}
}
