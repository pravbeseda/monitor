package hub_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/state"
	"github.com/pravbeseda/monitor/internal/storage"
)

var leadPattern = regexp.MustCompile(`(?s)<div class="lead([^"]*)"(?: title="([^"]*)")?>(.*?)</div>`)

// lead is a site card's lead as a reader sees it: its class, what hover names, its body,
// and whether a line of the day follows it.
type lead struct {
	class, title, body string
	line               bool
}

func leadOf(t *testing.T, card string) (lead, bool) {
	t.Helper()
	found := leadPattern.FindStringSubmatchIndex(card)
	if found == nil {
		return lead{}, false
	}
	out := lead{class: card[found[2]:found[3]], body: card[found[6]:found[7]]}
	if found[4] >= 0 {
		out.title = card[found[4]:found[5]]
	}
	out.line = strings.HasPrefix(strings.TrimSpace(card[found[1]:]), `<svg class="spark"`)
	return out, true
}

func (l lead) values() []string { return line{body: l.body}.values() }

func (l lead) why() whySaid { return line{body: l.body}.whySaid() }

// blogA is the site the site card's rows assume.
func blogA() []state.Subject {
	return []state.Subject{
		siteOf("blog-a", "site.requests_per_min", 12.6), siteOf("blog-a", "site.pageviews_24h", 88),
		siteOf("blog-a", "site.server_error_pct", 0.1), siteOf("blog-a", "site.response_p95_seconds", 0.4),
	}
}

func siteRef(metric string) storage.SeriesRef {
	return storage.SeriesRef{Node: "sites", Metric: metric, Labels: map[string]string{"site": "blog-a"}}
}

// aDay are two points of the last day, enough for a line.
var aDay = []storage.Point{{TS: lastSeen.Add(-2 * time.Hour), Value: 10}, {TS: lastSeen.Add(-time.Hour), Value: 12}}

func siteCard(t *testing.T, subjects []state.Subject, source held, stored limits) string {
	t.Helper()
	return showCard(t, stateOf(subjects...), source, stored, "blog-a")
}

var siteSlotNames = []string{"Views/day", "Server errors", "Response p95"}

