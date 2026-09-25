package i18n_test

import (
	"testing"
	"time"
	// A zone name has to resolve here too, on a host that carries no zone database of its
	// own; the hub reaches the same database through internal/hub (ADR 0026).
	_ "time/tzdata"

	"github.com/pravbeseda/monitor/internal/i18n"
)

func TestNegotiate(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		header string
		want   i18n.Locale
	}{
		{"the query wins over the browser", "en", "ru-RU,ru;q=0.9", i18n.English},
		{"the browser is followed when the query is silent", "", "ru-RU,ru;q=0.9,en;q=0.8", i18n.Russian},
		{"nothing asked for means English", "", "", i18n.English},
		{"an unsupported language falls back to English", "de", "de-DE", i18n.English},
		{"an unsupported query leaves the choice to the browser", "de", "ru-RU", i18n.Russian},
		{"a regional tag still matches its language", "", "ru-BY", i18n.Russian},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := i18n.Negotiate(tc.query, tc.header); got != tc.want {
				t.Errorf("Negotiate(%q, %q) = %q, want %q", tc.query, tc.header, got, tc.want)
			}
		})
	}
}

func TestTranslate(t *testing.T) {
	if got := i18n.For(i18n.English).T("page.title"); got != "Monitor" {
		t.Errorf("English title = %q", got)
	}
	if got := i18n.For(i18n.Russian).T("table.free"); got == "" || got == "table.free" {
		t.Errorf("Russian text for table.free = %q, want a translation", got)
	}
}

// A key with no text is a bug in the catalogue, so it shows as itself rather than as an
// empty cell that nobody notices.
func TestTranslateShowsAnUnknownKey(t *testing.T) {
	if got := i18n.For(i18n.English).T("nothing.here"); got != "nothing.here" {
		t.Errorf("unknown key rendered as %q, want the key itself", got)
	}
}

func TestFormatsFollowTheLocale(t *testing.T) {
	at := time.Date(2026, 8, 28, 10, 5, 0, 0, time.UTC)
	tests := []struct {
		locale               i18n.Locale
		bytes, percent, when string
	}{
		{i18n.English, "1.5 GB", "34.2%", "2026-08-28 10:05 UTC"},
		{i18n.Russian, "1,5 ГБ", "34,2 %", "28.08.2026 10:05 UTC"},
	}

	for _, tc := range tests {
		t.Run(string(tc.locale), func(t *testing.T) {
			p := i18n.For(tc.locale)
			if got := p.Bytes(1.5e9); got != tc.bytes {
				t.Errorf("Bytes = %q, want %q", got, tc.bytes)
			}
			if got := p.Percent(34.24); got != tc.percent {
				t.Errorf("Percent = %q, want %q", got, tc.percent)
			}
			if got := p.Time(at); got != tc.when {
				t.Errorf("Time = %q, want %q", got, tc.when)
			}
		})
	}
}

func TestBytesScalesToTheValue(t *testing.T) {
	p := i18n.For(i18n.English)
	tests := map[float64]string{
		512:    "512 B",
		2048:   "2.0 kB",
		3.3e6:  "3.3 MB",
		2.5e12: "2.5 TB",
	}

	for value, want := range tests {
		if got := p.Bytes(value); got != want {
			t.Errorf("Bytes(%v) = %q, want %q", value, got, want)
		}
	}
}

// Parse reads a locale written by hand, where a regional tag is a mistake rather than a
// browser's habit.
func TestParse(t *testing.T) {
	spoken := []struct {
		tag  string
		want i18n.Locale
	}{
		{"en", i18n.English},
		{"EN", i18n.English},
		{"  ru  ", i18n.Russian},
	}
	for _, tc := range spoken {
		if got, ok := i18n.Parse(tc.tag); !ok || got != tc.want {
			t.Fatalf("Parse(%q) = %q, %v; want %q, true", tc.tag, got, ok, tc.want)
		}
	}
	for _, tag := range []string{"ru-BY", "", "english", "fr"} {
		if got, ok := i18n.Parse(tag); ok {
			t.Fatalf("Parse(%q) = %q, true; want it refused", tag, got)
		}
	}
}

// Nothing reaches a printer with an unknown locale today: configuration and Negotiate both
// refuse one. The invariant is held anyway because a locale passed straight to a channel
// would panic inside that channel's own goroutine, which costs the hub, not one message.
func TestPrinterFallsBackToEnglish(t *testing.T) {
	p := i18n.For(i18n.Locale("de"))
	if got := p.Locale(); got != i18n.English {
		t.Errorf("Locale = %q, want %q", got, i18n.English)
	}
	if got := p.Bytes(1.5e9); got != "1.5 GB" {
		t.Errorf("Bytes = %q, want the English form", got)
	}
	if got := p.Percent(34.24); got != "34.2%" {
		t.Errorf("Percent = %q, want the English form", got)
	}
	if got := p.Time(time.Date(2026, 8, 28, 10, 5, 0, 0, time.UTC)); got != "2026-08-28 10:05 UTC" {
		t.Errorf("Time = %q, want the English form", got)
	}
	if got := p.T("page.title"); got != "Monitor" {
		t.Errorf("T = %q, want the English text", got)
	}
}

