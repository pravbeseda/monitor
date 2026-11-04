package hub

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/state"
)

// A computer's card and a site's row of the sites table put each series where every other
// of their section puts it: fixed slots, each its values, then what needs attention beyond
// them (docs/specs/overview.md#computer-card and #site-table, ADR 0053, ADR 0055, ADR 0057).

const (
	failedUnits = "systemd.failed_units"
	// extraLines is how many extra lines a card of lines holds; the rest are counted.
	extraLines = 2
)

// slot is one of a card's fixed lines: the series of its own value and those beside it,
// muted, and the families whose other series it gathers.
type slot struct {
	key     string
	pick    func(it overviewItem) (own state.Subject, beside []state.Subject, found bool)
	gathers []string
}

var computerSlots = []slot{
	{key: "load", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		own, found := it.find("load.avg_15m", nil)
		return own, it.present([]string{"load.avg_1m", "load.avg_5m"}, nil), found
	}},
	{key: "memory", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		own, found := it.find("memory.available_pct", nil)
		return own, it.present([]string{"memory.available_bytes"}, nil), found
	}},
	{key: "disk", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		own, found := it.defaultOf("disk.free_pct")
		return own, it.present([]string{"disk.free_bytes"}, own.Labels), found
	}},
	{key: "backup", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		own, found := it.oldestBackup()
		return own, nil, found
	}, gathers: []string{"backup", "timemachine"}},
}

// siteSlots are the sites table's value columns, the traffic first, which the table draws
// with its line of the day.
var siteSlots = []slot{only("requests", "site.requests_per_min"), only("views", "site.pageviews_24h"),
	only("errors", "site.server_error_pct"), only("latency", "site.response_p95_seconds")}

// only is a slot holding one metric's series and nothing beside it.
func only(key, metric string) slot {
	return slot{key: key, pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		own, found := it.defaultOf(metric)
		return own, nil, found
	}}
}

func (it overviewItem) find(metric string, labels map[string]string) (state.Subject, bool) {
	at := slices.IndexFunc(it.series, func(s state.Subject) bool {
		return sameSeries(s, state.Subject{Node: s.Node, Metric: metric, Labels: labels})
	})
	if at < 0 {
		return state.Subject{}, false
	}
	return it.series[at], true
}

// present are the item's series of these metrics with these labels, in that order.
func (it overviewItem) present(metrics []string, labels map[string]string) []state.Subject {
	var out []state.Subject
	for _, metric := range metrics {
		if s, found := it.find(metric, labels); found {
			out = append(out, s)
		}
	}
	return out
}

// oldestBackup is the older of the backups the defaults pick, a fresh one before a stale
// one, a tie to the first in the page order.
func (it overviewItem) oldestBackup() (state.Subject, bool) {
	var picked []state.Subject
	for _, metric := range []string{"timemachine.backup_age_seconds", "backup.success_age_seconds"} {
		if s, found := it.defaultOf(metric); found {
			picked = append(picked, s)
		}
	}
	if len(picked) == 0 {
		return state.Subject{}, false
	}
	return slices.MaxFunc(picked, func(a, b state.Subject) int {
		switch {
		case a.Stale != b.Stale && a.Stale:
			return -1
		case a.Stale != b.Stale:
			return 1
		case *a.Value != *b.Value:
			return cmp.Compare(*a.Value, *b.Value)
		case pageBefore(a, b):
			return 1
		}
		return -1
	}), true
}

// lineSeries is one series of a line with the threshold values its readable threshold
// holds.
type lineSeries struct {
	state.Subject
	levels []threshold
}

// linePlan is one line: the series of its own value, nil for a slot the item has none for,
// the muted ones beside it, the one of them the line says why by, and the gathered one that
// speaks louder than them all. key names a slot, and is empty on an extra line.
type linePlan struct {
	key     string
	own     *state.Subject
	beside  []state.Subject
	speaker *lineSeries
	loud    *lineSeries
}

// linedPlan is what a computer's card or a site's row shows.
type linedPlan struct {
	slots  []linePlan
	extras []linePlan
	more   int
	failed *state.Subject
}

// pickedLine is a line's series before their thresholds are read.
type pickedLine struct {
	key      string
	own      *state.Subject
	beside   []state.Subject
	gathered []state.Subject
}

