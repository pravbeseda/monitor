package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/agent"
	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/sensor"
)

var start = time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

// stub is a sensor the test drives: it counts calls and returns what it was given.
type stub struct {
	name         string
	calls        int
	measurements []sensor.Measurement
	err          error
}

func (s *stub) Name() string     { return s.name }
func (s *stub) Applicable() bool { return true }

func (s *stub) Collect(context.Context) ([]sensor.Measurement, error) {
	s.calls++
	return s.measurements, s.err
}

// counting returns a fresh batch on every collection, each value carrying the number of
// the collection it came from, so a test can tell which end of the buffer survived.
type counting struct {
	name  string
	size  int
	calls int
}

func (c *counting) Name() string     { return c.name }
func (c *counting) Applicable() bool { return true }

func (c *counting) Collect(context.Context) ([]sensor.Measurement, error) {
	c.calls++
	batch := make([]sensor.Measurement, c.size)
	for i := range batch {
		batch[i] = reading("disk.free_bytes", float64(c.calls*c.size+i))
	}
	return batch, nil
}

// blocking never answers until the test releases it.
type blocking struct {
	name    string
	release chan struct{}
}

func (b *blocking) Name() string     { return b.name }
func (b *blocking) Applicable() bool { return true }

func (b *blocking) Collect(ctx context.Context) ([]sensor.Measurement, error) {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return nil, nil
}

func reading(metric string, value float64) sensor.Measurement {
	return sensor.Measurement{Metric: metric, Labels: map[string]string{"mount": "/"}, Value: value, TS: start}
}

// hub is a fake hub: it records every request and answers from a queue.
type hub struct {
	requests []api.Request
	answers  []answer
}

type answer struct {
	response api.Response
	err      error
}

func (h *hub) Send(_ context.Context, req api.Request) (api.Response, error) {
	h.requests = append(h.requests, req)
	if len(h.answers) == 0 {
		return api.Response{}, nil
	}
	next := h.answers[0]
	if len(h.answers) > 1 {
		h.answers = h.answers[1:]
	}
	return next.response, next.err
}

func (h *hub) last() api.Request { return h.requests[len(h.requests)-1] }

func measurements(req api.Request) []api.Measurement {
	if req.Measurements == nil {
		return nil
	}
	return *req.Measurements
}

// clock is the agent's time, advanced by the test.
type clock struct{ at time.Time }

func (c *clock) now() time.Time          { return c.at }
func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func configure(version string, baseTick string, sensors map[string]api.SensorConfig) api.Response {
	return api.Response{
		ConfigVersion: version,
		Config: &api.AgentConfig{
			BaseTick:    baseTick,
			Filesystems: []string{"apfs"},
			SkipMounts:  []string{"/System/Volumes/"},
			Sensors:     sensors,
		},
	}
}

func newAgent(t *testing.T, h *hub, c *clock, sensors ...sensor.Sensor) *agent.Agent {
	t.Helper()
	return agent.New(agent.Options{Node: "laptop-a", Sensors: sensors, Client: h, Now: c.now})
}

// spec: agent.md#ticking — the first tick announces the agent and asks for configuration.
func TestFirstTickAnnouncesTheAgent(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	a := newAgent(t, h, c, &stub{name: "disk"})

	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	req := h.last()
	if req.Node != "laptop-a" || req.ConfigVersion != "" {
		t.Errorf("request = %+v, want the node and no configuration version", req)
	}
	if req.Measurements == nil || len(*req.Measurements) != 0 {
		t.Errorf("measurements = %v, want an empty batch, not an absent one", req.Measurements)
	}
	if len(req.Manifest) != 1 || req.Manifest[0].Sensor != "disk" || !req.Manifest[0].Applicable {
		t.Errorf("manifest = %+v, want the sensors this build contains", req.Manifest)
	}
}

// spec: agent.md#ticking — the heartbeat carries an empty batch, not an absent one: the
// hub refuses a request whose measurements are missing.
func TestHeartbeatEncodesAnEmptyBatch(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	a := newAgent(t, h, c)

	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	body, err := json.Marshal(h.last())
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if !strings.Contains(string(body), `"measurements":[]`) {
		t.Errorf("request = %s, want an empty measurements array", body)
	}
}

