package hub_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
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
	dotPattern    = regexp.MustCompile(`<span class="dot([^"]*)" style="left: ([0-9.]+)%"></span>`)
	bandPattern   = regexp.MustCompile(`<span class="band" style="left: ([0-9.]+)%; width: ([0-9.]+)%"></span><span class="norm" style="left: ([0-9.]+)%"></span>`)
	tickPattern   = regexp.MustCompile(`<span class="tick (level-[a-z]+)" style="left: ([0-9.]+)%"></span>`)
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

func (l line) dot(t *testing.T) (class, left string) {
	t.Helper()
	found := dotPattern.FindStringSubmatch(l.body)
	if found == nil {
		t.Fatalf("line %q has no dot: %s", l.name, l.body)
	}
	return strings.TrimSpace(found[1]), found[2]
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
		if !backup.empty() || !strings.Contains(backup.body, "—") || strings.Contains(backup.body, `class="dot`) {
			t.Errorf("the backup slot is not a dash over an empty scale: %s", backup.body)
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
	t.Run("a muted series without its scale", func(t *testing.T) {
		body := computerCard(t, stateOf(without(linux("server-b"), "load.avg_15m")...), limits{}, "server-b")
		load := lineCalled(t, body, "Load")
		if !load.empty() || strings.Contains(load.body, "0.61") {
			t.Errorf("load without its 15-minute average: %s", load.body)
		}
	})
	t.Run("a muted series without its scale needing attention", func(t *testing.T) {
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
	t.Run("load: three averages, the 15-minute one on the scale", func(t *testing.T) {
		load := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Load")
		equal(t, "values", load.values(), []string{"0.52 (muted)", "0.61 (muted)", "0.67"})
		if _, left := load.dot(t); left != "90.9" {
			t.Errorf("the dot at %s%%, want 90.9%% of a scale ending at 0.737", left)
		}
	})
	t.Run("no 1-minute average", func(t *testing.T) {
		load := lineCalled(t, computerCard(t, stateOf(without(linux("server-b"), "load.avg_1m")...), limits{}, "server-b"), "Load")
		equal(t, "values", load.values(), []string{"0.61 (muted)", "0.67"})
	})
	t.Run("memory: bytes muted, then the percent on the scale", func(t *testing.T) {
		memory := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Free RAM")
		equal(t, "values", memory.values(), []string{"2.0 GB (muted)", "38.5%"})
		if _, left := memory.dot(t); left != "38.5" {
			t.Errorf("the dot at %s%%, want 38.5%%", left)
		}
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
			t.Errorf("hover = %q, want the series on the scale", load.title)
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
func TestComputerCardScale(t *testing.T) {
	three := func(loads ...state.Subject) state.State {
		var subjects []state.Subject
		for i, node := range []string{"server-b", "server-c", "server-d"} {
			subjects = append(subjects, replaced(linux(node), loads[i])...)
		}
		return stateOf(subjects...)
	}
	dots := func(t *testing.T, current state.State, stored limits) []string {
		t.Helper()
		body := overviewUnder(t, current, stored)
		var out []string
		for _, node := range []string{"server-b", "server-c", "server-d"} {
			_, left := lineCalled(t, card(t, body, node), "Load").dot(t)
			out = append(out, left)
		}
		return out
	}
	loads := []state.Subject{plain("server-b", "load.avg_15m", 0.5), plain("server-c", "load.avg_15m", 2), plain("server-d", "load.avg_15m", 4)}

	t.Run("shared by the section", func(t *testing.T) {
		equal(t, "dots", dots(t, three(loads...), limits{}), []string{"11.4", "45.5", "90.9"})
	})
	t.Run("reaching a threshold", func(t *testing.T) {
		ref := storage.SeriesRef{Node: "server-b", Metric: "load.avg_15m", Labels: map[string]string{}}
		stored := limits{heldKey(ref): {Direction: storage.Above, Warning: f(5)}}
		equal(t, "dots", dots(t, three(loads...), stored), []string{"9.1", "36.4", "72.7"})
	})
	t.Run("reaching a band", func(t *testing.T) {
		equal(t, "dots", dots(t, three(banded(loads[0], 0.1, 0.5, 6, 0), loads[1], loads[2]), limits{}), []string{"7.6", "30.3", "60.6"})
	})
	t.Run("nothing above zero", func(t *testing.T) {
		zero := []state.Subject{plain("server-b", "load.avg_15m", 0), plain("server-c", "load.avg_15m", 0), plain("server-d", "load.avg_15m", 0)}
		equal(t, "dots", dots(t, three(zero...), limits{}), []string{"0.0", "0.0", "0.0"})
	})
	t.Run("a percentage runs to 100", func(t *testing.T) {
		if _, left := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{}, "server-b"), "Disk / free").dot(t); left != "40.0" {
			t.Errorf("the dot at %s%%, want 40%%", left)
		}
	})
	t.Run("stale series set no scale", func(t *testing.T) {
		var asleep []state.Subject
		for _, s := range replaced(linux("laptop-a"), jobAge("laptop-a", "a", 40*86400)) {
			asleep = append(asleep, staled(s))
		}
		current := stateOf(append(asleep, linux("server-b")...)...)
		body := overviewUnder(t, current, limits{})
		if _, left := lineCalled(t, card(t, body, "laptop-a"), "Backup").dot(t); left != "100.0" {
			t.Errorf("the stale backup at %s%%, want the scale's end", left)
		}
		if _, left := lineCalled(t, card(t, body, "server-b"), "Backup").dot(t); left != "90.9" {
			t.Errorf("the fresh backup at %s%%, want 90.9%%", left)
		}
	})
	t.Run("stale series set the scale when none is fresh", func(t *testing.T) {
		var asleep []state.Subject
		for _, s := range replaced(linux("laptop-a"), jobAge("laptop-a", "a", 40*86400)) {
			asleep = append(asleep, staled(s))
		}
		if _, left := lineCalled(t, computerCard(t, stateOf(asleep...), limits{}, "laptop-a"), "Backup").dot(t); left != "90.9" {
			t.Errorf("the stale backup at %s%%, want 90.9%% of a scale it sets", left)
		}
	})
}

// spec: overview.md#computer-card
func TestComputerCardBandAndTicks(t *testing.T) {
	memory := func(t *testing.T, s state.Subject) line {
		t.Helper()
		return lineCalled(t, computerCard(t, stateOf(replaced(linux("server-b"), s)...), limits{}, "server-b"), "Free RAM")
	}
	t.Run("a band and its norm", func(t *testing.T) {
		found := bandPattern.FindStringSubmatch(memory(t, banded(plain("server-b", "memory.available_pct", 38.5), 43, 46, 52, 0)).body)
		if found == nil || found[1] != "43.0" || found[2] != "9.0" || found[3] != "46.0" {
			t.Errorf("band = %q, want 43 to 52, norm at 46", found)
		}
	})
	t.Run("no anomaly, no band", func(t *testing.T) {
		if strings.Contains(memory(t, plain("server-b", "memory.available_pct", 38.5)).body, `class="band"`) {
			t.Errorf("a band without an anomaly")
		}
	})
	t.Run("past the band, unmarked", func(t *testing.T) {
		if class, _ := memory(t, banded(plain("server-b", "memory.available_pct", 60), 43, 46, 52, 0)).dot(t); class != "" {
			t.Errorf("dot marked %q, want unmarked", class)
		}
	})
	t.Run("a band of one value", func(t *testing.T) {
		found := bandPattern.FindStringSubmatch(memory(t, banded(plain("server-b", "memory.available_pct", 0), 0, 0, 0, 0)).body)
		if found == nil || found[1] != "0.0" || found[2] != "0.0" {
			t.Errorf("band = %q, want a sliver at zero", found)
		}
	})
	t.Run("below zero", func(t *testing.T) {
		if _, left := memory(t, plain("server-b", "memory.available_pct", -3)).dot(t); left != "0.0" {
			t.Errorf("the dot at %s%%, want 0", left)
		}
	})
	t.Run("unusual already", func(t *testing.T) {
		if class, _ := memory(t, banded(plain("server-b", "memory.available_pct", 60), 43, 46, 52, 1)).dot(t); class != "level-unusual" {
			t.Errorf("dot marked %q, want unusual", class)
		}
	})

	ref := storage.SeriesRef{Node: "server-b", Metric: "disk.free_pct", Labels: map[string]string{"mount": "/", "fs": "ext4", "removable": "false"}}
	ticks := func(t *testing.T, th storage.Threshold) []string {
		t.Helper()
		disk := lineCalled(t, computerCard(t, stateOf(linux("server-b")...), limits{heldKey(ref): th}, "server-b"), "Disk / free")
		var out []string
		for _, found := range tickPattern.FindAllStringSubmatch(disk.body, -1) {
			out = append(out, found[1]+" "+found[2])
		}
		return out
	}
	t.Run("the scale in words for a screen reader", func(t *testing.T) {
		subjects := replaced(linux("server-b"), banded(freePct("server-b", "/", 40), 38, 40, 42, 0))
		disk := lineCalled(t, computerCard(t, stateOf(subjects...), limits{heldKey(ref): below(f(10), f(5))}, "server-b"), "Disk / free")
		containsAll(t, "line", disk.body, `<span class="row-mark-word">usually 38.0%–42.0%, warning at 10.0%, critical at 5.0%</span>`)
	})
	t.Run("ticks at the threshold's values", func(t *testing.T) {
		equal(t, "ticks", ticks(t, below(f(10), f(5))), []string{"level-warning 10.0", "level-critical 5.0"})
	})
	t.Run("a value off the scale has no tick", func(t *testing.T) {
		equal(t, "ticks", ticks(t, below(f(-5), f(-10))), nil)
	})
	t.Run("an unreadable threshold has no tick", func(t *testing.T) {
		equal(t, "ticks", ticks(t, storage.Threshold{Direction: "sideways", Warning: f(10)}), nil)
	})
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
		if class, _ := memory.dot(t); class != "level-unusual" {
			t.Errorf("dot %q", class)
		}
		equal(t, "values", memory.values(), []string{"2.0 GB (muted)", "38.5% (level-unusual marked)"})
		if memory.why() != "usually 46.0%" {
			t.Errorf("why = %q", memory.why())
		}
	})
	t.Run("a level under a readable threshold", func(t *testing.T) {
		disk := lineCalled(t, show(t, limits{heldKey(diskRef): below(f(10), f(5))}, judgedAt(freePct("server-b", "/", 8), evaluate.Warning)), "Disk / free")
		if class, _ := disk.dot(t); class != "level-warning" {
			t.Errorf("dot %q", class)
		}
		equal(t, "values", disk.values(), []string{"40.0 GB (muted)", "8.0% (level-warning marked)"})
		if disk.why() != "threshold 10.0%" {
			t.Errorf("why = %q", disk.why())
		}
	})
	t.Run("the one on the scale first among equals", func(t *testing.T) {
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
		if class, _ := memory.dot(t); class != "" {
			t.Errorf("dot %q, want unmarked", class)
		}
		if memory.why() != "usually 3.0 GB" {
			t.Errorf("why = %q", memory.why())
		}
	})
	t.Run("stale", func(t *testing.T) {
		load := lineCalled(t, show(t, limits{}, staled(plain("server-b", "load.avg_15m", 0.67))), "Load")
		if class, _ := load.dot(t); !strings.Contains(load.class, "faded") || class != "level-stale" || load.why() != "" {
			t.Errorf("a stale line: class %q, dot %q, why %q", load.class, class, load.why())
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
		if _, left := extra.dot(t); left != "90.9" || extra.key != "" {
			t.Errorf("extra line %q at %s%%, want its own scale", extra.key, left)
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

// spec: overview.md#computer-card — a line's values never wrap, and take a column of one
// width on every card, so the scales are as wide.
func TestComputerCardLayout(t *testing.T) {
	body := overviewUnder(t, stateOf(linux("server-b")...), limits{})
	if !regexp.MustCompile(`li\.line \{ display: grid; grid-template-columns: [0-9.]+rem minmax\([^)]*\) [0-9.]+rem;`).MatchString(body) {
		t.Errorf("the values do not take a column of one width")
	}
	containsAll(t, "stylesheet", body, ".line-values { white-space: nowrap;", "li.line.faded .value:not(.marked)",
		// A marked word or value keeps its mark's colour: the muted colour is for the unmarked.
		`.value.muted:not([class*="level-"]) { color: var(--muted); }`, `.line-why:not([class*="level-"]) { color: var(--muted); }`,
		`li.line.faded .line-why:not([class*="level-"])`)
	if found := regexp.MustCompile(`\.(line-why|value\.muted) \{[^}]*color`).FindString(body); found != "" {
		t.Errorf("%q gives every one the muted colour, over its mark's", found)
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
		if class, _ := backup.dot(t); class != "" {
			t.Errorf("dot %q, want the text colour", class)
		}
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
		scale := banded(successOf(), 60, 120, 180, 1)
		is(t, saidOn(t, limits{}, gathering("server-b"), scale, failedAt("job-a", evaluate.Critical)),
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
	t.Run("the one on the scale more urgent", func(t *testing.T) {
		scale := judgedAt(successOf(), evaluate.Critical)
		stored := limits{heldKey(refOf(scale)): {Direction: storage.Above, Critical: f(1800)}}
		is(t, saidOn(t, stored, gathering("server-b"), scale, failedAt("job-a", evaluate.Warning)), whySaid{text: "threshold 30.0 min"})
	})
	t.Run("the one on the scale first among equals", func(t *testing.T) {
		scale := banded(successOf(), 60, 120, 180, 1)
		is(t, saidOn(t, limits{}, gathering("server-b"), scale, unusualArchive("job-a")), whySaid{text: "usually 2.0 min"})
	})
	t.Run("a Time Machine destination on the scale, the job's success at warning", func(t *testing.T) {
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
	t.Run("the one on the scale stale, a gathered series watched and stale", func(t *testing.T) {
		backup := backupOf(t, show(t, limits{}, gathering("server-b"), staled(successOf()),
			watchedOnly(staled(borg("server-b", "backup.latest_archive_age_seconds", "job-a", 3600)))))
		if !strings.Contains(backup.class, "faded") {
			t.Errorf("line %q, want faded", backup.class)
		}
		is(t, backup.whySaid(), whySaid{})
	})
	t.Run("the one on the scale stale", func(t *testing.T) {
		backup := backupOf(t, show(t, limits{}, gathering("server-b"), staled(successOf()), unusualArchive("job-a")))
		if class, _ := backup.dot(t); !strings.Contains(backup.class, "faded") || class != "level-stale" {
			t.Errorf("line %q, dot %q, want faded and hollow", backup.class, class)
		}
		is(t, backup.whySaid(), whySaid{"unusual", "level-unusual", archiveHint, archiveHint})
	})
	t.Run("no tick for a gathered threshold", func(t *testing.T) {
		archive := borg("server-b", "backup.latest_archive_age_seconds", "job-a", 3600)
		backup := backupOf(t, show(t, limits{heldKey(refOf(archive)): {Direction: storage.Above, Warning: f(1800)}}, gathering("server-b")))
		if ticks := tickPattern.FindAllString(backup.body, -1); len(ticks) != 0 {
			t.Errorf("ticks %q", ticks)
		}
	})
	t.Run("the scale set by the ones on it", func(t *testing.T) {
		backup := backupOf(t, show(t, limits{}, gathering("server-b"), borg("server-b", "backup.latest_archive_age_seconds", "job-a", 40*86400)))
		if _, left := backup.dot(t); left != "90.9" {
			t.Errorf("dot at %s%%, want the scale set by the success age alone", left)
		}
		equal(t, "values", backup.values(), []string{"1.0 h"})
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
