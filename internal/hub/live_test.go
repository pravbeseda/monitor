package hub_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pravbeseda/monitor/internal/storage"
	"github.com/pravbeseda/monitor/internal/version"
)

// liveMarker tells the shell's script which rendering an answer is, and carries the notice
// it shows when the page is not being refreshed.
func liveMarker(notice, lang string) string {
	return `<meta name="monitor-live" content="` + notice + `" data-rendering="` + version.Current + " " + lang + `">`
}

func getIn(t *testing.T, store storage.Storage, target, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept-Language", acceptLanguage)
	rec := httptest.NewRecorder()
	routesWith(t, store, at).ServeHTTP(rec, req)
	return rec
}

// spec: web.md#live — every hub page keeps itself current and names its rendering, refusals
// and failures rendered as pages included, with the notice in the reader's language.
func TestEveryPageKeepsItselfCurrent(t *testing.T) {
	pages := map[string]struct {
		store  storage.Storage
		target string
	}{
		"index":             {stored{states: []storage.NodeState{laptop}}, "/"},
		"chart":             {served{series: []storage.SeriesPoints{volume()}}, oneVolume},
		"refusal":           {served{}, "/history?metric=disk.free_pct&nonsense=1"},
		"failure as a page": {served{err: errors.New("database is locked")}, oneVolume},
	}
	notices := map[string]string{
		"en": "Not refreshed: the hub is not answering",
		"ru": "Не обновляется: хаб не отвечает",
	}

	for name, page := range pages {
		for lang, notice := range notices {
			t.Run(name+"/"+lang, func(t *testing.T) {
				body := getIn(t, page.store, page.target, lang).Body.String()
				if want := liveMarker(notice, lang); !strings.Contains(body, want) {
					t.Errorf("page does not carry %q", want)
				}
				for _, want := range []string{
					// Only a body from the same rendering replaces a body.
					`var marker = 'meta[name="monitor-live"]';`,
					// A background tab fetches nothing.
					`document.visibilityState`,
					"var period = 30000;",
					// Whether an answer counts is settled before what it looks like.
					"answer.status === 401",
					"answer.status >= 500",
					"answer.redirected",
				} {
					if !strings.Contains(body, want) {
						t.Errorf("page does not carry %q", want)
					}
				}
			})
		}
	}
}

// spec: web.md#live — a page whose address names its language refreshes in that language,
// whatever the browser asks for, so a refresh never reloads it into another one.
func TestTheMarkerFollowsTheQuerysLanguage(t *testing.T) {
	body := getIn(t, stored{states: []storage.NodeState{laptop}}, "/?lang=ru", "en").Body.String()

	if want := liveMarker("Не обновляется: хаб не отвечает", "ru"); !strings.Contains(body, want) {
		t.Errorf("page does not carry %q", want)
	}
}
