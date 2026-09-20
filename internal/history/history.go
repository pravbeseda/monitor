// Package history serves what a metric did over a window. It assembles series and knows
// nothing about SVG or HTTP: the built-in page reads it in process, anything outside the
// hub reads the same series over /api/v1/history (ADR 0018, docs/specs/history.md).
package history

import (
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// Query is one history request: which series, over which window.
type Query struct {
	Metric string
	// Node empty selects every node; Labels are matched by exact equality.
	Node   string
	Labels map[string]string
	Window time.Duration
}

// Window is the span a result covers, both bounds included.
type Window struct {
	From time.Time
	To   time.Time
}

// Series is one metric of one node under one label set, as every renderer receives it.
type Series struct {
	Node   string
	Metric string
	Labels map[string]string
	Unit   Unit
	// Interval is what the node resolves for the sensor this metric belongs to, and zero
	// for a metric no rule declares. A renderer breaks a line where two points are more
	// than three intervals apart — the age evaluation calls stale.
	Interval time.Duration
	// Stored is how many points the window holds, before any reduction.
	Stored  int
	Reduced bool
	Points  []storage.Point
}

// Result is one answered history query.
type Result struct {
	Window Window
	Series []Series
}

// Unit is what a value is measured in. The set is open: a consumer treats a unit it does
// not know as opaque rather than as a bare number.
type Unit string

// The units a metric id can declare today.
const (
	Bytes    Unit = "bytes"
	Percent  Unit = "percent"
	Duration Unit = "duration"
	// Number is what a metric id declaring no unit reads as.
	Number Unit = "number"
)

// UnitOf reads the unit from the metric id, which is where the wire format keeps it until
// metrics are declared.
func UnitOf(metric string) Unit {
	switch {
	case strings.HasSuffix(metric, "_bytes"):
		return Bytes
	case strings.HasSuffix(metric, "_pct"):
		return Percent
	case strings.HasSuffix(metric, "_seconds"):
		return Duration
	default:
		return Number
	}
}

// Gap reports whether two consecutive points straddle a hole: further apart than the age at
// which evaluation stops trusting a value, and reading that factor from evaluation rather
// than restating it. A series with no interval has no gaps, because nothing says how often
// it should have reported.
func Gap(interval time.Duration, earlier, later time.Time) bool {
	return interval > 0 && later.Sub(earlier) > evaluate.StaleFactor*interval
}
