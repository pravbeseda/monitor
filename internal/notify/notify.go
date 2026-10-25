// Package notify delivers what evaluation decides to say. A channel formats and sends; it
// never decides what is worth sending (docs/specs/evaluation.md#messages).
package notify

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
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
	if m.Metric != evaluate.SilenceMetric {
		subject += " · " + Name(p, m.Name, m.Metric, m.Labels)
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

// decoration are the labels that say nothing to a reader of an alert: the filesystem a
// volume is formatted with, and whether it can be unplugged. Everything else names the
// series, and a message that cannot be told from another one about the same metric is a
// message about nothing (ADR 0033).
var decoration = map[string]bool{"fs": true, "removable": true}

// Naming is what identifies the series inside its node: a mount point reads as itself,
// and any other label as the pair it is. A value carrying a space or an `=` is quoted, or
// it would read as two labels — `{queue: "payments region=eu"}` is one series and
// `{queue: payments, region: eu}` is another.
func Naming(labels map[string]string) string {
	named := make([]string, 0, len(labels))
	for _, key := range slices.Sorted(maps.Keys(labels)) {
		switch {
		case decoration[key]:
		case key == "mount" && labels[key] != "":
			named = append(named, plain(labels[key]))
		default:
			named = append(named, plain(key)+"="+plain(labels[key]))
		}
	}
	return strings.Join(named, " ")
}

// Name is how a message and a page name a series: its metric's declared name in the
// printer's locale with its labels filled in, then what the labels it did not use name. A
// metric nobody named reads as its id (docs/specs/metric-names.md#naming).
func Name(p *i18n.Printer, declared names.Text, metric string, labels map[string]string) string {
	out, _ := name(p, declared, metric, labels)
	return out
}

// Heading is how a page heads a series: its Name, then the volume's filesystem and its
// being removable, which a message leaves out, unless the name already said them.
func Heading(p *i18n.Printer, declared names.Text, metric string, labels map[string]string) string {
	heading, rest := name(p, declared, metric, labels)
	if suffix := volume(p, rest); suffix != "" {
		heading += " · " + suffix
	}
	return heading
}

// name is a series' Name and the labels its declared name did not use.
func name(p *i18n.Printer, declared names.Text, metric string, labels map[string]string) (string, map[string]string) {
	template, named := declared.In(p.Locale())
	if !named {
		template = metric
	}
	out, used := names.Fill(template, labels, filled)
	rest := maps.Clone(labels)
	maps.DeleteFunc(rest, func(key, _ string) bool { return used[key] })
	if more := Naming(rest); more != "" {
		out += " " + more
	}
	return out, rest
}

// filled is a label's value as a name says it: quoted like any label value, and an empty
// one as an empty pair of quotes, since it still tells one series from another.
func filled(value string) string {
	if value == "" {
		return `""`
	}
	return plain(value)
}

// Describe is how a page names a series by its labels: its Naming, then the volume's
// filesystem and whether it can be unplugged, which a page has room for.
func Describe(p *i18n.Printer, labels map[string]string) string {
	parts := make([]string, 0, 2)
	if named := Naming(labels); named != "" {
		parts = append(parts, named)
	}
	if suffix := volume(p, labels); suffix != "" {
		parts = append(parts, suffix)
	}
	return strings.Join(parts, " · ")
}

// volume is the decoration a message leaves out: a volume's filesystem and whether it is
// removable.
func volume(p *i18n.Printer, labels map[string]string) string {
	parts := make([]string, 0, 2)
	if fs := labels["fs"]; fs != "" {
		parts = append(parts, fs)
	}
	if labels["removable"] == "true" {
		parts = append(parts, p.T("label.removable"))
	}
	return strings.Join(parts, " · ")
}

// plain is a label's key or value as it reads in a message, quoted when reading it back
// would otherwise be ambiguous.
func plain(text string) string {
	if strings.ContainsAny(text, " =\"") {
		return strconv.Quote(text)
	}
	return text
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
	return Value(p, m.Metric, value)
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
