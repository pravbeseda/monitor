// Package note bounds the notes a measurement may carry: short lines about one value, shown
// verbatim wherever that value is (ADR 0060, docs/specs/ingest.md#notes).
package note

import "github.com/pravbeseda/monitor/internal/names"

const (
	// Most is how many notes one measurement keeps.
	Most = 20
	// Longest is the most characters — Unicode code points — a note may have.
	Longest = 128
)

// Keep is what a measurement keeps of the notes it came with: the usable ones, in their
// order, at most Most of them; nil when none is left.
func Keep(notes []string) []string {
	var kept []string
	for _, text := range notes {
		if len(kept) == Most {
			break
		}
		if names.Line(text, Longest) {
			kept = append(kept, text)
		}
	}
	return kept
}
