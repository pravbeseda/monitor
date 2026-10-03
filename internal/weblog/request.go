// Package weblog reads a web server's access log in the combined format: a line becomes a
// Request, and each figure the site-traffic sensor reports is an aggregator over requests
// (docs/specs/site-traffic.md).
package weblog

import (
	"math"
	"path"
	"strconv"
	"strings"
	"time"
)

// Request is one line of the log, as much of it as any figure needs.
type Request struct {
	Time      time.Time
	Method    string
	Target    string
	Status    int
	UserAgent string
	// Duration is the request time in seconds, nginx's $request_time, when the line
	// carries an rt field.
	Duration    float64
	HasDuration bool
}

const timeLayout = "02/Jan/2006:15:04:05 -0700"

// Parse reads one line in the combined format, ignoring whatever follows it but an rt
// field. It reports false for a line in no format it knows.
func Parse(line string) (Request, bool) {
	var r Request
	rest := line
	// The address, the identity and the user: three fields that matter to no figure.
	for range 3 {
		var ok bool
		if _, rest, ok = strings.Cut(rest, " "); !ok {
			return Request{}, false
		}
	}
	stamp, rest, ok := bracketed(rest)
	if !ok {
		return Request{}, false
	}
	if r.Time, ok = parseTime(stamp); !ok {
		return Request{}, false
	}

	request, rest, ok := quoted(strings.TrimPrefix(rest, " "))
	if !ok {
		return Request{}, false
	}
	if parts := strings.Split(request, " "); len(parts) == 3 {
		r.Method, r.Target = parts[0], parts[1]
	}

	status, rest, _ := strings.Cut(strings.TrimPrefix(rest, " "), " ")
	if r.Status, ok = parseStatus(status); !ok {
		return Request{}, false
	}
	// The bytes sent: a number or "-", which no figure reads.
	if _, rest, ok = strings.Cut(rest, " "); !ok {
		return Request{}, false
	}
	if _, rest, ok = quoted(rest); !ok {
		return Request{}, false
	}
	if r.UserAgent, rest, ok = quoted(strings.TrimPrefix(rest, " ")); !ok {
		return Request{}, false
	}

	for _, field := range strings.Fields(rest) {
		value, found := strings.CutPrefix(field, "rt=")
		if !found {
			continue
		}
		seconds, err := strconv.ParseFloat(value, 64)
		if err == nil && seconds >= 0 && !math.IsInf(seconds, 0) {
			r.Duration, r.HasDuration = seconds, true
		}
	}
	return r, true
}

// botMarks are what a crawler, a script or a preview fetcher writes into its user agent. A
// substring list errs both ways, and is a product default rather than a setting.
var botMarks = []string{
	"bot", "crawl", "spider", "slurp", "curl", "wget", "python", "go-http-client",
	"java/", "headless", "scrapy", "httpclient", "facebookexternalhit",
}

// PageView reports whether a person most likely asked for a page.
func (r Request) PageView() bool {
	answered := r.Status >= 200 && r.Status < 300 || r.Status == 304
	if r.Method != "GET" || !answered {
		return false
	}
	if !pageTarget(r.Target) {
		return false
	}
	agent := strings.ToLower(r.UserAgent)
	if agent == "" || agent == "-" {
		return false
	}
	for _, mark := range botMarks {
		if strings.Contains(agent, mark) {
			return false
		}
	}
	return true
}

// pageTarget reports whether the path's last segment names a page: no extension, or one a
// page is served under. A dot that opens the segment starts no extension.
func pageTarget(target string) bool {
	target, _, _ = strings.Cut(target, "?")
	segment := path.Base("/" + target)
	dot := strings.LastIndexByte(segment, '.')
	if dot <= 0 {
		return true
	}
	switch strings.ToLower(segment[dot+1:]) {
	case "html", "htm", "php":
		return true
	}
	return false
}

func parseTime(stamp string) (time.Time, bool) {
	t, err := time.Parse(timeLayout, stamp)
	return t, err == nil
}

func parseStatus(text string) (int, bool) {
	if len(text) != 3 {
		return 0, false
	}
	status, err := strconv.Atoi(text)
	return status, err == nil && status >= 100
}

// bracketed reads "[...]" at the start of s and returns what it held and what follows.
func bracketed(s string) (inside, rest string, ok bool) {
	if !strings.HasPrefix(s, "[") {
		return "", "", false
	}
	end := strings.IndexByte(s, ']')
	if end < 0 {
		return "", "", false
	}
	return s[1:end], s[end+1:], true
}

// quoted reads a double-quoted field at the start of s, a backslash escaping the character
// after it, and returns its text as written and what follows the closing quote.
func quoted(s string) (inside, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return s[1:i], s[i+1:], true
		}
	}
	return "", "", false
}
