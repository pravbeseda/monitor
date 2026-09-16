package hub

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The chart is drawn on the server, in a fixed viewBox the page scales to its width: no
// client-side charting library and no frontend build (ADR 0005).
const (
	chartWidth  = 820
	chartHeight = 280
	plotLeft    = 70
	plotRight   = 20
	plotTop     = 16
	plotBottom  = 34
	axisTicks   = 5
	// densePeriod is the window below which an axis is read by time of day rather than
	// by date.
	densePeriod = 48 * time.Hour
)

// chart is the geometry a template needs; every number is already scaled to the viewBox
// and every label already translated.
type chart struct {
	Width, Height float64
	Left, Top     float64
	// Baseline and RightEdge are the plot's far sides, already offset by their margins,
	// so the template does no arithmetic.
	Baseline, RightEdge float64
	Lines               []string
	Dots                []dot
	XTicks, YTicks      []tick
	// Zone names the reading of the time axis once: a tick label has no room for it
	// (spec: web.md#zone).
	Zone string
}

type dot struct{ X, Y float64 }

type tick struct {
	At float64
	// LabelAt is where the label sits: beside the value axis, below the time axis.
	LabelAt float64
	Label   string
}

// draw scales one series into the viewBox. The value axis starts at zero: a free-space
// chart scaled to its own minimum turns a quiet week into a cliff.
func draw(printer *i18n.Printer, series history.Series, window history.Window) chart {
	out := chart{
		Width: chartWidth, Height: chartHeight,
		Left: plotLeft, Top: plotTop,
		Baseline: chartHeight - plotBottom, RightEdge: chartWidth - plotRight,
		Zone: printer.Zone(window.To),
	}
	span := window.To.Sub(window.From)
	if span <= 0 {
		return out
	}
	highest := 0.0
	for _, point := range series.Points {
		if point.Value > highest {
			highest = point.Value
		}
	}
	// A series of zeroes still needs an axis, and a full one needs headroom above it —
	// unless the headroom is what overflows, in which case the peak touches the top.
	top := highest * 1.1
	if math.IsInf(top, 1) {
		top = highest
	}
	if top <= 0 {
		top = 1
	}

	x := func(at time.Time) float64 {
		return plotLeft + float64(at.Sub(window.From))/float64(span)*float64(chartWidth-plotLeft-plotRight)
	}
	y := func(value float64) float64 {
		return float64(chartHeight-plotBottom) - value/top*float64(chartHeight-plotBottom-plotTop)
	}

	for _, segment := range segments(series.Points, series.Interval) {
		if len(segment) == 1 {
			out.Dots = append(out.Dots, dot{X: x(segment[0].TS), Y: y(segment[0].Value)})
			continue
		}
		pairs := make([]string, 0, len(segment))
		for _, point := range segment {
			pairs = append(pairs, fmt.Sprintf("%.1f,%.1f", x(point.TS), y(point.Value)))
		}
		out.Lines = append(out.Lines, strings.Join(pairs, " "))
	}

	for i := 0; i <= axisTicks; i++ {
		at := window.From.Add(time.Duration(float64(span) * float64(i) / axisTicks))
		out.XTicks = append(out.XTicks, tick{
			At:      x(at),
			LabelAt: chartHeight - 12,
			Label:   axisLabel(printer, at, span),
		})

		// Dividing before multiplying keeps a legal but enormous value from overflowing
		// into an infinity the SVG cannot carry.
		value := top / axisTicks * float64(i)
		out.YTicks = append(out.YTicks, tick{
			At:      y(value),
			LabelAt: y(value) + 4,
			Label:   format(printer, series.Metric, value),
		})
	}
	return out
}

func axisLabel(printer *i18n.Printer, at time.Time, span time.Duration) string {
	if span < densePeriod {
		return printer.Clock(at)
	}
	return printer.Day(at)
}

// segments cuts the series where the node was not reporting, so a silence is a hole in the
// line rather than a straight run across it (docs/specs/history.md#gaps).
func segments(points []storage.Point, interval time.Duration) [][]storage.Point {
	var out [][]storage.Point
	start := 0
	for i := 1; i < len(points); i++ {
		if history.Gap(interval, points[i-1].TS, points[i].TS) {
			out = append(out, points[start:i])
			start = i
		}
	}
	if start < len(points) {
		out = append(out, points[start:])
	}
	return out
}
