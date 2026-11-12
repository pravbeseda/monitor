package hub

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// A computer's page lays its series out as tiles: one of the skin's own for each family a
// computer reports, a generic one for any other series, a line only where a trend says
// something (docs/specs/overview.md#computer-page, ADR 0058).

// volumeWindow is a volume's line: a disk fills over days, not hours.
const volumeWindow = 10 * 24 * time.Hour

// totalFloor is the least free share a total is derived from: the share is rounded to two
// decimals, which says too little of a fuller volume.
const totalFloor = 1.0

type tileKind int

const (
	tileGeneric tileKind = iota
	tileVolume
	tileMemory
	tileLoad
	// tileSingle is a generic tile without a line.
	tileSingle
)

// tileID is the tile a series is on: a volume by its labels, a generic tile by its series.
type tileID struct {
	kind           tileKind
	metric, labels string
}

// loadAverages are the load tile's series and the minutes each averages, in its order.
var loadAverages = []struct{ metric, minutes string }{
	{"load.avg_1m", "1"}, {"load.avg_5m", "5"}, {"load.avg_15m", "15"},
}

// tileView is one tile as the template sees it.
type tileView struct {
	Name, Aside string
	Class, Word string
	// Values lead the tile, parted by " · "; Of and Share follow them, muted.
	Values []figureView
	Of     string
	Share  *figureView
	Bar    *barView
	Line   *chart
	Usual  string
	// Notes are what the values came with, in their order (ADR 0060).
	Notes []string
}

// barView is a share used, scaled to 100, with the thresholds' ticks on the same scale.
type barView struct {
	Used  float64
	Class string
	Label string
	Ticks []levelTick
}

type levelTick struct {
	At    float64
	Class string
}

func tileOf(s state.Subject) tileID {
	bare := len(s.Labels) == 0
	switch {
	case s.Metric == "disk.free_bytes" || s.Metric == "disk.free_pct":
		return tileID{kind: tileVolume, labels: storage.LabelKey(s.Labels)}
	case bare && (s.Metric == "memory.available_bytes" || s.Metric == "memory.available_pct"):
		return tileID{kind: tileMemory}
	case bare && slices.ContainsFunc(loadAverages, func(a struct{ metric, minutes string }) bool { return a.metric == s.Metric }):
		return tileID{kind: tileLoad}
	case bare && (s.Metric == "uptime.boot_seconds" || s.Metric == failedUnits):
		return tileID{kind: tileSingle, metric: s.Metric}
	}
	return tileID{kind: tileGeneric, metric: s.Metric, labels: storage.LabelKey(s.Labels)}
}

// tiler draws the tiles of one item's page.
type tiler struct {
	printer *i18n.Printer
	reader  history.Reader
	store   ThresholdReader
	it      overviewItem
	lang    string
}

