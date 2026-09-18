// Package hub wires the hub's HTTP surface together.
package hub

import (
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Routes mounts every endpoint the hub serves. The version prefix is part of the
// contract: every new endpoint keeps it.
func Routes(cfg *config.Config, store storage.Storage, now func() time.Time) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/ingest", ingest.NewHandler(cfg, store, now))
	mux.Handle(ingest.AgentPrefix, ingest.NewAgentHandler(cfg))
	mux.Handle("GET /{$}", Page(store, intervalOf(cfg)))

	read := reader(cfg, store, now)
	mux.Handle("GET /api/v1/series", SeriesAPI(read))
	mux.Handle("GET /api/v1/history", HistoryAPI(read))
	mux.Handle("GET /history", HistoryPage(read))
	return mux
}