// spec: agent.md#ticking — a sensor whose interval elapsed collects; one that has not, does not.
func TestCollectsOnlyDueSensors(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{
		"disk": {Enabled: true, Interval: "15m"},
	})}}
	disk := &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}}
	a := newAgent(t, h, c, disk)

	if err := a.Tick(context.Background()); err != nil { // configuration arrives here
		t.Fatalf("first tick: %v", err)
	}
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil { // due: never collected before
		t.Fatalf("second tick: %v", err)
	}
	if disk.calls != 1 {
		t.Fatalf("disk collected %d times, want once it was configured", disk.calls)
	}
	if got := measurements(h.last()); len(got) != 1 || got[0].Metric != "disk.free_bytes" {
		t.Errorf("measurements = %+v, want the collected reading", got)
	}

	c.advance(5 * time.Minute) // less than the 15m interval
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("third tick: %v", err)
	}

	if disk.calls != 1 {
		t.Errorf("disk collected %d times, want it left alone until its interval elapses", disk.calls)
	}
	if got := measurements(h.last()); len(got) != 0 {
		t.Errorf("measurements = %+v, want an empty batch when nothing is due", got)
	}
}

// agentAfterOneCollection is an agent whose disk sensor, on an hourly interval, collected
// one tick ago.
func agentAfterOneCollection(t *testing.T) (*agent.Agent, *clock, *stub) {
	t.Helper()
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{
		"disk": {Enabled: true, Interval: "1h"},
	})}}
	disk := &stub{name: "disk"}
	a := newAgent(t, h, c, disk)
	for range 2 { // the first tick brings the configuration, the second collects
		if err := a.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		c.advance(5 * time.Minute)
	}
	if disk.calls != 1 {
		t.Fatalf("disk collected %d times, want once before the test begins", disk.calls)
	}
	return a, c, disk
}

// spec: agent.md#ticking — the time a node spends asleep counts toward the interval.
// This pins the rule, not the regression: the test clock carries no monotonic reading, and
// Go offers no way to build one that stands still while the wall clock moves on.
func TestSleepCountsTowardTheInterval(t *testing.T) {
	a, c, disk := agentAfterOneCollection(t)

	c.advance(3 * time.Hour)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("tick after waking: %v", err)
	}

	if disk.calls != 2 {
		t.Errorf("disk collected %d times, want the first tick after waking to collect", disk.calls)
	}
}

// spec: agent.md#ticking — a clock set forward costs one early collection, then the interval holds.
func TestClockSetForwardCollectsOnceEarly(t *testing.T) {
	a, c, disk := agentAfterOneCollection(t)

	c.advance(2 * time.Hour)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("tick after the clock was set forward: %v", err)
	}
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("next tick: %v", err)
	}

	if disk.calls != 2 {
		t.Errorf("disk collected %d times, want one early collection and then its interval", disk.calls)
	}
}

// spec: agent.md#ticking — a clock set back does not silence a sensor until it catches up.
func TestClockSetBackMakesTheSensorDue(t *testing.T) {
	a, c, disk := agentAfterOneCollection(t)

	c.advance(-2 * time.Hour)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("tick after the clock was set back: %v", err)
	}
	if disk.calls != 2 {
		t.Fatalf("disk collected %d times, want the tick after the clock was set back to collect", disk.calls)
	}

	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("next tick: %v", err)
	}
	if disk.calls != 2 {
		t.Errorf("disk collected %d times, want the interval to run from the collection after the set-back", disk.calls)
	}
}

// spec: agent.md#ticking — a disabled sensor is not called, whatever its interval.
func TestSkipsDisabledSensor(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{
		"disk": {Enabled: false, Interval: "1m"},
	})}}
	disk := &stub{name: "disk"}
	a := newAgent(t, h, c, disk)

	for range 2 {
		if err := a.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %v", err)
		}
		c.advance(10 * time.Minute)
	}

	if disk.calls != 0 {
		t.Errorf("disk collected %d times, want it switched off", disk.calls)
	}
}