// spec: web.md#zone — a printer writes instants in the reader's zone, marked with it.
func TestPrinterWritesInTheReaderZone(t *testing.T) {
	at := time.Date(2026, 8, 28, 22, 5, 0, 0, time.UTC)
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load the zone: %v", err)
	}
	p := i18n.For(i18n.English).In(moscow)

	if got := p.Time(at); got != "2026-08-29 01:05 MSK" {
		t.Errorf("Time = %q, want the Moscow reading", got)
	}
	if got := p.Clock(at); got != "01:05" {
		t.Errorf("Clock = %q, want the Moscow hour", got)
	}
	if got := p.Day(at); got != "Aug 29" {
		t.Errorf("Day = %q, want the Moscow day", got)
	}
	if got := p.Zone(at); got != "MSK" {
		t.Errorf("Zone = %q, want the abbreviation", got)
	}
	if got := p.Locale(); got != i18n.English {
		t.Errorf("Locale = %q, want the locale to survive In", got)
	}
}

// spec: web.md#zone — a printer nobody gave a zone writes UTC, and says so.
func TestPrinterWithoutAZoneWritesUTC(t *testing.T) {
	at := time.Date(2026, 8, 28, 22, 5, 0, 0, time.UTC)
	p := i18n.For(i18n.English)

	if got := p.Time(at); got != "2026-08-28 22:05 UTC" {
		t.Errorf("Time = %q, want UTC", got)
	}
	if got := p.Zone(at); got != "UTC" {
		t.Errorf("Zone = %q, want UTC", got)
	}
	if got := p.In(nil).Time(at); got != "2026-08-28 22:05 UTC" {
		t.Errorf("Time = %q, want UTC for a zone that is not there", got)
	}
}

// spec: web.md#zone — a zone with no abbreviation is marked by its offset.
func TestPrinterMarksAnOffsetOnlyZone(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Novosibirsk")
	if err != nil {
		t.Fatalf("load the zone: %v", err)
	}
	at := time.Date(2026, 8, 28, 10, 5, 0, 0, time.UTC)
	if got := i18n.For(i18n.English).In(zone).Zone(at); got != "+07" {
		t.Errorf("Zone = %q, want the offset", got)
	}
}

// spec: timeline.md#changes — a day heading: today, yesterday, or the day and month, with
// the year when it is not the current one, as each language writes them.
func TestDateNamesTheDay(t *testing.T) {
	now := time.Date(2026, time.September, 24, 15, 40, 0, 0, time.UTC)
	for _, tc := range []struct {
		locale i18n.Locale
		at     time.Time
		want   string
	}{
		{i18n.English, now.Add(-time.Hour), "Today"},
		{i18n.Russian, now.Add(-16 * time.Hour), "Вчера"},
		{i18n.English, time.Date(2026, time.September, 21, 9, 0, 0, 0, time.UTC), "21 September"},
		{i18n.Russian, time.Date(2026, time.September, 21, 9, 0, 0, 0, time.UTC), "21 сентября"},
		{i18n.English, time.Date(2025, time.September, 21, 9, 0, 0, 0, time.UTC), "21 September 2025"},
		{i18n.Russian, time.Date(2025, time.March, 1, 9, 0, 0, 0, time.UTC), "1 марта 2025"},
	} {
		if got := i18n.For(tc.locale).Date(tc.at, now); got != tc.want {
			t.Errorf("%s Date(%v) = %q, want %q", tc.locale, tc.at, got, tc.want)
		}
	}
}

// spec: timeline.md#changes — days are counted in the reader's zone.
func TestDateCountsDaysInTheReadersZone(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load the zone: %v", err)
	}
	now := time.Date(2026, time.September, 24, 0, 30, 0, 0, time.UTC) // 03:30 in Moscow
	at := time.Date(2026, time.September, 23, 22, 0, 0, 0, time.UTC)  // 01:00 in Moscow, the same day
	if got := i18n.For(i18n.English).In(moscow).Date(at, now); got != "Today" {
		t.Errorf("Date = %q, want today in Moscow", got)
	}
}

// spec: timeline.md#changes — yesterday is the calendar day before, even where the clocks
// skip midnight.
func TestDateKnowsYesterdayWhereClocksSkipMidnight(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatalf("load the zone: %v", err)
	}
	now := time.Date(2025, time.September, 8, 0, 30, 0, 0, santiago)
	p := i18n.For(i18n.English).In(santiago)
	if got := p.Date(time.Date(2025, time.September, 7, 18, 0, 0, 0, santiago), now); got != "Yesterday" {
		t.Errorf("Sunday = %q, want Yesterday", got)
	}
	if got := p.Date(time.Date(2025, time.September, 6, 23, 45, 0, 0, santiago), now); got != "6 September" {
		t.Errorf("Saturday = %q, want its date", got)
	}
}
