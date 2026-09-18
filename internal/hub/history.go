package hub

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/config"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/storage"
)

// seriesJSON is one series as the listing endpoint answers it: what exists, with no
// points. The history endpoint answers the same identity plus what happened.
type seriesJSON struct {
	Node     string            `json:"node"`
	Metric   string            `json:"metric"`
	Labels   map[string]string `json:"labels"`
	Unit     history.Unit      `json:"unit"`
	Interval string            `json:"interval"`
}

type historySeriesJSON struct {
	seriesJSON
	Reduced bool        `json:"reduced"`
	Stored  int         `json:"stored"`
	Points  []pointJSON `json:"points"`
}

type pointJSON struct {
	TS    string  `json:"ts"`
	Value float64 `json:"value"`
}

type windowJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SeriesAPI answers what exists: every series matching a selection, with no points.
func SeriesAPI(reader history.Reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, err := history.ParseSelection(r.URL.Query())
		if err != nil {
			answerError(w, err)
			return
		}
		series, err := reader.List(r.Context(), query)
		if err != nil {
			answerError(w, err)
			return
		}
		listed := make([]seriesJSON, 0, len(series))
		for _, one := range series {
			listed = append(listed, describe(one))
		}
		answer(w, http.StatusOK, struct {
			Series []seriesJSON `json:"series"`
		}{listed})
	})
}

// HistoryAPI answers one window of history.
func HistoryAPI(reader history.Reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, err := history.ParseQuery(r.URL.Query())
		if err != nil {
			answerError(w, err)
			return
		}
		result, err := reader.Read(r.Context(), query)
		if err != nil {
			answerError(w, err)
			return
		}
		series := make([]historySeriesJSON, 0, len(result.Series))
		for _, one := range result.Series {
			out := historySeriesJSON{
				seriesJSON: describe(one),
				Reduced:    one.Reduced,
				Stored:     one.Stored,
				Points:     make([]pointJSON, 0, len(one.Points)),
			}
			for _, point := range one.Points {
				out.Points = append(out.Points, pointJSON{TS: stamp(point.TS), Value: point.Value})
			}
			series = append(series, out)
		}
		answer(w, http.StatusOK, struct {
			Window windowJSON          `json:"window"`
			Series []historySeriesJSON `json:"series"`
		}{
			Window: windowJSON{From: stamp(result.Window.From), To: stamp(result.Window.To)},
			Series: series,
		})
	})
}

func describe(one history.Series) seriesJSON {
	out := seriesJSON{
		Node:   one.Node,
		Metric: one.Metric,
		Labels: one.Labels,
		Unit:   one.Unit,
	}
	if one.Interval > 0 {
		out.Interval = api.FormatDuration(one.Interval)
	}
	return out
}

func stamp(t time.Time) string { return t.UTC().Format(api.TimeLayout) }

// answerError separates a query this endpoint will not answer from a hub that could not
// answer it. Both carry an English message: the API is diagnostic, the page translates.
func answerError(w http.ResponseWriter, err error) {
	var refusal history.Refusal
	if errors.As(err, &refusal) {
		answer(w, http.StatusBadRequest, api.ErrorBody{Error: refusal.Message})
		return
	}
	slog.Error("read history", "error", err)
	answer(w, http.StatusInternalServerError, api.ErrorBody{Error: "the stored measurements could not be read"})
}

// answer sends one JSON body. A window that ends now makes every response unique, so none
// of them may be cached.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// The status line is already sent, so a failed write can only be logged, not fixed.
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write a history response", "error", err)
	}
}

// reader wires history to the two things it reads: the stored points, and the interval of
// each series.
func reader(cfg *config.Config, store storage.Storage, now func() time.Time) history.Reader {
	return history.Reader{Source: store, Now: now, Interval: intervalOf(cfg)}
}

// intervalOf is the interval a node resolves for the sensor behind a metric — the same
// input evaluation ages a subject by, so one definition of silence serves both
// (docs/specs/history.md#gaps).
func intervalOf(cfg *config.Config) history.Interval {
	return func(node, metric string) time.Duration {
		sensor, declared := evaluate.SensorOf(metric)
		if !declared {
			return 0
		}
		entry, known := cfg.Node(node)
		if !known {
			return 0
		}
		return entry.Target().Intervals[sensor]
	}
}
