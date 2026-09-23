package anomaly

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// Points is what a norm needs of persistence: one series' points inside [from, to], both
// bounds inclusive, oldest first.
type Points interface {
	Points(ctx context.Context, ref storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error]
}

// Norms holds the norm of every series for the current hour. The period moves only with
// the hour, so a series' points are read on the first answer of each hour that asks for it
// and never again inside that hour.
type Norms struct {
	store Points

	mu sync.Mutex
	// until ends the period the held norms were read for.
	until time.Time
	// held is keyed by the subject encoding evaluation and the state use, and records
	// "no norm" as well as a norm, so a series too young for one is not read again.
	held map[string]held
}

type held struct {
	norm Norm
	ok   bool
}

// NewNorms reads norms from store.
func NewNorms(store Points) *Norms {
	return &Norms{store: store}
}

// For returns the norms of refs for the hour now falls in, keyed by the subject encoding
// the state uses, reading only those this hour has not read yet. A series with no norm is
// absent. A failed read is not remembered, so the next answer tries again.
func (n *Norms) For(ctx context.Context, now time.Time, refs []storage.SeriesRef) (map[string]Norm, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	from, to := Period(now)
	if !to.Equal(n.until) {
		n.until, n.held = to, map[string]held{}
	}
	// The answer is a map of its own: the held one changes under the next caller.
	answer := make(map[string]Norm, len(refs))
	for _, ref := range refs {
		key, err := storage.Subject(ref).Key()
		if err != nil {
			return nil, err
		}
		one, known := n.held[key]
		if !known {
			points, err := collect(n.store.Points(ctx, ref, from, to))
			if err != nil {
				return nil, fmt.Errorf("read the norm of %s on %s: %w", ref.Metric, ref.Node, err)
			}
			one.norm, one.ok = NormOf(points)
			n.held[key] = one
		}
		if one.ok {
			answer[key] = one.norm
		}
	}
	return answer, nil
}

func collect(seq iter.Seq2[storage.Point, error]) ([]storage.Point, error) {
	var out []storage.Point
	for point, err := range seq {
		if err != nil {
			return nil, err
		}
		out = append(out, point)
	}
	return out, nil
}
