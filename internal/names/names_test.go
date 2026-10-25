package names_test

import (
	"testing"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
)

func quote(value string) string { return "<" + value + ">" }

// spec: metric-names.md#naming — references are read left to right; anything else is literal.
func TestFillReadsReferencesLeftToRight(t *testing.T) {
	labels := map[string]string{"mount": "/srv", "fs": "ext4", "site": "{mount}"}
	cases := []struct {
		template, want string
		used           []string
	}{
		{"Free on {mount}", "Free on </srv>", []string{"mount"}},
		{"Free {", "Free {", nil},
		{"Free on {{mount}}", "Free on {</srv>}", []string{"mount"}},
		{"Free on { mount }", "Free on { mount }", nil},
		{"Site {site}", "Site <{mount}>", []string{"site"}},
		{"Free on {queue} now", "Free on now", nil},
		{"{queue} free", "free", nil},
		{"Free on {mount} {fs}", "Free on </srv> <ext4>", []string{"mount", "fs"}},
		{"<b>x</b>", "<b>x</b>", nil},
	}
	for _, c := range cases {
		got, used := names.Fill(c.template, labels, quote)
		if got != c.want {
			t.Errorf("Fill(%q) = %q, want %q", c.template, got, c.want)
		}
		if len(used) != len(c.used) {
			t.Errorf("Fill(%q) used %v, want %v", c.template, used, c.used)
		}
		for _, key := range c.used {
			if !used[key] {
				t.Errorf("Fill(%q) did not use %q", c.template, key)
			}
		}
	}
}

// spec: metric-names.md#naming — the reader's locale, then English, then nothing.
func TestANameFallsBackToEnglishOnly(t *testing.T) {
	book := names.Book{Metrics: map[string]names.Text{
		"disk.free_pct": {i18n.English: "Free on {mount}", i18n.Russian: "Свободно {mount}"},
		"load.avg_1m":   {i18n.English: "Load 1 min"},
		"memory.x":      {i18n.Russian: "Память"},
	}}
	cases := []struct {
		locale       i18n.Locale
		metric, want string
		named        bool
	}{
		{i18n.Russian, "disk.free_pct", "Свободно {mount}", true},
		{i18n.English, "disk.free_pct", "Free on {mount}", true},
		{i18n.Russian, "load.avg_1m", "Load 1 min", true},
		{i18n.English, "memory.x", "", false},
		{i18n.English, "queue.depth", "", false},
	}
	for _, c := range cases {
		got, named := book.Metrics[c.metric].In(c.locale)
		if got != c.want || named != c.named {
			t.Errorf("Metric(%s, %s) = %q, %v; want %q, %v", c.locale, c.metric, got, named, c.want, c.named)
		}
	}
}

// spec: metric-names.md#naming — a family without a name is headed by its id.
func TestFamilyFallsBackToItsID(t *testing.T) {
	book := names.Book{Families: map[string]names.Text{"disk": {i18n.English: "Disk", i18n.Russian: "Диск"}}}
	if got := book.Family(i18n.Russian, "disk"); got != "Диск" {
		t.Errorf("Family(ru, disk) = %q, want Диск", got)
	}
	if got := book.Family(i18n.Russian, "queue"); got != "queue" {
		t.Errorf("Family(ru, queue) = %q, want queue", got)
	}
}

// spec: metric-names.md#ingest — what a declared name may hold.
func TestValid(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Free on {mount}", true},
		{"Свободно памяти", true},
		{"", false},
		{"Free on {mount}\n✅ resolved", false},
		{"Free\u202eon", false},
		{"\x7f", false},
		{"Free\u2028on", false},
		{"Free\u2029on", false},
		{string(make([]rune, 80)), false},
		{"Свободно Свободно Свободно Свободно Свободно Свободно Свободно Свободно Свободно", true},
		{"Свободно Свободно Свободно Свободно Свободно Свободно Свободно Свободно Свободноо", false},
	}
	for _, c := range cases {
		if got := names.Valid(c.name); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
