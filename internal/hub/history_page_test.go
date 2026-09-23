package hub_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/storage"
)

func page(t *testing.T, store hub.Store, target string) (int, string) {
	t.Helper()
	recorder := get(t, store, target)
	// Every page is HTML, refusals included: a header set after the status line is lost.
	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("GET %s answered content type %q, want HTML", target, got)
	}
	return recorder.Code, recorder.Body.String()
}

const oneVolume = "/history?metric=disk.free_pct&node=server-b&label.mount=%2F&label.fs=ext4"

// spec: history.md#page — a chart of the selected series, with both axes.
func TestHistoryPageDrawsTheSeries(t *testing.T) {
	status, body := page(t, served{series: []seriesPoints{volume()}}, oneVolume)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, want := range []string{
		"<svg", "<polyline",
		"<h1>server-b · disk.free_pct · / · ext4</h1>", // the heading names the series
		"34.1% · 2026-08-31 11:30 UTC",                 // the newest value and when it was collected
		">0.0%<",                                       // the axis starts at zero
		">37.6%<",                                      // and reaches above the highest value
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not show %q", want)
		}
	}
}

// spec: history.md#page — window links keep node, metric, labels and language.
func TestHistoryPageOffersTheWindows(t *testing.T) {
	_, body := page(t, served{series: []seriesPoints{volume()}}, oneVolume+"&window=7d&lang=ru")

	for _, want := range []string{"window=24h", "window=30d", "label.mount=%2F", "label.fs=ext4", "lang=ru"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not offer %q", want)
		}
	}
	if !strings.Contains(body, "<strong>7d</strong>") {
		t.Error("page does not mark the window it is showing")
	}
}

// spec: history.md#page — a query selecting more than one series draws nothing and links instead.
func TestHistoryPageLinksWhenTheQueryIsAmbiguous(t *testing.T) {
	second := volume()
	second.Labels = map[string]string{"mount": "/data", "fs": "ext4"}
	store := served{series: []seriesPoints{volume(), second}}

	_, body := page(t, store, "/history?metric=disk.free_pct")

	if strings.Contains(body, "<polyline") {
		t.Error("page drew a chart for an ambiguous query")
	}
	for _, want := range []string{"label.mount=%2F", "label.mount=%2Fdata"} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not link to %q", want)
		}
	}
}

// spec: history.md#page — a query selecting no series says so.
func TestHistoryPageSaysWhenThereIsNoData(t *testing.T) {
	status, body := page(t, served{}, oneVolume)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "No data for this window") || strings.Contains(body, "<polyline") {
		t.Errorf("page = %q, want it to say the window is empty", body)
	}
}

// spec: history.md#page — a single point is drawn on its own, with no line.
func TestHistoryPageDrawsASinglePointWithoutALine(t *testing.T) {
	one := volume()
	one.Points = one.Points[:1]

	_, body := page(t, served{series: []seriesPoints{one}}, oneVolume)

	if strings.Contains(body, "<polyline") || !strings.Contains(body, "<circle") {
		t.Errorf("page = %q, want a lone point and no line", body)
	}
}

// spec: history.md#page — a silence longer than three intervals breaks the line.
func TestHistoryPageBreaksTheLineOverASilence(t *testing.T) {
	silent := volume()
	silent.Points = []storage.Point{
		{TS: collected.Add(-20 * time.Hour), Value: 40},
		{TS: collected.Add(-20*time.Hour + 15*time.Minute), Value: 39},
		{TS: collected.Add(-time.Hour), Value: 38},
		{TS: collected.Add(-45 * time.Minute), Value: 37},
	}

	_, body := page(t, served{series: []seriesPoints{silent}}, oneVolume)

	if lines := strings.Count(body, "<polyline"); lines != 2 {
		t.Errorf("page drew %d polylines, want the line broken in two", lines)
	}
}

// spec: history.md#page — a refused query is refused on the page too, translated.
func TestHistoryPageRefusesAMalformedQuery(t *testing.T) {
	status, body := page(t, served{}, "/history?metric=disk.free_pct&window=2w&lang=ru")

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "Окно задаётся") {
		t.Errorf("page = %q, want the refusal in Russian", body)
	}
}

// spec: history.md#page — every value on /debug links to its own history.
func TestDebugLinksEachValueToItsHistory(t *testing.T) {
	recorder := show(t, stored{states: []storage.NodeState{laptop}}, "/debug", "")

	body := recorder.Body.String()
	for _, want := range []string{
		"/history?",
		"metric=disk.free_pct",
		"node=laptop-a",
		"label.mount=%2F",
		"label.fs=apfs",
		"label.removable=false",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index page does not link with %q", want)
		}
	}
}

// spec: history.md#page — the chart speaks the reader's language.
func TestHistoryPageSpeaksTheReadersLanguage(t *testing.T) {
	_, body := page(t, served{series: []seriesPoints{volume()}}, oneVolume+"&lang=ru")

	if !strings.Contains(body, "34,1 %") {
		t.Errorf("page = %q, want Russian formatting", body)
	}
}

// spec: history.md#page — the window being shown is marked, including the default one.
func TestHistoryPageMarksTheWindowItShows(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	_, body := page(t, store, oneVolume)
	if !strings.Contains(body, "<strong>24h</strong>") {
		t.Errorf("page = %q, want the default window marked", body)
	}
}

