package hub_test

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/storage"
)

func (j judged) ConfigureEvery(_ context.Context, metric string, th *storage.Threshold, own storage.SeriesRef, exclude bool) error {
	for _, one := range j.series {
		if one.Metric != metric {
			continue
		}
		if th == nil {
			delete(j.held, key(one.SeriesRef))
			continue
		}
		applied := *th
		applied.Series = one.SeriesRef
		j.held[key(one.SeriesRef)] = applied
	}
	if exclude {
		j.excluded[key(own)] = true
	} else {
		delete(j.excluded, key(own))
	}
	return nil
}

func flagOf(node, backup, job string) storage.SeriesRef {
	return storage.SeriesRef{Node: node, Metric: "backup.failed", Labels: map[string]string{"backup": backup, "job": job}}
}

// flags are the four failure flags of #all: two on server-b, two on server-c, which the
// file does not name.
var flags = []storage.SeriesRef{
	flagOf("server-c", "web", "verify"), flagOf("server-b", "web", "copy"),
	flagOf("server-c", "web", "copy"), flagOf("server-b", "vpn", "copy"),
}

const flagAddress = "/thresholds?label.backup=web&label.job=copy&metric=backup.failed&node=server-b"

func applying(values url.Values, step string) url.Values {
	out := maps.Clone(values)
	out.Set("apply", step)
	return out
}

var everyPattern = regexp.MustCompile(`(?s)<li class="every-series">(.*?)</li>`)

// listed is the series the page lists for confirmation, as text.
func listed(body string) []string {
	var out []string
	for _, found := range everyPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.Join(strings.Fields(tagPattern.ReplaceAllString(found[1], " ")), " "))
	}
	return out
}

func heldBy(store judged) map[string]storage.Threshold {
	return maps.Clone(store.held)
}

// spec: thresholds.md#all — the form offers to apply its values to every series.
func TestThresholdFormOffersEverySeries(t *testing.T) {
	_, body := openForm(t, holding(flags...), flagAddress)
	containsAll(t, "form", body, `<button type="submit" name="apply" value="every">Apply to every series of this metric</button>`)
}

// spec: thresholds.md#all — the button lists every series of the metric by node, then name,
// with its current threshold, keeps what was typed, and stores nothing.
func TestApplyingToEverySeriesAsksFirst(t *testing.T) {
	store := holding(append(flags, dataSeries)...)
	two := 2.0
	store.held[key(flags[0])] = storage.Threshold{Series: flags[0], Direction: storage.Above, Critical: &two}
	before := heldBy(store)

	rec := saveForm(t, store, flagAddress, applying(form("above", "0", ""), "every"), "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, body)
	}
	got := listed(body)
	if len(got) != 4 {
		t.Fatalf("listed %q, want the four flags", got)
	}
	for i, want := range [][]string{{"server-b", "vpn", "none"}, {"server-b", "web", "none"}, {"server-c", "copy", "none"}, {"server-c", "verify", "above the threshold", "critical 2"}} {
		for _, part := range want {
			if !strings.Contains(got[i], part) {
				t.Errorf("item %d = %q, want %q in it", i, got[i], part)
			}
		}
	}
	containsAll(t, "page", body, "will be replaced", "the switch is this series",
		`<button type="submit" name="apply" value="confirm">Confirm</button>`)
	if valueOf(t, body, "warning") != "0" || !checked(body, "above") {
		t.Errorf("the typed values were not kept: %s", body)
	}
	if strings.Contains(body, `<meta name="monitor-live"`) {
		t.Error("the list declares itself live, so it would be refreshed away")
	}
	if len(store.held) != len(before) {
		t.Errorf("stored %v before Confirm", store.held)
	}

	t.Run("blank", func(t *testing.T) {
		body := saveForm(t, store, flagAddress, applying(form("above", "", ""), "every"), "").Body.String()
		containsAll(t, "page", body, "will be removed")
	})
	t.Run("one series", func(t *testing.T) {
		body := saveForm(t, holding(flags[1]), flagAddress, applying(form("above", "0", ""), "every"), "").Body.String()
		if got := listed(body); len(got) != 1 {
			t.Errorf("listed %q, want the one series", got)
		}
	})
	t.Run("a value a save refuses", func(t *testing.T) {
		rec := saveForm(t, store, flagAddress, applying(form("above", "lots", ""), "every"), "")
		if rec.Code != http.StatusBadRequest || len(listed(rec.Body.String())) != 0 {
			t.Errorf("status %d, listed %q: want the save's refusal", rec.Code, listed(rec.Body.String()))
		}
	})
	t.Run("in Russian", func(t *testing.T) {
		body := saveForm(t, store, flagAddress+"&lang=ru", applying(form("above", "0", ""), "every"), "").Body.String()
		containsAll(t, "page", body, "Подтвердить", "Применить ко всем сериям этой метрики", "нет")
	})
}

