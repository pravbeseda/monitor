package evaluate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// anomalies records when each series of a named node became unusual and when it came back
// (docs/specs/anomaly.md#record). It writes nothing a message or the digest reads: an
// anomaly is shown, never notified (ADR 0036).
func (e *Evaluator) anomalies(ctx context.Context, snap storage.Snapshot, now time.Time) error {
	excluded := keys(snap.Excluded, func(ref storage.SeriesRef) storage.Subject { return storage.Subject(ref) })
	open := keys(snap.Unusual, func(record storage.Anomaly) storage.Subject { return record.Subject })
	targets := make(map[string]Target, len(e.targets))
	for _, target := range e.targets {
		targets[target.Node] = target
	}

	// Stale values judge nothing, so a stale series is left out, unless it is excluded:
	// an exclusion withdraws its anomaly whatever the values say.
	type series struct {
		subject  storage.Subject
		key      string
		value    float64
		excluded bool
	}
	var candidates []series
	var refs []storage.SeriesRef
	for _, node := range snap.Nodes {
		target, named := targets[node.Node]
		if !named {
			continue
		}
		heard := Heard(node.LastSeen, snap.Outages, now)
		for _, value := range node.Values {
			one := series{subject: storage.Subject{Node: node.Node, Metric: value.Metric, Labels: value.Labels}, value: value.Value}
			key, err := one.subject.Key()
			if err != nil {
				slog.Error("identify a subject", "node", one.subject.Node, "metric", one.subject.Metric, "error", err)
				continue
			}
			one.key, one.excluded = key, excluded[key]
			switch {
			case one.excluded:
				candidates = append(candidates, one)
			case !target.Frozen(value.Sensor, heard, value.TS, now):
				candidates = append(candidates, one)
				refs = append(refs, storage.SeriesRef(one.subject))
			}
		}
	}

	norms, err := e.norms.For(ctx, now, refs)
	if err != nil {
		return fmt.Errorf("record anomalies: %w", err)
	}
	for _, one := range candidates {
		norm, known := norms[one.key]
		held := open[one.key]
		switch {
		case one.excluded || !known:
			if held {
				err = e.store.CloseAnomaly(ctx, one.subject, now, nil)
			}
		default:
			found := norm.Judge(one.value, held)
			switch unusual := found.Unusual(); {
			case unusual && !held:
				low, high := norm.Band()
				err = e.store.OpenAnomaly(ctx, storage.Anomaly{Subject: one.subject, Began: now, Value: one.value, Low: low, High: high})
			case !unusual && held:
				back := one.value
				err = e.store.CloseAnomaly(ctx, one.subject, now, &back)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// keys is the set of the subjects items name, by their subject keys.
func keys[T any](items []T, subjectOf func(T) storage.Subject) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		subject := subjectOf(item)
		key, err := subject.Key()
		if err != nil {
			slog.Error("identify a subject", "node", subject.Node, "metric", subject.Metric, "error", err)
			continue
		}
		out[key] = true
	}
	return out
}