// spec: agent.md#ticking — one sensor failing does not stop the tick.
func TestSensorErrorDoesNotStopTheTick(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{
		"disk":    {Enabled: true, Interval: "5m"},
		"battery": {Enabled: true, Interval: "5m"},
	})}}
	broken := &stub{name: "battery", err: errors.New("no battery")}
	working := &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}}
	a := newAgent(t, h, c, broken, working)

	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if got := measurements(h.last()); len(got) != 1 || got[0].Metric != "disk.free_bytes" {
		t.Errorf("measurements = %+v, want the working sensor's reading", got)
	}
}

// spec: agent.md#ticking — a sensor that does not answer is left behind by the tick.
func TestSensorThatHangsDoesNotStopTheTick(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "100ms", map[string]api.SensorConfig{
		"disk":    {Enabled: true, Interval: "100ms"},
		"battery": {Enabled: true, Interval: "100ms"},
	})}}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stuck := &blocking{name: "battery", release: release}
	working := &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}}
	a := newAgent(t, h, c, stuck, working)

	_ = a.Tick(context.Background()) // the configuration arrives here
	c.advance(time.Second)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if got := measurements(h.last()); len(got) != 1 || got[0].Metric != "disk.free_bytes" {
		t.Errorf("measurements = %+v, want the tick to arrive without the stuck sensor", got)
	}
}

// spec: agent.md#delivering — a hub that failed gets the same measurements again.
func TestKeepsMeasurementsUntilTheyAreAccepted(t *testing.T) {
	tests := map[string]error{
		"the hub is down":       agent.StatusError{Status: http.StatusInternalServerError},
		"the hub is busy":       agent.StatusError{Status: http.StatusTooManyRequests},
		"the network is absent": errors.New("dial tcp: no route to host"),
	}

	for name, failure := range tests {
		t.Run(name, func(t *testing.T) {
			h, c := &hub{}, &clock{at: start}
			h.answers = []answer{
				{response: configure("v1", "5m", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "5m"}})},
				{err: failure},
				{},
			}
			disk := &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}}
			a := newAgent(t, h, c, disk)

			_ = a.Tick(context.Background()) // configuration
			c.advance(5 * time.Minute)
			if err := a.Tick(context.Background()); err == nil {
				t.Fatal("Tick reported success while the hub refused it")
			}
			c.advance(5 * time.Minute)
			if err := a.Tick(context.Background()); err != nil {
				t.Fatalf("Tick: %v", err)
			}

			if got := measurements(h.last()); len(got) != 2 {
				t.Errorf("measurements = %+v, want the kept reading resent beside the new one", got)
			}
		})
	}
}

// spec: agent.md#delivering — a rejected shape is dropped rather than resent forever.
func TestDropsMeasurementsTheHubRefused(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{
		{response: configure("v1", "5m", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "5m"}})},
		{err: agent.StatusError{Status: http.StatusBadRequest}},
		{},
	}
	disk := &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}}
	a := newAgent(t, h, c, disk)

	_ = a.Tick(context.Background())
	c.advance(5 * time.Minute)
	_ = a.Tick(context.Background())
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if got := measurements(h.last()); len(got) != 1 {
		t.Errorf("measurements = %+v, want only the new reading", got)
	}
}

