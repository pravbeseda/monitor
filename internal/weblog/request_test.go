package weblog_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/weblog"
)

const browser = "Mozilla/5.0 (X11; Linux x86_64)"

// line writes a combined-format line with the given request, status and user agent.
func line(request string, status string, agent string, after string) string {
	return `203.0.113.7 - - [03/Oct/2026:10:00:00 +0300] "` + request + `" ` + status +
		` 5120 "-" "` + agent + `"` + after
}

// spec: site-traffic.md#the-log — the time, the status and the request time are read.
func TestParseReadsCombinedWithRequestTime(t *testing.T) {
	got, ok := weblog.Parse(line("GET /about/ HTTP/1.1", "200", browser, " rt=0.120"))
	if !ok {
		t.Fatal("a combined line was not recognised")
	}
	want := time.Date(2026, 10, 3, 7, 0, 0, 0, time.UTC)
	if !got.Time.Equal(want) {
		t.Fatalf("time %v, want %v", got.Time, want)
	}
	if got.Status != 200 {
		t.Fatalf("status %d, want 200", got.Status)
	}
	if !got.HasDuration || got.Duration != 0.120 {
		t.Fatalf("duration %v (%t), want 0.120", got.Duration, got.HasDuration)
	}
}

// spec: site-traffic.md#the-log — whatever follows combined is ignored, as the stock main
// format's forwarded address is; only an rt token is read.
func TestParseIgnoresWhatFollowsCombined(t *testing.T) {
	cases := map[string]struct {
		after    string
		duration float64
		has      bool
	}{
		"nothing after":           {"", 0, false},
		"main's forwarded-for":    {` "-"`, 0, false},
		"rt among other fields":   {` rt=0.250 uct="0.001" urt="0.249"`, 0.250, true},
		"rt with no value":        {` rt=-`, 0, false},
		"rt that is not a number": {` rt=abc`, 0, false},
		"a negative rt":           {` rt=-1`, 0, false},
		"rt glued to another key": {` xrt=0.5`, 0, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := weblog.Parse(line("GET / HTTP/1.1", "200", browser, c.after))
			if !ok {
				t.Fatal("not recognised")
			}
			if got.HasDuration != c.has || got.Duration != c.duration {
				t.Fatalf("duration %v (%t), want %v (%t)", got.Duration, got.HasDuration, c.duration, c.has)
			}
		})
	}
}

// spec: site-traffic.md#reading — a request field that is not a request is still a request
// with its status, and never a page view.
func TestParseKeepsAMalformedRequestField(t *testing.T) {
	for _, request := range []string{"-", `\x16\x03\x01\x00\xF5\x01`} {
		got, ok := weblog.Parse(line(request, "400", "-", ""))
		if !ok {
			t.Fatalf("%q: not recognised", request)
		}
		if got.Status != 400 || got.PageView() {
			t.Fatalf("%q: status %d, page view %t", request, got.Status, got.PageView())
		}
		counted := weblog.NewInterval()
		counted.Add(got)
		if f := counted.Report(clock, time.Minute); f[1].Metric != "site.client_error_pct" || f[1].Value != 100 {
			t.Fatalf("%q: figures %v, want it counted as a 4xx request", request, f)
		}
	}
}

// spec: site-traffic.md#reading — a line in no recognised format is not a request.
func TestParseRefusesWhatIsNotCombined(t *testing.T) {
	for _, text := range []string{
		"",
		"garbage",
		`{"time":"2026-10-03T10:00:00Z","status":200}`,
		`203.0.113.7 - - [not a time] "GET / HTTP/1.1" 200 5 "-" "x"`,
		`203.0.113.7 - - [03/Oct/2026:10:00:00 +0300] "GET / HTTP/1.1" abc 5 "-" "x"`,
		`203.0.113.7 - - [03/Oct/2026:10:00:00 +0300] "GET / HTTP/1.1" 200 5 "-"`,
		`203.0.113.7 - - [03/Oct/2026:10:00:00 +0300] "GET / HTTP/1.1`,
	} {
		if _, ok := weblog.Parse(text); ok {
			t.Errorf("%q was recognised", text)
		}
	}
}

// spec: site-traffic.md#the-log — a quote escaped inside a quoted field does not end it.
func TestParseReadsEscapedQuotes(t *testing.T) {
	got, ok := weblog.Parse(line("GET / HTTP/1.1", "200", `Mozilla/5.0 \"quoted\"`, " rt=0.1"))
	if !ok || got.UserAgent != `Mozilla/5.0 \"quoted\"` || !got.HasDuration {
		t.Fatalf("got %+v, recognised %t", got, ok)
	}
}

// spec: site-traffic.md#page-views
func TestPageView(t *testing.T) {
	cases := []struct {
		request, status, agent string
		want                   bool
	}{
		{"GET / HTTP/1.1", "200", browser, true},
		{"GET /blog/post HTTP/1.1", "200", browser, true},
		{"GET /index.html HTTP/1.1", "200", browser, true},
		{"GET /INDEX.HTM HTTP/1.1", "200", browser, true},
		{"GET /page.php?id=3 HTTP/1.1", "200", browser, true},
		{"GET /index.php/blog/post HTTP/1.1", "200", browser, true},
		{"GET /about/ HTTP/1.1", "304", browser, true},
		{"GET /video HTTP/1.1", "206", browser, true},
		{"GET /.well-known/x HTTP/1.1", "200", browser, true},
		{"GET /style.css HTTP/1.1", "200", browser, false},
		{"GET /LOGO.PNG HTTP/1.1", "200", browser, false},
		{"GET /api/v1.2 HTTP/1.1", "200", browser, false},
		{"GET /style.css?v=1.html HTTP/1.1", "200", browser, false},
		{"HEAD / HTTP/1.1", "200", browser, false},
		{"POST / HTTP/1.1", "200", browser, false},
		{"GET / HTTP/1.1", "301", browser, false},
		{"GET / HTTP/1.1", "404", browser, false},
		{"GET / HTTP/1.1", "500", browser, false},
		{"GET / HTTP/1.1", "200", "Mozilla/5.0 (compatible; Googlebot/2.1)", false},
		{"GET / HTTP/1.1", "200", "curl/8.0", false},
		{"GET / HTTP/1.1", "200", "python-requests/2.31", false},
		{"GET / HTTP/1.1", "200", "Mozilla/5.0 (Linux; Android 13; CUBOT KingKong)", false},
		{"GET / HTTP/1.1", "200", "-", false},
		{"GET / HTTP/1.1", "200", "", false},
	}
	for _, c := range cases {
		got, ok := weblog.Parse(line(c.request, c.status, c.agent, ""))
		if !ok {
			t.Fatalf("%s %s %q: not recognised", c.request, c.status, c.agent)
		}
		if got.PageView() != c.want {
			t.Errorf("%s %s %q: page view %t, want %t", c.request, c.status, c.agent, got.PageView(), c.want)
		}
	}
}
