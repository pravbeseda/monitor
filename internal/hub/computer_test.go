package hub_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/hub"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

var (
	linePattern   = regexp.MustCompile(`(?s)<li class="line([^"]*)"(?: data-line="([^"]*)")?(?: title="([^"]*)")?>(.*?)</li>`)
	lineName      = regexp.MustCompile(`<span class="line-name">([^<]*)</span>`)
	valuePattern  = regexp.MustCompile(`<span class="value([^"]*)"[^>]*>([^<]*)</span>`)
	whyPattern    = regexp.MustCompile(`<span class="line-why([^"]*)"(?: title="([^"]*)")?>([^<]*)(?:<span class="row-mark-word"> ([^<]*)</span>)?</span>`)
	failedPattern = regexp.MustCompile(`<span class="card-failed([^"]*)"[^>]*>([^<]*)</span>`)
	seenPattern   = regexp.MustCompile(`<span class="card-seen ?([^"]*)">last report [^<]*</span>`)
)

// line is one line of a computer's card as a reader sees it.
type line struct {
	class, key, title, name, body string
}

func linesOf(card string) []line {
	var out []line
	for _, found := range linePattern.FindAllStringSubmatch(card, -1) {
		one := line{class: found[1], key: found[2], title: found[3], body: found[4]}
		if name := lineName.FindStringSubmatch(found[4]); name != nil {
			one.name = name[1]
		}
		out = append(out, one)
	}
	return out
}

func lineNames(card string) []string {
	var out []string
	for _, one := range linesOf(card) {
		out = append(out, one.name)
	}
	return out
}

func lineCalled(t *testing.T, card, name string) line {
	t.Helper()
	for _, one := range linesOf(card) {
		if one.name == name {
			return one
		}
	}
	t.Fatalf("no line %q in %s", name, card)
	return line{}
}

// values are a line's values as "text" or "text (class)", the classes trimmed.
func (l line) values() []string {
	var out []string
	for _, found := range valuePattern.FindAllStringSubmatch(l.body, -1) {
		text, class := found[2], strings.TrimSpace(found[1])
		if class != "" {
			text += " (" + class + ")"
		}
		out = append(out, text)
	}
	return out
}

func (l line) why() string { return l.whySaid().text }

// whySaid is what a line says why it is marked: its text, its colour, what hover says and
// what a screen reader is told beyond the text.
type whySaid struct{ text, class, hover, reader string }

func (l line) whySaid() whySaid {
	found := whyPattern.FindStringSubmatch(l.body)
	if found == nil {
		return whySaid{}
	}
	return whySaid{text: found[3], class: strings.TrimSpace(found[1]), hover: found[2], reader: found[4]}
}

func (l line) empty() bool { return strings.Contains(l.class, "empty") }

// linux is a Linux server with every slot, failed units at zero, nothing needing attention.
func linux(node string) []state.Subject {
	return []state.Subject{
		plain(node, "load.avg_1m", 0.52), plain(node, "load.avg_5m", 0.61), plain(node, "load.avg_15m", 0.67),
		plain(node, "memory.available_pct", 38.5), plain(node, "memory.available_bytes", 2e9),
		freePct(node, "/", 40), free(node, "/", 40e9),
		plain(node, "systemd.failed_units", 0), jobAge(node, "a", 3600),
	}
}

func computerCard(t *testing.T, current state.State, stored limits, name string) string {
	t.Helper()
	return showCard(t, current, held{}, stored, name)
}

func without(subjects []state.Subject, metrics ...string) []state.Subject {
	return slices.DeleteFunc(slices.Clone(subjects), func(s state.Subject) bool { return slices.Contains(metrics, s.Metric) })
}

func replaced(subjects []state.Subject, with state.Subject) []state.Subject {
	out := make([]state.Subject, len(subjects))
	for i, s := range subjects {
		if s.Metric == with.Metric && s.Node == with.Node && maps.Equal(s.Labels, with.Labels) {
			s = with
		}
		out[i] = s
	}
	return out
}

// upserted is subjects with this one in place of the series it is, or added when none is.
func upserted(subjects []state.Subject, with state.Subject) []state.Subject {
	if slices.ContainsFunc(subjects, func(s state.Subject) bool {
		return s.Metric == with.Metric && s.Node == with.Node && maps.Equal(s.Labels, with.Labels)
	}) {
		return replaced(subjects, with)
	}
	return append(slices.Clone(subjects), with)
}

