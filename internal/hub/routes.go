// Package hub wires the hub's HTTP surface together.
package hub

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/ingest"
	"github.com/pravbeseda/monitor/internal/names"
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

// NameReader is where a page reads what the sensors call their metrics
// (docs/specs/metric-names.md).
type NameReader interface {
	Names(ctx context.Context) (names.Book, error)
}

// namesFrom is the names a page shows. Reading them is not worth the page: a failure is
// logged, and the page shows ids instead.
func namesFrom(ctx context.Context, from NameReader) names.Book {
	book, err := from.Names(ctx)
	if err != nil {
		slog.Error("read the names of metrics", "error", err)
	}
	return book
}

// Routes mounts every endpoint the hub serves. The version prefix is part of the
// contract: every new endpoint keeps it. norms is where the state finds each series' norm.
func Routes(cfg *config.Config, store Store, norms NormReader, now func() time.Time) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/ingest", ingest.NewHandler(cfg, store, now))
	mux.Handle(ingest.AgentPrefix, ingest.NewAgentHandler(cfg))
	current := ReadState(store, targetOf(cfg), norms, now)
	mux.Handle("GET /{$}", Root())
	mux.Handle("GET /timeline", Timeline(current, store, targetOf(cfg)))
	mux.Handle("GET /debug", Debug(current))
	mux.Handle("GET /public/", Public())

	read := reader(cfg, store, now)
	mux.Handle("GET /api/v1/series", SeriesAPI(read))
	mux.Handle("GET /api/v1/history", HistoryAPI(read))
	mux.Handle("GET /api/v1/state", StateAPI(current))
	mux.Handle("GET /history", HistoryPage(read, store))
	mux.Handle("GET /overview", Overview(current, read, layoutOf(cfg)))
	mux.Handle("GET /item", Item(current, read, store, layoutOf(cfg), targetOf(cfg)))
	// One handler for both methods: a save is the same page answering for itself, and a
	// method it does not take is its own refusal rather than the mux's (thresholds.md).
	mux.Handle("/thresholds", Thresholds(store, func(node string) bool {
		_, known := cfg.Node(node)
		return known
	}))
	return mux
}
