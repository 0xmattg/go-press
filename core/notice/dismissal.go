// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package notice

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// Ways a user dismisses a notice.
const (
	// ModeSnooze hides the notice for its RemindAfter ("×").
	ModeSnooze = "snooze"
	// ModeAcknowledge hides this revision of the notice for good
	// ("Got it").
	ModeAcknowledge = "acknowledge"
)

// MaxDismissals is how many records a user keeps; Prune drops the oldest.
const MaxDismissals = 100

// Dismissal is one user's record of dismissing one revision of a notice.
type Dismissal struct {
	Mode  string     `json:"mode"`
	At    time.Time  `json:"at"`
	Until *time.Time `json:"until,omitempty"`
}

// Dismissals maps Key(source, id, revision) to one user's dismissal of
// that revision. The admin stores it as JSON, one option per user.
type Dismissals map[string]Dismissal

// Key identifies one revision of a notice, for example
// "core.official/gp-2026-001@2".
func Key(source, id string, revision int) string {
	return source + "/" + id + "@" + strconv.Itoa(revision)
}

// KeyOf is Key for a notice.
func KeyOf(n Notice) string {
	return Key(n.Source, n.ID, n.Revision)
}

// ParseDismissals reads stored dismissals. Unreadable data and records with
// an unknown mode are ignored: at worst a notice shows again.
func ParseDismissals(raw string) Dismissals {
	d := Dismissals{}
	if raw == "" {
		return d
	}
	var stored map[string]Dismissal
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return d
	}
	for key, rec := range stored {
		switch {
		case rec.Mode == ModeAcknowledge:
			rec.Until = nil
		case rec.Mode == ModeSnooze && rec.Until != nil:
		default:
			continue
		}
		d[key] = rec
	}
	return d
}

// Hides reports whether the user dismissed this revision of n: they
// acknowledged it, or they snoozed it and now is before the end of the
// snooze.
func (d Dismissals) Hides(n Notice, now time.Time) bool {
	rec, ok := d[KeyOf(n)]
	if !ok {
		return false
	}
	if rec.Mode == ModeAcknowledge {
		return true
	}
	return rec.Until != nil && now.Before(*rec.Until)
}

// Record stores the user's dismissal of this revision of n at now. A
// snooze lasts n.RemindAfter, or DefaultRemindAfter when it is not set.
// Record does not check that n allows mode; the caller does.
func (d Dismissals) Record(n Notice, mode string, now time.Time) {
	rec := Dismissal{Mode: mode, At: now.UTC()}
	if mode == ModeSnooze {
		remind := n.RemindAfter
		if remind <= 0 {
			remind = DefaultRemindAfter
		}
		until := now.Add(remind).UTC()
		rec.Until = &until
	}
	d[KeyOf(n)] = rec
}

// Prune drops snoozes that ended by now and records of notices that are not
// in current, the notices the sources provide now (a revision that was
// replaced, or a notice that expired or was withdrawn), then keeps the
// MaxDismissals newest records.
func (d Dismissals) Prune(current []Notice, now time.Time) {
	live := make(map[string]bool, len(current))
	for _, n := range current {
		live[KeyOf(n)] = true
	}
	for key, rec := range d {
		ended := rec.Mode == ModeSnooze && (rec.Until == nil || !now.Before(*rec.Until))
		if ended || !live[key] {
			delete(d, key)
		}
	}
	if len(d) <= MaxDismissals {
		return
	}
	keys := make([]string, 0, len(d))
	for key := range d {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if !d[keys[i]].At.Equal(d[keys[j]].At) {
			return d[keys[i]].At.After(d[keys[j]].At)
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys[MaxDismissals:] {
		delete(d, key)
	}
}

// Encode returns the JSON the admin stores.
func (d Dismissals) Encode() (string, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
