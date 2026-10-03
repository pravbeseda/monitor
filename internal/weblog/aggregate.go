package weblog

import (
	"math"
	"slices"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

// Figure is one value an aggregator reports.
type Figure struct {
	Metric string
	Value  float64
}

// Aggregator turns the requests of one site into the figures of one or more metrics.
type Aggregator interface {
	Add(r Request)
	// Report returns the figures at now, interval being the time since the previous report
	// by the same clock.
	Report(now time.Time, interval time.Duration) []Figure
}

// Window is an aggregator that looks back over a span of time, which reading back fills
// when a log is first read.
type Window interface {
	Aggregator
	Span() time.Duration
}

// Aggregators returns, fresh for one site, the aggregators of every figure the sensor
// reports: those that count only the interval since the previous report, and the windows.
func Aggregators() (interval []Aggregator, windows []Window) {
	return []Aggregator{NewInterval()}, []Window{NewResponseTime(), NewPageViews()}
}

// Interval counts the requests of one interval: their rate and the shares that failed.
type Interval struct {
	requests, client, server int
}

// NewInterval returns an Interval with nothing counted.
func NewInterval() *Interval { return &Interval{} }

// Add counts one request.
func (a *Interval) Add(r Request) {
	a.requests++
	switch {
	case r.Status >= 400 && r.Status < 500:
		a.client++
	case r.Status >= 500 && r.Status < 600:
		a.server++
	}
}

// Report starts the next interval afresh. An interval that is not positive — a clock set
// back — reports nothing, since no rate can be taken over it.
func (a *Interval) Report(_ time.Time, interval time.Duration) []Figure {
	counted := *a
	*a = Interval{}
	if interval <= 0 {
		return nil
	}
	share := func(n int) float64 {
		if counted.requests == 0 {
			return 0
		}
		return sensor.Round2(100 * float64(n) / float64(counted.requests))
	}
	return []Figure{
		{Metric: "site.requests_per_min", Value: sensor.Round2(float64(counted.requests) / interval.Minutes())},
		{Metric: "site.client_error_pct", Value: share(counted.client)},
		{Metric: "site.server_error_pct", Value: share(counted.server)},
	}
}

const responseTimeWindow = time.Hour

// ResponseTime holds the request times of the last hour, so that a quiet site still has a
// reading at night.
type ResponseTime struct {
	samples []sample
}

type sample struct {
	at      time.Time
	seconds float64
}

// NewResponseTime returns a ResponseTime holding no request.
func NewResponseTime() *ResponseTime { return &ResponseTime{} }

// Add counts one request.
func (a *ResponseTime) Add(r Request) {
	if r.HasDuration {
		a.samples = append(a.samples, sample{at: r.Time, seconds: r.Duration})
	}
}

// Report gives the nearest-rank 95th percentile of the times inside the hour before now,
// and nothing when there is none. A time ahead of now is kept for later.
func (a *ResponseTime) Report(now time.Time, _ time.Duration) []Figure {
	from := now.Add(-responseTimeWindow)
	a.samples = slices.DeleteFunc(a.samples, func(s sample) bool { return !s.at.After(from) })
	var inside []float64
	for _, s := range a.samples {
		if !s.at.After(now) {
			inside = append(inside, s.seconds)
		}
	}
	if len(inside) == 0 {
		return nil
	}
	slices.Sort(inside)
	rank := int(math.Ceil(0.95 * float64(len(inside))))
	return []Figure{{Metric: "site.response_p95_seconds", Value: sensor.Round2(inside[rank-1])}}
}

// Span is the hour the percentile is taken over.
func (a *ResponseTime) Span() time.Duration { return responseTimeWindow }

const pageViewMinutes = 24 * 60

// PageViews counts page views per minute over a sliding day: a count per minute, never a
// line.
type PageViews struct {
	perMinute map[int64]int
}

// NewPageViews returns a PageViews holding no view.
func NewPageViews() *PageViews { return &PageViews{perMinute: map[int64]int{}} }

// Add counts one request.
func (a *PageViews) Add(r Request) {
	if r.PageView() {
		a.perMinute[minute(r.Time)]++
	}
}

// Report counts the views of the 1440 minutes ending with now's minute. A view ahead of it
// is kept until its minute comes.
func (a *PageViews) Report(now time.Time, _ time.Duration) []Figure {
	last := minute(now)
	total := 0
	for m, n := range a.perMinute {
		switch {
		case m <= last-pageViewMinutes:
			delete(a.perMinute, m)
		case m <= last:
			total += n
		}
	}
	return []Figure{{Metric: "site.pageviews_24h", Value: float64(total)}}
}

// Span is the day the views are counted over.
func (a *PageViews) Span() time.Duration { return pageViewMinutes * time.Minute }

func minute(t time.Time) int64 {
	return t.Unix() / 60
}