// tiles lays the series out, given in the page order, as tiles in the order of their first
// series.
func (t tiler) tiles(ctx context.Context, series []state.Subject) ([]tileView, error) {
	var order []tileID
	byID := map[tileID][]state.Subject{}
	for _, s := range series {
		id := tileOf(s)
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = append(byID[id], s)
	}
	out := make([]tileView, 0, len(order))
	for _, id := range order {
		view, err := t.tile(ctx, id.kind, byID[id])
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (t tiler) tile(ctx context.Context, kind tileKind, group []state.Subject) (tileView, error) {
	cell := cellOf(group, false)
	out := tileView{Class: cellNames[cell].class, Word: t.printer.T(cellNames[cell].word)}
	family := t.it.names.Family(t.printer.Locale(), names.FamilyOf(group[0].Metric))
	switch kind {
	case tileVolume:
		out.Name = notify.Describe(t.printer, t.it.ownLabels(group[0]))
		if out.Name == "" {
			out.Name = family
		}
		return out, t.space(ctx, &out, group, "disk.free_bytes", "disk.free_pct", volumeWindow)
	case tileMemory:
		out.Name = family
		return out, t.space(ctx, &out, group, "memory.available_bytes", "memory.available_pct", history.DefaultWindow)
	case tileLoad:
		out.Name = family
		return out, t.load(ctx, &out, group)
	}
	s := group[0]
	out.Name = t.it.chartName(t.printer, s)
	out.Values = []figureView{t.figure(s, history.DefaultWindow)}
	out.Usual, out.Notes = usual(t.printer, group), s.Notes
	if kind == tileSingle {
		return out, nil
	}
	return out, t.line(ctx, &out, s, history.DefaultWindow)
}

// space lays out a volume or the memory: the free bytes of the total, the free share, the
// share used and the line of the free share.
func (t tiler) space(ctx context.Context, out *tileView, group []state.Subject, bytesMetric, shareMetric string, window time.Duration) error {
	bytes, hasBytes := pick(group, bytesMetric)
	share, hasShare := pick(group, shareMetric)
	var shown []state.Subject
	if hasBytes {
		shown = append(shown, bytes)
		out.Values = []figureView{t.figure(bytes, window)}
	}
	if hasShare {
		shown = append(shown, share)
		figure := t.figure(share, window)
		if hasBytes {
			out.Share = &figure
		} else {
			out.Values = []figureView{figure}
		}
	}
	out.Usual, out.Notes = usual(t.printer, shown), notesOf(shown)
	if !hasShare {
		return nil
	}
	free := *share.Value
	if hasBytes && free >= totalFloor && free <= 100 {
		out.Of = fmt.Sprintf(t.printer.T("tile.of"), format(t.printer, bytesMetric, *bytes.Value*100/free))
	}
	_, _, levels, err := levelsOf(ctx, t.store, share)
	if err != nil {
		return err
	}
	if free >= 0 && free <= 100 {
		out.Bar = t.bar(share, levels)
	}
	out.Line, err = lineOf(ctx, t.printer, t.reader, share, window, sparkFrame, levels...)
	return err
}

func (t tiler) bar(share state.Subject, levels []threshold) *barView {
	used := 100 - *share.Value
	out := &barView{Used: used, Label: fmt.Sprintf(t.printer.T("tile.used"), t.printer.Percent(used))}
	if markKindOf(share) >= markWarning {
		out.Class = levelOf(t.printer, share.Level).Class
	}
	for _, at := range levels {
		if at.value < 0 || at.value > 100 {
			continue
		}
		out.Ticks = append(out.Ticks, levelTick{At: 100 - at.value, Class: levelOf(t.printer, &at.level).Class})
	}
	return out
}

// load lays out the load averages the item has, and the line of the 15-minute one.
func (t tiler) load(ctx context.Context, out *tileView, group []state.Subject) error {
	var minutes []string
	var shown []state.Subject
	for _, average := range loadAverages {
		if s, found := pick(group, average.metric); found {
			minutes = append(minutes, average.minutes)
			shown = append(shown, s)
			out.Values = append(out.Values, t.figure(s, history.DefaultWindow))
		}
	}
	out.Aside = fmt.Sprintf(t.printer.T("tile.minutes"), strings.Join(minutes, " · "))
	out.Usual, out.Notes = usual(t.printer, shown), notesOf(shown)
	if fifteen, found := pick(group, "load.avg_15m"); found {
		return t.line(ctx, out, fifteen, history.DefaultWindow)
	}
	return nil
}

// line draws a tile's series over its window, its readable threshold across it.
func (t tiler) line(ctx context.Context, out *tileView, s state.Subject, window time.Duration) error {
	_, _, levels, err := levelsOf(ctx, t.store, s)
	if err != nil {
		return err
	}
	out.Line, err = lineOf(ctx, t.printer, t.reader, s, window, sparkFrame, levels...)
	return err
}

// figure is a tile's value, marked as a panel's figure, its series named on hover.
func (t tiler) figure(s state.Subject, window time.Duration) figureView {
	text := format(t.printer, s.Metric, *s.Value)
	if s.Metric == failedUnits {
		text = whole(*s.Value)
	}
	out := figureOf(t.printer, s, text, func(s state.Subject) string {
		return historyLink(s.Node, s.Metric, s.Labels, t.lang, windowName(window))
	})
	out.Title = t.it.rowName(t.printer, s)
	if out.Hint != "" {
		out.Title += " · " + out.Hint
	}
	return out
}

// usual is the usual value of the first fresh unusual series among these.
func usual(printer *i18n.Printer, series []state.Subject) string {
	for _, s := range series {
		if !s.Stale && s.Anomaly != nil && s.Anomaly.Rank > 0 {
			return usualOf(printer, s)
		}
	}
	return ""
}

// notesOf is the notes of a tile's values, in the order of its values.
func notesOf(series []state.Subject) []string {
	var out []string
	for _, s := range series {
		out = append(out, s.Notes...)
	}
	return out
}

func pick(group []state.Subject, metric string) (state.Subject, bool) {
	at := slices.IndexFunc(group, func(s state.Subject) bool { return s.Metric == metric })
	if at < 0 {
		return state.Subject{}, false
	}
	return group[at], true
}
