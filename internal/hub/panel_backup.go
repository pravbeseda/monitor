package hub

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/notify"
	"github.com/pravbeseda/monitor/internal/state"
)

// failedMetric is the flag a backup row's name stands for, in place of a column of its own.
const failedMetric = "backup.failed"

// backupColumns are the backup panel's columns for the metrics it knows, in their order.
var backupColumns = []column{
	{"backup.success_age_seconds", ago("backup.success_age_seconds")},
	{"backup.latest_archive_age_seconds", ago("backup.latest_archive_age_seconds")},
	{"backup.archives", func(_ *i18n.Printer, value float64) string {
		return strconv.FormatFloat(math.Round(value), 'f', 0, 64)
	}},
	{"backup.stored_bytes", formatting("backup.stored_bytes")},
	{"backup.latest_archive_original_bytes", formatting("backup.latest_archive_original_bytes")},
}

// runWord is what a failure flag's value says about the job's last run.
func runWord(printer *i18n.Printer, value float64) string {
	switch value {
	case 0:
		return printer.T("panel.backup.ok")
	case 1:
		return printer.T("panel.backup.failed")
	}
	return fmt.Sprintf(printer.T("panel.backup.run"), format(printer, failedMetric, value))
}

// ago reads a value of metric, formatted in its unit, as a time ago.
func ago(metric string) func(*i18n.Printer, float64) string {
	return func(printer *i18n.Printer, value float64) string {
		return fmt.Sprintf(printer.T("panel.backup.ago"), format(printer, metric, value))
	}
}

// isJob is whether a label set is a job's: job is its only label besides the group's.
func isJob(named map[string]string) bool {
	_, has := named["job"]
	return has && len(named) == 1
}

// backupTable is the backup panel: a row per job, by its name, then a row per other label
// set in the page order; the known columns that any row has, then the others
// (docs/specs/overview.md#panels).
func backupTable(printer *i18n.Printer, book names.Book, series []panelSeries, link func(state.Subject) string) ([]columnView, []panelRow) {
	var jobs, others [][]panelSeries
	for _, set := range labelSets(series) {
		if isJob(set[0].named) {
			jobs = append(jobs, set)
		} else {
			others = append(others, set)
		}
	}
	slices.SortFunc(jobs, func(a, b []panelSeries) int { return strings.Compare(a[0].named["job"], b[0].named["job"]) })

	known := map[string]bool{failedMetric: true}
	var columns []column
	for _, c := range backupColumns {
		known[c.metric] = true
		if slices.ContainsFunc(series, func(s panelSeries) bool { return s.Metric == c.metric }) {
			columns = append(columns, c)
		}
	}
	columns = append(columns, columnsOf(series, known)...)

	return tableOf(printer, book, append(jobs, others...), columns, func(set []panelSeries) figureView {
		text := notify.Describe(printer, set[0].named)
		if isJob(set[0].named) {
			text = notify.Quoted(set[0].named["job"])
		}
		at := slices.IndexFunc(set, func(s panelSeries) bool { return s.Metric == failedMetric })
		if at < 0 {
			return figureView{Text: text}
		}
		flag := set[at].Subject
		run := runWord(printer, *flag.Value)
		if text == "" {
			text = run
		}
		name := figureOf(printer, flag, text, link)
		var hint []string
		if run != text {
			hint = append(hint, run)
		}
		if name.Hint != "" {
			hint = append(hint, name.Hint)
		}
		name.Hint = strings.Join(hint, " · ")
		return name
	}, link)
}