// spec: thresholds.md#all — "Confirm" applies the form to every series of the metric, the
// switch to this one only, and answers with a redirect.
func TestConfirmingAppliesToEverySeries(t *testing.T) {
	store := holding(append(flags, dataSeries)...)
	rec := saveForm(t, store, flagAddress, excluding(applying(form("above", "0", ""), "confirm")), "")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != flagAddress {
		t.Fatalf("status %d to %q, want a redirect to the form", rec.Code, rec.Header().Get("Location"))
	}
	for _, ref := range flags {
		th, ok := store.held[key(ref)]
		if !ok || th.Direction != storage.Above || th.Warning == nil || *th.Warning != 0 || th.Critical != nil {
			t.Errorf("%v = %+v, want above, warning 0", ref.Labels, th)
		}
	}
	if _, ok := store.held[key(dataSeries)]; ok {
		t.Error("another metric's series was changed")
	}
	if !store.excluded[key(flags[1])] || len(store.excluded) != 1 {
		t.Errorf("excluded %v, want only the opened series", store.excluded)
	}

	t.Run("blank", func(t *testing.T) {
		saveForm(t, store, flagAddress, applying(form("above", "", ""), "confirm"), "")
		if len(store.held) != 0 {
			t.Errorf("held %v, want every threshold removed", store.held)
		}
	})
	t.Run("a value a save refuses", func(t *testing.T) {
		rec := saveForm(t, store, flagAddress, applying(form("above", "2", "1"), "confirm"), "")
		if rec.Code != http.StatusBadRequest || len(store.held) != 0 {
			t.Errorf("status %d, held %v: want the save's refusal, nothing stored", rec.Code, store.held)
		}
	})
}

// spec: thresholds.md#all — either post from another site, or for a series never stored,
// is refused and stores nothing.
func TestApplyingToEverySeriesIsGuardedAsASave(t *testing.T) {
	for _, step := range []string{"every", "confirm"} {
		store := holding(flags...)
		if rec := saveForm(t, store, flagAddress, applying(form("above", "0", ""), step), "https://elsewhere.example"); rec.Code != http.StatusForbidden {
			t.Errorf("%s from another site: status %d, want 403", step, rec.Code)
		}
		if rec := saveForm(t, store, flagAddress, applying(form("above", "0", ""), step), "none"); rec.Code != http.StatusForbidden {
			t.Errorf("%s with no origin: status %d, want 403", step, rec.Code)
		}
		if rec := saveForm(t, store, strings.Replace(flagAddress, "server-b", "server-x", 1), applying(form("above", "0", ""), step), ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s for an unknown series: status %d, want 404", step, rec.Code)
		}
		if len(store.held) != 0 {
			t.Errorf("%s stored %v", step, store.held)
		}
	}
}

// spec: thresholds.md#defaults — a seeded success age reads on its form as it was declared.
func TestASeededAgeReadsInItsUnit(t *testing.T) {
	ref := storage.SeriesRef{Node: "server-b", Metric: "backup.success_age_seconds", Labels: map[string]string{"backup": "web", "job": "copy"}}
	store := holding(ref)
	warning, critical := 108000.0, 172800.0
	store.held[key(ref)] = storage.Threshold{Series: ref, Direction: storage.Above, Warning: &warning, Critical: &critical}
	_, body := openForm(t, store, "/thresholds?label.backup=web&label.job=copy&metric=backup.success_age_seconds&node=server-b")
	if valueOf(t, body, "warning") != "30h" || valueOf(t, body, "critical") != "2d" {
		t.Errorf("form = %s, want 30h and 2d", body)
	}
}
