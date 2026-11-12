package note_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/note"
)

// spec: ingest.md#notes — what the hub cannot use is dropped alone, the rest kept in order.
func TestKeepDropsWhatCannotBeUsed(t *testing.T) {
	cyrillic := strings.Repeat("я", note.MaxLength)
	cases := []struct {
		name  string
		notes []string
		want  []string
	}{
		{"none", nil, nil},
		{"empty list", []string{}, nil},
		{"usable, in their order", []string{"b.service (exit-code)", "a.service (timeout)"}, []string{"b.service (exit-code)", "a.service (timeout)"}},
		{"empty and white space alone", []string{"", " \t", "a"}, []string{"a"}},
		{"control, format and line-separating characters", []string{"a\nb", "a\u200fb", "a\u2028b", "a\u2029b", "c"}, []string{"c"}},
		{"128 code points kept, 129 dropped", []string{cyrillic, cyrillic + "я"}, []string{cyrillic}},
		{"spaces around the text kept", []string{" a "}, []string{" a "}},
		{"identical notes both kept", []string{"a", "a"}, []string{"a", "a"}},
		{"invalid UTF-8", []string{"a\xffb", "c"}, []string{"c"}},
	}
	for _, c := range cases {
		if got := note.Keep(c.notes); !slices.Equal(got, c.want) {
			t.Errorf("%s: Keep = %q, want %q", c.name, got, c.want)
		}
	}
}

// spec: ingest.md#notes — the first 20 are kept, counted after unusable ones are dropped.
func TestKeepTakesTheFirstTwentyUsable(t *testing.T) {
	var notes []string
	for i := range note.MaxCount + 1 {
		notes = append(notes, string(rune('a'+i)))
	}
	if got := note.Keep(notes); !slices.Equal(got, notes[:note.MaxCount]) {
		t.Errorf("21 notes: Keep = %q, want the first 20", got)
	}
	notes[1] = " "
	want := append([]string{notes[0]}, notes[2:]...)
	if got := note.Keep(notes); !slices.Equal(got, want) {
		t.Errorf("21 notes, the second blank: Keep = %q, want %q", got, want)
	}
}
