package hub_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// limits are the stored thresholds of the series a card shows, keyed as held keys them.
type limits map[string]storage.Threshold

func (l limits) ThresholdOf(_ context.Context, ref storage.SeriesRef) (storage.Threshold, bool, error) {
	th, ok := l[heldKey(ref)]
	return th, ok, nil
}

func below(warning, critical *float64) storage.Threshold {
	return storage.Threshold{Direction: storage.Below, Warning: warning, Critical: critical}
}

func f(v float64) *float64 { return &v }

var (
	mainPattern   = regexp.MustCompile(`(?s)<li class="row main" data-series="([^"]*)">`)
	restPattern   = regexp.MustCompile(`(?s)<li class="row rest" data-series="([^"]*)">`)
	reasonPattern = regexp.MustCompile(`(?s)<p class="card-reason">(.*?)</p>`)
)

func showCard(t *testing.T, current state.State, source held, stored limits, name string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	read := func(context.Context) (state.State, error) { return current, nil }
	hub.Overview(read, historyOf(source), stored, layout()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return card(t, rec.Body.String(), name)
}

func captured(pattern *regexp.Regexp, body string) []string {
	var out []string
	for _, found := range pattern.FindAllStringSubmatch(body, -1) {
		out = append(out, found[1])
	}
	return out
}

func reason(body string) string {
	if found := reasonPattern.FindStringSubmatch(body); found != nil {
		return strings.TrimSpace(found[1])
	}
	return ""
}

func volumeRef(mount string) storage.SeriesRef {
	return storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": mount, "fs": "ext4", "removable": "false"}}
}

// spec: overview.md#card
func TestOverviewCard(t *testing.T) {
	computer := []state.Subject{freePct("server-b", "/", 40), plain("server-b", "memory.available_pct", 50),
		plain("server-b", "load.avg_15m", 1), plain("server-b", "systemd.failed_units", 0)}

	t.Run("the first two rows are the main numbers, the rest a run", func(t *testing.T) {
		body := showCard(t, stateOf(computer...), held{}, limits{}, "server-b")
		equal(t, "main", captured(mainPattern, body), []string{"disk.free_pct /", "memory.available_pct"})
		equal(t, "rest", captured(restPattern, body), []string{"load.avg_15m", "systemd.failed_units"})
	})

	t.Run("one row", func(t *testing.T) {
		body := showCard(t, stateOf(freePct("server-b", "/", 40)), held{}, limits{}, "server-b")
		equal(t, "main", captured(mainPattern, body), []string{"disk.free_pct /"})
		if !strings.Contains(body, `<ul class="main single">`) {
			t.Errorf("a lone main number is not at full width: %s", body)
		}
	})

	points := held{heldKey(volumeRef("/srv")): {{TS: lastSeen.Add(-10 * time.Minute), Value: 12}, {TS: lastSeen.Add(-5 * time.Minute), Value: 4}}}
	t.Run("threshold lines on a main number", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/srv", 4), evaluate.Critical)), points,
			limits{heldKey(volumeRef("/srv")): below(f(10), f(5))}, "server-b")
		if !strings.Contains(body, `class="threshold level-warning"`) || !strings.Contains(body, `class="threshold level-critical"`) {
			t.Errorf("no threshold lines: %s", body)
		}
		if !strings.Contains(body, `<polygon class="area"`) {
			t.Errorf("the line has no filled area: %s", body)
		}
	})

	t.Run("a negative threshold draws no line", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/srv", 4), evaluate.Warning)), points,
			limits{heldKey(volumeRef("/srv")): below(f(10), f(-5))}, "server-b")
		if strings.Count(body, `class="threshold`) != 1 {
			t.Errorf("want only the warning line: %s", body)
		}
	})

	t.Run("no point in the last day, no threshold lines", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/srv", 4), evaluate.Critical)), held{},
			limits{heldKey(volumeRef("/srv")): below(f(10), f(5))}, "server-b")
		if strings.Contains(body, "<svg") {
			t.Errorf("a line with no points: %s", body)
		}
	})

	t.Run("a row in the run keeps its mark", func(t *testing.T) {
		current := stateOf(judgedAt(freePct("server-b", "/a", 4), evaluate.Critical), judgedAt(freePct("server-b", "/b", 4), evaluate.Critical),
			judgedAt(freePct("server-b", "/c", 4), evaluate.Critical))
		body := showCard(t, current, held{}, limits{}, "server-b")
		if got := rowMark(t, body, "server-b", "disk.free_pct /c"); got != "critical" {
			t.Errorf("the third row is marked %q, want critical", got)
		}
	})

	t.Run("a critical card says the threshold it crossed", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/srv", 7), evaluate.Critical)), held{},
			limits{heldKey(volumeRef("/srv")): below(f(10), f(5))}, "server-b")
		if got := reason(body); got != "disk.free_pct /srv below 5.0%" {
			t.Errorf("reason = %q", got)
		}
	})

	t.Run("a threshold with no value for the level", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/srv", 7), evaluate.Critical)), held{},
			limits{heldKey(volumeRef("/srv")): below(f(10), nil)}, "server-b")
		if got := reason(body); got != "disk.free_pct /srv: critical" {
			t.Errorf("reason = %q", got)
		}
	})

	t.Run("an unusual card says the usual value", func(t *testing.T) {
		body := showCard(t, stateOf(ranked(free("server-b", "/", 9e9), 1)), held{}, limits{}, "server-b")
		if got := reason(body); got != "disk.free_bytes / usually 40.0 GB" {
			t.Errorf("reason = %q", got)
		}
	})

	t.Run("an ok card with nothing marked", func(t *testing.T) {
		body := showCard(t, stateOf(judgedAt(freePct("server-b", "/", 40), evaluate.OK)), held{}, limits{}, "server-b")
		if got := reason(body); got != "All within thresholds" {
			t.Errorf("reason = %q", got)
		}
	})

	t.Run("an ok card whose first row is stale", func(t *testing.T) {
		current := stateOf(staled(judgedAt(freePct("server-b", "/x", 4), evaluate.Critical)), judgedAt(freePct("server-b", "/", 40), evaluate.OK))
		body := showCard(t, current, held{}, limits{}, "server-b")
		if got, want := reason(body), "disk.free_pct /x: no fresh data since 2026-08-28 09:05 UTC"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})

	t.Run("no fresh data says when it last reported, once", func(t *testing.T) {
		body := showCard(t, stateOf(staled(freePct("server-b", "/", 40))), held{}, limits{}, "server-b")
		if got := reason(body); !strings.HasPrefix(got, "last report ") || strings.Count(body, "last report") != 1 {
			t.Errorf("reason = %q in %s", got, body)
		}
	})

	t.Run("reporting with nothing watched gives no reason", func(t *testing.T) {
		if body := showCard(t, stateOf(computer...), held{}, limits{}, "server-b"); strings.Contains(body, "card-reason") {
			t.Errorf("a reason on a card that watches nothing: %s", body)
		}
	})
}
