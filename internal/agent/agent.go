// Package agent runs the sensors on a node and pushes their measurements to the hub
// (docs/specs/agent.md).
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/version"
)

const (
	// BufferLimit caps what an unreachable hub may cost in memory: days of readings on a
	// laptop that spends a week off the network.
	BufferLimit = 10_000

	// bootstrapTick is how often the agent asks until the hub tells it otherwise.
	bootstrapTick = 5 * time.Minute
)

// Client is the hub as the loop sees it.
type Client interface {
	Send(ctx context.Context, req api.Request) (api.Response, error)
}

// Options are the three local values a node holds, plus what the loop is built from.
type Options struct {
	Node    string
	Sensors []sensor.Sensor
	Client  Client
	Now     func() time.Time
}

// Agent is the tick loop. It holds the configuration the hub last delivered, when each
// sensor last collected, and whatever the hub has not accepted yet.
type Agent struct {
	node    string
	sensors []sensor.Sensor
	client  Client
	now     func() time.Time

	version   string
	config    configuration
	collected map[string]time.Time
	buffer    []api.Measurement
}

type configuration struct {
	baseTick    time.Duration
	filesystems []string
	skipMounts  []string
	sensors     map[string]sensorSetting
}

type sensorSetting struct {
	enabled  bool
	interval time.Duration
}

// New builds an agent that has no configuration yet: the first tick asks for one.
func New(opts Options) *Agent {
	return &Agent{
		node:      opts.Node,
		sensors:   opts.Sensors,
		client:    opts.Client,
		now:       opts.Now,
		config:    configuration{baseTick: bootstrapTick, sensors: map[string]sensorSetting{}},
		collected: map[string]time.Time{},
	}
}

// BaseTick is how long the loop waits between ticks.
func (a *Agent) BaseTick() time.Duration { return a.config.baseTick }

// Filesystems is the allow-list the disk sensor reads on every collection.
func (a *Agent) Filesystems() []string { return a.config.filesystems }

// SkipMounts are the mount-point prefixes the disk sensor leaves alone.
func (a *Agent) SkipMounts() []string { return a.config.skipMounts }

// Run ticks until the context ends.
func (a *Agent) Run(ctx context.Context) error {
	for {
		if err := a.Tick(ctx); err != nil {
			slog.Error("tick failed", "error", err)
		}
		timer := time.NewTimer(a.BaseTick())
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Tick collects from every due sensor, posts one request, and applies the configuration
// the answer carries.
func (a *Agent) Tick(ctx context.Context) error {
	now := a.now()
	a.buffer = append(a.buffer, a.collect(ctx, now)...)
	a.trimBuffer()

	// An empty batch has to reach the hub as [] and not as null: a heartbeat carries no
	// measurements, but it does carry the field.
	batch := a.buffer
	if batch == nil {
		batch = []api.Measurement{}
	}
	resp, err := a.client.Send(ctx, api.Request{
		Node:          a.node,
		AgentVersion:  version.Current,
		ConfigVersion: a.version,
		TS:            now.UTC().Format(time.RFC3339),
		Manifest:      a.manifest(),
		Measurements:  &batch,
	})
	if err != nil {
		if !retryable(err) {
			a.buffer = nil
		}
		return fmt.Errorf("deliver to the hub: %w", err)
	}
	a.buffer = nil

	return a.apply(resp)
}

// collect asks every enabled sensor whose interval has elapsed. A sensor that fails costs
// only its own reading.
func (a *Agent) collect(ctx context.Context, now time.Time) []api.Measurement {
	var out []api.Measurement
	for _, s := range a.sensors {
		setting, configured := a.config.sensors[s.Name()]
		if !configured || !setting.enabled {
			continue
		}
		if last, seen := a.collected[s.Name()]; seen {
			// By the wall clock, since the monotonic one stands still while the node sleeps;
			// a clock set back makes the sensor due instead of silent until it catches up.
			elapsed := now.Round(0).Sub(last)
			if elapsed >= 0 && elapsed < setting.interval {
				continue
			}
		}
		a.collected[s.Name()] = now

		measurements, err := a.collectOne(ctx, s)
		if err != nil {
			slog.Error("sensor failed", "sensor", s.Name(), "error", err)
			continue
		}
		name := s.Name()
		for _, m := range measurements {
			value := m.Value
			out = append(out, api.Measurement{
				Metric: m.Metric,
				Labels: m.Labels,
				Value:  &value,
				TS:     m.TS.UTC().Format(time.RFC3339),
				Sensor: &name,
			})
		}
	}
	return out
}

// collectOne gives one sensor half a tick to answer. A collection stuck in a system call
// keeps running — the kernel owns it — but the tick, and with it the heartbeat, does not
// wait for it.
func (a *Agent) collectOne(ctx context.Context, s sensor.Sensor) ([]sensor.Measurement, error) {
	budget := a.config.baseTick / 2
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	type answer struct {
		measurements []sensor.Measurement
		err          error
	}
	// Buffered, so an abandoned collection can finish and be garbage collected.
	done := make(chan answer, 1)
	go func() {
		measurements, err := s.Collect(ctx)
		done <- answer{measurements: measurements, err: err}
	}()

	select {
	case got := <-done:
		return got.measurements, got.err
	case <-ctx.Done():
		return nil, fmt.Errorf("no answer within %v: %w", budget, ctx.Err())
	}
}

// trimBuffer keeps the newest measurements: an old reading is the one worth losing.
func (a *Agent) trimBuffer() {
	if len(a.buffer) > BufferLimit {
		a.buffer = a.buffer[len(a.buffer)-BufferLimit:]
	}
}

func (a *Agent) manifest() []api.SensorStatus {
	manifest := make([]api.SensorStatus, 0, len(a.sensors))
	for _, s := range a.sensors {
		manifest = append(manifest, api.SensorStatus{Sensor: s.Name(), Applicable: s.Applicable()})
	}
	return manifest
}

// apply replaces the configuration wholesale; one it cannot parse leaves the working one
// in place.
func (a *Agent) apply(resp api.Response) error {
	if resp.Config == nil || resp.ConfigVersion == "" || resp.ConfigVersion == a.version {
		return nil
	}
	parsed, err := parse(*resp.Config)
	if err != nil {
		return fmt.Errorf("configuration %s: %w", resp.ConfigVersion, err)
	}
	a.config = parsed
	a.version = resp.ConfigVersion
	return nil
}

func parse(delivered api.AgentConfig) (configuration, error) {
	baseTick, err := time.ParseDuration(delivered.BaseTick)
	if err != nil {
		return configuration{}, fmt.Errorf("base_tick %q is not a duration", delivered.BaseTick)
	}
	out := configuration{
		baseTick:    baseTick,
		filesystems: delivered.Filesystems,
		skipMounts:  delivered.SkipMounts,
		sensors:     make(map[string]sensorSetting, len(delivered.Sensors)),
	}
	for name, s := range delivered.Sensors {
		interval, err := time.ParseDuration(s.Interval)
		if err != nil {
			return configuration{}, fmt.Errorf("sensor %s: interval %q is not a duration", name, s.Interval)
		}
		out.sensors[name] = sensorSetting{enabled: s.Enabled, interval: interval}
	}
	return out, nil
}