// spec: overview.md#site-card
func TestSiteCard(t *testing.T) {
	traffic := held{heldKey(siteRef("site.requests_per_min")): aDay}

	t.Run("any site card", func(t *testing.T) {
		body := siteCard(t, blogA(), traffic, limits{})
		containsAll(t, "card", body, `<span class="card-state">`)
		if !strings.HasPrefix(body, `<a class="card state-reporting"`) {
			t.Errorf("no stripe of the state's colour: %.80s", body)
		}
		if strings.Contains(body, "card-reason") || strings.Contains(body, `class="row main"`) {
			t.Errorf("a site's card carries rows or a reason: %s", body)
		}
	})
	t.Run("the site above", func(t *testing.T) {
		body := siteCard(t, blogA(), traffic, limits{})
		head, found := leadOf(t, body)
		if !found || !strings.Contains(head.body, "Requests/min") || !head.line {
			t.Fatalf("lead %+v, want Requests/min with its line of the day: %s", head, body)
		}
		equal(t, "lead", head.values(), []string{"12.60"})
		if strings.Contains(body, `class="threshold`) {
			t.Errorf("a threshold drawn on the line of the day: %s", body)
		}
		equal(t, "lines", lineNames(body), siteSlotNames)
		var values []string
		for _, one := range linesOf(body) {
			values = append(values, strings.Join(one.values(), " "))
		}
		equal(t, "values", values, []string{"88.00", "0.1%", "0.4 s"})
		if strings.Contains(body, "card-more") || strings.Contains(body, "last report") {
			t.Errorf("more or a last report on a reporting site: %s", body)
		}
	})
	t.Run("no views", func(t *testing.T) {
		body := siteCard(t, without(blogA(), "site.pageviews_24h"), traffic, limits{})
		equal(t, "lines", lineNames(body), siteSlotNames)
		if !linesOf(body)[0].empty() {
			t.Errorf("the views line is not a dash")
		}
	})
	t.Run("no response time", func(t *testing.T) {
		body := siteCard(t, without(blogA(), "site.response_p95_seconds"), traffic, limits{})
		equal(t, "lines", lineNames(body), siteSlotNames)
		if !linesOf(body)[2].empty() {
			t.Errorf("the response line is not a dash")
		}
	})
	t.Run("no requests", func(t *testing.T) {
		body := siteCard(t, without(blogA(), "site.requests_per_min"), held{}, limits{})
		head, found := leadOf(t, body)
		if !found || !strings.Contains(head.body, "—") || head.line {
			t.Errorf("lead %+v, want a dash with no line", head)
		}
		equal(t, "lines", lineNames(body), siteSlotNames)
		var values []string
		for _, one := range linesOf(body) {
			values = append(values, strings.Join(one.values(), " "))
		}
		equal(t, "values", values, []string{"88.00", "0.1%", "0.4 s"})
	})
	t.Run("no point in the last day", func(t *testing.T) {
		head, _ := leadOf(t, siteCard(t, blogA(), held{}, limits{}))
		if head.line {
			t.Errorf("a line of the day with no point in it")
		}
		equal(t, "lead", head.values(), []string{"12.60"})
	})
	t.Run("none of the four", func(t *testing.T) {
		body := siteCard(t, []state.Subject{siteOf("blog-a", "uptime.x", 9)}, held{}, limits{})
		if _, found := leadOf(t, body); found {
			t.Errorf("a lead on a site with none of its series: %s", body)
		}
		equal(t, "lines", lineNames(body), []string{"uptime.x"})
	})
	t.Run("the lead unusual", func(t *testing.T) {
		head, _ := leadOf(t, siteCard(t, replaced(blogA(), banded(siteOf("blog-a", "site.requests_per_min", 34.2), 8, 9.5, 11, 1)), traffic, limits{}))
		equal(t, "lead", head.values(), []string{"34.20 (level-unusual marked)"})
		if head.why().text != "usually 9.50" {
			t.Errorf("why %+v", head.why())
		}
	})
	t.Run("the lead at warning under a readable threshold", func(t *testing.T) {
		stored := limits{heldKey(siteRef("site.requests_per_min")): {Direction: storage.Above, Warning: f(30)}}
		body := siteCard(t, replaced(blogA(), judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Warning)), traffic, stored)
		head, _ := leadOf(t, body)
		equal(t, "lead", head.values(), []string{"34.20 (level-warning marked)"})
		if head.why().text != "threshold 30.00" || !head.line || strings.Contains(body, `class="threshold`) {
			t.Errorf("why %+v, line %v: %s", head.why(), head.line, body)
		}
	})
	t.Run("the lead stale", func(t *testing.T) {
		head, _ := leadOf(t, siteCard(t, replaced(blogA(), staled(siteOf("blog-a", "site.requests_per_min", 12.6))), traffic, limits{}))
		if !strings.Contains(head.class, "faded") {
			t.Errorf("lead class %q, want faded", head.class)
		}
		// The line of the day fades with the lead.
		containsAll(t, "stylesheet", overview(t, blogA()...), ".lead.faded + svg.spark")
	})
	t.Run("the lead at critical and stale", func(t *testing.T) {
		head, _ := leadOf(t, siteCard(t, replaced(blogA(), staled(judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Critical))), traffic, limits{}))
		equal(t, "lead", head.values(), []string{"34.20 (level-stale)"})
		if !strings.Contains(head.class, "faded") || head.why() != (whySaid{}) {
			t.Errorf("lead class %q, why %+v", head.class, head.why())
		}
	})
	t.Run("the lead at warning, its threshold unreadable", func(t *testing.T) {
		head, _ := leadOf(t, siteCard(t, replaced(blogA(), judgedAt(siteOf("blog-a", "site.requests_per_min", 34.2), evaluate.Warning)), traffic, limits{}))
		if head.why().text != "warning" {
			t.Errorf("why %+v", head.why())
		}
	})
	t.Run("the lead unusual is no extra line", func(t *testing.T) {
		body := siteCard(t, replaced(blogA(), banded(siteOf("blog-a", "site.requests_per_min", 34.2), 8, 9.5, 11, 1)), traffic, limits{})
		equal(t, "lines", lineNames(body), siteSlotNames)
	})
	t.Run("only the lead", func(t *testing.T) {
		body := siteCard(t, []state.Subject{siteOf("blog-a", "site.requests_per_min", 12.6)}, traffic, limits{})
		equal(t, "lines", lineNames(body), siteSlotNames)
		for _, one := range linesOf(body) {
			if !one.empty() {
				t.Errorf("line %q is not a dash", one.name)
			}
		}
	})
	t.Run("the lead read by a screen reader", func(t *testing.T) {
		body := siteCard(t, blogA(), traffic, limits{})
		head, _ := leadOf(t, body)
		if strings.Contains(body, `class="lead" aria-hidden`) || !strings.Contains(head.body, "Requests/min") || !strings.Contains(head.body, ">12.60<") {
			t.Errorf("the lead's name and value are not text a reader reads: %s", head.body)
		}
		if !strings.Contains(body, `<svg class="spark" viewBox="0 0 `) || !regexp.MustCompile(`</div>\s*<svg class="spark"[^>]*aria-hidden="true"`).MatchString(body) {
			t.Errorf("the line of the day is read out: %s", body)
		}
	})
	t.Run("views unusual", func(t *testing.T) {
		body := siteCard(t, replaced(blogA(), banded(siteOf("blog-a", "site.pageviews_24h", 423), 15, 19, 25, 1)), traffic, limits{})
		views := lineCalled(t, body, "Views/day")
		equal(t, "values", views.values(), []string{"423.00 (level-unusual marked)"})
		if views.why() != "usually 19.00" {
			t.Errorf("why = %q", views.why())
		}
	})
	t.Run("errors at critical", func(t *testing.T) {
		body := siteCard(t, replaced(blogA(), judgedAt(siteOf("blog-a", "site.server_error_pct", 9), evaluate.Critical)), traffic, limits{})
		equal(t, "values", lineCalled(t, body, "Server errors").values(), []string{"9.0% (level-critical marked)"})
		if !strings.HasPrefix(body, `<a class="card state-critical"`) {
			t.Errorf("the card is not red: %.80s", body)
		}
	})
	t.Run("a series no slot shows", func(t *testing.T) {
		body := siteCard(t, append(blogA(), judgedAt(siteOf("blog-a", "site.client_error_pct", 4), evaluate.Warning)), traffic, limits{})
		equal(t, "lines", lineNames(body), append(append([]string(nil), siteSlotNames...), "site.client_error_pct"))
	})
	t.Run("at most two extra lines, then more", func(t *testing.T) {
		body := siteCard(t, append(blogA(), judgedAt(siteOf("blog-a", "queue.a", 1), evaluate.Warning),
			judgedAt(siteOf("blog-a", "queue.b", 1), evaluate.Warning), judgedAt(siteOf("blog-a", "queue.c", 1), evaluate.Warning)), traffic, limits{})
		equal(t, "lines", lineNames(body), append(append([]string(nil), siteSlotNames...), "queue.a", "queue.b"))
		containsAll(t, "card", body, `<span class="card-more">1 more</span>`)
	})
	t.Run("extra lines in the order attention lists them", func(t *testing.T) {
		body := siteCard(t, append(blogA(), ranked(siteOf("blog-a", "uptime.x", 9), 1), judgedAt(siteOf("blog-a", "site.client_error_pct", 4), evaluate.Warning)), traffic, limits{})
		equal(t, "lines", lineNames(body), append(append([]string(nil), siteSlotNames...), "site.client_error_pct", "uptime.x"))
	})
	t.Run("a silent site", func(t *testing.T) {
		body := siteCard(t, append(blogA(), silenceOf("sites", evaluate.Critical)), traffic, limits{})
		if found := seenPattern.FindStringSubmatch(body); found == nil || strings.TrimSpace(found[1]) != "late" {
			t.Errorf("no brighter last report on a silent site: %s", body)
		}
	})
	t.Run("a site with no fresh data", func(t *testing.T) {
		var asleep []state.Subject
		for _, s := range blogA() {
			asleep = append(asleep, staled(s))
		}
		if found := seenPattern.FindStringSubmatch(siteCard(t, asleep, traffic, limits{})); found == nil || strings.TrimSpace(found[1]) != "late" {
			t.Errorf("no brighter last report on a site with no fresh data")
		}
	})
	t.Run("names on hover", func(t *testing.T) {
		body := siteCard(t, blogA(), traffic, limits{})
		if head, _ := leadOf(t, body); head.title != "site.requests_per_min" {
			t.Errorf("lead hover %q", head.title)
		}
		if views := lineCalled(t, body, "Views/day"); views.title != "site.pageviews_24h" {
			t.Errorf("views hover %q", views.title)
		}
	})
}
