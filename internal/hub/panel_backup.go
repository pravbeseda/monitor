package hub

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
)

// backupFigures are the backup panel's words for the metrics it knows, in the order a line
// shows them.
var backupFigures = []struct {
	metric string
	say    func(printer *i18n.Printer, value float64) string
}{
	{"backup.failed", func(printer *i18n.Printer, value float64) string {
		switch value {
		case 0:
			return printer.T("panel.backup.ok")
		case 1:
			return printer.T("panel.backup.failed")
		}
		return fmt.Sprintf(printer.T("panel.backup.run"), format(printer, "backup.failed", value))
	}},
	{"backup.success_age_seconds", saying("panel.backup.succeeded", "backup.success_age_seconds")},
	{"backup.latest_archive_age_seconds", saying("panel.backup.newest", "backup.latest_archive_age_seconds")},
	{"backup.archives", func(printer *i18n.Printer, value float64) string {
		return fmt.Sprintf(printer.T("panel.backup.archives"), strconv.FormatFloat(math.Round(value), 'f', 0, 64))
	}},
	{"backup.stored_bytes", saying("panel.backup.stored", "backup.stored_bytes")},
	{"backup.latest_archive_original_bytes", saying("panel.backup.original", "backup.latest_archive_original_bytes")},
}

// saying puts a value, formatted in its metric's unit, into a catalogue sentence.
func saying(key, metric string) func(*i18n.Printer, float64) string {
	return func(printer *i18n.Printer, value float64) string {
		return fmt.Sprintf(printer.T(key), format(printer, metric, value))
	}
}

// backupLines are the backup panel: a line per job, by its name, then a line per other
// label set in the page order (docs/specs/overview.md#panels).
func backupLines(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) []panelLine {
	var jobs, others [][]panelSeries
	for _, set := range labelSets(series) {
		if _, has := set[0].named["job"]; has && len(set[0].named) == 1 {
			jobs = append(jobs, set)
		} else {
			others = append(others, set)
		}
	}
	slices.SortFunc(jobs, func(a, b []panelSeries) int { return strings.Compare(a[0].named["job"], b[0].named["job"]) })

	out := make([]panelLine, 0, len(jobs)+len(others))
	for i, set := range append(jobs, others...) {
		line := panelLine{Name: notify.Describe(printer, set[0].named)}
		if i < len(jobs) {
			line.Name = notify.Quoted(set[0].named["job"])
		}
		byMetric := map[string]state.Subject{}
		for _, s := range set {
			byMetric[s.Metric] = s.Subject
		}
		figure := func(s state.Subject, text string) {
			line.Figures = append(line.Figures, figureView{Text: text, History: link(s), Mark: markOf(printer, s)})
		}
		for _, known := range backupFigures {
			if s, found := byMetric[known.metric]; found {
				figure(s, known.say(printer, *s.Value))
				delete(byMetric, known.metric)
			}
		}
		for _, metric := range slices.Sorted(maps.Keys(byMetric)) {
			s := byMetric[metric]
			figure(s, columnLabel(printer, book, metric)+" "+format(printer, metric, *s.Value))
		}
		out = append(out, line)
	}
	return out
}
