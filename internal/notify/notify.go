// Package notify delivers what evaluation decides to say. A channel formats and sends; it
// never decides what is worth sending (docs/specs/evaluation.md#messages).
package notify

import (
	"fmt"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
)

// levelKeys name the catalogue entry of each level, so a level's stored name and its
// user-facing text can never be the same string by accident.
var levelKeys = map[evaluate.Level]string{
	evaluate.OK:       "level.ok",
	evaluate.Warning:  "level.warning",
	evaluate.Critical: "level.critical",
}

// Render is the one text every channel sends. A message whose levels are equal is a repeat
// or a digest entry for a subject that has not moved, and reads as a standing level rather
// than as a change.
func Render(p *i18n.Printer, m evaluate.Message) string {
	subject := m.Node
	if mount := m.Labels["mount"]; mount != "" {
		subject += " " + mount
	}

	var line string
	if m.From == m.To {
		line = fmt.Sprintf(p.T("notify.standing"),
			subject, p.T(levelKeys[m.To]), p.Time(m.Since))
	} else {
		line = fmt.Sprintf(p.T("notify.changed"),
			subject, p.T(levelKeys[m.To]), p.T(levelKeys[m.From]), p.Time(m.Since))
	}
	if detail := detail(p, m); detail != "" {
		line += " — " + detail
	}
	return line
}

// RenderDigest is the day's summary as one message: a title, one line per subject, and
// what is not being watched at all — a hub judging nothing must not read like a quiet one
// (docs/specs/evaluation.md#digest).
func RenderDigest(p *i18n.Printer, entries []evaluate.Message, unwatched int) string {
	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, p.T("digest.title"))
	for _, entry := range entries {
		lines = append(lines, Render(p, entry))
	}
	if len(entries) == 0 {
		lines = append(lines, p.T("digest.nothing_watched"))
	}
	if unwatched > 0 {
		lines = append(lines, fmt.Sprintf(p.T("digest.unwatched"), unwatched))
	}
	return strings.Join(lines, "\n")
}

// detail renders the value that produced a message, in the unit its metric id declares
// (ADR 0033). The silence subject has no value at all and says so.
func detail(p *i18n.Printer, m evaluate.Message) string {
	if m.Metric == evaluate.SilenceMetric {
		return p.T("notify.silent")
	}
	value, measured := m.Readings[m.Metric]
	if !measured {
		return ""
	}
	return fmt.Sprintf(p.T("notify.reading"), m.Metric, Value(p, m.Metric, value))
}

// Value renders one reading the way its metric id says to. It is exported because every
// surface that shows a value — a page, a message, a digest — must render it the same way
// (docs/specs/history.md#wire-format).
func Value(p *i18n.Printer, metric string, value float64) string {
	switch history.UnitOf(metric) {
	case history.Bytes:
		return p.Bytes(value)
	case history.Percent:
		return p.Percent(value)
	case history.Duration:
		return p.Duration(value)
	default:
		return p.Number(value)
	}
}
