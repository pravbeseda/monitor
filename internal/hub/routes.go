// Package hub wires the hub's HTTP surface together.
package hub

import (
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Store is what the hub's HTTP surface needs of persistence: ingest and history through
// the shared boundary, the state through its own.
type Store interface {
	storage.Storage
	Snapshots
	ThresholdStore
	EventLog
}

// Routes mounts every endpoint the hub serves. The version prefix is part of the
// contract: every new endpoint keeps it.
func Routes(cfg *config.Config, store Store, now func() time.Time) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/ingest", ingest.NewHandler(cfg, store, now))
	mux.Handle(ingest.AgentPrefix, ingest.NewAgentHandler(cfg))
	current := ReadState(store, targetOf(cfg), anomaly.NewNorms(store), now)
	mux.Handle("GET /{$}", Root())
	mux.Handle("GET /board", Board(current))
	mux.Handle("GET /timeline", Timeline(current, store, targetOf(cfg)))
	mux.Handle("GET /debug", Debug(current))

	read := reader(cfg, store, now)
	mux.Handle("GET /api/v1/series", SeriesAPI(read))
	mux.Handle("GET /api/v1/history", HistoryAPI(read))
	mux.Handle("GET /api/v1/state", StateAPI(current))
	mux.Handle("GET /history", HistoryPage(read))
	// One handler for both methods: a save is the same page answering for itself, and a
	// method it does not take is its own refusal rather than the mux's (thresholds.md).
	mux.Handle("/thresholds", Thresholds(store, func(node string) bool {
		_, known := cfg.Node(node)
		return known
	}))
	return mux
}
