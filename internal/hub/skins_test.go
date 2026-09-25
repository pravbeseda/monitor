package hub_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var tabPattern = regexp.MustCompile(`<a class="tab" href="([^"]*)" data-skin="([^"]*)"( aria-current="page")?>([^<]*)</a>`)

// tab is one tab as a page draws it.
type tab struct {
	href, skin, label string
	open              bool
}

func tabsOf(t *testing.T, body string) []tab {
	t.Helper()
	var out []tab
	for _, found := range tabPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, tab{href: strings.ReplaceAll(found[1], "&amp;", "&"), skin: found[2], open: found[3] != "", label: found[4]})
	}
	return out
}

func openTab(tabs []tab) string {
	for _, one := range tabs {
		if one.open {
			return one.skin
		}
	}
	return ""
}

// spec: web.md#skins — `/` redirects to the skin the last tab click remembered, mission
// control when there is none, keeping the query, and no cache keeps the answer.
func TestTheRootRedirectsToTheRememberedSkin(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, cookie, want string
	}{
		{"no tab ever clicked", http.MethodGet, "/", "", "/board"},
		{"the table remembered", http.MethodGet, "/", "debug", "/debug"},
		{"the timeline remembered", http.MethodGet, "/", "timeline", "/timeline"},
		{"a skin this hub no longer has", http.MethodGet, "/", "city", "/board"},
		{"the language kept", http.MethodGet, "/?lang=ru", "debug", "/debug?lang=ru"},
		{"HEAD", http.MethodHead, "/", "debug", "/debug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "skin", Value: tc.cookie})
			}
			rec := httptest.NewRecorder()
			routesWith(t, stored{}, func() time.Time { return lastSeen }).ServeHTTP(rec, req)

			if rec.Code != http.StatusFound || rec.Header().Get("Location") != tc.want {
				t.Fatalf("%s %s = %d to %q, want 302 to %q", tc.method, tc.target, rec.Code, rec.Header().Get("Location"), tc.want)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := rec.Header().Get("Vary"); !strings.Contains(got, "Cookie") {
				t.Errorf("Vary = %q, want it to name the cookie", got)
			}
			if cookie := rec.Header().Get("Set-Cookie"); cookie != "" {
				t.Errorf("the hub wrote a cookie: %s", cookie)
			}
		})
	}
}

// spec: web.md#skins — every page carries the tabs, one per skin in their order, each
// linking to its address in the page's language; a skin marks its own, a drill-down none.
func TestEveryPageCarriesTheTabs(t *testing.T) {
	at := func() time.Time { return lastSeen }
	for _, tc := range []struct {
		target, open string
	}{
		{"/board", "board"},
		{"/debug", "debug"},
		{"/timeline", "timeline"},
		{oneVolume, ""},
		{dataAddress, ""},
	} {
		t.Run(tc.target, func(t *testing.T) {
			store := holding(dataSeries)
			rec := httptest.NewRecorder()
			routesWith(t, store, at).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
			tabs := tabsOf(t, rec.Body.String())

			var skins []string
			for _, one := range tabs {
				skins = append(skins, one.skin+" "+one.href+" "+one.label)
			}
			want := []string{"board /board Mission control", "timeline /timeline Timeline", "debug /debug All series"}
			if strings.Join(skins, ", ") != strings.Join(want, ", ") {
				t.Errorf("tabs = %v, want %v", skins, want)
			}
			if got := openTab(tabs); got != tc.open {
				t.Errorf("open tab = %q, want %q", got, tc.open)
			}
		})
	}
}

// spec: web.md#skins — the tabs speak the page's language and keep it on their links.
func TestTheTabsKeepTheLanguage(t *testing.T) {
	rec := httptest.NewRecorder()
	routesWith(t, stored{}, func() time.Time { return lastSeen }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug?lang=ru", nil))

	var got []string
	for _, one := range tabsOf(t, rec.Body.String()) {
		got = append(got, one.href+" "+one.label)
	}
	want := []string{"/board?lang=ru Центр управления", "/timeline?lang=ru Лента", "/debug?lang=ru Все серии"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("tabs = %v, want %v", got, want)
	}
}

// spec: web.md#skins — a skin's page writes no cookie: only a tab click, in the browser,
// makes a choice, so a refresh or a reload changes nothing.
func TestASkinPageChoosesNothing(t *testing.T) {
	for _, target := range []string{"/board", "/timeline", "/debug"} {
		rec := httptest.NewRecorder()
		routesWith(t, stored{}, func() time.Time { return lastSeen }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", target, rec.Code)
		}
		if cookie := rec.Header().Get("Set-Cookie"); cookie != "" {
			t.Errorf("GET %s wrote a cookie: %s", target, cookie)
		}
	}
}

// spec: web.md#skins — every page carries the script that, on a click on a tab, stores that
// tab's skin for a year and for the whole site.
func TestEveryPageStoresTheTabClicked(t *testing.T) {
	for _, target := range []string{"/board", "/timeline", "/debug", oneVolume, dataAddress, "/history?metric=disk.free_pct&nonsense=1"} {
		body := getState(t, holding(dataSeries), target, at).Body.String()
		for _, want := range []string{
			`event.target.closest("a.tab[data-skin]")`,
			`"skin=" + tab.getAttribute("data-skin") + "; path=/; max-age=31536000; samesite=lax"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s carries no %s", target, want)
			}
		}
	}
}
