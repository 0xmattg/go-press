// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	corecache "github.com/0xmattg/go-press/core/cache"
	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/pkg/middleware"
)

// AccountProfilePath is where a signed-in member's profile form posts. Any
// theme, theme packages included, can offer the form (see the
// accountProfile template function); core validates and stores the change.
const AccountProfilePath = user.AccountProfilePath

// ProfileBio returns a member's public bio, or "" when there is none. It
// lets themes read bios through theme.ProfileApp.
func (e *Engine) ProfileBio(userID uint) (string, error) {
	if e == nil || e.Users == nil {
		return "", nil
	}
	return e.Users.ProfileBio(userID)
}

// avatarDir is the directory under the upload directory that holds avatars
// members upload; avatarURLPrefix is its public URL.
const (
	avatarDir       = "avatars"
	avatarURLPrefix = "/static/uploads/" + avatarDir + "/"
)

// maxProfileRequestBytes bounds the whole form: the avatar plus the text
// fields.
const maxProfileRequestBytes = media.MaxAvatarBytes + 64<<10

// handleAccountProfile saves a member's display name, bio and avatar.
//
// Form fields: display_name, bio, avatar (an image file, optional),
// remove_avatar ("1" to go back to the initials) and return_to (a path on
// this site). It answers with a redirect to return_to carrying
// profile=saved, or profile_error=<code>: display_name, bio, link, avatar,
// avatar_size, or failed. The member must be signed in, active and allowed
// profile.update; the request must come from this site.
func (e *Engine) handleAccountProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !middleware.IsSameOrigin(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	account := user.CurrentUser(c)
	if account == nil {
		c.Redirect(http.StatusSeeOther, "/login?return_to="+url.QueryEscape(user.SafeReturnTo(c.Query("return_to"), "/")))
		return
	}
	if !e.CanPublicUser(c, "profile", "update") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxProfileRequestBytes)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooLarge *http.MaxBytesError
		code := "failed"
		if errors.As(err, &tooLarge) {
			code = "avatar_size"
		}
		profileRedirect(c, user.SafeReturnTo(c.Query("return_to"), "/"), "profile_error", code)
		return
	}
	returnTo := user.SafeReturnTo(c.PostForm("return_to"), "/")

	displayName, err := user.NormalizeDisplayName(c.PostForm("display_name"))
	if err == nil {
		var bio string
		bio, err = user.NormalizeBio(c.PostForm("bio"))
		if err == nil {
			e.saveAccountProfile(c, account, displayName, bio, returnTo)
			return
		}
	}
	var profileErr *user.ProfileError
	code := "failed"
	if errors.As(err, &profileErr) {
		code = profileErr.Code
	}
	profileRedirect(c, returnTo, "profile_error", code)
}

// saveAccountProfile stores the avatar file, if any, then the profile. A
// file whose profile could not be saved is removed again, and the member's
// previous uploaded avatar is removed once the new one is saved.
func (e *Engine) saveAccountProfile(c *gin.Context, account *user.User, displayName, bio, returnTo string) {
	update := user.ProfileUpdate{DisplayName: displayName, Bio: bio}
	var written string
	switch {
	case c.PostForm("remove_avatar") == "1":
		empty := ""
		update.AvatarURL = &empty
	default:
		file, err := c.FormFile("avatar")
		if err == nil {
			path, publicURL, code := e.storeAvatar(account.ID, file)
			if code != "" {
				profileRedirect(c, returnTo, "profile_error", code)
				return
			}
			written = path
			update.AvatarURL = &publicURL
		} else if !errors.Is(err, http.ErrMissingFile) {
			profileRedirect(c, returnTo, "profile_error", "avatar")
			return
		}
	}

	users := e.Users
	if users == nil {
		users = user.NewRepository(e.DB)
	}
	if err := users.UpdateProfile(account.ID, update); err != nil {
		logger.Error("Account profile could not be saved", "user", account.ID, "error", err)
		if written != "" {
			_ = os.Remove(written)
		}
		profileRedirect(c, returnTo, "profile_error", "failed")
		return
	}
	if update.AvatarURL != nil && *update.AvatarURL != account.AvatarURL {
		e.removeUploadedAvatar(account.ID, account.AvatarURL)
	}
	// Names and avatars appear on cached public pages (comments, forums).
	if e.Cache != nil {
		corecache.InvalidatePageCache(e.Cache)
	}
	profileRedirect(c, returnTo, "profile", "saved")
}

// storeAvatar processes an uploaded avatar and writes it under the upload
// directory. It returns the file path and public URL, or an error code.
func (e *Engine) storeAvatar(userID uint, header *multipart.FileHeader) (string, string, string) {
	if header.Size > media.MaxAvatarBytes {
		return "", "", "avatar_size"
	}
	f, err := header.Open()
	if err != nil {
		return "", "", "avatar"
	}
	data, err := io.ReadAll(io.LimitReader(f, media.MaxAvatarBytes+1))
	_ = f.Close()
	if err != nil {
		return "", "", "avatar"
	}
	encoded, ext, err := media.ProcessAvatar(data)
	switch {
	case errors.Is(err, media.ErrAvatarTooLarge), errors.Is(err, media.ErrAvatarSize):
		return "", "", "avatar_size"
	case err != nil:
		return "", "", "avatar"
	}
	uploads := strings.TrimSpace(e.Config.CMS.UploadDir)
	if uploads == "" {
		return "", "", "failed"
	}
	dir := filepath.Join(uploads, avatarDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Error("Avatar directory could not be created", "error", err)
		return "", "", "failed"
	}
	name, err := avatarFileName(userID, ext)
	if err != nil {
		return "", "", "failed"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		logger.Error("Avatar could not be written", "user", userID, "error", err)
		return "", "", "failed"
	}
	return path, avatarURLPrefix + name, ""
}

// profileRedirect sends the member back to returnTo with one status
// parameter, replacing any status a previous submission left in the URL.
func profileRedirect(c *gin.Context, returnTo, key, value string) {
	target, err := url.Parse(returnTo)
	if err != nil {
		target = &url.URL{Path: "/"}
	}
	q := target.Query()
	q.Del("profile")
	q.Del("profile_error")
	q.Set(key, value)
	target.RawQuery = q.Encode()
	c.Redirect(http.StatusSeeOther, target.String())
}

// avatarFileName names a member's avatar file: the user ID, so ownership is
// visible in the name, and a random part, so a new avatar gets a new URL.
func avatarFileName(userID uint, ext string) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "u" + strconv.FormatUint(uint64(userID), 10) + "-" + hex.EncodeToString(random[:]) + ext, nil
}

// removeUploadedAvatar deletes the member's previous avatar file when it is
// one they uploaded (an avatar from an identity provider is an external URL
// and stays untouched).
func (e *Engine) removeUploadedAvatar(userID uint, oldURL string) {
	name, ok := strings.CutPrefix(oldURL, avatarURLPrefix)
	if !ok || strings.ContainsAny(name, `/\`) || !strings.HasPrefix(name, "u"+strconv.FormatUint(uint64(userID), 10)+"-") {
		return
	}
	if err := os.Remove(filepath.Join(e.Config.CMS.UploadDir, avatarDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("Previous avatar could not be removed", "user", userID, "error", err)
	}
}
