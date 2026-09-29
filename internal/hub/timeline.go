package hub

import (
	"context"
	"fmt"
	"html/template"
	"iter"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/timeline"
)

var timelineTemplate = template.Must(template.ParseFS(templates,
	"templates/timeline.html", "templates/attention.html", "templates/shell.html"))

// shownChanges is how many of the newest changes the page lists.
const shownChanges = 50

// labelEvery is how many cells apart the hours under the lanes are written.
const labelEvery = 4

// EventLog is what the timeline reads of the log of level changes and of the anomalies
// recorded beside it, declared where it is consumed.
type EventLog interface {
	// EventsBetween returns the changes recorded after from and up to to, oldest first.
	EventsBetween(ctx context.Context, from, to time.Time) ([]storage.Transition, error)
	// RecentEvents returns the newest changes of the named nodes, newest first.
	RecentEvents(ctx context.Context, nodes []string, limit int) ([]storage.Transition, error)
	// AnomaliesSince returns every anomaly open, or ended after from.
	AnomaliesSince(ctx context.Context, from time.Time) ([]storage.Anomaly, error)
	// RecentAnomalies returns the anomalies of the named nodes that hold their newest
	// limit starts and ends.
	RecentAnomalies(ctx context.Context, nodes []string, limit int) ([]storage.Anomaly, error)
}

// TimelineStore is what the lanes and the changes read beside the state.
type TimelineStore interface {
	Snapshots
	EventLog
	Points(ctx context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error]
}

// timelineView is the timeline as the template sees it: what needs attention under "now",
// then the lanes and the changes.
type timelineView struct {
	shell
	attentionView
	NowLabel     string
	LanesLabel   string
	ChangesLabel string
	NoChanges    string
	Hours        []string
	Lanes        []laneView
	Legend       []cellView
	Days         []dayView
}

type laneView struct {
	Node  string
	Cells []cellView
}

// cellView is one hour of a lane, or one entry of the legend, which has no Title.
type cellView struct {
	Class string
	Title string
	Word  string
}

type dayView struct {
	Label   string
	Changes []changeView
}

type changeView struct {
	Time  string
	Class string
	Node  string
	Name  string
	Link  string
	Move  string
	Value string
	// Usual is the band an anomaly's start was judged against.
	Usual string
}

// cellNames name each cell state on the page, by its class and its word, in the order of
// the legend.
var cellNames = [...]struct{ class, word string }{
	timeline.Silent:      {"silent", "cell.silent"},
	timeline.NoFreshData: {"no-data", "value.stale"},
	timeline.Critical:    {"critical", "level.critical"},
	timeline.Warning:     {"warning", "level.warning"},
	timeline.Unusual:     {"unusual", "timeline.unusual"},
	timeline.OK:          {"ok", "level.ok"},
	timeline.Reporting:   {"reporting", "cell.reporting"},
}

// endOfTime bounds the read of the log from above. A tick may land after the state was
// read, and the change it wrote is what closes the span before it (ADR 0038).
var endOfTime = time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)

// Timeline renders the timeline skin: what needs attention now, each node's last 24 hours,
// and the levels that changed (docs/specs/timeline.md).
func Timeline(read StateReader, store TimelineStore, targets func(node string) (evaluate.Target, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		zone := zoneOf(r)
		printer := i18n.For(i18n.Negotiate(values.Get("lang"), r.Header.Get("Accept-Language"))).In(zone)
		lang := language(values)

		current, err := read(r.Context())
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		view := timelineView{
			shell:         shellOf(printer, "timeline.title", "timeline", lang),
			attentionView: attentionOf(printer, current, lang),
			NowLabel:      printer.T("timeline.now"),
			LanesLabel:    printer.T("timeline.lanes"),
			ChangesLabel:  printer.T("timeline.changes"),
			NoChanges:     printer.T("timeline.no_changes"),
		}

		nodes := takingPart(current)
		if err := view.addLanes(r.Context(), printer, store, targets, nodes, current.At, zone); err != nil {
			storageFailure(w, printer, err)
			return
		}
		changes, err := store.RecentEvents(r.Context(), nodes, shownChanges)
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		anomalies, err := store.RecentAnomalies(r.Context(), nodes, shownChanges)
		if err != nil {
			storageFailure(w, printer, err)
			return
		}
		view.addChanges(printer, changes, anomalies, targets, current.At, lang)

		shellHeaders(w)
		if err := timelineTemplate.Execute(w, view); err != nil {
			slog.Error("render the timeline", "error", err)
		}
	})
}