// spec: agent.md#delivering — the buffer has a ceiling, and the oldest go first.
func TestBufferDropsTheOldestMeasurements(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{
		{response: configure("v1", "5m", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "5m"}})},
		{err: agent.StatusError{Status: http.StatusInternalServerError}},
		{err: agent.StatusError{Status: http.StatusInternalServerError}},
		{},
	}
	// Every collection fills the buffer on its own, and its values say which one it was.
	disk := &counting{name: "disk", size: agent.BufferLimit}
	a := newAgent(t, h, c, disk)

	_ = a.Tick(context.Background())
	for range 3 {
		c.advance(5 * time.Minute)
		_ = a.Tick(context.Background())
	}

	got := measurements(h.last())
	if len(got) != agent.BufferLimit {
		t.Fatalf("measurements = %d, want the buffer capped at %d", len(got), agent.BufferLimit)
	}
	// The third collection is the newest, so its values are the ones worth keeping.
	newest := float64(disk.calls * agent.BufferLimit)
	first, last := *got[0].Value, *got[len(got)-1].Value
	if first != newest || last != newest+float64(agent.BufferLimit-1) {
		t.Errorf("kept %v..%v, want the newest collection %v..%v",
			first, last, newest, newest+float64(agent.BufferLimit-1))
	}
}

// spec: agent.md#applying-configuration
func TestAppliesConfigurationAndKeepsItUntilItChanges(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{
		{response: configure("v1", "10m", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "10m"}})},
		{}, // an empty body: nothing to change
	}
	a := newAgent(t, h, c, &stub{name: "disk"})

	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if a.BaseTick() != 10*time.Minute {
		t.Errorf("base tick = %v, want the configured 10m", a.BaseTick())
	}
	if fs := a.Filesystems(); len(fs) != 1 || fs[0] != "apfs" {
		t.Errorf("filesystems = %v, want the configured allow-list", fs)
	}
	if skip := a.SkipMounts(); len(skip) != 1 || skip[0] != "/System/Volumes/" {
		t.Errorf("skip_mounts = %v, want the configured skip list", skip)
	}

	c.advance(10 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if h.last().ConfigVersion != "v1" {
		t.Errorf("config_version = %q, want the version the agent now holds", h.last().ConfigVersion)
	}
	if a.BaseTick() != 10*time.Minute {
		t.Errorf("base tick = %v, want it kept while the body is empty", a.BaseTick())
	}
}

// spec: agent.md#applying-configuration — with every sensor off, the heartbeat remains.
func TestKeepsTickingWithoutSensors(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{})}}
	a := newAgent(t, h, c)

	for range 2 {
		if err := a.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %v", err)
		}
		c.advance(5 * time.Minute)
	}

	if len(h.requests) != 2 {
		t.Errorf("sent %d requests, want one per tick", len(h.requests))
	}
}

// spec: agent.md#applying-configuration — a configuration the agent cannot parse is refused
// without losing the one in use.
func TestKeepsWorkingConfigurationWhenTheNewOneIsBroken(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{
		{response: configure("v1", "10m", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "10m"}})},
		{response: configure("v2", "soon", map[string]api.SensorConfig{"disk": {Enabled: true, Interval: "10m"}})},
	}
	a := newAgent(t, h, c, &stub{name: "disk"})

	_ = a.Tick(context.Background())
	c.advance(10 * time.Minute)

	if err := a.Tick(context.Background()); err == nil {
		t.Fatal("Tick accepted a configuration it cannot parse")
	}
	if a.BaseTick() != 10*time.Minute {
		t.Errorf("base tick = %v, want the working configuration kept", a.BaseTick())
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	a := newAgent(t, h, c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := a.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v, want the context's error", err)
	}
}

// spec: ingest.md#wire-format — every measurement the agent sends names the sensor that
// produced it, which is what the hub measures staleness against.
func TestMeasurementsNameTheirSensor(t *testing.T) {
	h, c := &hub{}, &clock{at: start}
	h.answers = []answer{{response: configure("v1", "5m", map[string]api.SensorConfig{
		"disk": {Enabled: true, Interval: "5m"},
	})}}
	a := newAgent(t, h, c, &stub{name: "disk", measurements: []sensor.Measurement{reading("disk.free_bytes", 1)}})

	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	c.advance(5 * time.Minute)
	if err := a.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	sent := measurements(h.last())
	if len(sent) != 1 {
		t.Fatalf("measurements = %+v, want one", sent)
	}
	if sent[0].Sensor == nil || *sent[0].Sensor != "disk" {
		t.Errorf("sensor = %v, want %q", sent[0].Sensor, "disk")
	}
}