func banded(s state.Subject, low, norm, high float64, rank int) state.Subject {
	score := 0.0
	s.Anomaly = &anomaly.Anomaly{Norm: norm, Low: low, High: high, Score: &score, Rank: rank}
	return s
}

var slotNames = []string{"Load", "Free RAM", "Disk / free", "Backup"}

// spec: overview.md#computer-card
func TestComputerCardSlots(t *testing.T) {
	t.Run("any computer card", func(t *testing.T) {
		root := storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/", "fs": "ext4", "removable": "false"}}
		day := held{heldKey(root): {{TS: lastSeen.Add(-2 * time.Hour), Value: 9}, {TS: lastSeen.Add(-time.Hour), Value: 8}}}
		for _, extra := range [][]state.Subject{
			{judgedAt(freePct("server-b", "/", 8), evaluate.Warning)},
			{silenceOf("server-b", evaluate.Critical)},
		} {
			subjects := replaced(append(linux("server-b"), extra...), extra[0])
			body := showCard(t, stateOf(subjects...), day, limits{}, "server-b")
			containsAll(t, "card", body, `<span class="card-state">`)
			if strings.Contains(body, "<svg") || strings.Contains(body, "card-reason") {
				t.Errorf("a computer's card carries a line of the day or a reason: %s", body)
			}
		}
	})
	t.Run("the four slots in order, nothing after", func(t *testing.T) {
		body := computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), slotNames)
		if strings.Contains(body, "card-more") {
			t.Errorf("more after the slots: %s", body)
		}
	})
	t.Run("a Mac with Time Machine and no systemd", func(t *testing.T) {
		mac := append(without(linux("laptop-a"), "systemd.failed_units", "backup.success_age_seconds"), tmAge("laptop-a", "a", 3600))
		equal(t, "lines", lineNames(computerCard(t, stateOf(mac...), limits{}, "laptop-a")), slotNames)
	})
	t.Run("no backup", func(t *testing.T) {
		body := computerCard(t, stateOf(without(linux("server-b"), "backup.success_age_seconds")...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), slotNames)
		backup := linesOf(body)[3]
		if !backup.empty() || !strings.Contains(backup.body, "—") {
			t.Errorf("the backup slot is not a dash: %s", backup.body)
		}
	})
	t.Run("no disk", func(t *testing.T) {
		body := computerCard(t, stateOf(without(linux("server-b"), "disk.free_pct", "disk.free_bytes")...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), []string{"Load", "Free RAM", "Disk free", "Backup"})
		if !linesOf(body)[2].empty() {
			t.Errorf("the disk slot is not a dash")
		}
	})
	t.Run("every volume removable", func(t *testing.T) {
		subjects := append(without(linux("server-b"), "disk.free_pct", "disk.free_bytes"), removable(freePct("server-b", "/usb", 3)))
		body := computerCard(t, stateOf(subjects...), limits{}, "server-b")
		if disk := lineCalled(t, body, "Disk free"); !disk.empty() {
			t.Errorf("a removable volume fills the disk slot: %s", disk.body)
		}
	})
	t.Run("nothing drawn between a line's name and its values", func(t *testing.T) {
		subjects := replaced(linux("server-b"), banded(freePct("server-b", "/", 40), 38, 40, 42, 1))
		ref := storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/", "fs": "ext4", "removable": "false"}}
		body := computerCard(t, stateOf(subjects...), limits{heldKey(ref): below(f(10), f(5))}, "server-b")
		nameThenValues := regexp.MustCompile(`^<span class="line-name">[^<]*</span>\s*<span class="line-values">.*</span>$`)
		for _, one := range linesOf(body) {
			if !nameThenValues.MatchString(one.body) || strings.Contains(one.body, "warning at") {
				t.Errorf("line %q holds more than its name and its values: %s", one.name, one.body)
			}
		}
	})
	t.Run("a muted series without its value", func(t *testing.T) {
		body := computerCard(t, stateOf(without(linux("server-b"), "load.avg_15m")...), limits{}, "server-b")
		load := lineCalled(t, body, "Load")
		if !load.empty() || strings.Contains(load.body, "0.61") {
			t.Errorf("load without its 15-minute average: %s", load.body)
		}
	})
	t.Run("a muted series without its value needing attention", func(t *testing.T) {
		subjects := replaced(without(linux("server-b"), "load.avg_15m"), judgedAt(plain("server-b", "load.avg_5m", 9), evaluate.Critical))
		body := computerCard(t, stateOf(subjects...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), append(append([]string(nil), slotNames...), "load.avg_5m"))
	})
	t.Run("no slot", func(t *testing.T) {
		body := computerCard(t, stateOf(plain("box-x", "uptime.boot_seconds", 9), plain("box-x", "queue.depth", 2)), limits{}, "box-x")
		equal(t, "lines", lineNames(body), []string{"queue.depth", "uptime.boot_seconds"})
	})
	t.Run("failed units its only series", func(t *testing.T) {
		body := computerCard(t, stateOf(plain("server-b", "systemd.failed_units", 0)), limits{}, "server-b")
		if found := failedPattern.FindStringSubmatch(body); found == nil || found[2] != "failed units: 0" {
			t.Errorf("an empty card: %s", body)
		}
	})
	t.Run("no series", func(t *testing.T) {
		body := computerCard(t, neverReported(stateOf(linux("server-b")...), "server-c"), limits{}, "server-c")
		containsAll(t, "card", body, "No series yet")
		if len(linesOf(body)) != 0 {
			t.Errorf("lines on a card with no series: %s", body)
		}
	})
	// lastReport is the card's last report and its class, and whether it closes the card.
	lastReport := func(t *testing.T, body string) (string, bool) {
		t.Helper()
		found := seenPattern.FindStringSubmatch(body)
		if found == nil {
			t.Fatalf("no last report: %s", body)
		}
		return strings.TrimSpace(found[1]), strings.Index(body, found[0]) > strings.LastIndex(body, `<li class="line`)
	}
	t.Run("a computer reporting", func(t *testing.T) {
		class, atFoot := lastReport(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"))
		if class != "" || !atFoot {
			t.Errorf("last report %q, at its foot %v; want muted, at its foot", class, atFoot)
		}
	})
	t.Run("a silent computer", func(t *testing.T) {
		body := computerCard(t, stateOf(append(linux("server-b"), silenceOf("server-b", evaluate.Critical))...), limits{}, "server-b")
		if class, atFoot := lastReport(t, body); class != "late" || !atFoot {
			t.Errorf("last report %q, at its foot %v; want brighter, at its foot", class, atFoot)
		}
	})
	t.Run("no fresh data", func(t *testing.T) {
		var asleep []state.Subject
		for _, s := range linux("laptop-a") {
			asleep = append(asleep, staled(s))
		}
		if class, atFoot := lastReport(t, computerCard(t, stateOf(asleep...), limits{}, "laptop-a")); class != "late" || !atFoot {
			t.Errorf("last report %q, at its foot %v; want brighter, at its foot", class, atFoot)
		}
	})
}

