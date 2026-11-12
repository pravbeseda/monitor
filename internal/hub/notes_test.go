package hub_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

func noted(s state.Subject, notes ...string) state.Subject {
	s.Notes = notes
	return s
}

// spec: overview.md#computer-page — a tile's notes stand under its values and its usual
// value, muted, one to a line, a stale value's included.
func TestATileListsTheNotesOfItsValues(t *testing.T) {
	two := []string{"a.service (exit-code)", "b.service (timeout)"}
	tileBody := func(s state.Subject) string {
		t.Helper()
		return tileOf(t, computer(t, []state.Subject{s}, recent(bareRef("systemd.failed_units")), logbook{}, ""), "Failed units").body
	}

	one := tileBody(noted(plain("server-b", "systemd.failed_units", 2), two...))
	inOrder(t, one, ">2<", `<span class="tile-note">a.service (exit-code)</span>`, `<span class="tile-note">b.service (timeout)</span>`)

	if none := tileBody(plain("server-b", "systemd.failed_units", 0)); strings.Contains(none, "tile-note") {
		t.Errorf("a value with no notes lists some: %s", none)
	}
	containsAll(t, "stale", tileBody(staled(noted(plain("server-b", "systemd.failed_units", 2), two...))),
		`<span class="tile-note">a.service (exit-code)</span>`)
	inOrder(t, tileBody(banded(noted(plain("server-b", "systemd.failed_units", 2), two...), 0, 0, 0, 1)),
		"usually 0", `<span class="tile-note">a.service (exit-code)</span>`)

	destination := noted(reading("server-b", "timemachine.backup_age_seconds", map[string]string{"destination": "nas"}, 7200), "n-1")
	dest := storage.SeriesRef{Node: "server-b", Metric: "timemachine.backup_age_seconds", Labels: destination.Labels}
	generic := tileOf(t, computer(t, []state.Subject{destination}, recent(dest), logbook{}, ""), "timemachine.backup_age_seconds destination=nas").body
	inOrder(t, generic, ">2.0 h<", `<span class="tile-note">n-1</span>`)
}

// spec: overview.md#computer-page — a note too wide for its tile breaks inside it.
func TestANoteBreaksInsideItsTile(t *testing.T) {
	body := computer(t, []state.Subject{noted(plain("server-b", "systemd.failed_units", 1), strings.Repeat("x", 100))},
		recent(bareRef("systemd.failed_units")), logbook{}, "")
	containsAll(t, "style", body, ".tile-note { display: block; color: var(--muted); font-size: 0.75rem; overflow-wrap: anywhere; }")
}

// spec: overview.md#computer-card — the card's head counts the failed units and shows no
// notes.
func TestTheCardHeadShowsNoNotes(t *testing.T) {
	body := computerCard(t, stateOf(replaced(linux("server-b"), noted(plain("server-b", "systemd.failed_units", 2), "a.service (exit-code)"))...), limits{}, "server-b")
	if found := failedPattern.FindStringSubmatch(body); found == nil || found[2] != "failed units: 2" {
		t.Fatalf("head = %v, want failed units: 2", found)
	}
	if strings.Contains(body, "a.service") {
		t.Errorf("the card shows a note")
	}
}

// spec: timeline.md#changes — a transition recorded with notes shows its value, then its
// notes as a message writes them.
func TestAChangeShowsTheNotesOfItsValue(t *testing.T) {
	store := timelineStore(reportingNode("server-b"))
	change := storage.Transition{
		Subject: storage.Subject{Node: "server-b", Metric: "systemd.failed_units"},
		At:      lastSeen.Add(-time.Hour), From: "ok", To: "critical", FromSince: lastSeen.Add(-2 * time.Hour),
		Readings: map[string]float64{"systemd.failed_units": 2},
		Notes:    []string{"a.service (exit-code)", "b.service (timeout)"},
	}
	store.events = []storage.Transition{change}
	_, got := changesOf(showTimeline(t, store, "/timeline"))
	if len(got) != 1 || !strings.HasSuffix(got[0], "ok → critical 2: a.service (exit-code), b.service (timeout)") {
		t.Fatalf("changes = %q, want the value then its notes", got)
	}
}

// spec: attention.md#items — an item's value is followed by the notes it came with.
func TestAnItemShowsTheNotesOfItsValue(t *testing.T) {
	units := noted(judgedAt(reading("server-b", "systemd.failed_units", nil, 2), evaluate.Critical), "a.service (exit-code)", "b.service (timeout)")
	body := showAttention(t, stateOf(silenceOf("server-b", evaluate.OK), units), "/timeline")
	containsAll(t, "the level item", oneItem(t, body), "2: a.service (exit-code), b.service (timeout)")
}
