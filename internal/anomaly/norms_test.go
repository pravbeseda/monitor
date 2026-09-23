package anomaly_test

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/storage"
)

// points is a store holding one series' points, honouring both bounds of a read the way
// storage does, and counting the reads it answers.
type points struct {
	held  []storage.Point
	reads int
	fail  error
}

func (p *points) Points(_ context.Context, _ storage.SeriesRef, from, to time.Time) iter.Seq2[storage.Point, error] {
	p.reads++
	return func(yield func(storage.Point, error) bool) {
		if p.fail != nil {
			yield(storage.Point{}, p.fail)
			return
		}
		for _, point := range p.held {
			if point.TS.Before(from) || point.TS.After(to) {
				continue
			}
			if !yield(point, nil) {
				return
			}
		}
	}
}

var (
	series  = storage.SeriesRef{Node: "server-b", Metric: "load.avg_5m", Labels: map[string]string{}}
	askedAt = time.Date(2026, 9, 23, 10, 20, 0, 0, time.UTC)
)

// everyFiveMinutes is a series that has reported every 5 minutes for weeks, at value 1.
func everyFiveMinutes() []storage.Point {
	var out []storage.Point
	for at := askedAt.Add(-30 * 24 * time.Hour); !at.After(askedAt); at = at.Add(5 * time.Minute) {
		out = append(out, storage.Point{TS: at, Value: 1})
	}
	return out
}

func normAt(t *testing.T, norms *anomaly.Norms, now time.Time) (anomaly.Norm, bool) {
	t.Helper()
	held, err := norms.For(context.Background(), now, []storage.SeriesRef{series})
	if err != nil {
		t.Fatal(err)
	}
	key, err := storage.Subject(series).Key()
	if err != nil {
		t.Fatal(err)
	}
	norm, ok := held[key]
	return norm, ok
}

// spec: anomaly.md#norm-period — the bounds of the period, each one point on its own side
// of it: a point at 5 inside a period of ones is what the norm's median tells apart.
func TestThePeriodBoundsAreInclusive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		at     time.Time
		inside bool
	}{
		{"exactly 24 hours before the hour", time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC), true},
		{"a millisecond later", time.Date(2026, 9, 22, 10, 0, 0, 1e6, time.UTC), false},
		{"exactly 8 days before the hour", time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC), true},
		{"a millisecond earlier", time.Date(2026, 9, 15, 9, 59, 59, 999e6, time.UTC), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 24 points that make a norm on their own, and the point under test: with it
			// inside, the period holds 25 points whose median is still 1 but whose
			// highest is the 5.
			var held []storage.Point
			for i := range 24 {
				held = append(held, storage.Point{TS: time.Date(2026, 9, 17, i, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour), Value: 1})
			}
			held = append(held, storage.Point{TS: tc.at, Value: 5})
			norm, ok := normAt(t, anomaly.NewNorms(&points{held: held}), askedAt)
			if !ok {
				t.Fatal("no norm")
			}
			// Inside, 5 is the period's highest value and the side above reaches it, so 9
			// scores 2; outside, the side above is the floor of 1% and 9 scores 800.
			got := *norm.Judge(9).Score
			if inside := got == 2; inside != tc.inside {
				t.Fatalf("score of 9 = %v: inside = %v, want %v", got, inside, tc.inside)
			}
		})
	}
}

// spec: anomaly.md#norm-period — first reported six hours before the period ended.
func TestASeriesFirstSeenYesterdayHasNoNorm(t *testing.T) {
	var held []storage.Point
	for at := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC); !at.After(askedAt); at = at.Add(5 * time.Minute) {
		held = append(held, storage.Point{TS: at, Value: 1})
	}
	if _, ok := normAt(t, anomaly.NewNorms(&points{held: held}), askedAt); ok {
		t.Fatal("a norm from six hours")
	}
}

// spec: anomaly.md#norm-period — a silence of three days inside the norm period.
func TestASilenceInsideThePeriodLeavesTheRestAsNorm(t *testing.T) {
	var held []storage.Point
	for _, p := range everyFiveMinutes() {
		if p.TS.After(time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)) && p.TS.Before(time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)) {
			continue
		}
		held = append(held, p)
	}
	if _, ok := normAt(t, anomaly.NewNorms(&points{held: held}), askedAt); !ok {
		t.Fatal("no norm")
	}
}

// Every answer inside one hour shares its period, so a series is read once an hour.
func TestANormIsReadOncePerHour(t *testing.T) {
	store := &points{held: everyFiveMinutes()}
	norms := anomaly.NewNorms(store)
	for _, at := range []time.Time{askedAt, askedAt.Add(39 * time.Minute)} {
		if _, ok := normAt(t, norms, at); !ok {
			t.Fatal("no norm")
		}
	}
	if store.reads != 1 {
		t.Fatalf("reads within one hour = %d, want 1", store.reads)
	}
	if _, ok := normAt(t, norms, time.Date(2026, 9, 23, 11, 0, 0, 0, time.UTC)); !ok {
		t.Fatal("no norm")
	}
	if store.reads != 2 {
		t.Fatalf("reads after the hour turned = %d, want 2", store.reads)
	}
}

// spec: anomaly.md#reading — the stored points cannot be read, and the next answer tries
// again.
func TestAFailedReadIsTriedAgain(t *testing.T) {
	store := &points{held: everyFiveMinutes(), fail: errors.New("disk I/O error")}
	norms := anomaly.NewNorms(store)
	if _, err := norms.For(context.Background(), askedAt, []storage.SeriesRef{series}); err == nil {
		t.Fatal("no error")
	}
	store.fail = nil
	if _, ok := normAt(t, norms, askedAt); !ok {
		t.Fatal("no norm on the second answer")
	}
}