// spec: overview.md#computer-card
func TestComputerCardValues(t *testing.T) {
	t.Run("load: three averages, the 15-minute one its own", func(t *testing.T) {
		load := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Load")
		equal(t, "values", load.values(), []string{"0.52 (muted)", "0.61 (muted)", "0.67"})
	})
	t.Run("no 1-minute average", func(t *testing.T) {
		load := lineCalled(t, computerCard(t, stateOf(without(linux("server-b"), "load.avg_1m")...), limits{}, "server-b"), "Load")
		equal(t, "values", load.values(), []string{"0.61 (muted)", "0.67"})
	})
	t.Run("memory: bytes muted, then the percent its own", func(t *testing.T) {
		memory := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Free RAM")
		equal(t, "values", memory.values(), []string{"2.0 GB (muted)", "38.5%"})
	})
	t.Run("the least free volume", func(t *testing.T) {
		subjects := append(linux("server-b"), freePct("server-b", "/srv", 12), free("server-b", "/srv", 12e9))
		disk := lineCalled(t, computerCard(t, stateOf(subjects...), limits{}, "server-b"), "Disk /srv free")
		equal(t, "values", disk.values(), []string{"12.0 GB (muted)", "12.0%"})
	})
	t.Run("the oldest backup", func(t *testing.T) {
		subjects := append(without(linux("server-b"), "backup.success_age_seconds"), tmAge("server-b", "a", 2*86400), jobAge("server-b", "b", 9*86400))
		backup := lineCalled(t, computerCard(t, stateOf(subjects...), limits{}, "server-b"), "Backup")
		equal(t, "values", backup.values(), []string{"9.0 d"})
	})
	t.Run("the oldest fresh backup", func(t *testing.T) {
		subjects := append(without(linux("server-b"), "backup.success_age_seconds"), tmAge("server-b", "a", 2*86400), staled(jobAge("server-b", "b", 9*86400)))
		backup := lineCalled(t, computerCard(t, stateOf(subjects...), limits{}, "server-b"), "Backup")
		equal(t, "values", backup.values(), []string{"2.0 d"})
	})
	t.Run("a tie to the first in the page order", func(t *testing.T) {
		subjects := append(without(linux("server-b"), "backup.success_age_seconds"), tmAge("server-b", "a", 86400), jobAge("server-b", "b", 86400))
		if backup := lineCalled(t, computerCard(t, stateOf(subjects...), limits{}, "server-b"), "Backup"); !strings.HasPrefix(backup.title, "backup.success_age_seconds") {
			t.Errorf("hover = %q, want the backup job", backup.title)
		}
	})
	t.Run("a line's name on hover", func(t *testing.T) {
		if load := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Load"); load.title != "load.avg_15m" {
			t.Errorf("hover = %q, want the series of its own value", load.title)
		}
	})
}

