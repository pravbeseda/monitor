package evaluate_test

import (
	"slices"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
)

func expectNotes(t *testing.T, messages []evaluate.Message, want ...string) {
	t.Helper()
	if len(messages) != 1 {
		t.Fatalf("delivered %+v, want one message", messages)
	}
	if got := messages[0].Notes; !slices.Equal(got, want) {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

// spec: evaluation.md#messages — a message carries the notes its value came with.
func TestAMessageCarriesTheNotesOfItsValue(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3), "a.service (exit-code)", "b.service (timeout)")
	expectNotes(t, delivered(t, db, tick, watching(t)), "a.service (exit-code)", "b.service (timeout)")
}

// spec: evaluation.md#messages — a change delivered on a later tick, after the series' notes
// changed, carries the notes recorded with the change.
func TestARetriedMessageCarriesTheNotesOfItsChange(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3), "a.service (exit-code)")
	pass(t, evaluatorWith(db, &recorder{failing: map[string]bool{"/": true}}, tick, watching(t)))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(3), "b.service (exit-code)")
	expectNotes(t, delivered(t, db, later, watching(t)), "a.service (exit-code)")
}

// spec: evaluation.md#messages — a repeat of a standing critical carries the notes of the
// value it stands on now.
func TestARepeatCarriesTheNotesOfTheValueNow(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3), "a.service (exit-code)")
	pass(t, evaluator(db, tick, watching(t)))

	due := tick.Add(24 * time.Hour)
	collect(t, db, due, volume("/"), gb(3), "b.service (exit-code)")
	expectNotes(t, delivered(t, db, due, watching(t)), "b.service (exit-code)")
}

// spec: evaluation.md#persistence-and-restart — the value and the level unchanged, the notes
// changed: no event and no message.
func TestChangedNotesAloneMoveNothing(t *testing.T) {
	db := open(t)
	collect(t, db, tick, volume("/"), gb(3), "a.service (exit-code)")
	pass(t, evaluator(db, tick, watching(t)))
	before := len(logged(t, db))

	later := tick.Add(time.Minute)
	collect(t, db, later, volume("/"), gb(3), "b.service (exit-code)")
	if got := delivered(t, db, later, watching(t)); len(got) != 0 {
		t.Fatalf("changed notes delivered %+v", got)
	}
	if after := len(logged(t, db)); after != before {
		t.Fatalf("changed notes wrote %d event(s)", after-before)
	}
}

// spec: evaluation.md#messages — a digest entry for a subject standing in warning with no
// transition of its own carries the notes of now.
// spec: evaluation.md#digest — a subject that moved into warning and stands there is listed
// once, with the value and the notes of its move.
func TestDigestEntriesCarryTheNotesOfTheirValues(t *testing.T) {
	db := open(t)
	// Both before yesterday's 09:00, so no digest closes the window in between: the first
	// pass opens it, and /old's move is the instant it opens at, outside it.
	collect(t, db, yesterday.Add(-3*time.Hour), volume("/old"), gb(9), "old-then")
	pass(t, evaluator(db, yesterday.Add(-3*time.Hour), watching(t)))
	collect(t, db, yesterday.Add(-time.Hour), volume("/moved"), gb(9), "moved-then")
	pass(t, evaluator(db, yesterday.Add(-time.Hour), watching(t)))

	channel := &recorder{}
	at := occurrence.Add(3 * time.Hour)
	collect(t, db, at, volume("/old"), gb(8), "old-now")
	collect(t, db, at, volume("/moved"), gb(8), "moved-now")
	pass(t, digesting(db, channel, at, watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 2 {
		t.Fatalf("the digest went out as %v", summaries)
	}
	want := map[string]struct {
		note  string
		value float64
	}{"/moved": {"moved-then", gb(9)}, "/old": {"old-now", gb(8)}}
	for _, entry := range summaries[0] {
		expected := want[entry.Labels["mount"]]
		if got := entry.Notes; len(got) != 1 || got[0] != expected.note {
			t.Errorf("entry %s carries %q, want %q", entry.Labels["mount"], got, expected.note)
		}
		if got := entry.Readings["disk.free_bytes"]; got != expected.value {
			t.Errorf("entry %s reads %v, want %v", entry.Labels["mount"], got, expected.value)
		}
	}
}
