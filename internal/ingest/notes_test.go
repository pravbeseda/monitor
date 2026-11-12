package ingest_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// noted is the valid body with its one measurement carrying the given notes field.
func noted(notes string) string {
	return strings.Replace(strings.ReplaceAll(validBody, "%s", ""),
		`"value": 123456789}`, `"value": 123456789, "notes": `+notes+`}`, 1)
}

// spec: ingest.md#notes — notes reach storage as they came, and a field the hub cannot use
// costs the notes, never the measurement.
func TestNotesReachStorageOrAreDroppedAlone(t *testing.T) {
	cases := []struct {
		name, notes string
		want        []string
	}{
		{"two notes, in their order", `["b.service (exit-code)", "a.service (timeout)"]`, []string{"b.service (exit-code)", "a.service (timeout)"}},
		{"null", `null`, nil},
		{"an empty list", `[]`, nil},
		{"not a list of strings", `["a.service", 1]`, nil},
		{"an object", `{"a": "b"}`, nil},
		{"a blank note among them", `["a", " ", "b"]`, []string{"a", "b"}},
	}
	for _, c := range cases {
		h, store, _ := newHandler(t)
		rec := post(t, h, bearer(), noted(c.notes))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body %q", c.name, rec.Code, rec.Body.String())
		}
		if len(store.saved) != 1 || len(store.saved[0].Measurements) != 1 {
			t.Fatalf("%s: saved %+v, want the measurement", c.name, store.saved)
		}
		if got := store.saved[0].Measurements[0].Notes; !slices.Equal(got, c.want) {
			t.Errorf("%s: notes = %q, want %q", c.name, got, c.want)
		}
	}
}
