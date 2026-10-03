package weblog_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/weblog"
)

var clock = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func request(status int, at time.Time) weblog.Request {
	return weblog.Request{Time: at, Method: "GET", Target: "/", Status: status, UserAgent: browser}
}

func timed(seconds float64, at time.Time) weblog.Request {
	r := request(200, at)
	r.Duration, r.HasDuration = seconds, true
	return r
}

func figures(t *testing.T, a weblog.Aggregator, interval time.Duration) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, f := range a.Report(clock, interval) {
		if _, twice := out[f.Metric]; twice {
			t.Fatalf("%s reported twice", f.Metric)
		}
		out[f.Metric] = f.Value
	}
	return out
}

func expect(t *testing.T, got map[string]float64, want map[string]float64) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// spec: site-traffic.md#reading — the rate and the error shares of an interval.
func TestIntervalCountsRateAndShares(t *testing.T) {
	statuses := map[string][]int{
		"7 ok, 2 not found, 1 bad gateway": {200, 200, 200, 200, 200, 200, 200, 404, 404, 502},
		"1 ok, 2 not found":                {200, 404, 404},
	}
	want := map[string]map[string]float64{
		"7 ok, 2 not found, 1 bad gateway": {
			"site.requests_per_min": 2, "site.client_error_pct": 20, "site.server_error_pct": 10,
		},
		"1 ok, 2 not found": {
			"site.requests_per_min": 0.6, "site.client_error_pct": 66.67, "site.server_error_pct": 0,
		},
	}
	for name, list := range statuses {
		t.Run(name, func(t *testing.T) {
			a := weblog.NewInterval()
			for _, status := range list {
				a.Add(request(status, clock))
			}
			expect(t, figures(t, a, 5*time.Minute), want[name])
		})
	}
}

// spec: site-traffic.md#reading — an interval with no line is a rate and shares of zero.
func TestIntervalWithNoRequest(t *testing.T) {
	expect(t, figures(t, weblog.NewInterval(), 5*time.Minute), map[string]float64{
		"site.requests_per_min": 0, "site.client_error_pct": 0, "site.server_error_pct": 0,
	})
}

// spec: site-traffic.md#reading — each interval counts afresh.
func TestIntervalStartsOverAfterAReport(t *testing.T) {
	a := weblog.NewInterval()
	a.Add(request(500, clock))
	figures(t, a, 5*time.Minute)
	expect(t, figures(t, a, 5*time.Minute), map[string]float64{
		"site.requests_per_min": 0, "site.client_error_pct": 0, "site.server_error_pct": 0,
	})
}

// spec: site-traffic.md#the-24-hour-window — a clock set back reports no interval metric.
func TestIntervalWithAClockSetBack(t *testing.T) {
	a := weblog.NewInterval()
	a.Add(request(200, clock))
	expect(t, figures(t, a, -time.Minute), map[string]float64{})
}

// spec: site-traffic.md#reading — the nearest-rank 95th percentile over the last hour.
func TestResponseTimePercentile(t *testing.T) {
	cases := map[string]struct {
		requests []weblog.Request
		want     map[string]float64
	}{
		"twenty from 0.01 to 0.20": {
			requests: func() []weblog.Request {
				var out []weblog.Request
				for i := 1; i <= 20; i++ {
					out = append(out, timed(float64(i)/100, clock.Add(-time.Minute)))
				}
				return out
			}(),
			want: map[string]float64{"site.response_p95_seconds": 0.19},
		},
		"three timed and two not": {
			requests: []weblog.Request{
				timed(0.1, clock), timed(0.2, clock), timed(0.3, clock),
				request(200, clock), request(200, clock),
			},
			want: map[string]float64{"site.response_p95_seconds": 0.3},
		},
		"one at 0.125 rounds half away from zero": {
			requests: []weblog.Request{timed(0.125, clock)},
			want:     map[string]float64{"site.response_p95_seconds": 0.13},
		},
		"one 40 minutes ago": {
			requests: []weblog.Request{timed(0.4, clock.Add(-40*time.Minute))},
			want:     map[string]float64{"site.response_p95_seconds": 0.4},
		},
		"none timed": {
			requests: []weblog.Request{request(200, clock)},
			want:     map[string]float64{},
		},
		"only one an hour old": {
			requests: []weblog.Request{timed(0.4, clock.Add(-time.Hour))},
			want:     map[string]float64{},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a := weblog.NewResponseTime()
			for _, r := range c.requests {
				a.Add(r)
			}
			expect(t, figures(t, a, 5*time.Minute), c.want)
		})
	}
}

// spec: site-traffic.md#the-24-hour-window
func TestPageViewsOverADay(t *testing.T) {
	views := func(n int, at time.Time) []weblog.Request {
		out := make([]weblog.Request, n)
		for i := range out {
			out[i] = request(200, at)
		}
		return out
	}
	cases := map[string]struct {
		requests []weblog.Request
		want     float64
	}{
		"300 from 12:01 yesterday on, and 50 before": {
			requests: append(views(300, clock.Add(-23*time.Hour)), views(50, clock.Add(-25*time.Hour))...),
			want:     300,
		},
		"one at 12:00:30 yesterday": {requests: views(1, clock.Add(-24*time.Hour+30*time.Second)), want: 0},
		"one at 12:01:00 yesterday": {requests: views(1, clock.Add(-24*time.Hour+time.Minute)), want: 1},
		"one logged at 13:30 +0300 today": {
			requests: views(1, time.Date(2026, 10, 3, 13, 30, 0, 0, time.FixedZone("", 3*3600))),
			want:     1,
		},
		"one a minute ahead of the clock": {requests: views(1, clock.Add(time.Minute)), want: 0},
		"requests that are not page views": {
			requests: []weblog.Request{request(404, clock), {Time: clock, Method: "POST", Target: "/", Status: 200, UserAgent: browser}},
			want:     0,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a := weblog.NewPageViews()
			for _, r := range c.requests {
				a.Add(r)
			}
			expect(t, figures(t, a, 5*time.Minute), map[string]float64{"site.pageviews_24h": c.want})
		})
	}
}

// spec: site-traffic.md#the-24-hour-window — a view ahead of the clock counts once the
// clock reaches its minute.
func TestPageViewAheadCountsLater(t *testing.T) {
	a := weblog.NewPageViews()
	a.Add(request(200, clock.Add(time.Minute)))
	if got := a.Report(clock.Add(time.Minute), 5*time.Minute); len(got) != 1 || got[0].Value != 1 {
		t.Fatalf("got %v, want one view", got)
	}
}