// spec: overview.md#computer-card
func TestComputerCardFailedUnits(t *testing.T) {
	units := func(s state.Subject) []state.Subject { return replaced(linux("server-b"), s) }
	for _, tc := range []struct {
		name     string
		subjects []state.Subject
		want     string
	}{
		{"two, unwatched and usual", units(plain("server-b", "systemd.failed_units", 2)), "failed units: 2"},
		{"two at critical", units(judgedAt(plain("server-b", "systemd.failed_units", 2), evaluate.Critical)), "failed units: 2 (level-critical)"},
		{"zero", linux("server-b"), ""},
		{"two, stale", units(staled(plain("server-b", "systemd.failed_units", 2))), "failed units: 2 (level-stale)"},
		{"zero, watched and stale", units(staled(watchedOnly(plain("server-b", "systemd.failed_units", 0)))), "failed units: 0 (level-stale)"},
		{"zero, unusual", units(banded(plain("server-b", "systemd.failed_units", 0), 3, 3, 3, 1)), "failed units: 0 (level-unusual)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := computerCard(t, stateOf(tc.subjects...), limits{}, "server-b")
			got := ""
			if found := failedPattern.FindStringSubmatch(body); found != nil {
				got = found[2]
				if class := strings.TrimSpace(found[1]); class != "" {
					got += " (" + class + ")"
				}
			}
			if got != tc.want {
				t.Errorf("head = %q, want %q", got, tc.want)
			}
			equal(t, "lines", lineNames(body), slotNames)
			if found := strings.Index(body, "card-failed"); found >= 0 && found > strings.Index(body, "card-state") {
				t.Errorf("failed units after the state's word: %s", body)
			}
		})
	}
}

