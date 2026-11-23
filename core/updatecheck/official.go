// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package updatecheck

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/notice"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/pkg/semver"
	"github.com/0xmattg/go-press/pkg/sign"
)

const (
	// KeyOfficialNotices holds the accepted announcement bundle.
	KeyOfficialNotices = "system.update_notices"
	// OfficialNoticeSource is the notice center source of the announcements.
	OfficialNoticeSource = "core.official"
)

// The public keys that sign official announcements, base64, with their key
// IDs: a primary and a standby, as for the market keys. Only announcements
// signed by one of them are shown. If the primary is lost or leaks, the
// project signs with the standby and a later release replaces the pair; keys
// are only ever rotated by releasing a new version of GoPress. The private
// keys are kept offline by the GoPress project and never stored on the
// update server. Test keys never go here. TestBuiltinNoticeKeysArePinned
// fails when a key here changes, so a change is always deliberate.
var builtinNoticeKeys = []string{
	"wMKHiAQYiG9OghiMivPTR6ORB9cYqAWLbHuUIp0dKV8=", // notice-a, kid 32fb4fc1f1904526 (primary)
	"XUoAGEPkGFWbpEtDSwXCtckAdze5iaDzxWnCymZIo0U=", // notice-b, kid 31ff5ee550b94dcc (standby)
}

// BuiltinNoticeKeys returns the keys official announcements are verified
// with.
func BuiltinNoticeKeys() sign.KeySet {
	set, err := sign.ParseKeySet(builtinNoticeKeys)
	if err != nil {
		// The list is pinned by a test; an invalid key is a build mistake.
		panic("updatecheck: invalid built-in announcement key: " + err.Error())
	}
	return set
}

// officialState is the stored announcement bundle. Sequence stays when the
// announcements are cleared, so an older bundle cannot come back.
type officialState struct {
	Sequence   int64          `json:"sequence"`
	ReceivedAt time.Time      `json:"received_at,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at,omitempty"`
	Notices    []RemoteNotice `json:"notices,omitempty"`
}

func (s *Service) loadOfficial() {
	s.official = officialState{}
	if s.store == nil {
		return
	}
	raw := strings.TrimSpace(s.store.Get(KeyOfficialNotices))
	if raw == "" {
		return
	}
	if err := json.Unmarshal([]byte(raw), &s.official); err != nil {
		s.official = officialState{}
	}
}

func (s *Service) persistOfficialLocked() {
	if s.store == nil {
		return
	}
	raw, err := json.Marshal(s.official)
	if err != nil {
		return
	}
	if err := s.store.Set(KeyOfficialNotices, string(raw)); err != nil {
		logger.Info("Failed to persist GoPress announcements", "error", err)
	}
}

// acceptNotices handles the announcements of the official core update
// check. No bundle clears them. A bundle that does not verify or does not
// parse is logged and the previous announcements stay; an older bundle than
// the one accepted is refused; an expired one clears them.
func (s *Service) acceptNotices(env *SignedNotices, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if env == nil {
		if len(s.official.Notices) > 0 {
			s.official = officialState{Sequence: s.official.Sequence}
			s.persistOfficialLocked()
		}
		return
	}
	if len(s.noticeKeys) == 0 {
		// No announcement keys are built into this version yet.
		return
	}
	bundle, err := ParseNotices(*env, s.noticeKeys, now)
	switch {
	case errors.Is(err, ErrNoticesExpired):
		if bundle != nil && bundle.Sequence > s.official.Sequence {
			s.official = officialState{Sequence: bundle.Sequence, ReceivedAt: now}
			s.persistOfficialLocked()
		}
		return
	case err != nil:
		logger.Warn("GoPress announcements refused; the previous ones are kept", "error", err)
		return
	}
	if bundle.Sequence < s.official.Sequence {
		logger.Warn("GoPress announcements refused: older than the bundle already accepted",
			"sequence", bundle.Sequence, "accepted", s.official.Sequence)
		return
	}
	if bundle.Sequence == s.official.Sequence {
		return
	}
	for _, reason := range bundle.Dropped {
		logger.Warn("GoPress announcement left out", "reason", reason)
	}
	s.official = officialState{Sequence: bundle.Sequence, ReceivedAt: now, ExpiresAt: bundle.ExpiresAt, Notices: bundle.Notices}
	s.persistOfficialLocked()
	logger.Info("GoPress announcements updated", "sequence", bundle.Sequence, "count", len(bundle.Notices))
}

// OfficialNotices returns the accepted announcements that apply to this
// site, as notice center notices with text in lang: not expired, and about a
// core, theme or plugin version this site runs. Upgrading past the affected
// versions makes an announcement go away. With update checks off there are
// none.
func (s *Service) OfficialNotices(lang string) []notice.Notice {
	if s == nil || !s.Enabled() {
		return nil
	}
	now := s.now()
	s.mu.Lock()
	state := s.official
	s.mu.Unlock()
	if len(state.Notices) == 0 || !now.Before(state.ExpiresAt) {
		return nil
	}
	var out []notice.Notice
	for _, n := range state.Notices {
		published, _ := time.Parse(time.RFC3339, n.PublishedAt)
		expires, err := time.Parse(time.RFC3339, n.ExpiresAt)
		if err != nil || !now.Before(expires) || !s.affected(n.Affects) {
			continue
		}
		item := notice.Notice{
			Source: OfficialNoticeSource, ID: n.ID, Revision: n.Revision,
			Category: n.Category, Severity: n.Severity,
			Title: pickText(n.Title, lang), Message: pickText(n.Message, lang),
			Snoozable: true, Acknowledgeable: true,
			RemindAfter: time.Duration(n.RemindAfterHours) * time.Hour,
			PublishedAt: published, ExpiresAt: expires,
		}
		if n.LinkURL != "" {
			item.Link = &notice.Link{URL: n.LinkURL, Label: pickText(n.LinkLabel, lang)}
		}
		out = append(out, item)
	}
	return out
}

// affected reports whether the site runs a version an announcement is
// about. No targets means every site.
func (s *Service) affected(targets []NoticeTarget) bool {
	if len(targets) == 0 {
		return true
	}
	for _, t := range targets {
		registered, ok := s.registry.Get(t.Kind, t.Slug)
		if !ok {
			continue
		}
		if match, err := semver.Satisfies(registered.CurrentVersion, t.Versions); err == nil && match {
			return true
		}
	}
	return false
}

// pickText returns the text for lang, then for its base language ("zh"
// for "zh-CN"), then English.
func pickText(texts map[string]string, lang string) string {
	if text := texts[lang]; text != "" {
		return text
	}
	if base, _, ok := strings.Cut(lang, "-"); ok {
		if text := texts[base]; text != "" {
			return text
		}
	}
	return texts["en"]
}
