// Package names holds what the sensors say their metrics and families are called, and fills
// a metric's name with the labels of one series (docs/specs/metric-names.md).
package names

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pravbeseda/monitor/internal/i18n"
)

// MaxLength is the longest name kept, in characters.
const MaxLength = 80

// Text is one name in each locale it was declared in.
type Text map[i18n.Locale]string

// Book is every name the hub has been told: metrics by id, families by the part of an id
// before its first dot.
type Book struct {
	Metrics  map[string]Text
	Families map[string]Text
}

// In is the name in locale, in English when locale has none; false when neither was
// declared, and what it names is to be shown by its id.
func (t Text) In(locale i18n.Locale) (string, bool) {
	if name, ok := t[locale]; ok {
		return name, true
	}
	name, ok := t[i18n.English]
	return name, ok
}

// FamilyOf is the family of a metric id: the part before its first dot, or the whole id
// when it has none.
func FamilyOf(metric string) string {
	family, _, _ := strings.Cut(metric, ".")
	return family
}

// Bare is a metric's name with its references left out: what it says of every series.
func Bare(template string) string {
	bare, _ := Fill(template, nil, nil)
	return bare
}

// Family is the name of a family in locale, in English when locale has none, and its id
// when neither was declared.
func (b Book) Family(locale i18n.Locale, family string) string {
	if name, named := b.Families[family].In(locale); named {
		return name
	}
	return family
}

// Fill replaces each reference to a label — `{` + key + `}`, read left to right — with that
// label's value as quote renders it, and reports which labels it used. A reference to a
// label the series lacks is left out with one space beside it; any other character is
// literal, and a filled value is never read again.
func Fill(template string, labels map[string]string, quote func(string) string) (string, map[string]bool) {
	var out strings.Builder
	used := map[string]bool{}
	for rest := template; rest != ""; {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:open])
		key, length := reference(rest[open:])
		if length == 0 {
			out.WriteByte('{')
			rest = rest[open+1:]
			continue
		}
		rest = rest[open+length:]
		value, ok := labels[key]
		if !ok {
			// The reference goes with one of the spaces around it, or the name keeps a gap.
			if out.Len() == 0 || strings.HasSuffix(out.String(), " ") {
				rest = strings.TrimPrefix(rest, " ")
			}
			continue
		}
		out.WriteString(quote(value))
		used[key] = true
	}
	return strings.TrimSpace(out.String()), used
}

// reference reads `{key}` at the start of text and returns the key and the reference's
// length in bytes, or a length of zero when text does not start with one.
func reference(text string) (string, int) {
	end := strings.IndexByte(text, '}')
	if end < 2 {
		return "", 0
	}
	key := text[1:end]
	for _, r := range key {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", 0
		}
	}
	return key, end + 1
}

// Valid reports whether a declared name may be kept: not empty, at most MaxLength
// characters, and free of control, format and line-separating characters, which would let
// a name break a message into lines or turn its text around.
func Valid(name string) bool {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxLength {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return false
		}
	}
	return true
}