// spec: overview.md#computer-card
func TestComputerCardMarks(t *testing.T) {
	diskRef := storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/", "fs": "ext4", "removable": "false"}}
	show := func(t *testing.T, stored limits, with ...state.Subject) string {
		t.Helper()
		subjects := linux("server-b")
		for _, s := range with {
			subjects = replaced(subjects, s)
		}
		return computerCard(t, stateOf(subjects...), stored, "server-b")
	}

	t.Run("unusual", func(t *testing.T) {
		memory := lineCalled(t, show(t, limits{}, banded(plain("server-b", "memory.available_pct", 38.5), 43, 46, 52, 1)), "Free RAM")
		equal(t, "values", memory.values(), []string{"2.0 GB (muted)", "38.5% (level-unusual marked)"})
		if memory.why() != "usually 46.0%" {
			t.Errorf("why = %q", memory.why())
		}
	})
	t.Run("a level under a readable threshold", func(t *testing.T) {
		disk := lineCalled(t, show(t, limits{heldKey(diskRef): below(f(10), f(5))}, judgedAt(freePct("server-b", "/", 8), evaluate.Warning)), "Disk / free")
		equal(t, "values", disk.values(), []string{"40.0 GB (muted)", "8.0% (level-warning marked)"})
		if disk.why() != "threshold 10.0%" {
			t.Errorf("why = %q", disk.why())
		}
	})
	t.Run("the line's own first among equals", func(t *testing.T) {
		above := func(metric string) string {
			return heldKey(storage.SeriesRef{Node: "server-b", Metric: metric, Labels: map[string]string{}})
		}
		stored := limits{above("load.avg_15m"): {Direction: storage.Above, Warning: f(0.5)}, above("load.avg_1m"): {Direction: storage.Above, Warning: f(0.4)}}
		load := lineCalled(t, show(t, stored, judgedAt(plain("server-b", "load.avg_15m", 0.67), evaluate.Warning), judgedAt(plain("server-b", "load.avg_1m", 0.52), evaluate.Warning)), "Load")
		if load.why() != "threshold 0.50" {
			t.Errorf("why = %q, want the 15-minute threshold", load.why())
		}
	})
	t.Run("a level under an unreadable threshold", func(t *testing.T) {
		disk := lineCalled(t, show(t, limits{}, judgedAt(freePct("server-b", "/", 8), evaluate.Warning)), "Disk / free")
		if disk.why() != "warning" {
			t.Errorf("why = %q", disk.why())
		}
	})
	t.Run("the most urgent mark speaks", func(t *testing.T) {
		ref := storage.SeriesRef{Node: "server-b", Metric: "load.avg_1m", Labels: map[string]string{}}
		load := lineCalled(t, show(t, limits{heldKey(ref): {Direction: storage.Above, Critical: f(8)}},
			banded(plain("server-b", "load.avg_15m", 0.67), 0.1, 0.2, 0.3, 1), judgedAt(plain("server-b", "load.avg_1m", 9), evaluate.Critical)), "Load")
		equal(t, "values", load.values(), []string{"9.00 (muted level-critical marked)", "0.61 (muted)", "0.67 (level-unusual marked)"})
		if load.why() != "threshold 8.00" {
			t.Errorf("why = %q", load.why())
		}
	})
	t.Run("a muted series marked", func(t *testing.T) {
		memory := lineCalled(t, show(t, limits{}, banded(plain("server-b", "memory.available_bytes", 2e9), 2.5e9, 3e9, 3.5e9, 1)), "Free RAM")
		equal(t, "values", memory.values(), []string{"2.0 GB (muted level-unusual marked)", "38.5%"})
		if memory.why() != "usually 3.0 GB" {
			t.Errorf("why = %q", memory.why())
		}
	})
	t.Run("stale", func(t *testing.T) {
		load := lineCalled(t, show(t, limits{}, staled(plain("server-b", "load.avg_15m", 0.67))), "Load")
		if !strings.Contains(load.class, "faded") || load.why() != "" {
			t.Errorf("a stale line: class %q, why %q", load.class, load.why())
		}
	})
	t.Run("stale, a muted series fresh and unusual", func(t *testing.T) {
		load := lineCalled(t, show(t, limits{}, staled(plain("server-b", "load.avg_15m", 0.67)), banded(plain("server-b", "load.avg_1m", 3), 0.1, 0.2, 0.3, 1)), "Load")
		if !strings.Contains(load.class, "faded") || load.values()[0] != "3.00 (muted level-unusual marked)" || load.why() != "usually 0.20" {
			t.Errorf("class %q, values %q, why %q", load.class, load.values(), load.why())
		}
	})
}

// spec: overview.md#computer-card
func TestComputerCardExtraLines(t *testing.T) {
	t.Run("what needs attention that no line shows", func(t *testing.T) {
		subjects := append(linux("server-b"), freePct("server-b", "/srv", 12), free("server-b", "/srv", 12e9))
		subjects = replaced(subjects, judgedAt(free("server-b", "/", 9e9), evaluate.Warning))
		body := computerCard(t, stateOf(subjects...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), []string{"Load", "Free RAM", "Disk /srv free", "Backup", "disk.free_bytes /"})
		extra := linesOf(body)[4]
		equal(t, "values", extra.values(), []string{"9.0 GB (level-warning marked)"})
		if extra.key != "" {
			t.Errorf("extra line keyed %q, want none", extra.key)
		}
	})
	t.Run("at most two, then more", func(t *testing.T) {
		subjects := append(linux("server-b"),
			judgedAt(plain("server-b", "queue.a", 1), evaluate.Warning), judgedAt(plain("server-b", "queue.b", 1), evaluate.Warning),
			judgedAt(plain("server-b", "queue.c", 1), evaluate.Warning))
		body := computerCard(t, stateOf(subjects...), limits{}, "server-b")
		if names := lineNames(body); len(names) != 6 {
			t.Errorf("lines = %q, want the slots and two extra", names)
		}
		containsAll(t, "card", body, `<span class="card-more">1 more</span>`)
	})
	t.Run("a muted series needing attention", func(t *testing.T) {
		body := computerCard(t, stateOf(replaced(linux("server-b"), banded(plain("server-b", "memory.available_bytes", 2e9), 2.5e9, 3e9, 3.5e9, 1))...), limits{}, "server-b")
		equal(t, "lines", lineNames(body), slotNames)
	})
}

