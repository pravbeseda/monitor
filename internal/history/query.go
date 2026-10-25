package history

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/api"
)

// Refusal is a query the endpoint will not answer. Key names the catalogue entry a page
// shows; Message is the English one the API returns (ADR 0008).
type Refusal struct {
	Key     string
	Message string
}

func (r Refusal) Error() string { return r.Message }

func refuse(key, format string, args ...any) error {
	return Refusal{Key: "history.error." + key, Message: fmt.Sprintf(format, args...)}
}

// LabelPrefix marks a label filter in a query. It is exported because a link that reaches
// one series has to be built with the same prefix this parser reads.
const LabelPrefix = "label."

const (
	// DefaultWindow is what a query naming none asks for; minWindow and maxWindow bound
	// the ones it may name.
	DefaultWindow = 24 * time.Hour
	minWindow     = time.Minute
	maxWindow     = 365 * 24 * time.Hour
)

var windowPattern = regexp.MustCompile(`^([0-9]+)([mhd])$`)

var units = map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}

var selectors = []string{"metric", "node", "window"}

// listSelectors are the parameters that select series without asking for points: the
// listing endpoint takes no window, and silently ignoring one would answer a question
// nobody asked.
var listSelectors = []string{"metric", "node"}

// ParseQuery reads one history query, refusing anything it cannot answer exactly as asked:
// an unknown parameter would otherwise let ?windwo=7d answer with a window nobody
// requested, and a future ?at= would answer a time-travel question with live data.
// Parameters in `allow` are accepted and ignored — the page's ?lang=.
func ParseQuery(values url.Values, allow ...string) (Query, error) {
	return parse(values, selectors, allow)
}

// ParseSelection reads a query that names series without asking for their points.
func ParseSelection(values url.Values, allow ...string) (Query, error) {
	return parse(values, listSelectors, allow)
}

func parse(values url.Values, selectors []string, allow []string) (Query, error) {
	query := Query{Labels: map[string]string{}, Window: DefaultWindow}
	for key, given := range values {
		if len(given) == 0 {
			return Query{}, refuse("empty_parameter", "parameter %q carries no value", key)
		}
		if len(given) > 1 {
			return Query{}, refuse("repeated_parameter", "parameter %q is given more than once", key)
		}
		switch {
		case slices.Contains(selectors, key) || slices.Contains(allow, key):
		case strings.HasPrefix(key, LabelPrefix):
			name := strings.TrimPrefix(key, LabelPrefix)
			if name == "" || given[0] == "" {
				return Query{}, refuse("empty_parameter", "label filter %q names no label or no value", key)
			}
			query.Labels[name] = given[0]
		default:
			return Query{}, refuse("unknown_parameter", "unknown parameter %q", key)
		}
	}

	query.Metric = values.Get("metric")
	if !api.MetricID.MatchString(query.Metric) {
		return Query{}, refuse("metric", "a history query needs one metric matching [a-z0-9_.]+")
	}
	if node, given := values["node"]; given {
		if node[0] == "" {
			return Query{}, refuse("empty_parameter", "parameter \"node\" carries no value")
		}
		query.Node = node[0]
	}
	if window, given := values["window"]; given {
		parsed, err := parseWindow(window[0])
		if err != nil {
			return Query{}, err
		}
		query.Window = parsed
	}
	return query, nil
}

// ParseWindow reads a window as a query spells it (docs/specs/history.md#window).
func ParseWindow(raw string) (time.Duration, error) { return parseWindow(raw) }

func parseWindow(raw string) (time.Duration, error) {
	bad := refuse("window", "window must be a whole number of minutes, hours or days between 1m and 365d")
	match := windowPattern.FindStringSubmatch(raw)
	if match == nil {
		return 0, bad
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, bad
	}
	// The count is bounded in its own unit first: multiplying an arbitrary integer into a
	// Duration overflows its nanoseconds and wraps a decade back into a legal window.
	unit := units[match[2]]
	if count > int(maxWindow/unit) {
		return 0, bad
	}
	window := time.Duration(count) * unit
	if window < minWindow {
		return 0, bad
	}
	return window, nil
}
