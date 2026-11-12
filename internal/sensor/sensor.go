// Package sensor defines what every sensor looks like from the agent's side. The
// interface must not reveal whether an implementation is built in or external
// (ADR 0003).
package sensor

import (
	"context"
	"math"
	"time"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
)

// Measurement is one reading, as collected on the node.
type Measurement struct {
	Metric string
	Labels map[string]string
	Value  float64
	TS     time.Time
	// Notes are short lines about this one value, such as what a count counts, shown
	// verbatim beside it on the surfaces ADR 0060 names. They leave the node, so a sensor
	// puts in one only what may travel to the hub and its channels.
	Notes []string
}

// Sensor takes one kind of reading. Name and Applicable make up the manifest the agent
// sends; Collect is called on the schedule the hub configures.
type Sensor interface {
	Name() string
	// Applicable reports whether this machine can produce the reading at all.
	Applicable() bool
	// Collect may return measurements beside an error when one of several things it reads
	// failed: the agent logs the error and posts the measurements.
	Collect(ctx context.Context) ([]Measurement, error)
}

// Named is a sensor that says what its metrics and their families are called; one that
// does not is shown by its ids (docs/specs/metric-names.md).
type Named interface {
	Names() names.Book
}

// Defaulted is a sensor that declares a default threshold for a metric whose meaning is the
// same on every installation; the hub seeds each series with it once
// (docs/specs/thresholds.md#defaults).
type Defaulted interface {
	Defaults() map[string]Default
}

// Default is a threshold declared for a metric: a direction and up to two values in the
// series' own unit.
type Default struct {
	Direction Direction
	Warning   *float64
	Critical  *float64
}

// Direction is which side of a default is bad.
type Direction string

// The two directions a default can be read in.
const (
	Below Direction = "below"
	Above Direction = "above"
)

// Catalogued names metrics, and the families they belong to, from the interface's
// catalogue under `metric.<id>` and `family.<family>`, as a built-in sensor does (ADR 0008).
func Catalogued(metrics ...string) names.Book {
	book := names.Book{Metrics: map[string]names.Text{}, Families: map[string]names.Text{}}
	for _, metric := range metrics {
		family := names.FamilyOf(metric)
		book.Metrics[metric] = catalogued("metric." + metric)
		book.Families[family] = catalogued("family." + family)
	}
	return book
}

func catalogued(key string) names.Text {
	text := names.Text{}
	for _, locale := range i18n.Locales {
		text[locale] = i18n.For(locale).T(key)
	}
	return text
}

// Round2 rounds a reading to the two decimals the sensors report ratios and averages in.
func Round2(value float64) float64 {
	return math.Round(value*100) / 100
}
