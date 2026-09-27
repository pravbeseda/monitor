package collect

import (
	"context"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/version"
)

var start = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

type drive struct{}

func (drive) Name() string     { return "gdrive" }
func (drive) Applicable() bool { return true }
func (drive) Collect(context.Context) ([]sensor.Measurement, error) {
	return []sensor.Measurement{{Metric: "gdrive.free_bytes", Value: 1, TS: start}}, nil
}

// reports hands each report to the test, and answers the first with a configuration that
// runs gdrive, as ingest answers a loop that holds none.
type reports struct {
	got chan api.Request
}

func (r reports) Accept(_ context.Context, _ config.Node, req api.Request) (api.Response, error) {
	r.got <- req
	if req.ConfigVersion != "" {
		return api.Response{}, nil
	}
	return api.Response{ConfigVersion: "v1", Config: &api.AgentConfig{
		BaseTick: "5m",
		Sensors:  map[string]api.SensorConfig{"gdrive": {Enabled: true, Interval: "1h"}},
	}}, nil
}

func (r reports) next(t *testing.T) api.Request {
	t.Helper()
	select {
	case req := <-r.got:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("no report from the service node")
		return api.Request{}
	}
}

// spec: services.md#collection — the hub collects each service node from the start, and
// leaves an agent's node to its agent; the node reports the hub's own version.
func TestRunCollectsEachServiceNodeAtStart(t *testing.T) {
	in := reports{got: make(chan api.Request, 4)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	nodes := []config.Node{{Name: "laptop-a", Class: "laptop"}, {Name: "cloud", Class: config.ServiceClass}}
	go func() {
		defer close(done)
		Run(ctx, nodes, []sensor.Sensor{drive{}}, in, func() time.Time { return start })
	}()

	if first := in.next(t); first.Node != "cloud" || first.ConfigVersion != "" || first.AgentVersion != version.Current {
		t.Errorf("first report = %+v, want cloud asking for its configuration as version %s", first, version.Current)
	}
	if second := in.next(t); second.ConfigVersion != "v1" || second.Measurements == nil || len(*second.Measurements) != 1 {
		t.Errorf("second report = %+v, want gdrive's reading under v1, without waiting a base tick", second)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going after its context ended")
	}
}