// takingPart is every node the configuration names that has reported, in name order.
func takingPart(current state.State) []string {
	var out []string
	for _, node := range current.Nodes {
		if node.Configured {
			out = append(out, node.Node)
		}
	}
	sort.Strings(out)
	return out
}

func (v *timelineView) addLanes(ctx context.Context, printer *i18n.Printer, store TimelineStore,
	targets func(node string) (evaluate.Target, bool), nodes []string, now time.Time, zone *time.Location,
) error {
	starts := timeline.Starts(now, zone)
	for i, start := range starts {
		label := ""
		if i%labelEvery == 0 {
			label = printer.Clock(start)
		}
		v.Hours = append(v.Hours, label)
	}
	for _, name := range cellNames {
		v.Legend = append(v.Legend, cellView{Class: name.class, Word: printer.T(name.word)})
	}

	snap, err := store.Snapshot(ctx, nil)
	if err != nil {
		return err
	}
	// The log is read after its from, and the window begins at its first instant.
	events, err := store.EventsBetween(ctx, starts[0].Add(-time.Nanosecond), endOfTime)
	if err != nil {
		return err
	}
	spans, err := timeline.Spans(snap.States, events, now)
	if err != nil {
		return err
	}
	records, err := store.AnomaliesSince(ctx, starts[0])
	if err != nil {
		return err
	}
	unusual, err := timeline.Anomalies(records, now)
	if err != nil {
		return err
	}
	values := map[string][]storage.Value{}
	for _, node := range snap.Nodes {
		values[node.Node] = node.Values
	}

	for _, node := range nodes {
		target, _ := targets(node)
		lane, err := laneOf(ctx, store, target, values[node], spans, unusual, starts[0], now)
		if err != nil {
			return err
		}
		out := laneView{Node: node}
		for i, cell := range timeline.Cells(lane, starts, now) {
			name := cellNames[cell]
			out.Cells = append(out.Cells, cellView{Class: name.class, Title: printer.Clock(starts[i]) + " " + printer.T(name.word)})
		}
		v.Lanes = append(v.Lanes, out)
	}
	return nil
}

// laneOf gathers one node's silence and, for every series it sends, when it was fresh, the
// levels it held and when it was unusual. A series is read back only as far as a point can
// still be fresh at the window's start.
func laneOf(ctx context.Context, store TimelineStore, target evaluate.Target, values []storage.Value,
	spans map[string][]timeline.Span, unusual map[string][]timeline.Interval, from, now time.Time,
) (timeline.Node, error) {
	var out timeline.Node
	silence, err := storage.Subject{Node: target.Node, Metric: evaluate.SilenceMetric}.Key()
	if err != nil {
		return out, err
	}
	for _, span := range spans[silence] {
		if span.Level == evaluate.Critical {
			out.Silent = append(out.Silent, span.Interval)
		}
	}
	for _, value := range values {
		lasting := target.Lasting(value.Sensor)
		since := from.Add(-lasting)
		if lasting == 0 || value.TS.Before(since) {
			continue
		}
		ref := storage.SeriesRef{Node: target.Node, Metric: value.Metric, Labels: value.Labels}
		var stamps []time.Time
		for point, err := range store.Points(ctx, ref, since, now) {
			if err != nil {
				return out, err
			}
			stamps = append(stamps, point.TS)
		}
		key, err := storage.Subject(ref).Key()
		if err != nil {
			return out, err
		}
		out.Series = append(out.Series, timeline.Series{Fresh: timeline.Fresh(stamps, lasting), Spans: spans[key], Unusual: unusual[key]})
	}
	return out, nil
}

// entry is one change the list may show, with what orders it.
type entry struct {
	at time.Time
	// anomaly is true for an anomaly's start or end, which the tick records after the
	// levels, so it lists above a transition of the same instant.
	anomaly bool
	key     string
	view    changeView
}

