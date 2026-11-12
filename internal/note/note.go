// Package note bounds the notes a measurement may carry: short lines about one value, shown
// verbatim wherever that value is (ADR 0060, docs/specs/ingest.md#notes).
package note

import "github.com/pravbeseda/monitor/internal/names"

const (
	// MaxCount is how many notes one measurement keeps.
	MaxCount = 20
	// MaxLength is the most characters — Unicode code points — a note may have.
	MaxLength = 128
)

// Keep is what a measurement keeps of the notes it came with: the usable ones, in their
// order, at most MaxCount of them; nil when none is left.
func Keep(notes []string) []string {
	var kept []string
	for _, text := range notes {
		if len(kept) == MaxCount {
			break
		}
		if names.ValidLine(text, MaxLength) {
			kept = append(kept, text)
		}
	}
	return kept
}
