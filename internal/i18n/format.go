package i18n

import (
	"fmt"
	"strings"
	"time"
)

// Byte sizes use decimal units, the way disks are sold and the way both macOS and the
// hosting panels report them.
var byteUnits = map[Locale][]string{
	English: {"B", "kB", "MB", "GB", "TB", "PB"},
	Russian: {"Б", "кБ", "МБ", "ГБ", "ТБ", "ПБ"},
}

var dayLayouts = map[Locale]string{
	English: "Jan 02",
	Russian: "02.01",
}

var timeLayouts = map[Locale]string{
	English: "2006-01-02 15:04 MST",
	Russian: "02.01.2006 15:04 MST",
}

// Bytes renders a size in the largest unit that keeps it above one.
func (p *Printer) Bytes(value float64) string {
	units := byteUnits[p.locale]
	size, unit := value, units[0]
	for _, next := range units[1:] {
		if size < 1000 {
			break
		}
		size, unit = size/1000, next
	}
	if unit == units[0] {
		return fmt.Sprintf("%s %s", p.number(size, 0), unit)
	}
	return fmt.Sprintf("%s %s", p.number(size, 1), unit)
}

// Percent renders a percentage the way each language writes one: 34.2% but 34,2 %.
func (p *Printer) Percent(value float64) string {
	if p.locale == Russian {
		return p.number(value, 1) + " %"
	}
	return p.number(value, 1) + "%"
}

// Time renders an instant in the printer's zone, marked with it: the hub stores UTC, so a
// page that does not say which zone it is written in says nothing (spec: web.md#zone).
func (p *Printer) Time(at time.Time) string {
	return p.at(at).Format(timeLayouts[p.locale])
}

// Clock labels a chart axis spanning hours, where the full timestamp of Time would not fit.
func (p *Printer) Clock(at time.Time) string { return p.at(at).Format("15:04") }

// Day labels a chart axis spanning days, in the order each language writes a date.
func (p *Printer) Day(at time.Time) string {
	return p.at(at).Format(dayLayouts[p.locale])
}

// Zone names the zone the labels around it are read in, for the one place a page states it
// rather than repeating it on every tick. Zones without an abbreviation name their offset.
func (p *Printer) Zone(at time.Time) string { return p.at(at).Format("MST") }

// at moves an instant into the printer's zone. A printer nobody gave one writes UTC, which
// is also what a zero Printer does.
func (p *Printer) at(instant time.Time) time.Time {
	if p.zone == nil {
		return instant.UTC()
	}
	return instant.In(p.zone)
}

// Number renders a plain value: a metric whose id carries no unit still has to be shown.
func (p *Printer) Number(value float64) string { return p.number(value, 2) }

// number applies the decimal separator of the locale.
func (p *Printer) number(value float64, decimals int) string {
	text := fmt.Sprintf("%.*f", decimals, value)
	if p.locale == Russian {
		return strings.Replace(text, ".", ",", 1)
	}
	return text
}