// lines pick an item's lines: its slots, none when it has no slot's own value, then what
// needs attention that no line shows. shown are the series the card accounts for already.
func (it overviewItem) lines(slots []slot, attention []item, shown []state.Subject) (slotted, extra []pickedLine, more int) {
	for _, one := range slots {
		line := pickedLine{key: one.key}
		if own, beside, found := one.pick(it); found {
			line.own, line.beside, line.gathered = &own, beside, it.gathered(one.gathers, own)
			shown = append(append(append(shown, own), beside...), line.gathered...)
		}
		slotted = append(slotted, line)
	}
	if !slices.ContainsFunc(slotted, func(p pickedLine) bool { return p.own != nil }) {
		slotted = nil
	}

	var series []state.Subject
	for _, s := range it.attended(attention) {
		if !slices.ContainsFunc(shown, func(o state.Subject) bool { return sameSeries(o, s) }) {
			series = append(series, s)
		}
	}
	if len(slotted) == 0 && len(series) == 0 {
		for _, s := range it.shownSeries() {
			if s.Metric != failedUnits && len(series) < fallbackRows {
				series = append(series, s)
			}
		}
	}
	if len(series) > extraLines {
		more = len(series) - extraLines
		series = series[:extraLines]
	}
	for _, s := range series {
		extra = append(extra, pickedLine{own: &s})
	}
	return slotted, extra, more
}

// gathered are the item's series of these families but the line's own, in the page order.
func (it overviewItem) gathered(families []string, own state.Subject) []state.Subject {
	var out []state.Subject
	for _, s := range it.series {
		if slices.Contains(families, names.FamilyOf(s.Metric)) && !sameSeries(s, own) {
			out = append(out, s)
		}
	}
	return out
}

// linedPlan is an item's lines in these slots with the thresholds they say, and its failed
// units.
func (it overviewItem) linedPlan(ctx context.Context, store ThresholdReader, attention []item, slots []slot) (linedPlan, error) {
	var out linedPlan
	var shown []state.Subject
	failed, found := it.find(failedUnits, nil)
	if found {
		shown = append(shown, failed)
	}
	slotted, extra, more := it.lines(slots, attention, shown)
	out.more = more
	// The head says the failed units when they are any, marked, or all the card has.
	if found && (*failed.Value > 0 || markKindOf(failed) != unmarked || len(slotted)+len(extra) == 0) {
		out.failed = &failed
	}
	var err error
	if out.slots, err = linePlans(ctx, store, slotted); err != nil {
		return out, err
	}
	out.extras, err = linePlans(ctx, store, extra)
	return out, err
}

func linePlans(ctx context.Context, store ThresholdReader, lines []pickedLine) ([]linePlan, error) {
	var out []linePlan
	for _, line := range lines {
		plan := linePlan{key: line.key, own: line.own, beside: line.beside}
		if line.own != nil {
			speaker, err := lineSeriesOf(ctx, store, speakerOf(*line.own, line.beside))
			if err != nil {
				return nil, err
			}
			plan.speaker = &speaker
			if loud, found := loudest(append([]state.Subject{*line.own}, line.beside...), line.gathered); found {
				louder, err := lineSeriesOf(ctx, store, loud)
				if err != nil {
					return nil, err
				}
				plan.loud = &louder
			}
		}
		out = append(out, plan)
	}
	return out, nil
}

// speakerOf is the series a line says why by: the most urgent mark among its own and those
// beside it, its own first among equals.
func speakerOf(own state.Subject, beside []state.Subject) state.Subject {
	speaker := own
	for _, s := range beside {
		if markKindOf(s) > markKindOf(speaker) {
			speaker = s
		}
	}
	return speaker
}

func lineSeriesOf(ctx context.Context, store ThresholdReader, s state.Subject) (lineSeries, error) {
	_, _, levels, err := levelsOf(ctx, store, s)
	return lineSeries{Subject: s, levels: levels}, err
}

// loudest is the first gathered series whose mark is louder than every shown one's and
// than the gathered before it.
func loudest(shown, gathered []state.Subject) (state.Subject, bool) {
	bar := unmarked
	for _, s := range shown {
		bar = max(bar, markKindOf(s))
	}
	var out state.Subject
	found := false
	for _, s := range gathered {
		if mark := gatheredMark(s); mark > bar {
			out, bar, found = s, mark, true
		}
	}
	return out, found
}

// computerView is a computer's card. Seen, when it last reported, closes every one; Late says
// it stopped: silent, or holding no fresh data.
type computerView struct {
	Failed *valueView
	Lines  []lineView
	Seen   string
	Late   bool
}

type lineView struct {
	Key   string
	Name  string
	Title string
	Faded bool
	Empty bool
	// Values are the muted ones first, then the line's own.
	Values []valueView
	Why    string
	// WhyClass and WhyHint are set when a gathered series speaks: its mark's colour, and its
	// name and reason, said on hover and to a screen reader.
	WhyClass string
	WhyHint  string
}

