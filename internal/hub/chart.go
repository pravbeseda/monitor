package hub

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/history"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The chart is drawn on the server, in a fixed viewBox the page scales to its width: no
// client-side charting library and no frontend build (ADR 0005).
const (
	axisTicks = 5
	// densePeriod is the window below which an axis is read by time of day rather than
	// by date.
	densePeriod = 48 * time.Hour
)

// frame is the viewBox a chart is drawn in, the margins its axes' labels take, and whether
// the chart names its zone: one of many on a page leaves that to the page
// (docs/specs/web.md#zone).
type frame struct {
	width, height                         float64
	left, right, top, bottom, labelOffset float64
	zone                                  bool
}

// fullFrame is the history page's chart; smallFrame is one of many on an item's page.
var (
	fullFrame  = frame{width: 820, height: 280, left: 70, right: 20, top: 16, bottom: 34, labelOffset: 12, zone: true}
	smallFrame = frame{width: 400, height: 170, left: 62, right: 10, top: 14, bottom: 26, labelOffset: 8}
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
	// Areas close each line down to the baseline, for a skin that fills under it.
	Areas          []string
	Dots           []dot
	XTicks, YTicks []tick
	// Levels are the series' stored threshold values, drawn across the plot.
	Levels []levelLine
	// Zone names the reading of the time axis once: a tick label has no room for it
	// (spec: web.md#zone).
	Zone string
}

type dot struct{ X, Y float64 }

// levelLine is one threshold value across the plot, named by the level it opens.
type levelLine struct {
	Y float64
	levelView
}

// threshold is a value to draw across a chart, before it is scaled.
type threshold struct {
	value float64
	level evaluate.Level
}

type tick struct {
	At float64
	// LabelAt is where the label sits: beside the value axis, below the time axis.
	LabelAt float64
	Label   string
}

// draw scales one series into the frame. The value axis starts at zero: a free-space
// chart scaled to its own minimum turns a quiet week into a cliff. It reaches above the
// thresholds too, so a line the data never came near is still on the chart.
func draw(printer *i18n.Printer, series history.Series, window history.Window, in frame, levels ...threshold) chart {
	out := chart{
		Width: in.width, Height: in.height,
		Left: in.left, Top: in.top,
		Baseline: in.height - in.bottom, RightEdge: in.width - in.right,
	}
	if in.zone {
		out.Zone = printer.Zone(window.To)
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
	for _, level := range levels {
		if level.value > highest {
			highest = level.value
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
		return in.left + float64(at.Sub(window.From))/float64(span)*(in.width-in.left-in.right)
	}
	y := func(value float64) float64 {
		return in.height - in.bottom - value/top*(in.height-in.bottom-in.top)
	}
	for _, level := range levels {
		// The value axis starts at zero, so a negative value has no place on it.
		if level.value >= 0 {
			out.Levels = append(out.Levels, levelLine{Y: y(level.value), levelView: *levelOf(printer, &level.level)})
		}
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
		first, last := x(segment[0].TS), x(segment[len(segment)-1].TS)
		out.Areas = append(out.Areas, fmt.Sprintf("%.1f,%.1f %s %.1f,%.1f", first, out.Baseline, out.Lines[len(out.Lines)-1], last, out.Baseline))
	}

	for i := 0; i <= axisTicks; i++ {
		at := window.From.Add(time.Duration(float64(span) * float64(i) / axisTicks))
		out.XTicks = append(out.XTicks, tick{
			At:      x(at),
			LabelAt: in.height - in.labelOffset,
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
