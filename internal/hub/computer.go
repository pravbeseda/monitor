package hub

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/state"
)

// A computer's card puts each series where every other computer card puts it: four slots
// on scales the section shares, then what needs attention beyond them
// (docs/specs/overview.md#computer-card, ADR 0053).

const (
	failedUnits = "systemd.failed_units"
	// extraLines is how many extra lines a computer's card holds; the rest are counted.
	extraLines = 2
	// scaleHeadroom keeps the largest value on a shared scale short of its end.
	scaleHeadroom = 1.1
)

// slot is one of a computer card's fixed lines: the series on its scale and those beside
// it, muted, and the families whose other series it gathers.
type slot struct {
	key     string
	pick    func(it overviewItem) (scale state.Subject, beside []state.Subject, found bool)
	gathers []string
}

var slots = []slot{
	{key: "load", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		scale, found := it.find("load.avg_15m", nil)
		return scale, it.present([]string{"load.avg_1m", "load.avg_5m"}, nil), found
	}},
	{key: "memory", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		scale, found := it.find("memory.available_pct", nil)
		return scale, it.present([]string{"memory.available_bytes"}, nil), found
	}},
	{key: "disk", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		scale, found := it.defaultOf("disk.free_pct")
		return scale, it.present([]string{"disk.free_bytes"}, scale.Labels), found
	}},
	{key: "backup", pick: func(it overviewItem) (state.Subject, []state.Subject, bool) {
		scale, found := it.oldestBackup()
		return scale, nil, found
	}, gathers: []string{"backup", "timemachine"}},
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

// linePlan is one line before its scale is known: the series on the scale, nil for a slot
// the computer has none for, the muted ones beside it, and those it gathers. key names a
// slot, and is empty on an extra line.
type linePlan struct {
	key      string
	scale    *lineSeries
	beside   []lineSeries
	gathered []lineSeries
}

// computerPlan is what a computer's card shows, before the section's scales are known.
type computerPlan struct {
	slots  []linePlan
	extras []linePlan
	more   int
	failed *state.Subject
}

// pickedLine is a line's series before their thresholds are read.
type pickedLine struct {
	key      string
	scale    *state.Subject
	beside   []state.Subject
	gathered []state.Subject
}

// computerLines pick a computer's lines: its slots, none when it has a series on no slot's
// scale, then what needs attention that no line shows. shown are the series the card
// accounts for already.
func (it overviewItem) computerLines(attention []item, shown []state.Subject) (slotted, extra []pickedLine, more int) {
	for _, one := range slots {
		line := pickedLine{key: one.key}
		if scale, beside, found := one.pick(it); found {
			line.scale, line.beside, line.gathered = &scale, beside, it.gathered(one.gathers, scale)
			shown = append(append(append(shown, scale), beside...), line.gathered...)
		}
		slotted = append(slotted, line)
	}
	if !slices.ContainsFunc(slotted, func(p pickedLine) bool { return p.scale != nil }) {
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
		extra = append(extra, pickedLine{scale: &s})
	}
	return slotted, extra, more
}

// gathered are the item's series of these families but the one on the scale, in the page
// order.
func (it overviewItem) gathered(families []string, scale state.Subject) []state.Subject {
	var out []state.Subject
	for _, s := range it.series {
		if slices.Contains(families, names.FamilyOf(s.Metric)) && !sameSeries(s, scale) {
			out = append(out, s)
		}
	}
	return out
}

// computerPlan is a computer's lines with the thresholds they draw, and its failed units.
func (it overviewItem) computerPlan(ctx context.Context, store ThresholdReader, attention []item) (computerPlan, error) {
	var out computerPlan
	var shown []state.Subject
	failed, found := it.find(failedUnits, nil)
	if found {
		shown = append(shown, failed)
	}
	slotted, extra, more := it.computerLines(attention, shown)
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
		plan := linePlan{key: line.key}
		if line.scale != nil {
			scale, err := lineSeriesOf(ctx, store, *line.scale)
			if err != nil {
				return nil, err
			}
			plan.scale = &scale
			if plan.beside, err = lineSeriesOfAll(ctx, store, line.beside); err != nil {
				return nil, err
			}
			if plan.gathered, err = lineSeriesOfAll(ctx, store, line.gathered); err != nil {
				return nil, err
			}
		}
		out = append(out, plan)
	}
	return out, nil
}

func lineSeriesOf(ctx context.Context, store ThresholdReader, s state.Subject) (lineSeries, error) {
	_, _, levels, err := levelsOf(ctx, store, s)
	return lineSeries{Subject: s, levels: levels}, err
}

