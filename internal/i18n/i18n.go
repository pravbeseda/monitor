// Package i18n holds every user-facing string and the locale rules that go with them.
// No user-facing text is written at its usage site (ADR 0008); log lines and CLI output
// stay English and never come from here.
package i18n

import (
	"strings"
	"time"
)

// Locale is one of the two languages the interface speaks.
type Locale string

// The two languages of ADR 0008; English is also the fallback.
const (
	English Locale = "en"
	Russian Locale = "ru"
)

// Negotiate picks the locale for one request: a supported ?lang= wins, then the browser's
// Accept-Language, then English. A ?lang= naming a language the panel does not speak is
// ignored rather than honoured — a broken link should not cost the reader the language
// the browser already asked for.
func Negotiate(query, acceptLanguage string) Locale {
	if locale, ok := Match(query); ok {
		return locale
	}
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		if locale, ok := Match(tag); ok {
			return locale
		}
	}
	return English
}

// Match accepts a regional tag too: ru-BY is Russian. It is what turns a browser's tag
// into a locale a link can carry on.
func Match(tag string) (Locale, bool) {
	language, _, _ := strings.Cut(tag, "-")
	return Parse(language)
}

// Parse reads a locale written by hand — a configuration file, a flag — where a regional
// tag is a mistake rather than a browser's habit: such a file names one of the two
// languages the interface speaks, or the hub does not start.
func Parse(tag string) (Locale, bool) {
	switch Locale(strings.ToLower(strings.TrimSpace(tag))) {
	case English:
		return English, true
	case Russian:
		return Russian, true
	default:
		return "", false
	}
}

// Printer renders text and numbers in one locale, and instants in one zone.
type Printer struct {
	locale Locale
	zone   *time.Location
}

// For returns the printer of a locale, writing instants in UTC until a reader's zone is
// known. A locale the catalogue does not know becomes English here, so every printer speaks
// a language the tables and the catalogue hold.
func For(locale Locale) *Printer {
	switch locale {
	case English, Russian:
		return &Printer{locale: locale}
	default:
		return &Printer{locale: English}
	}
}

// In returns the same printer writing instants in the reader's zone (spec: web.md#zone).
func (p *Printer) In(zone *time.Location) *Printer {
	out := *p
	out.zone = zone
	return &out
}

// Locale is the language this printer speaks.
func (p *Printer) Locale() Locale { return p.locale }

// T looks a string up by its English identifier. An unknown key renders as itself: a
// missing translation is a bug, and an empty cell hides it.
func (p *Printer) T(key string) string {
	texts, known := catalogue[key]
	if !known {
		return key
	}
	if text, translated := texts[p.locale]; translated {
		return text
	}
	return texts[English]
}
