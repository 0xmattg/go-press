// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package user

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// ProfileBioMetaKey is the user meta key of a member's public bio.
const ProfileBioMetaKey = "profile_bio"

// AccountProfilePath is core's endpoint for a signed-in member's profile
// form. It answers with a redirect carrying profile=saved or
// profile_error=<one of ProfileErrorCodes>.
const AccountProfilePath = "/account/profile"

// ProfileErrorCodes are the profile_error values the profile endpoint
// reports.
var ProfileErrorCodes = []string{"display_name", "bio", "link", "avatar", "avatar_size", "failed"}

// ProfileResult reads the outcome of the last profile submission from the
// request it redirected to: saved, or an error code from ProfileErrorCodes
// (an unknown code reads as "failed").
func ProfileResult(r *http.Request) (saved bool, code string) {
	if r == nil || r.URL == nil {
		return false, ""
	}
	q := r.URL.Query()
	if code = strings.TrimSpace(q.Get("profile_error")); code != "" {
		for _, known := range ProfileErrorCodes {
			if code == known {
				return false, code
			}
		}
		return false, "failed"
	}
	return q.Get("profile") == "saved", ""
}

// AccountProfileView is what a theme needs to offer the signed-in member a
// profile form (the accountProfile template function).
type AccountProfileView struct {
	// FormURL is where the form posts (multipart/form-data).
	FormURL string
	// CanEdit reports whether the member may edit (profile.update).
	CanEdit bool
	// DisplayName, AvatarURL and Bio are the current values.
	DisplayName string
	AvatarURL   string
	Bio         string
	// Saved and Error report the last submission: Error is "" or one of
	// ProfileErrorCodes.
	Saved bool
	Error string
	// Limits the form can show and enforce: characters of the display name
	// and the bio, megabytes and media types of the avatar.
	MaxDisplayName int
	MaxBio         int
	MaxAvatarMB    int
	AvatarAccept   string
}

// Limits of the fields a member edits on their public profile.
const (
	MaxDisplayNameRunes = 50
	MaxBioRunes         = 160
)

// Profile validation errors. Their Code is what the profile endpoint reports
// to the theme.
var (
	ErrProfileDisplayName = &ProfileError{Code: "display_name"}
	ErrProfileBio         = &ProfileError{Code: "bio"}
	ErrProfileLink        = &ProfileError{Code: "link"}
)

// ProfileError is a profile field that cannot be saved.
type ProfileError struct{ Code string }

func (e *ProfileError) Error() string { return "user: invalid profile " + e.Code }

// NormalizeDisplayName returns the display name to store: spaces trimmed and
// runs of whitespace collapsed. It must have 1 to MaxDisplayNameRunes
// characters, no control characters and no link.
func NormalizeDisplayName(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	n := utf8.RuneCountInString(s)
	if n == 0 || n > MaxDisplayNameRunes || hasControl(s, false) {
		return "", ErrProfileDisplayName
	}
	if hasLink(s) {
		return "", ErrProfileLink
	}
	return s, nil
}

// NormalizeBio returns the bio to store: plain text of at most MaxBioRunes
// characters, lines trimmed, at most two consecutive line breaks, no control
// characters other than line breaks, and no link. An empty bio is allowed.
func NormalizeBio(s string) (string, error) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if utf8.RuneCountInString(s) > MaxBioRunes || hasControl(s, true) {
		return "", ErrProfileBio
	}
	if hasLink(s) {
		return "", ErrProfileLink
	}
	return s, nil
}

// hasLink reports whether s contains something a browser or a reader would
// take for a web address: a scheme separator or a "www." host.
func hasLink(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "://") || strings.Contains(lower, "www.") ||
		strings.Contains(lower, "http:") || strings.Contains(lower, "https:")
}

func hasControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if allowNewline && r == '\n' {
			continue
		}
		// Bidirectional controls can make a name read backwards, a common
		// way to impersonate someone.
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) || r == '\u2028' || r == '\u2029' || r == '\ufeff' {
			return true
		}
	}
	return false
}

// ProfileUpdate is what a member changes on their profile. A nil AvatarURL
// leaves the avatar as it is; an empty one removes it.
type ProfileUpdate struct {
	DisplayName string
	Bio         string
	AvatarURL   *string
}

// UpdateProfile stores a member's display name, bio and, when given, avatar
// in one transaction. The values must already be normalized.
func (r *Repository) UpdateProfile(userID uint, p ProfileUpdate) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		fields := map[string]interface{}{"display_name": p.DisplayName}
		if p.AvatarURL != nil {
			fields["avatar_url"] = *p.AvatarURL
		}
		res := tx.Model(&User{}).Where("id = ?", userID).Updates(fields)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		repo := NewRepository(tx)
		if p.Bio == "" {
			return repo.DeleteMeta(userID, ProfileBioMetaKey)
		}
		return repo.SaveMeta(userID, ProfileBioMetaKey, p.Bio)
	})
}

// ProfileBio returns a member's public bio, or "" when there is none.
func (r *Repository) ProfileBio(userID uint) (string, error) {
	var meta UserMeta
	err := r.db.Where("user_id = ? AND meta_key = ?", userID, ProfileBioMetaKey).First(&meta).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return meta.MetaValue, err
}
