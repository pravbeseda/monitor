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

// EventLog is what the timeline reads of the log of level changes, declared where it is
// consumed.
type EventLog interface {
	// EventsBetween returns the changes recorded after from and up to to, oldest first.
	EventsBetween(ctx context.Context, from, to time.Time) ([]storage.Transition, error)
	// RecentEvents returns the newest changes of the named nodes, newest first.
	RecentEvents(ctx context.Context, nodes []string, limit int) ([]storage.Transition, error)
}

// TimelineStore is what the lanes and the changes read beside the state.
type TimelineStore interface {
	Snapshots
	EventLog
	Points(ctx context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error]
}

// timelineView is the timeline as the template sees it: mission control's attention list
// under "now", then the lanes and the changes.
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
}

// cellNames name each cell state on the page, by its class and its word, in the order of
// the legend.
var cellNames = [...]struct{ class, word string }{
	timeline.Silent:      {"silent", "cell.silent"},
	timeline.NoFreshData: {"no-data", "value.stale"},
	timeline.Critical:    {"critical", "level.critical"},
	timeline.Warning:     {"warning", "level.warning"},
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
		view.addChanges(printer, changes, targets, current.At, lang)

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
	events, err := store.EventsBetween(ctx, starts[0], endOfTime)
	if err != nil {
		return err
	}
	spans, err := timeline.Spans(snap.States, events, now)
	if err != nil {
		return err
	}
	values := map[string][]storage.Value{}
	for _, node := range snap.Nodes {
		values[node.Node] = node.Values
	}

	for _, node := range nodes {
		target, _ := targets(node)
		lane, err := laneOf(ctx, store, target, values[node], spans, starts[0], now)
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

// laneOf gathers one node's silence and, for every series it sends, when it was fresh and
// the levels it held. A series is read back only as far as a point can still be fresh at the
// window's start.
func laneOf(ctx context.Context, store TimelineStore, target evaluate.Target, values []storage.Value,
	spans map[string][]timeline.Span, from, now time.Time,
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
		out.Series = append(out.Series, timeline.Series{Fresh: timeline.Fresh(stamps, lasting), Spans: spans[key]})
	}
	return out, nil
}

func (v *timelineView) addChanges(printer *i18n.Printer, changes []storage.Transition,
	targets func(node string) (evaluate.Target, bool), now time.Time, lang string,
) {
	for _, change := range changes {
		label := printer.Date(change.At, now)
		if n := len(v.Days); n == 0 || v.Days[n-1].Label != label {
			v.Days = append(v.Days, dayView{Label: label})
		}
		day := &v.Days[len(v.Days)-1]
		day.Changes = append(day.Changes, changeOf(printer, change, targets, lang))
	}
}

func changeOf(printer *i18n.Printer, change storage.Transition, targets func(node string) (evaluate.Target, bool), lang string) changeView {
	to, _ := evaluate.ParseLevel(change.To)
	out := changeView{Time: printer.Clock(change.At), Class: to.String(), Node: change.Node}
	if change.Metric == evaluate.SilenceMetric {
		out.Link = pageLink("/debug", lang)
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

	out.Name, out.Link = change.Metric, historyLink(change.Node, change.Metric, change.Labels, lang, "")
	if named := notify.Naming(change.Labels); named != "" {
		out.Name += " · " + named
	}
	from, _ := evaluate.ParseLevel(change.From)
	out.Move = printer.T("level."+from.String()) + " → " + printer.T("level."+to.String())
	if reading, found := change.Readings[change.Metric]; found {
		out.Value = format(printer, change.Metric, reading)
	}
	return out
}