// spec: history.md#page — a window of days is read by date, one of hours by time of day.
func TestHistoryPageLabelsTheTimeAxisForItsWindow(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	if _, body := page(t, store, oneVolume); !strings.Contains(body, ">12:00<") {
		t.Errorf("a 24-hour window does not label its axis by time of day: %q", body)
	}
	if _, body := page(t, store, oneVolume+"&window=7d"); !strings.Contains(body, ">Aug 31<") {
		t.Errorf("a 7-day window does not label its axis by date: %q", body)
	}
}

// spec: history.md#page — the axes carry at most six labelled ticks each.
func TestHistoryPageKeepsTheAxesReadable(t *testing.T) {
	_, body := page(t, served{series: []seriesPoints{volume()}}, oneVolume)

	// The zone the axis is read in is a caption, not a tick (spec: web.md#zone).
	ticks := strings.Count(body, "<text") - strings.Count(body, `class="zone"`)
	if ticks != 12 {
		t.Errorf("chart carries %d axis labels, want six on each axis", ticks)
	}
}

// spec: history.md#page — the axis speaks the reader's language too, not only the heading.
func TestHistoryPageTranslatesTheAxis(t *testing.T) {
	_, body := page(t, served{series: []seriesPoints{volume()}}, oneVolume+"&window=7d&lang=ru")

	if !strings.Contains(body, ">31.08<") {
		t.Errorf("page = %q, want Russian dates on the axis", body)
	}
}

// spec: history.md#page — a failed read is a page, in the reader's language, not plain text.
func TestHistoryPageReportsAFailedReadAsAPage(t *testing.T) {
	status, body := page(t, served{stored: stored{err: errFailed}}, oneVolume+"&lang=ru")

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if !strings.Contains(body, "<html") || !strings.Contains(body, "Панель сейчас не может") {
		t.Errorf("page = %q, want a translated page", body)
	}
	if strings.Contains(body, errFailed.Error()) {
		t.Error("page leaks the storage error to the reader")
	}
}

// spec: history.md#refusals — every refusal the page can meet has a translation.
func TestHistoryPageTranslatesEveryRefusal(t *testing.T) {
	crowded := served{}
	for i := range 51 {
		one := volume()
		one.Labels = map[string]string{"mount": string(rune('a'+i%26)) + string(rune('a'+i/26))}
		crowded.series = append(crowded.series, one)
	}
	if status, body := page(t, crowded, "/history?metric=disk.free_pct&lang=ru"); status != http.StatusBadRequest ||
		!strings.Contains(body, "больше серий") {
		t.Errorf("a query selecting 51 series answered %d with %q", status, body)
	}

	for target, russian := range map[string]string{
		"/history?node=server-b":                            "метрика",
		"/history?metric=disk.free_pct&window=2w":           "Окно задаётся",
		"/history?metric=disk.free_pct&node=":               "без значения",
		"/history?metric=disk.free_pct&at=now":              "которого панель не знает",
		"/history?metric=disk.free_pct&window=1h&window=2h": "больше одного раза",
	} {
		status, body := page(t, served{}, target+"&lang=ru")
		if status != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, status)
		}
		if !strings.Contains(body, russian) {
			t.Errorf("GET %s does not refuse in Russian with %q: %q", target, russian, body)
		}
	}
}

// spec: history.md#page — the window shown is marked whatever the query spelled it as, and
// a regional tag survives into every link the page builds.
func TestHistoryPageMarksTheWindowItShowsAndKeepsTheLanguage(t *testing.T) {
	store := served{series: []seriesPoints{volume()}}

	_, body := page(t, store, oneVolume+"&window=1440m&lang=ru-BY")

	if !strings.Contains(body, "<strong>24h</strong>") {
		t.Errorf("page = %q, want 1440m marked as the 24-hour window", body)
	}
	if !strings.Contains(body, "lang=ru") {
		t.Errorf("page = %q, want its links to carry the language it rendered in", body)
	}
}

// spec: history.md#page — a volume with no free space left still has an axis to be drawn
// against, and an enormous one does not turn the axis into infinities.
func TestHistoryPageDrawsExtremeValues(t *testing.T) {
	full := volume()
	full.Points = []storage.Point{{TS: collected.Add(-15 * time.Minute), Value: 0}, {TS: collected, Value: 0}}

	_, body := page(t, served{series: []seriesPoints{full}}, oneVolume)
	if !strings.Contains(body, "<polyline") || strings.Contains(body, "NaN") {
		t.Errorf("page = %q, want a chart of an empty volume", body)
	}

	huge := volume()
	huge.Metric = "disk.free_bytes"
	huge.Points = []storage.Point{{TS: collected.Add(-15 * time.Minute), Value: 1.7e308}, {TS: collected, Value: 1.7e308}}

	_, body = page(t, served{series: []seriesPoints{huge}}, "/history?metric=disk.free_bytes&node=server-b")
	if strings.Contains(body, "NaN") || strings.Contains(body, "Inf") {
		t.Errorf("page = %q, want finite axis labels", body)
	}
}
