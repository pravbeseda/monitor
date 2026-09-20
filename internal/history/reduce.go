package history

import (
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

const (
	// rawLimit is the number of stored points a series may hold before it is reduced.
	rawLimit = 1000
	buckets  = rawLimit / 2
)

// collector takes one series' points as they arrive, oldest first, and holds only what the
// answer can carry: the points themselves while they are few enough, and both extremes of
// every bucket once they are not (docs/specs/history.md#reduction). Which extreme matters is
// a property of the metric and this package judges no value, so both survive.
type collector struct {
	from   time.Time
	width  time.Duration
	stored int
	raw    []storage.Point // the points as stored, dropped once there are too many of them
	bucket []extremes      // allocated in their place
	newest storage.Point
}

// extremes is the lowest and the highest point of one bucket, the same point until a second
// value arrives.
type extremes struct {
	low, high storage.Point
	filled    bool
}

func newCollector(window Window) *collector {
	// A window shorter than the bucket count is not reachable through a query, but a zero
	// width would divide by zero rather than answer badly.
	return &collector{from: window.From, width: max(window.To.Sub(window.From)/buckets, 1)}
}

// add takes the next point of the series: inside the window the collector was built for,
// and newer than every point before it.
func (c *collector) add(point storage.Point) {
	c.stored++
	c.newest = point
	if c.bucket != nil {
		c.bucketise(point)
		return
	}
	c.raw = append(c.raw, point)
	if len(c.raw) <= rawLimit {
		return
	}
	c.bucket = make([]extremes, buckets)
	for _, held := range c.raw {
		c.bucketise(held)
	}
	c.raw = nil
}

func (c *collector) bucketise(point storage.Point) {
	at := int(point.TS.Sub(c.from) / c.width)
	if at >= buckets {
		at = buckets - 1 // the last bucket is closed at the end of the window.
	}
	held := &c.bucket[at]
	if !held.filled {
		*held = extremes{low: point, high: point, filled: true}
		return
	}
	// Points arrive oldest first and a tie resolves to the earliest timestamp, so only a
	// strictly more extreme value replaces one already held.
	if point.Value < held.low.Value {
		held.low = point
	}
	if point.Value > held.high.Value {
		held.high = point
	}
}

// result is what the series contributes to the answer, and whether it was reduced.
func (c *collector) result() ([]storage.Point, bool) {
	if c.bucket == nil {
		return c.raw, false
	}
	out := make([]storage.Point, 0, 2*buckets+1)
	for _, held := range c.bucket {
		if !held.filled {
			continue // a bucket holding no point contributes nothing; no value is invented.
		}
		first, second := held.low, held.high
		if second.TS.Before(first.TS) {
			first, second = second, first
		}
		out = append(out, first)
		if !second.TS.Equal(first.TS) {
			out = append(out, second)
		}
	}
	// The newest point survives whatever the bucketing does, so a chart's right edge and
	// the index page never disagree about the current value. It is the last point of the
	// last occupied bucket, so appending keeps the answer in timestamp order.
	if !out[len(out)-1].TS.Equal(c.newest.TS) {
		out = append(out, c.newest)
	}
	return out, true
}