// spec: overview.md#computer-card — a line's values never wrap and a long name is cut short;
// name and values stand centred between the rules; the item's name stays whole beside long
// words in the card's head.
func TestComputerCardLayout(t *testing.T) {
	body := overviewUnder(t, stateOf(linux("server-b")...), limits{})
	containsAll(t, "stylesheet", body,
		"li.line { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center;",
		".line-name { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }",
		".line-values { white-space: nowrap;", "li.line.faded .value:not(.marked)",
		// A marked word or value keeps its mark's colour: the muted colour is for the unmarked.
		`.value.muted:not([class*="level-"]) { color: var(--muted); }`, `.line-why:not([class*="level-"]) { color: var(--muted); }`,
		`li.line.faded .line-why:not([class*="level-"])`,
		// The item's name stays whole; a long state's word gives way to it.
		".card-name { flex: none; max-width: 100%;", ".card-state { min-width: 0; overflow: hidden; text-overflow: ellipsis;",
		".card-flags { display: flex; align-items: center; gap: 0.4rem; min-width: 0; }",
		".card-failed { min-width: 0; overflow: hidden; text-overflow: ellipsis;")
	if found := regexp.MustCompile(`\.(line-why|value\.muted) \{[^}]*color`).FindString(body); found != "" {
		t.Errorf("%q gives every one the muted colour, over its mark's", found)
	}
}

