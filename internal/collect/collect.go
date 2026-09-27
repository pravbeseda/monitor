// Package collect runs the service nodes the hub collects itself: the agent's own loop, with
// the sensors the hub carries, reporting to ingest in process (ADR 0039,
// docs/specs/services.md).
package collect

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pravbeseda/monitor/internal/agent"
	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/sensor"
)

// Ingest is where a service node's reports go: the endpoint without its transport.
type Ingest interface {
	Accept(ctx context.Context, node config.Node, req api.Request) (api.Response, error)
}

// Run collects every service node among nodes until ctx ends, and returns once all have
// stopped. A loop's first tick asks for its configuration and makes the node known; the
// second follows at once, so the sensors collect at start rather than a base tick later.
func Run(ctx context.Context, nodes []config.Node, sensors []sensor.Sensor, in Ingest, now func() time.Time) {
	var running sync.WaitGroup
	for _, node := range nodes {
		if !node.Service() {
			continue
		}
		loop := agent.New(agent.Options{
			Node:    node.Name,
			Sensors: sensors,
			Client:  client{node: node, ingest: in},
			Now:     now,
		})
		running.Go(func() {
			if err := loop.Tick(ctx); err != nil {
				slog.Error("tick failed", "node", node.Name, "error", err)
			}
			_ = loop.Run(ctx)
		})
	}
	running.Wait()
}

// client is the agent loop's hub, in process.
type client struct {
	node   config.Node
	ingest Ingest
}

func (c client) Send(ctx context.Context, req api.Request) (api.Response, error) {
	return c.ingest.Accept(ctx, c.node, req)
}
