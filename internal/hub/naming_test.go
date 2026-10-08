package hub_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

func site(name string) map[string]string { return map[string]string{"site": name} }

// siteSeries is one site's requests a minute, as the sites node stores them.
func siteSeries(name string) seriesPoints {
	return seriesPoints{
		SeriesRef: storage.SeriesRef{Node: "sites", Metric: "site.requests_per_min", Labels: site(name)},
		Sensor:    "access_log",
		Points:    []storage.Point{{TS: collected.Add(-time.Hour), Value: 1}, {TS: collected, Value: 2}},
	}
}

// spec: history.md#page — what the labels name: two sites never read alike on /debug, on
// the chart and on the thresholds form.
func TestEveryPageNamesASeriesByAllItsLabels(t *testing.T) {
	values := []storage.Value{
		{Metric: "site.requests_per_min", Sensor: "access_log", Labels: site("blog-a"), Value: 1, TS: lastSeen},
		{Metric: "site.requests_per_min", Sensor: "access_log", Labels: site("shop-c"), Value: 2, TS: lastSeen},
	}
	debug := showDebug(t, stored{states: []storage.NodeState{{Node: "sites", LastSeen: lastSeen, Values: values}}}, "/debug")
	for _, want := range []string{"<td>site=blog-a</td>", "<td>site=shop-c</td>"} {
		if !strings.Contains(debug, want) {
			t.Errorf("/debug does not show %q: %s", want, debug)
		}
	}

	store := served{series: []seriesPoints{siteSeries("blog-a"), siteSeries("shop-c")}}
	_, chart := page(t, store, "/history?metric=site.requests_per_min&node=sites&label.site=blog-a")
	if want := "<h1>sites · site.requests_per_min · site=blog-a</h1>"; !strings.Contains(chart, want) {
		t.Errorf("chart heading is not %q: %s", want, chart)
	}
	_, choice := page(t, store, "/history?metric=site.requests_per_min&node=sites")
	for _, want := range []string{"sites · site=blog-a<", "sites · site=shop-c<"} {
		if !strings.Contains(choice, want) {
			t.Errorf("the choice of series does not offer %q: %s", want, choice)
		}
	}

	ref := siteSeries("blog-a").SeriesRef
	_, form := openForm(t, holding(ref), "/thresholds?label.site=blog-a&metric=site.requests_per_min&node=sites")
	if want := "sites · site.requests_per_min · site=blog-a"; !strings.Contains(form, want) {
		t.Errorf("thresholds form does not name %q: %s", want, form)
	}

	spaced := siteSeries("")
	spaced.Metric, spaced.Labels = "disk.free_pct", map[string]string{"mount": "/Volumes/Backup Disk", "fs": "apfs"}
	_, quoted := page(t, served{series: []seriesPoints{spaced}}, "/history?metric=disk.free_pct&node=sites")
	if want := `· &#34;/Volumes/Backup Disk&#34; · apfs</h1>`; !strings.Contains(quoted, want) {
		t.Errorf("a spaced mount is not quoted as %q: %s", want, quoted)
	}
}