func (v *timelineView) addChanges(printer *i18n.Printer, changes []storage.Transition, anomalies []storage.Anomaly,
	targets func(node string) (evaluate.Target, bool), now time.Time, lang string,
) {
	entries := make([]entry, 0, len(changes)+2*len(anomalies))
	for _, change := range changes {
		entries = append(entries, entry{at: change.At, view: changeOf(printer, change, targets, lang)})
	}
	for _, record := range anomalies {
		key, err := record.Key()
		if err != nil {
			slog.Error("identify an anomaly", "node", record.Node, "metric", record.Metric, "error", err)
			continue
		}
		entries = append(entries, entry{at: record.Began, anomaly: true, key: key, view: startOf(printer, record, lang)})
		if record.Back != nil {
			entries = append(entries, entry{at: record.Ended, anomaly: true, key: key, view: endOf(printer, record, lang)})
		}
	}
	// Transitions come newest first already, ties as the log recorded them; anomalies are
	// put in the same order: newest first, then the reverse of the order a tick walks.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		switch {
		case !a.at.Equal(b.at):
			return a.at.After(b.at)
		case a.anomaly != b.anomaly:
			return a.anomaly
		case a.anomaly:
			return a.key > b.key
		}
		return false
	})
	if len(entries) > shownChanges {
		entries = entries[:shownChanges]
	}

	for _, one := range entries {
		label := printer.Date(one.at, now)
		if n := len(v.Days); n == 0 || v.Days[n-1].Label != label {
			v.Days = append(v.Days, dayView{Label: label})
		}
		day := &v.Days[len(v.Days)-1]
		day.Changes = append(day.Changes, one.view)
	}
}

// seriesChange is what every change of a series shows: when, which series, and a link to
// its chart.
func seriesChange(printer *i18n.Printer, at time.Time, subject storage.Subject, class, lang string) changeView {
	out := changeView{Time: printer.Clock(at), Class: class, Node: subject.Node, Name: subject.Metric,
		Link: historyLink(subject.Node, subject.Metric, subject.Labels, lang, "")}
	if named := notify.Naming(subject.Labels); named != "" {
		out.Name += " · " + named
	}
	return out
}

// startOf is an anomaly's start: the value it was recorded with and the band it was judged
// against (docs/specs/timeline.md#changes).
func startOf(printer *i18n.Printer, record storage.Anomaly, lang string) changeView {
	out := seriesChange(printer, record.Began, record.Subject, "unusual", lang)
	out.Move, out.Value = printer.T("timeline.unusual"), format(printer, record.Metric, record.Value)
	low, high := format(printer, record.Metric, record.Low), format(printer, record.Metric, record.High)
	if low == high {
		out.Usual = fmt.Sprintf(printer.T("timeline.usually"), low)
	} else {
		out.Usual = fmt.Sprintf(printer.T("timeline.usually_between"), low, high)
	}
	return out
}

// endOf is an anomaly's end: the value it came back at.
func endOf(printer *i18n.Printer, record storage.Anomaly, lang string) changeView {
	out := seriesChange(printer, record.Ended, record.Subject, "usual", lang)
	out.Move, out.Value = printer.T("timeline.usual_again"), format(printer, record.Metric, *record.Back)
	return out
}

func changeOf(printer *i18n.Printer, change storage.Transition, targets func(node string) (evaluate.Target, bool), lang string) changeView {
	to, _ := evaluate.ParseLevel(change.To)
	if change.Metric == evaluate.SilenceMetric {
		out := changeView{Time: printer.Clock(change.At), Class: to.String(), Node: change.Node, Link: pageLink("/debug", lang)}
		if to == evaluate.OK {
			out.Name = printer.T("timeline.reporting")
			return out
		}
		out.Name = printer.T("timeline.fell_silent")
		if target, known := targets(change.Node); known {
			out.Value = fmt.Sprintf(printer.T("timeline.no_report"), printer.Duration(target.SilenceAfter.Seconds()))
		}
		return out
	}

	out := seriesChange(printer, change.At, change.Subject, to.String(), lang)
	from, _ := evaluate.ParseLevel(change.From)
	out.Move = printer.T("level."+from.String()) + " → " + printer.T("level."+to.String())
	if reading, found := change.Readings[change.Metric]; found {
		out.Value = format(printer, change.Metric, reading)
	}
	return out
}
