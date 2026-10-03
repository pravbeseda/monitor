// Package accesslog is the site-traffic sensor: it follows the access log of each site the
// hub gives this host and reports, per site, the figures of docs/specs/site-traffic.md.
package accesslog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/weblog"
)

// Name is the sensor id the configuration and the manifest use.
const Name = "access_log"

// Site is one site this host serves, as the hub delivers it.
type Site struct {
	Name string
	Log  string
}

// Sensor holds, per site, where it stopped reading and the windows it keeps — in memory
// only: it never writes.
type Sensor struct {
	sites func() []Site
	now   func() time.Time

	// collecting lets one collection read at a time: one the agent abandoned keeps reading,
	// and the next must not start a second read of the same logs.
	collecting sync.Mutex
	followed   map[string]*site
	// closeAfter is a Close that found a collection reading, which that collection carries
	// out when it ends.
	closeAfter atomic.Bool

	readBack func(b *weblog.Back, since time.Time, each func(weblog.Request)) (time.Time, error)
	backs    sync.WaitGroup
}

type site struct {
	log  string
	tail *weblog.Tail
	last time.Time

	// mu guards the aggregators, which reading back feeds beside the collections.
	mu       sync.Mutex
	interval []weblog.Aggregator
	windows  []weblog.Window
	caughtUp bool
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over the sites the agent last received and the agent's clock.
func New(sites func() []Site, now func() time.Time) *Sensor {
	return &Sensor{
		sites:    sites,
		now:      now,
		followed: map[string]*site{},
		readBack: (*weblog.Back).Read,
	}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return Name }

// Applicable is true everywhere: any machine may serve a site.
func (s *Sensor) Applicable() bool { return true }

// Close releases every log the sensor follows, so that a host no longer serving any site
// holds none open. It never waits for a collection still reading: that one releases them
// when it ends.
func (s *Sensor) Close() {
	s.closeAfter.Store(true)
	s.releaseIfAsked()
}

// releaseIfAsked carries out a Close once no collection reads. Close and the end of a
// collection both call it after their own step, so neither misses the other.
func (s *Sensor) releaseIfAsked() {
	if s.closeAfter.Load() && s.collecting.TryLock() {
		if s.closeAfter.Swap(false) {
			s.release()
		}
		s.collecting.Unlock()
	}
}

func (s *Sensor) release() {
	for name, st := range s.followed {
		st.tail.Close()
		delete(s.followed, name)
	}
}

// Collect reads what each site's log gained since the previous collection. A site whose log
// cannot be read is an error that costs the other sites nothing: their measurements come
// back beside it.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	if !s.collecting.TryLock() {
		return nil, errors.New("the previous collection is still running")
	}
	defer func() {
		s.collecting.Unlock()
		s.releaseIfAsked()
	}()

	now := s.now()
	wanted := s.sites()
	s.forget(wanted)

	var out []sensor.Measurement
	var errs []error
	for _, want := range sortedSites(wanted) {
		st, known := s.followed[want.Name]
		if !known {
			if err := s.follow(want, now); err != nil {
				errs = append(errs, fmt.Errorf("site %s: %w", want.Name, err))
			}
			continue
		}
		figures, err := st.collect(now)
		if err != nil {
			errs = append(errs, fmt.Errorf("site %s: %w", want.Name, err))
		}
		for _, f := range figures {
			out = append(out, sensor.Measurement{
				Metric: f.Metric, Labels: map[string]string{"site": want.Name}, Value: f.Value, TS: now,
			})
		}
	}
	return out, errors.Join(errs...)
}

// forget drops every site no longer delivered, or delivered with another log.
func (s *Sensor) forget(wanted []Site) {
	logs := make(map[string]string, len(wanted))
	for _, w := range wanted {
		logs[w.Name] = w.Log
	}
	for name, st := range s.followed {
		if log, ok := logs[name]; !ok || log != st.log {
			st.tail.Close()
			delete(s.followed, name)
		}
	}
}

// follow opens a site's log at its end and reads back what came before it beside the
// collections, which count the interval from that end meanwhile.
func (s *Sensor) follow(want Site, now time.Time) error {
	tail, back, err := weblog.Open(want.Log)
	if err != nil {
		return err
	}
	interval, windows := weblog.Aggregators()
	st := &site{log: want.Log, tail: tail, last: now, interval: interval, windows: windows}
	s.followed[want.Name] = st

	var reach time.Duration
	for _, w := range windows {
		reach = max(reach, w.Span())
	}
	since := now.Add(-reach)
	s.backs.Go(func() {
		reached, err := s.readBack(back, since, func(r weblog.Request) {
			st.mu.Lock()
			defer st.mu.Unlock()
			// Each window takes only its own span, so the hour of response times is never
			// a day of them while reading back runs.
			for _, w := range st.windows {
				if r.Time.After(now.Add(-w.Span())) {
					w.Add(r)
				}
			}
		})
		if err != nil {
			slog.Error("read back an access log", "site", want.Name, "log", want.Log, "error", err)
		}
		if reached.IsZero() || reached.After(since) {
			slog.Warn("the rotated logs do not cover the window", "site", want.Name, "log", want.Log,
				"window_from", since, "reached", reached)
		}
		st.mu.Lock()
		st.caughtUp = true
		st.mu.Unlock()
	})
	return nil
}

func (st *site) collect(now time.Time) ([]weblog.Figure, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	lines, recognised := 0, 0
	err := st.tail.Read(func(line string) {
		lines++
		r, ok := weblog.Parse(line)
		if !ok {
			return
		}
		recognised++
		for _, a := range st.interval {
			a.Add(r)
		}
		for _, w := range st.windows {
			w.Add(r)
		}
	})
	if err != nil {
		return nil, err
	}
	unreadable := lines > 0 && recognised == 0
	interval := now.Sub(st.last)
	st.last = now

	var figures []weblog.Figure
	for _, a := range st.interval {
		// Reported even when unreadable, so that the next interval starts afresh.
		if reported := a.Report(now, interval); !unreadable {
			figures = append(figures, reported...)
		}
	}
	if st.caughtUp {
		for _, w := range st.windows {
			figures = append(figures, w.Report(now, interval)...)
		}
	}
	if unreadable {
		return figures, fmt.Errorf("%s: no new line is in the combined format", st.log)
	}
	return figures, nil
}

func sortedSites(sites []Site) []Site {
	out := append([]Site(nil), sites...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
