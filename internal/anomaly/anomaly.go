// Package anomaly says how unlike its own week a series' newest value is: the norm of every
// series, the score of its newest value against it, and the rank of the unusual ones
// (docs/specs/anomaly.md, ADR 0036). It judges no level and writes nothing.
package anomaly

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/pravbeseda/monitor/internal/storage"
)

// cutoff is the score, in either direction, at which a value is unusual: twice as far from
// the norm as the edge of the band.
const cutoff = 2

// A norm needs this much history before anything can be unusual against it.
const (
	minPoints = 24
	minSpan   = 24 * time.Hour
)

// Period is the norm period of an answer given at now: the seven days ending 24 hours
// before the current UTC hour began, both bounds inclusive. Every answer inside one hour
// shares it.
func Period(now time.Time) (from, to time.Time) {
	hour := now.UTC().Truncate(time.Hour)
	return hour.Add(-192 * time.Hour), hour.Add(-24 * time.Hour)
}

// Norm is what a series usually does: its median, and how wide each side of its band is.
// A side of zero width is one the series never left a norm of zero on.
type Norm struct {
	median float64
	above  float64
	below  float64
}

// NormOf draws the norm from the points of a norm period. It is false when they are too
// few, or span less than a day: a series first seen yesterday cannot be unusual yet.
func NormOf(points []storage.Point) (Norm, bool) {
	if len(points) < minPoints {
		return Norm{}, false
	}
	first, last := points[0].TS, points[0].TS
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
		if p.TS.Before(first) {
			first = p.TS
		}
		if p.TS.After(last) {
			last = p.TS
		}
	}
	if last.Sub(first) < minSpan {
		return Norm{}, false
	}
	slices.Sort(values)

	median := percentile(values, 50)
	floor := 0.01 * math.Abs(median)
	above := width(percentile(values, 99)-median, values[len(values)-1]-median, floor, smallestStep(values, median, math.Inf(1)))
	below := width(median-percentile(values, 1), median-values[0], floor, smallestStep(values, math.Inf(-1), median))
	return Norm{median: median, above: above, below: below}, true
}

// percentile is the p-th percentile by nearest rank: a value the series really reported.
func percentile(sorted []float64, p int) float64 {
	return sorted[(p*len(sorted)+99)/100-1]
}

// width is one side of the band: its distance from the norm, or the period's extreme when
// that is zero, and never less than the floor or the smallest step that side has taken.
func width(band, extreme, floor, step float64) float64 {
	if band == 0 {
		band = extreme
	}
	return max(band, floor, step)
}

// smallestStep is the smallest difference between two distinct values in [from, to], or
// zero when that range holds one value.
func smallestStep(sorted []float64, from, to float64) float64 {
	smallest := 0.0
	for i := 1; i < len(sorted); i++ {
		a, b := sorted[i-1], sorted[i]
		if a < from || b > to || a == b {
			continue
		}
		if step := b - a; smallest == 0 || step < smallest {
			smallest = step
		}
	}
	return smallest
}

// Anomaly is a series' newest value against its norm. A nil Score is a value off a side of
// no width, which is as unusual as a value can be. Rank is zero when it is not unusual.
type Anomaly struct {
	Norm  float64
	Score *float64
	Rank  int
}

// Unusual is whether the value ranks at all.
func (a Anomaly) Unusual() bool {
	return a.Score == nil || math.Abs(*a.Score) >= cutoff
}

// Judge scores a value in widths of the side of the norm it lies on, rounded to two
// decimals: the rounded score is the one reported, and the one compared.
func (n Norm) Judge(value float64) Anomaly {
	out := Anomaly{Norm: n.median}
	var score float64
	switch {
	case value > n.median && n.above == 0, value < n.median && n.below == 0:
		return out
	case value > n.median:
		score = (value - n.median) / n.above
	case value < n.median:
		score = -(n.median - value) / n.below
	}
	score = math.Round(score*100) / 100
	if score == 0 {
		// −0 is reported as 0.
		score = 0
	}
	out.Score = &score
	return out
}

// Rank orders the unusual anomalies of one answer, given in the order the state lists its
// subjects: those with no score first, then by the magnitude of the score, ties in the
// given order. A nil entry is a subject that carries no anomaly.
func Rank(anomalies []*Anomaly) {
	var unusual []*Anomaly
	for _, a := range anomalies {
		if a == nil {
			continue
		}
		a.Rank = 0
		if a.Unusual() {
			unusual = append(unusual, a)
		}
	}
	sort.SliceStable(unusual, func(i, j int) bool {
		a, b := unusual[i].Score, unusual[j].Score
		if a == nil || b == nil {
			return a == nil && b != nil
		}
		return math.Abs(*a) > math.Abs(*b)
	})
	for i, a := range unusual {
		a.Rank = i + 1
	}
}
