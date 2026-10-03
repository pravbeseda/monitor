package accesslog_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/accesslog"
)

const browser = "Mozilla/5.0 (X11; Linux x86_64)"

var start = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func logLine(target string, status int, at time.Time) string {
	return fmt.Sprintf(`203.0.113.7 - - [%s] "GET %s HTTP/1.1" %d 5 "-" "%s" rt=0.100`+"\n",
		at.Format("02/Jan/2006:15:04:05 -0700"), target, status, browser)
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// rig is a sensor over a clock the test moves and a list of sites it edits.
type rig struct {
	t      *testing.T
	sensor *accesslog.Sensor
	clock  time.Time
	sites  []accesslog.Site
	dir    string
}

func newRig(t *testing.T, names ...string) *rig {
	r := &rig{t: t, clock: start, dir: t.TempDir()}
	for _, name := range names {
		r.sites = append(r.sites, accesslog.Site{Name: name, Log: r.path(name)})
		appendTo(t, r.path(name), "")
	}
	r.sensor = accesslog.New(func() []accesslog.Site { return r.sites }, func() time.Time { return r.clock })
	// Reading back left running would log into the next test's capture.
	t.Cleanup(func() {
		r.sensor.WaitReadBack()
		r.sensor.Close()
	})
	return r
}

func (r *rig) path(name string) string { return filepath.Join(r.dir, name+".access.log") }

// collect advances the clock by step, collects, and returns the figures per site.
func (r *rig) collect(step time.Duration) (map[string]map[string]float64, error) {
	r.t.Helper()
	r.clock = r.clock.Add(step)
	got, err := r.sensor.Collect(context.Background())
	out := map[string]map[string]float64{}
	for _, m := range got {
		if !m.TS.Equal(r.clock) || len(m.Labels) != 1 {
			r.t.Fatalf("unexpected measurement %+v", m)
		}
		site := m.Labels["site"]
		if out[site] == nil {
			out[site] = map[string]float64{}
		}
		out[site][m.Metric] = m.Value
	}
	return out, err
}

func (r *rig) settled(step time.Duration) map[string]map[string]float64 {
	r.t.Helper()
	got, err := r.collect(step)
	if err != nil {
		r.t.Fatalf("Collect: %v", err)
	}
	return got
}

var _ sensor.Sensor = (*accesslog.Sensor)(nil)

// spec: site-traffic.md#rotation — the first collection of a new site reports nothing for
// it and starts reading back; the interval metrics start with the next one, and the
// windows join once reading back ends.
func TestFirstCollectionReadsBack(t *testing.T) {
	r := newRig(t, "blog-a")
	appendTo(t, r.path("blog-a"), logLine("/", 200, start.Add(-2*time.Hour))+logLine("/", 200, start.Add(-30*time.Minute)))
	if got := r.settled(0); len(got) != 0 {
		t.Fatalf("first collection reported %v", got)
	}
	r.sensor.WaitReadBack()
	appendTo(t, r.path("blog-a"), logLine("/", 200, start.Add(time.Minute)))
	got := r.settled(5 * time.Minute)["blog-a"]
	want := map[string]float64{
		"site.requests_per_min": 0.2, "site.client_error_pct": 0, "site.server_error_pct": 0,
		"site.response_p95_seconds": 0.1, "site.pageviews_24h": 3,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// spec: site-traffic.md#reading — reading back still running: the interval metrics only,
// counting from the end of the log as reading back found it.
func TestReadingBackStillRunning(t *testing.T) {
	r := newRig(t, "blog-a")
	release := r.sensor.HoldReadBack()
	defer release()
	r.settled(0)
	appendTo(t, r.path("blog-a"), logLine("/", 404, start))
	got := r.settled(5 * time.Minute)["blog-a"]
	want := map[string]float64{
		"site.requests_per_min": 0.2, "site.client_error_pct": 100, "site.server_error_pct": 0,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// spec: site-traffic.md#reading — one site's missing log costs the others nothing, and the
// error names the site and its path.
func TestMissingLogCostsTheOtherSitesNothing(t *testing.T) {
	r := newRig(t, "blog-a")
	r.sites = append(r.sites, accesslog.Site{Name: "shop-c", Log: r.path("shop-c")})
	_, _ = r.collect(0)
	r.sensor.WaitReadBack()
	got, err := r.collect(5 * time.Minute)
	if err == nil || !strings.Contains(err.Error(), "shop-c") || !strings.Contains(err.Error(), r.path("shop-c")) {
		t.Fatalf("error %v names neither shop-c nor its path", err)
	}
	if _, ok := got["blog-a"]; !ok || len(got) != 1 {
		t.Fatalf("got %v, want blog-a's metrics only", got)
	}
}

// spec: site-traffic.md#reading — new lines none of which are combined: no interval metric
// and an error naming the site; the windows still report.
func TestALogInAnotherFormat(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	appendTo(t, r.path("blog-a"), `{"status":200}`+"\n")
	got, err := r.collect(5 * time.Minute)
	if err == nil || !strings.Contains(err.Error(), "blog-a") || !strings.Contains(err.Error(), "combined") {
		t.Fatalf("error %v does not say blog-a's log is not combined", err)
	}
	want := map[string]float64{"site.pageviews_24h": 0}
	if fmt.Sprint(got["blog-a"]) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got["blog-a"], want)
	}
}

// spec: site-traffic.md#reading — a line in no recognised format among valid ones is skipped.
func TestAStrayLineIsSkipped(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	appendTo(t, r.path("blog-a"), "garbage\n"+logLine("/", 200, start))
	got := r.settled(5 * time.Minute)["blog-a"]
	if got["site.requests_per_min"] != 0.2 {
		t.Fatalf("got %v, want one request counted", got)
	}
}

// spec: site-traffic.md#reading — a collection due while the previous one still reads
// starts nothing and says so.
func TestOneCollectionAtATime(t *testing.T) {
	blocked := make(chan struct{})
	entered := make(chan struct{})
	s := accesslog.New(func() []accesslog.Site {
		close(entered)
		<-blocked
		return nil
	}, time.Now)
	t.Cleanup(s.Close)
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = s.Collect(context.Background()) })
	<-entered
	_, err := s.Collect(context.Background())
	close(blocked)
	wg.Wait()
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("error %v, want one saying the previous collection is still running", err)
	}
}

