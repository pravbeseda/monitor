// Package hub wires the hub's HTTP surface together.
package hub

import (
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Store is what the hub's HTTP surface needs of persistence: ingest and history through
// the shared boundary, the state through its own.
type Store interface {
	storage.Storage
	Snapshots
}

// Routes mounts every endpoint the hub serves. The version prefix is part of the
// contract: every new endpoint keeps it.
func Routes(cfg *config.Config, store Store, now func() time.Time) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/ingest", ingest.NewHandler(cfg, store, now))
	mux.Handle(ingest.AgentPrefix, ingest.NewAgentHandler(cfg))
	current := ReadState(store, targetOf(cfg), now)
	mux.Handle("GET /{$}", Page(current))

	read := reader(cfg, store, now)
	mux.Handle("GET /api/v1/series", SeriesAPI(read))
	mux.Handle("GET /api/v1/history", HistoryAPI(read))
	mux.Handle("GET /api/v1/state", StateAPI(current))
	mux.Handle("GET /history", HistoryPage(read))
	return mux
}
