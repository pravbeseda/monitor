package evaluate

import (
	"context"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Store is what evaluation needs of persistence, declared where it is consumed: the hub's
// own Storage boundary keeps what ingest and the history pages use, and the state declares
// its own, so adding to this one costs nothing to their test doubles.
type Store interface {
	// Snapshot is the one view a tick evaluates against: nodes and their latest values,
	// the level every subject held, and each subject's newest transition among the levels
	// named as owed. Which levels those are is this package's rule (ADR 0016), so it
	// travels with the call rather than living in the store.
	Snapshot(ctx context.Context, owed []string) (storage.Snapshot, error)

	SaveState(ctx context.Context, state storage.State) error
	// DeleteState forgets a subject nothing watches any more (ADR 0032).
	DeleteState(ctx context.Context, subject storage.Subject) error
	ApplyTransition(ctx context.Context, change storage.Transition) error
	RecordNotified(ctx context.Context, subject storage.Subject, at time.Time) error

	EventsBetween(ctx context.Context, from, to time.Time) ([]storage.Transition, error)
	LastDigestAt(ctx context.Context) (time.Time, bool, error)
	SetLastDigestAt(ctx context.Context, at time.Time) error
	LastTickAt(ctx context.Context) (time.Time, bool, error)
	SetLastTickAt(ctx context.Context, at time.Time) error
	RecordOutage(ctx context.Context, outage storage.Outage) error

	OpenAnomaly(ctx context.Context, record storage.Anomaly) error
	CloseAnomaly(ctx context.Context, subject storage.Subject, at time.Time, back *float64) error
}

// NormReader is where the pass finds each series' norm for the hour it runs in.
type NormReader interface {
	For(ctx context.Context, now time.Time, refs []storage.SeriesRef) (map[string]anomaly.Norm, error)
}

// The hub's storage has to satisfy it, and the compiler is what says so.
var _ Store = (*storage.SQLite)(nil)