// spec: overview.md#computer-card — at the page's full width, four computer cards stand in
// a row and three of a site's or a service's.
func TestComputerCardsFourInARow(t *testing.T) {
	body := overviewUnder(t, stateOf(linux("server-b")...), limits{})
	rem := func(pattern string) float64 {
		t.Helper()
		found := regexp.MustCompile(pattern).FindStringSubmatch(body)
		if found == nil {
			t.Fatalf("no %s in the stylesheet", pattern)
		}
		value, err := strconv.ParseFloat(found[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	page := rem(`body \{[^}]*max-width: ([0-9.]+)rem;`) - 2*rem(`body \{[^}]*padding: 0 ([0-9.]+)rem;`)
	gap := rem(`\.cards \{[^}]*gap: ([0-9.]+)rem;`)
	perRow := func(card float64) int { return int((page + gap) / (card + gap)) }
	if got := perRow(rem(`section\[data-section="overview\.computers"\] \.cards \{ grid-template-columns: repeat\(auto-fill, minmax\(min\(100%, ([0-9.]+)rem\), 1fr\)\);`)); got != 4 {
		t.Errorf("%d computer cards in a row, want 4", got)
	}
	if got := perRow(rem(`\.cards \{ display: grid; grid-template-columns: repeat\(auto-fill, minmax\(min\(100%, ([0-9.]+)rem\), 1fr\)\);`)); got != 3 {
		t.Errorf("%d other cards in a row, want 3", got)
	}
}

// spec: overview.md#computer-card — a site's card keeps its rows and their lines.
func TestASiteCardKeepsItsRows(t *testing.T) {
	body := card(t, overview(t, siteOf("blog-a", "site.requests_per_min", 4)), "blog-a")
	if !strings.Contains(body, `<li class="row main"`) || len(linesOf(body)) != 0 {
		t.Errorf("a site's card is not rows: %s", body)
	}
}

// overviewUnder is the whole overview under the given thresholds.
func overviewUnder(t *testing.T, current state.State, stored limits) string {
	t.Helper()
	rec := httptest.NewRecorder()
	read := func(context.Context) (state.State, error) { return current, nil }
	hub.Overview(read, historyOf(held{}), stored, layout()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func borg(node, metric, job string, value float64) state.Subject {
	return reading(node, metric, map[string]string{"backup": "borg", "job": job}, value)
}

func refOf(s state.Subject) storage.SeriesRef {
	return storage.SeriesRef{Node: s.Node, Metric: s.Metric, Labels: s.Labels}
}

// gathering is the computer the gathering rows assume: the linux server with the job
// job-a of borg in place of its backup, the job's newest archive and failure flag besides.
func gathering(node string) []state.Subject {
	return append(without(linux(node), "backup.success_age_seconds"),
		borg(node, "backup.success_age_seconds", "job-a", 3600),
		borg(node, "backup.latest_archive_age_seconds", "job-a", 3600),
		borg(node, "backup.failed", "job-a", 0))
}

var borgNames = names.Book{Metrics: map[string]names.Text{
	"backup.failed":                     {i18n.English: "Failed {backup} {job}"},
	"backup.latest_archive_age_seconds": {i18n.English: "Newest archive {backup} {job}"},
	"backup.archives":                   {i18n.English: "Archives {backup} {job}"},
}}

// spec: overview.md#computer-card — the backup slot gathers the backup families.
func TestComputerCardGathersTheBackup(t *testing.T) {
	show := func(t *testing.T, stored limits, subjects []state.Subject, with ...state.Subject) string {
		t.Helper()
		for _, s := range with {
			subjects = upserted(subjects, s)
		}
		current := stateOf(subjects...)
		current.Names = borgNames
		return computerCard(t, current, stored, "server-b")
	}
	backupOf := func(t *testing.T, body string) line {
		t.Helper()
		return lineCalled(t, body, "Backup")
	}
	saidOn := func(t *testing.T, stored limits, subjects []state.Subject, with ...state.Subject) whySaid {
		t.Helper()
		return backupOf(t, show(t, stored, subjects, with...)).whySaid()
	}
	is := func(t *testing.T, got, want whySaid) {
		t.Helper()
		if got != want {
			t.Errorf("why %+v, want %+v", got, want)
		}
	}
	archiveHint := "Newest archive borg job-a: usually 4.0 h"
	unusualArchive := func(job string) state.Subject {
		return banded(borg("server-b", "backup.latest_archive_age_seconds", job, 5*86400), 3600, 4*3600, 6*3600, 1)
	}
	failedAt := func(job string, level evaluate.Level) state.Subject {
		return judgedAt(borg("server-b", "backup.failed", job, 1), level)
	}
	successOf := func() state.Subject {
		return borg("server-b", "backup.success_age_seconds", "job-a", 3600)
	}

	t.Run("a gathered series unusual", func(t *testing.T) {
		body := show(t, limits{}, gathering("server-b"), unusualArchive("job-a"))
		equal(t, "lines", lineNames(body), slotNames)
		backup := backupOf(t, body)
		is(t, backup.whySaid(), whySaid{"unusual", "level-unusual", archiveHint, archiveHint})
		equal(t, "values", backup.values(), []string{"1.0 h"})
	})
	t.Run("many gathered, nothing else needing attention", func(t *testing.T) {
		body := show(t, limits{}, gathering("server-b"), unusualArchive("job-a"), unusualArchive("job-b"),
			banded(borg("server-b", "backup.archives", "job-a", 40), 9, 10, 11, 1))
		equal(t, "lines", lineNames(body), slotNames)
		if strings.Contains(body, "card-more") {
			t.Errorf("the gathered series are counted: %s", body)
		}
	})
	t.Run("gathered and other series needing attention", func(t *testing.T) {
		body := show(t, limits{}, gathering("server-b"), unusualArchive("job-a"), unusualArchive("job-b"),
			judgedAt(plain("server-b", "queue.a", 1), evaluate.Warning), judgedAt(plain("server-b", "queue.b", 1), evaluate.Warning),
			judgedAt(plain("server-b", "queue.c", 1), evaluate.Warning))
		equal(t, "lines", lineNames(body), append(slices.Clone(slotNames), "queue.a", "queue.b"))
		containsAll(t, "card", body, `<span class="card-more">1 more</span>`)
	})
	t.Run("the most urgent mark speaks", func(t *testing.T) {
		own := banded(successOf(), 60, 120, 180, 1)
		is(t, saidOn(t, limits{}, gathering("server-b"), own, failedAt("job-a", evaluate.Critical)),
			whySaid{"critical", "level-critical", "Failed borg job-a: critical", "Failed borg job-a: critical"})
	})
	t.Run("a readable threshold", func(t *testing.T) {
		stored := limits{heldKey(refOf(failedAt("job-a", evaluate.Warning))): {Direction: storage.Above, Warning: f(0)}}
		hint := "Failed borg job-a: threshold 0.00"
		is(t, saidOn(t, stored, gathering("server-b"), failedAt("job-a", evaluate.Warning)), whySaid{"warning", "level-warning", hint, hint})
	})
	t.Run("an unreadable threshold", func(t *testing.T) {
		hint := "Failed borg job-a: warning"
		is(t, saidOn(t, limits{}, gathering("server-b"), failedAt("job-a", evaluate.Warning)), whySaid{"warning", "level-warning", hint, hint})
	})
	t.Run("the first in the page order among equals", func(t *testing.T) {
		hint := "Failed borg job-a: critical"
		is(t, saidOn(t, limits{}, gathering("server-b"), failedAt("job-b", evaluate.Critical), failedAt("job-a", evaluate.Critical)),
			whySaid{"critical", "level-critical", hint, hint})
	})
	t.Run("the line's own more urgent", func(t *testing.T) {
		own := judgedAt(successOf(), evaluate.Critical)
		stored := limits{heldKey(refOf(own)): {Direction: storage.Above, Critical: f(1800)}}
		is(t, saidOn(t, stored, gathering("server-b"), own, failedAt("job-a", evaluate.Warning)), whySaid{text: "threshold 30.0 min"})
	})
	t.Run("the line's own first among equals", func(t *testing.T) {
		own := banded(successOf(), 60, 120, 180, 1)
		is(t, saidOn(t, limits{}, gathering("server-b"), own, unusualArchive("job-a")), whySaid{text: "usually 2.0 min"})
	})
	t.Run("a Time Machine destination on the line, the job's success at warning", func(t *testing.T) {
		subjects := append(gathering("server-b"), tmAge("server-b", "disk-a", 9*86400))
		body := show(t, limits{}, subjects, judgedAt(successOf(), evaluate.Warning))
		equal(t, "lines", lineNames(body), slotNames)
		hint := "backup.success_age_seconds backup=borg job=job-a: warning"
		is(t, backupOf(t, body).whySaid(), whySaid{"warning", "level-warning", hint, hint})
	})
	t.Run("watched and stale", func(t *testing.T) {
		body := show(t, limits{}, gathering("server-b"), watchedOnly(staled(borg("server-b", "backup.latest_archive_age_seconds", "job-a", 3600))))
		equal(t, "lines", lineNames(body), slotNames)
		hint := "Newest archive borg job-a: no fresh data"
		is(t, backupOf(t, body).whySaid(), whySaid{"no fresh data", "level-stale", hint, hint})
	})
	t.Run("stale and unwatched", func(t *testing.T) {
		is(t, saidOn(t, limits{}, gathering("server-b"), staled(borg("server-b", "backup.latest_archive_age_seconds", "job-a", 3600))), whySaid{})
	})
	t.Run("a stale level is no level", func(t *testing.T) {
		hint := "Failed borg job-a: no fresh data"
		is(t, saidOn(t, limits{}, gathering("server-b"), staled(failedAt("job-a", evaluate.Critical))), whySaid{"no fresh data", "level-stale", hint, hint})
	})
	t.Run("the line's own stale, a gathered series watched and stale", func(t *testing.T) {
		backup := backupOf(t, show(t, limits{}, gathering("server-b"), staled(successOf()),
			watchedOnly(staled(borg("server-b", "backup.latest_archive_age_seconds", "job-a", 3600)))))
		if !strings.Contains(backup.class, "faded") {
			t.Errorf("line %q, want faded", backup.class)
		}
		is(t, backup.whySaid(), whySaid{})
	})
	t.Run("the line's own stale", func(t *testing.T) {
		backup := backupOf(t, show(t, limits{}, gathering("server-b"), staled(successOf()), unusualArchive("job-a")))
		if !strings.Contains(backup.class, "faded") {
			t.Errorf("line %q, want faded", backup.class)
		}
		is(t, backup.whySaid(), whySaid{"unusual", "level-unusual", archiveHint, archiveHint})
	})
	t.Run("the card's state", func(t *testing.T) {
		current := stateOf(append(upserted(gathering("server-b"), failedAt("job-a", evaluate.Critical)), linux("box-x")...)...)
		current.Names = borgNames
		body := overviewUnder(t, current, limits{})
		if word := stateWord(t, body, "server-b"); word != "critical" {
			t.Errorf("pill %q", word)
		}
		equal(t, "sections", flatten(sections(body)), []string{"Computers · 2 · 1 critical", "server-b", "box-x"})
	})
	t.Run("a backup slot reading a dash gathers none", func(t *testing.T) {
		body := show(t, limits{}, without(gathering("server-b"), "backup.success_age_seconds"), failedAt("job-a", evaluate.Critical))
		equal(t, "lines", lineNames(body), append(slices.Clone(slotNames), "Failed borg job-a"))
		if !backupOf(t, body).empty() {
			t.Errorf("the backup slot is not a dash")
		}
	})
	t.Run("not a backup family", func(t *testing.T) {
		body := show(t, limits{}, gathering("server-b"), banded(plain("server-b", "gdrive.free_pct", 40), 50, 60, 70, 1))
		equal(t, "lines", lineNames(body), append(slices.Clone(slotNames), "gdrive.free_pct"))
	})
}
