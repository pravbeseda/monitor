package hub

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The wire form of docs/specs/state.md#wire-format. A pointer is a field that can be null.
type stateJSON struct {
	At       string        `json:"at"`
	Level    *string       `json:"level"`
	Nodes    []nodeJSON    `json:"nodes"`
	Subjects []subjectJSON `json:"subjects"`
	Readings []readingJSON `json:"readings"`
}

type nodeJSON struct {
	Node         string  `json:"node"`
	Configured   bool    `json:"configured"`
	AgentVersion *string `json:"agent_version"`
	LastSeen     string  `json:"last_seen"`
	Level        *string `json:"level"`
}

type subjectJSON struct {
	Node   string            `json:"node"`
	Rule   string            `json:"rule"`
	Labels map[string]string `json:"labels"`
	Level  *string           `json:"level"`
	Since  *string           `json:"since"`
	Stale  bool              `json:"stale"`
	Values []valueJSON       `json:"values"`
}

type valueJSON struct {
	Metric string       `json:"metric"`
	Unit   history.Unit `json:"unit"`
	Value  float64      `json:"value"`
	TS     string       `json:"ts"`
}

type readingJSON struct {
	Node   string            `json:"node"`
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
	Unit   history.Unit      `json:"unit"`
	Value  float64           `json:"value"`
	TS     string            `json:"ts"`
	Stale  *bool             `json:"stale"`
}

// Snapshots is what the state needs of persistence, declared where it is consumed. The
// web reads no transition, so it names no level as owed.
type Snapshots interface {
	Snapshot(ctx context.Context, owed []string) (storage.Snapshot, error)
}

// StateReader reads the state at the instant it is called.
type StateReader func(ctx context.Context) (state.State, error)

// ReadState builds the state from one consistent read of storage, resolving each node as
// evaluation does, so the two cannot disagree about which subjects exist or are fresh.
func ReadState(store Snapshots, targets func(node string) (evaluate.Target, bool), now func() time.Time) StateReader {
	return func(ctx context.Context) (state.State, error) {
		snap, err := store.Snapshot(ctx, nil)
		if err != nil {
			return state.State{}, err
		}
		return state.Build(targets, snap, now()), nil
	}
}

// StateAPI answers what the state of everything is now. It takes no parameter: `at=` time
// travel is a later stage, and refusing it keeps a caller from reading the present as the
// past (docs/specs/state.md#endpoint).
func StateAPI(read StateReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			answer(w, http.StatusBadRequest, api.ErrorBody{Error: "the state takes no query parameters"})
			return
		}
		current, err := read(r.Context())
		if err != nil {
			slog.Error("read the state", "error", err)
			answer(w, http.StatusInternalServerError, api.ErrorBody{Error: "the stored state could not be read"})
			return
		}
		answer(w, http.StatusOK, encodeState(current))
	})
}

func encodeState(current state.State) stateJSON {
	out := stateJSON{
		At:       stamp(current.At),
		Level:    levelJSON(current.Level),
		Nodes:    make([]nodeJSON, 0, len(current.Nodes)),
		Subjects: make([]subjectJSON, 0, len(current.Subjects)),
		Readings: make([]readingJSON, 0, len(current.Readings)),
	}
	for _, node := range current.Nodes {
		one := nodeJSON{
			Node:       node.Node,
			Configured: node.Configured,
			LastSeen:   stamp(node.LastSeen),
			Level:      levelJSON(node.Level),
		}
		if node.AgentVersion != "" {
			one.AgentVersion = &node.AgentVersion
		}
		out.Nodes = append(out.Nodes, one)
	}
	for _, subject := range current.Subjects {
		one := subjectJSON{
			Node:   subject.Node,
			Rule:   subject.Rule,
			Labels: labelsJSON(subject.Labels),
			Level:  levelJSON(subject.Level),
			Stale:  subject.Stale,
			Values: make([]valueJSON, 0, len(subject.Values)),
		}
		if subject.Level != nil {
			since := stamp(subject.Since)
			one.Since = &since
		}
		for _, value := range subject.Values {
			one.Values = append(one.Values, valueJSON{Metric: value.Metric, Unit: value.Unit, Value: value.Value, TS: stamp(value.TS)})
		}
		out.Subjects = append(out.Subjects, one)
	}
	for _, reading := range current.Readings {
		out.Readings = append(out.Readings, readingJSON{
			Node:   reading.Node,
			Metric: reading.Metric,
			Labels: labelsJSON(reading.Labels),
			Unit:   reading.Unit,
			Value:  reading.Value.Value,
			TS:     stamp(reading.TS),
			Stale:  reading.Stale,
		})
	}
	return out
}

func levelJSON(level *evaluate.Level) *string {
	if level == nil {
		return nil
	}
	name := level.String()
	return &name
}

// labelsJSON never answers null: an unlabelled series has an empty label set, not none.
func labelsJSON(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}
