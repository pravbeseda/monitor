package evaluate_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// counting is the hub's own store, counting how often a pass asked it for a snapshot:
// that is what a loop can be observed by.
type counting struct {
	evaluate.Store
	passes atomic.Int32
}

func (c *counting) Snapshot(ctx context.Context, owed []string) (storage.Snapshot, error) {
	c.passes.Add(1)
	return c.Store.Snapshot(ctx, owed)
}

// spec: evaluation.md#the-tick — evaluation runs on its own schedule until the hub stops.
func TestRunEvaluatesUntilTheHubStops(t *testing.T) {
	store := &counting{Store: open(t)}
	e := evaluate.New(evaluate.Options{
		Store: store, Notifier: &recorder{},
		Digest: schedule, Started: time.Now(), Now: time.Now,
	})

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx, time.Millisecond)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for store.passes.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("the loop ran %d passes in two seconds", store.passes.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop outlived the hub it belongs to")
	}
}

// spec: evaluation.md#node-silence — the outage is recorded as the hub starts, before its
// first tick, so the state leaves it out from the start.
func TestRunRecordsTheOutageBeforeItsFirstTick(t *testing.T) {
	db := open(t)
	started := time.Now()
	if err := db.SetLastTickAt(context.Background(), started.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	e := evaluate.New(evaluate.Options{
		Store: db, Notifier: &recorder{},
		Digest: schedule, Started: started, Now: time.Now,
	})
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx, time.Hour)
		close(done)
	}()
	defer func() { stop(); <-done }()

	deadline := time.After(2 * time.Second)
	for {
		snap, err := db.Snapshot(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Outages) == 1 && snap.Outages[0].To.Equal(started.UTC().Truncate(time.Millisecond)) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("outages before the first tick = %+v", snap.Outages)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
