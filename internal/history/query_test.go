package history_test

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/history"
)

func values(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", raw, err)
	}
	return v
}

// spec: history.md#refusals — every malformed query is refused, never answered with a guess.
func TestParseQueryRefuses(t *testing.T) {
	for _, raw := range []string{
		"node=server-b",
		"metric=",
		"metric=Disk%20Free",
		"metric=disk.free_pct&node=",
		"metric=disk.free_pct&label.=%2F",
		"metric=disk.free_pct&label.",
		"metric=disk.free_pct&metric=disk.free_bytes",
		"metric=disk.free_pct&window=1h&window=2h",
		"metric=disk.free_pct&windwo=7d",
		"metric=disk.free_pct&at=2026-08-01T00:00:00Z",
		"metric=disk.free_pct&window=0h",
		"metric=disk.free_pct&window=-1d",
		"metric=disk.free_pct&window=soon",
		"metric=disk.free_pct&window=2w",
		"metric=disk.free_pct&window=1h30m",
		"metric=disk.free_pct&window=90s",
		"metric=disk.free_pct&window=24H",
		"metric=disk.free_pct&window=366d",
	} {
		if _, err := history.ParseQuery(values(t, raw)); err == nil {
			t.Errorf("ParseQuery(%q) = no error, want a refusal", raw)
		} else if refusal := new(history.Refusal); !errors.As(err, refusal) {
			t.Errorf("ParseQuery(%q) refused with %v, want a Refusal carrying a message key", raw, err)
		}
	}
}

// spec: history.md#window — the window's grammar and its inclusive bounds.
func TestParseQueryWindows(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"metric=disk.free_pct":             24 * time.Hour,
		"metric=disk.free_pct&window=30m":  30 * time.Minute,
		"metric=disk.free_pct&window=7d":   7 * 24 * time.Hour,
		"metric=disk.free_pct&window=1m":   time.Minute,
		"metric=disk.free_pct&window=365d": 365 * 24 * time.Hour,
	} {
		q, err := history.ParseQuery(values(t, raw))
		if err != nil {
			t.Errorf("ParseQuery(%q): %v", raw, err)
			continue
		}
		if q.Window != want {
			t.Errorf("ParseQuery(%q).Window = %v, want %v", raw, q.Window, want)
		}
	}
}

// spec: history.md#selection — node and label filters narrow the selection.
func TestParseQueryKeepsFilters(t *testing.T) {
	q, err := history.ParseQuery(values(t, "metric=disk.free_pct&node=server-b&label.mount=%2F&label.fs=ext4&lang=ru"), "lang")
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if q.Metric != "disk.free_pct" || q.Node != "server-b" {
		t.Fatalf("query = %+v, want disk.free_pct on server-b", q)
	}
	if q.Labels["mount"] != "/" || q.Labels["fs"] != "ext4" || len(q.Labels) != 2 {
		t.Fatalf("labels = %v, want mount=/ and fs=ext4", q.Labels)
	}
}

// spec: history.md#selection — a label filter may ask for a label whose value is empty,
// spelled with or without the `=`.
func TestParseQueryKeepsAnEmptyLabelFilter(t *testing.T) {
	for _, raw := range []string{"metric=queue.depth&label.queue=", "metric=queue.depth&label.queue"} {
		for name, parse := range map[string]func(url.Values, ...string) (history.Query, error){
			"ParseQuery": history.ParseQuery, "ParseSelection": history.ParseSelection,
		} {
			q, err := parse(values(t, raw))
			if err != nil {
				t.Errorf("%s(%q): %v", name, raw, err)
				continue
			}
			if value, named := q.Labels["queue"]; !named || value != "" || len(q.Labels) != 1 {
				t.Errorf("%s(%q).Labels = %v, want queue empty alone", name, raw, q.Labels)
			}
		}
	}
}

// spec: history.md#refusals — a count large enough to overflow a duration is out of range,
// not a legal window that wrapped around.
func TestParseQueryRefusesAWindowThatWouldOverflow(t *testing.T) {
	for _, raw := range []string{"metric=disk.free_pct&window=213505d", "metric=disk.free_pct&window=999999999h"} {
		if got, err := history.ParseQuery(values(t, raw)); err == nil {
			t.Errorf("ParseQuery(%q) = window %v, want a refusal", raw, got.Window)
		}
	}
}

// spec: history.md#refusals — the listing endpoint takes no window.
func TestParseSelectionRefusesAWindow(t *testing.T) {
	if _, err := history.ParseSelection(values(t, "metric=disk.free_pct&window=7d")); err == nil {
		t.Error("ParseSelection accepted a window, want it refused rather than ignored")
	}
	if _, err := history.ParseSelection(values(t, "metric=disk.free_pct&node=server-b")); err != nil {
		t.Errorf("ParseSelection of a plain selection: %v", err)
	}
}

// A parameter carrying no value at all is refused, not indexed into: ParseQuery is exported
// and takes values a caller can build by hand.
func TestParseQueryRefusesAValuelessParameter(t *testing.T) {
	if _, err := history.ParseQuery(url.Values{"metric": {"disk.free_pct"}, "node": {}}); err == nil {
		t.Error("ParseQuery accepted a parameter with no value")
	}
}
