package hub_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

func (j judged) Newest(_ context.Context, sel storage.Selection, _ time.Time) ([]storage.SeriesNewest, error) {
	series, err := j.Series(context.Background(), sel)
	return series, err
}

// withPoints is the store with the last day of the series holding 40 GB, then 30 GB.
func withPoints(store judged) judged {
	store.points = map[string][]storage.Point{dataSeries.Metric: {
		{TS: at().Add(-2 * time.Hour), Value: 40e9},
		{TS: at().Add(-time.Hour), Value: 30e9},
	}}
	return store
}

// spec: thresholds.md#form — any series: its name above its node, a link to its history,
// and "Cancel"; the page on its own leads back to "All series", in the reader's language.
func TestThresholdFormNamesTheSeriesAndTheWayOut(t *testing.T) {
	_, body := openForm(t, holding(dataSeries), dataAddress+"&lang=ru")
	name := strings.Index(body, "disk.free_bytes /data")
	node := strings.Index(body, `<p class="meta">server-b</p>`)
	if name < 0 || node < name {
		t.Errorf("page = %s, want the series' name above its node", body)
	}
	containsAll(t, "page", body,
		`href="/history?`, "История этой серии",
		`<a class="button" href="/debug?lang=ru" data-dismiss>Отмена</a>`,
		`<p class="crumbs"><a href="/debug?lang=ru">Все серии</a></p>`)
}

// spec: thresholds.md#form — points in the last day: the newest value and a chart with the
// stored values drawn across it, carrying what a script needs to move the lines.
func TestThresholdFormDrawsTheLastDay(t *testing.T) {
	store := withPoints(holding(dataSeries))
	warning := 20e9
	store.held[key(dataSeries)] = storage.Threshold{Series: dataSeries, Direction: storage.Below, Warning: &warning}

	_, body := openForm(t, store, dataAddress)

	containsAll(t, "page", body, "30.0 GB", `<svg class="spark"`, `class="threshold level-warning"`,
		`data-scale="4.4e&#43;10"`, "&#34;gb&#34;:1000000000")
}

// spec: thresholds.md#form — the chart stays when the form is redrawn with a refusal or the
// list of every series, so the form does not jump under the reader.
func TestARedrawnFormKeepsItsChart(t *testing.T) {
	store := withPoints(holding(dataSeries))
	refused := saveForm(t, store, dataAddress, form("below", "10GB", "20GB"), "").Body.String()
	listed := saveForm(t, store, dataAddress, applying(form("below", "20GB", "10GB"), "every"), "").Body.String()
	for name, body := range map[string]string{"refused": refused, "listed": listed} {
		if !strings.Contains(body, `<svg class="spark"`) {
			t.Errorf("%s: want the chart: %s", name, body)
		}
	}
}

// spec: thresholds.md#form — no point in the last day, or points that cannot be read: the
// same form with no chart.
func TestThresholdFormStandsWithoutItsChart(t *testing.T) {
	broken := withPoints(holding(dataSeries))
	broken.pointsErr = errors.New("disk on fire")
	for name, store := range map[string]judged{"no points": holding(dataSeries), "unreadable": broken} {
		status, body := openForm(t, store, dataAddress)
		if status != http.StatusOK || strings.Contains(body, `<svg class="spark"`) {
			t.Errorf("%s: status %d, want the form with no chart: %s", name, status, body)
		}
		if valueOf(t, body, "warning") != "" {
			t.Errorf("%s: want the form as ever", name)
		}
	}
}

// spec: thresholds.md#form — off, the switch still says something: a hidden value after the
// box, so the box wins when it is on.
func TestThresholdFormSwitchSaysExcludeWhenOff(t *testing.T) {
	_, body := openForm(t, holding(dataSeries), dataAddress)
	box := strings.Index(body, `type="checkbox" name="anomalies" value="show"`)
	hidden := strings.Index(body, `type="hidden" name="anomalies" value="exclude"`)
	if box < 0 || hidden < box {
		t.Errorf("page = %s, want the box first and the hidden value after it", body)
	}
}

// spec: thresholds.md#saving — the answer to a save is the form saying it was saved, and a
// save from there goes to the address without the mark.
func TestAcceptedSaveSaysSo(t *testing.T) {
	store := holding(dataSeries)
	rec := saveForm(t, store, dataAddress, form("below", "20GB", "10GB"), "")
	location := rec.Header().Get("Location")
	if location != dataAddress+"&saved=1" {
		t.Fatalf("Location = %q, want the form marked saved", location)
	}

	status, body := openForm(t, store, location)
	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, body)
	}
	containsAll(t, "page", body, "data-saved", "Saved. It applies from the next check.")
	if strings.Contains(body, `action="`+strings.ReplaceAll(location, "&", "&amp;")) {
		t.Errorf("page = %s, want a form that posts without the mark", body)
	}
	if _, plain := openForm(t, store, dataAddress); strings.Contains(plain, "data-saved>") {
		t.Errorf("page = %s, want no such line on the form opened anew", plain)
	}
}

// spec: thresholds.md#saving — a decimal comma is the point it stands for; a single comma
// is always decimal; two separators are refused.
func TestThresholdSaveTakesADecimalComma(t *testing.T) {
	load := storage.SeriesRef{Node: "server-b", Metric: "load.avg_5m"}
	age := storage.SeriesRef{Node: "server-b", Metric: "job.age_seconds"}
	for _, tc := range []struct {
		ref     storage.SeriesRef
		address string
		written string
		want    float64
	}{
		{load, "/thresholds?metric=load.avg_5m&node=server-b", "6,5", 6.5},
		{load, "/thresholds?metric=load.avg_5m&node=server-b", "1,000", 1},
		{age, "/thresholds?metric=job.age_seconds&node=server-b", "1,5h", 5400},
	} {
		store := holding(tc.ref)
		if rec := saveForm(t, store, tc.address, form("above", tc.written, ""), ""); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s answered %d: %s", tc.written, rec.Code, rec.Body)
		}
		if got := store.held[key(tc.ref)].Warning; got == nil || *got != tc.want {
			t.Errorf("%s stored %v, want %v", tc.written, got, tc.want)
		}
	}

	store := holding(load)
	rec := saveForm(t, store, "/thresholds?metric=load.avg_5m&node=server-b", form("above", "1,000.5", ""), "")
	if rec.Code != http.StatusBadRequest || len(store.held) != 0 {
		t.Errorf("1,000.5 answered %d and stored %v, want a refusal", rec.Code, store.held)
	}
}

// spec: web.md#dialog — what the dialog says beyond the form comes with the form, so it
// needs no answer from the hub and is in the reader's language.
func TestThresholdFormCarriesTheDialogsWords(t *testing.T) {
	_, body := openForm(t, holding(dataSeries), dataAddress+"&lang=ru")
	containsAll(t, "page", body, `aria-label="Закрыть"`, "Изменения не сохранены", "Закрыть без сохранения",
		"Вернуться к форме", "Не сохранено: хаб не ответил")
}