// spec: site-traffic.md#rotation — a site moved away has its position dropped; given back,
// it starts over as a site not read yet.
func TestASiteMovedAwayStartsOver(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	kept := r.sites
	r.sites = nil
	if got := r.settled(5 * time.Minute); len(got) != 0 {
		t.Fatalf("a site moved away still reported %v", got)
	}
	r.sites = kept
	if got := r.settled(5 * time.Minute); len(got) != 0 {
		t.Fatalf("a site given back reported %v on its first collection", got)
	}
}

// spec: site-traffic.md#the-24-hour-window — rotated files that reach back only part of the
// day: a warning naming the site and how far back the window reaches.
func TestAShortWindowIsAWarning(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := newRig(t, "blog-a")
	appendTo(t, r.path("blog-a"), logLine("/", 200, start.Add(-10*time.Hour)))
	r.settled(0)
	r.sensor.WaitReadBack()
	text := logged.String()
	if !strings.Contains(text, "level=WARN") || !strings.Contains(text, "blog-a") || !strings.Contains(text, "02:00:00") {
		t.Fatalf("logged %q, want a warning naming blog-a and 02:00", text)
	}
}

// spec: site-traffic.md#the-24-hour-window — a clock set back: no interval metric, the
// window as usual.
func TestAClockSetBack(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	got := r.settled(-time.Minute)["blog-a"]
	want := map[string]float64{"site.pageviews_24h": 0}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// spec: site-traffic.md#reading — bots and static files count as requests, never as views.
func TestBotsAndStaticFilesAreRequestsNotViews(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	appendTo(t, r.path("blog-a"),
		strings.Replace(logLine("/", 200, start), browser, "Googlebot/2.1", 1)+logLine("/style.css", 200, start))
	got := r.settled(5 * time.Minute)["blog-a"]
	if got["site.requests_per_min"] != 0.4 || got["site.pageviews_24h"] != 0 {
		t.Fatalf("got %v, want 0.4 requests a minute and no page view", got)
	}
}

// spec: site-traffic.md#reading — an interval with no line, and no request in the last hour:
// zeroes, no response time, and the page views still in the window.
func TestAQuietInterval(t *testing.T) {
	r := newRig(t, "blog-a")
	appendTo(t, r.path("blog-a"), logLine("/", 200, start.Add(-3*time.Hour)))
	r.settled(0)
	r.sensor.WaitReadBack()
	got := r.settled(5 * time.Minute)["blog-a"]
	want := map[string]float64{
		"site.requests_per_min": 0, "site.client_error_pct": 0, "site.server_error_pct": 0,
		"site.pageviews_24h": 1,
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// spec: site-traffic.md#the-24-hour-window — a log holding no request at all reaches nowhere,
// which is a warning too.
func TestAnEmptyLogIsAWarning(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	if text := logged.String(); !strings.Contains(text, "level=WARN") || !strings.Contains(text, "blog-a") {
		t.Fatalf("logged %q, want a warning naming blog-a", text)
	}
}

// spec: site-traffic.md#the-24-hour-window — a new site whose log holds no request: 0.
func TestAnEmptyLogHoldsNoView(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	if got := r.settled(5 * time.Minute)["blog-a"]["site.pageviews_24h"]; got != 0 {
		t.Fatalf("pageviews_24h %v, want 0", got)
	}
}

// spec: agent.md#applying-configuration — a sensor told to release its logs lets them go, so
// a site given back starts over; told while a collection still reads, it lets them go once
// that collection ends, without waiting for it.
func TestCloseReleasesTheLogs(t *testing.T) {
	r := newRig(t, "blog-a")
	r.settled(0)
	r.sensor.WaitReadBack()
	r.sensor.Close()
	if got := r.settled(5 * time.Minute); len(got) != 0 {
		t.Fatalf("after Close the site reported %v, want it read anew", got)
	}
	r.sensor.WaitReadBack()

	blocked, entered := make(chan struct{}), make(chan struct{})
	sites := r.sites
	s := accesslog.New(func() []accesslog.Site {
		select {
		case <-entered:
		default:
			close(entered)
			<-blocked
		}
		return sites
	}, func() time.Time { return r.clock })
	t.Cleanup(func() {
		s.WaitReadBack()
		s.Close()
	})
	done := make(chan struct{})
	go func() {
		_, _ = s.Collect(context.Background())
		close(done)
	}()
	<-entered
	s.Close() // returns while the collection reads
	close(blocked)
	<-done
	s.WaitReadBack()
	if got, err := s.Collect(context.Background()); len(got) != 0 || err != nil {
		t.Fatalf("after a deferred Close the site reported %v (%v), want it read anew", got, err)
	}
}