func lineSeriesOfAll(ctx context.Context, store ThresholdReader, series []state.Subject) ([]lineSeries, error) {
	var out []lineSeries
	for _, s := range series {
		one, err := lineSeriesOf(ctx, store, s)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

// reach is the largest a series asks of its scale: its value, its band's top and its
// threshold's values.
func (s lineSeries) reach() float64 {
	largest := *s.Value
	if s.Anomaly != nil {
		largest = max(largest, s.Anomaly.High)
	}
	for _, level := range s.levels {
		largest = max(largest, level.value)
	}
	return largest
}

// scaleEnd is where a scale ends: a percentage's at 100, any other's past the largest
// reach of the series it holds, at 1 when nothing reaches above zero.
func scaleEnd(metric string, series []lineSeries) float64 {
	if history.UnitOf(metric) == history.Percent {
		return 100
	}
	largest := 0.0
	for _, s := range series {
		largest = max(largest, s.reach())
	}
	if largest <= 0 {
		return 1
	}
	return scaleHeadroom * largest
}

// sharedEnds are where each slot's scale ends on every computer card of the section, set
// by the series on it that hold fresh data, or by all of them when none does. A nil plan is
// a card that is not a computer's.
func sharedEnds(plans []*computerPlan) map[string]float64 {
	fresh, all := map[string][]lineSeries{}, map[string][]lineSeries{}
	metric := map[string]string{}
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		for _, line := range plan.slots {
			if line.scale == nil {
				continue
			}
			metric[line.key] = line.scale.Metric
			all[line.key] = append(all[line.key], *line.scale)
			if !line.scale.Stale {
				fresh[line.key] = append(fresh[line.key], *line.scale)
			}
		}
	}
	out := map[string]float64{}
	for key, m := range metric {
		if len(fresh[key]) == 0 {
			fresh[key] = all[key]
		}
		out[key] = scaleEnd(m, fresh[key])
	}
	return out
}

// computerView is a computer's card. Seen, when it last reported, closes every one; Late
// says it stopped: silent, or holding no fresh data.
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
	Band  *bandView
	Ticks []tickView
	Dot   *tickView
	// Values are the muted ones first, then the one on the scale.
	Values []valueView
	Why    string
	// WhyClass and WhyHint are set when a gathered series speaks: its mark's colour, and its
	// name and reason, said on hover and to a screen reader.
	WhyClass string
	WhyHint  string
	// Said is the scale in words, for a reader who cannot see it: the usual range and the
	// threshold values its ticks mark.
	Said string
}

type bandView struct{ Left, Width, Norm string }

type tickView struct{ Class, Left string }

type valueView struct {
	Text   string
	Class  string
	Word   string
	Muted  bool
	Marked bool
}

// place is a value's place on a scale ending at end, in percent of its width.
func place(value, end float64) float64 {
	return max(0, min(100, value/end*100))
}

func percent(p float64) string {
	return strconv.FormatFloat(p, 'f', 1, 64)
}

func (it overviewItem) computerLine(printer *i18n.Printer, plan linePlan, name string, end float64) lineView {
	out := lineView{Key: plan.key, Name: name}
	if plan.scale == nil {
		out.Empty = true
		return out
	}
	s := *plan.scale
	out.Title = it.rowName(printer, s.Subject)
	out.Faded = s.Stale
	var said []string
	if s.Anomaly != nil {
		low, high := place(s.Anomaly.Low, end), place(s.Anomaly.High, end)
		out.Band = &bandView{Left: percent(low), Width: percent(high - low), Norm: percent(place(s.Anomaly.Norm, end))}
		said = append(said, fmt.Sprintf(printer.T("overview.usual_range"), format(printer, s.Metric, s.Anomaly.Low), format(printer, s.Metric, s.Anomaly.High)))
	}
	for _, level := range s.levels {
		if level.value >= 0 && level.value <= end {
			out.Ticks = append(out.Ticks, tickView{Class: "level-" + level.level.String(), Left: percent(place(level.value, end))})
			said = append(said, fmt.Sprintf(printer.T("overview.tick"), printer.T("level."+level.level.String()), format(printer, s.Metric, level.value)))
		}
	}
	out.Said = strings.Join(said, ", ")
	out.Dot = &tickView{Left: percent(place(*s.Value, end))}
	if mark := markOf(printer, s.Subject); mark != nil {
		out.Dot.Class = mark.Class
	}

	speaker := s
	for _, one := range plan.beside {
		out.Values = append(out.Values, valueOf(printer, one.Subject, true))
		if markKindOf(one.Subject) > markKindOf(speaker.Subject) {
			speaker = one
		}
	}
	out.Values = append(out.Values, valueOf(printer, s.Subject, false))
	out.Why = whyOf(printer, speaker)

	var loud *lineSeries
	loudest := markKindOf(speaker.Subject)
	for i, one := range plan.gathered {
		if mark := gatheredMark(one.Subject); mark > loudest {
			loud, loudest = &plan.gathered[i], mark
		}
	}
	if loud != nil {
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
	case plan.key == "disk" && plan.scale == nil:
		return printer.T("overview.no_disk")
	case plan.key == "disk":
		return fmt.Sprintf(printer.T("overview.disk"), plan.scale.Labels["mount"])
	}
	return printer.T("overview." + plan.key)
}

// computerOf lays a computer's card out by its plan on the section's scales, and says how
// many extra lines it leaves out.
func (it overviewItem) computerOf(printer *i18n.Printer, plan computerPlan, ends map[string]float64) (*computerView, string) {
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
		out.Lines = append(out.Lines, it.computerLine(printer, line, slotName(printer, line), ends[line.key]))
	}
	for _, line := range plan.extras {
		end := scaleEnd(line.scale.Metric, []lineSeries{*line.scale})
		out.Lines = append(out.Lines, it.computerLine(printer, line, it.rowName(printer, line.scale.Subject), end))
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