type valueView struct {
	Text   string
	Class  string
	Word   string
	Muted  bool
	Marked bool
}

func (it overviewItem) lineViewOf(printer *i18n.Printer, plan linePlan, name string) lineView {
	out := lineView{Key: plan.key, Name: name}
	if plan.own == nil {
		out.Empty = true
		return out
	}
	s := *plan.own
	out.Title = it.rowName(printer, s)
	out.Faded = s.Stale
	for _, one := range plan.beside {
		out.Values = append(out.Values, valueOf(printer, one, true))
	}
	out.Values = append(out.Values, valueOf(printer, s, false))
	out.Why = whyOf(printer, *plan.speaker)

	if loud := plan.loud; loud != nil {
		mark := markOf(printer, loud.Subject)
		out.Why, out.WhyClass = mark.Word, mark.Class
		out.WhyHint = fmt.Sprintf(printer.T("overview.reason_level"), it.rowName(printer, loud.Subject), cmp.Or(whyOf(printer, *loud), mark.Word))
	}
	return out
}

// gatheredMark is the mark a gathered series speaks by: its own, but for no fresh data,
// which it speaks only while watched, as attention lists it.
func gatheredMark(s state.Subject) markKind {
	if mark := markKindOf(s); mark != markStale || s.Watched {
		return mark
	}
	return unmarked
}

func valueOf(printer *i18n.Printer, s state.Subject, muted bool) valueView {
	out := valueView{Text: format(printer, s.Metric, *s.Value), Muted: muted, Marked: markKindOf(s) >= markUnusual}
	if mark := markOf(printer, s); mark != nil {
		out.Class, out.Word = mark.Class, mark.Word
	}
	return out
}

// whyOf says why a line is marked: the norm of an unusual series, the threshold value its
// level holds, or the level's word when it cannot say one; nothing for an unmarked series.
func whyOf(printer *i18n.Printer, s lineSeries) string {
	switch markKindOf(s.Subject) {
	case markUnusual:
		return usualOf(printer, s.Subject)
	case markWarning, markCritical:
		for _, level := range s.levels {
			if level.level == *s.Level {
				return fmt.Sprintf(printer.T("overview.threshold"), format(printer, s.Metric, level.value))
			}
		}
		return printer.T("level." + s.Level.String())
	}
	return ""
}

// slotName names a slot; the disk's names the volume it shows.
func slotName(printer *i18n.Printer, plan linePlan) string {
	switch {
	case plan.key == "disk" && plan.own == nil:
		return printer.T("overview.no_disk")
	case plan.key == "disk":
		return fmt.Sprintf(printer.T("overview.disk"), plan.own.Labels["mount"])
	}
	return printer.T("overview." + plan.key)
}

// computerOf lays a computer's card out by its plan, and says how many extra lines it leaves
// out.
func (it overviewItem) computerOf(printer *i18n.Printer, plan linedPlan) (*computerView, string) {
	out := &computerView{
		Seen: it.lastReportText(printer),
		Late: it.stopped(),
	}
	if s := plan.failed; s != nil {
		out.Failed = &valueView{Text: fmt.Sprintf(printer.T("overview.failed"), int64(math.Round(*s.Value)))}
		if mark := markOf(printer, *s); mark != nil {
			out.Failed.Class, out.Failed.Word = mark.Class, mark.Word
		}
	}
	for _, line := range plan.slots {
		out.Lines = append(out.Lines, it.lineViewOf(printer, line, slotName(printer, line)))
	}
	for _, line := range plan.extras {
		out.Lines = append(out.Lines, it.lineViewOf(printer, line, it.rowName(printer, *line.own)))
	}
	more := ""
	if plan.more > 0 {
		more = fmt.Sprintf(printer.T("overview.more"), plan.more)
	}
	return out, more
}

// markKind is the one mark a series carries, ordered so that a larger one speaks louder.
type markKind int

const (
	unmarked markKind = iota
	markStale
	markUnusual
	markWarning
	markCritical
)

// markKindOf is a series' mark, the first that applies: a stale series is marked stale
// whatever level it stored, as attention gives it no level.
func markKindOf(s state.Subject) markKind {
	switch {
	case s.Stale:
		return markStale
	case s.Level != nil && *s.Level == evaluate.Critical:
		return markCritical
	case s.Level != nil && *s.Level == evaluate.Warning:
		return markWarning
	case s.Anomaly != nil && s.Anomaly.Rank > 0:
		return markUnusual
	}
	return unmarked
}
