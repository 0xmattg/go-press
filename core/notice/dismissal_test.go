// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package notice

import (
	"fmt"
	"testing"
	"time"
)

func normalized(t *testing.T, in ...Notice) []Notice {
	t.Helper()
	out, problems := Normalize(in, testNow)
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	return out
}

func TestAcknowledgeHidesOnlyThatRevision(t *testing.T) {
	n := normalized(t, valid("core.official", "gp-2026-001"))[0]
	d := Dismissals{}
	d.Record(n, ModeAcknowledge, testNow)
	if !d.Hides(n, testNow.Add(365*24*time.Hour)) {
		t.Fatal("an acknowledged notice must stay hidden")
	}
	n.Revision = 2
	if d.Hides(n, testNow) {
		t.Fatal("a new revision must show again")
	}
}

func TestSnoozeHidesForRemindAfter(t *testing.T) {
	n := valid("commerce", "gateway")
	n.RemindAfter = 2 * time.Hour
	n = normalized(t, n)[0]
	d := Dismissals{}
	d.Record(n, ModeSnooze, testNow)
	if !d.Hides(n, testNow.Add(time.Hour)) {
		t.Fatal("hidden during the snooze")
	}
	if d.Hides(n, testNow.Add(2*time.Hour)) {
		t.Fatal("shown again when the snooze ends")
	}
}

func TestDismissalsRoundTrip(t *testing.T) {
	ack := normalized(t, valid("core.official", "a"))[0]
	snooze := normalized(t, valid("commerce", "b"))[0]
	d := Dismissals{}
	d.Record(ack, ModeAcknowledge, testNow)
	d.Record(snooze, ModeSnooze, testNow)
	raw, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back := ParseDismissals(raw)
	if !back.Hides(ack, testNow) || !back.Hides(snooze, testNow) || back.Hides(snooze, testNow.Add(25*time.Hour)) {
		t.Fatalf("round trip lost data: %s", raw)
	}
	if _, ok := back["core.official/a@1"]; !ok {
		t.Fatalf("keys = %v, want source/id@revision", back)
	}
}

func TestParseDismissalsIgnoresBadData(t *testing.T) {
	if d := ParseDismissals("{not json"); len(d) != 0 {
		t.Fatalf("bad JSON -> %v", d)
	}
	d := ParseDismissals(`{"a/b@1":{"mode":"forever","at":"2026-11-23T08:00:00Z"},"a/c@1":{"mode":"snooze","at":"2026-11-23T08:00:00Z"},"a/d@1":{"mode":"acknowledge","at":"2026-11-23T08:00:00Z"}}`)
	if len(d) != 1 {
		t.Fatalf("parsed = %v, want only the acknowledgement (unknown mode and snooze without an end are dropped)", d)
	}
}

func TestPruneDropsEndedAndStaleRecords(t *testing.T) {
	current := normalized(t, valid("core.official", "live"), valid("commerce", "snoozed"))
	d := Dismissals{}
	d.Record(current[0], ModeAcknowledge, testNow)
	d.Record(current[1], ModeSnooze, testNow)
	d[Key("core.official", "live", 0)] = Dismissal{Mode: ModeAcknowledge, At: testNow}
	d[Key("gone", "withdrawn", 1)] = Dismissal{Mode: ModeAcknowledge, At: testNow}

	d.Prune(current, testNow.Add(25*time.Hour))
	if len(d) != 1 {
		t.Fatalf("after prune = %v, want only the live acknowledgement", d)
	}
	if _, ok := d[KeyOf(current[0])]; !ok {
		t.Fatalf("after prune = %v", d)
	}
}

func TestPruneKeepsNewestHundred(t *testing.T) {
	var in []Notice
	for i := 0; i < 12; i++ {
		for j := 0; j < 10; j++ {
			in = append(in, valid(fmt.Sprintf("source-%d", i), fmt.Sprintf("n%d", j)))
		}
	}
	current := normalized(t, in...)
	d := Dismissals{}
	for i, n := range current {
		d.Record(n, ModeAcknowledge, testNow.Add(time.Duration(i)*time.Minute))
	}
	d.Prune(current, testNow)
	if len(d) != MaxDismissals {
		t.Fatalf("kept %d, want %d", len(d), MaxDismissals)
	}
	if _, ok := d[KeyOf(current[0])]; ok {
		t.Fatal("the oldest record must be dropped first")
	}
	if _, ok := d[KeyOf(current[len(current)-1])]; !ok {
		t.Fatal("the newest record must be kept")
	}
}
