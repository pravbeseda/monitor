package notify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/notify"
)

func failedUnits(notes ...string) evaluate.Message {
	m := entering()
	m.Metric, m.Labels, m.Name = "systemd.failed_units", map[string]string{}, nil
	m.Readings = map[string]float64{"systemd.failed_units": 2}
	m.Notes = notes
	return m
}

// spec: evaluation.md#messages — the value, a colon and the notes parted by commas; with
// none, the value alone; the punctuation is the same in Russian.
func TestNotesFollowTheValue(t *testing.T) {
	for _, c := range []struct {
		name   string
		locale i18n.Locale
		m      evaluate.Message
		suffix string
	}{
		{"two notes", i18n.English, failedUnits("a.service (exit-code)", "b.service (timeout)"), " — 2: a.service (exit-code), b.service (timeout)"},
		{"no notes", i18n.English, failedUnits(), " — 2"},
		{"in Russian", i18n.Russian, failedUnits("a.service (exit-code)"), " — 2: a.service (exit-code)"},
		{"a note holding a comma or a quote", i18n.English, failedUnits(`a, b`, `say "c"`), ` — 2: "a, b", "say \"c\""`},
	} {
		if got := notify.Render(i18n.For(c.locale), c.m); !strings.HasSuffix(got, c.suffix) {
			t.Errorf("%s: %q, want it to end in %q", c.name, got, c.suffix)
		}
	}
}

// spec: evaluation.md#messages — a node's silence has no value and so no notes.
func TestTheSilenceMessageCarriesNoNotes(t *testing.T) {
	m := evaluate.Message{Node: "server-b", Metric: evaluate.SilenceMetric, From: evaluate.OK, To: evaluate.Critical, Notes: []string{"stray"}}
	if got := notify.Render(i18n.For(i18n.English), m); strings.Contains(got, "stray") {
		t.Fatalf("the silence message carries a note: %q", got)
	}
}

// spec: evaluation.md#messages — a text longer than Telegram takes is sent as several
// messages in order, parted between lines, a line too long alone parted inside it.
func TestTheTelegramChannelPartsALongText(t *testing.T) {
	short := make([]evaluate.Message, 200)
	for i := range short {
		short[i] = failedUnits("a.service (exit-code)")
	}
	// Astral characters count twice against Telegram's limit, so one such line is past it.
	long := []evaluate.Message{failedUnits(strings.Repeat("𝔞", 2100)), failedUnits("b.service (exit-code)")}

	for _, c := range []struct {
		name    string
		entries []evaluate.Message
		// whole says every part holds whole lines.
		whole bool
	}{
		{"many short lines", short, true},
		{"a line too long alone", long, false},
	} {
		var sent []string
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var body struct{ Text string }
			_ = json.Unmarshal(raw, &body)
			sent = append(sent, body.Text)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		channel := notify.Telegram{Token: "t", ChatID: "1", Locale: i18n.English, API: api.URL}
		err := channel.Digest(context.Background(), at, c.entries, 0)
		api.Close()
		if err != nil {
			t.Fatalf("%s: Digest: %v", c.name, err)
		}

		text := notify.RenderDigest(i18n.For(i18n.English), c.entries, 0)
		if len(sent) < 2 {
			t.Fatalf("%s: went out as %d message(s)", c.name, len(sent))
		}
		if got := strings.Join(sent, ""); strings.ReplaceAll(got, "\n", "") != strings.ReplaceAll(text, "\n", "") {
			t.Fatalf("%s: the parts do not add up to the text, in order", c.name)
		}
		lines := strings.Split(text, "\n")
		for i, part := range sent {
			if units := len(utf16.Encode([]rune(part))); units == 0 || units > 4096 {
				t.Errorf("%s: part %d is %d UTF-16 units", c.name, i, units)
			}
			if !c.whole {
				continue
			}
			for _, line := range strings.Split(part, "\n") {
				if !slices.Contains(lines, line) {
					t.Errorf("%s: part %d holds %q, not a whole line", c.name, i, line)
				}
			}
		}
	}
}
