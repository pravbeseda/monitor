package hub

import (
	"net/http"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/version"

	// The zone database travels in the binary, so a host carrying none of its own still
	// reads a page in the reader's zone (ADR 0026).
	_ "time/tzdata"
)

// zoneCookie is what the page shell writes. The hub only ever reads it: a hub that wrote a
// canonical name back over the browser's own would be answered by the shell rewriting it,
// and the two would trade the page forever (ADR 0026).
const zoneCookie = "tz"

// skinCookie is what a click on a tab stores. Like the zone, it is written by the page and
// only read here (ADR 0037).
const skinCookie = "skin"

// skin is one way of showing the whole hub, in the order its tab stands.
type skin struct {
	name, path, labelKey string
}

var skins = []skin{
	{name: "board", path: "/board", labelKey: "board.title"},
	{name: "timeline", path: "/timeline", labelKey: "timeline.title"},
	{name: "debug", path: "/debug", labelKey: "page.all_series"},
}

// Root sends the reader to the skin their last tab click chose, or to mission control
// (spec: web.md#skins). The answer depends on the cookie, so no cache may keep it.
func Root() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := skins[0].path
		if cookie, err := r.Cookie(skinCookie); err == nil {
			for _, one := range skins {
				if one.name == cookie.Value {
					target = one.path
				}
			}
		}
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Vary", "Cookie")
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// maxZoneName bounds the name before it reaches the zone database.
const maxZoneName = 64

// zoneOf is the zone the reader's browser reported, or UTC (spec: web.md#zone).
func zoneOf(r *http.Request) *time.Location {
	cookie, err := r.Cookie(zoneCookie)
	if err != nil || !addressesARegion(cookie.Value) {
		return time.UTC
	}
	zone, err := time.LoadLocation(cookie.Value)
	if err != nil {
		return time.UTC
	}
	return zone
}

// addressesARegion holds the line in front of time.LoadLocation, which opens files for the
// name it is given. A name has to address a region: the flat names a zone database also
// answers to include "Local", the hub host's own clock, and a page written in the server's
// zone is wrong in the one way that looks right. A browser already in UTC reports the flat
// name "UTC" and is refused here, which costs it nothing — a refusal renders in UTC.
func addressesARegion(name string) bool {
	if len(name) > maxZoneName || strings.HasPrefix(name, "/") || !strings.Contains(name, "/") {
		return false
	}
	for _, char := range name {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case char == '_', char == '+', char == '-', char == '/':
		default:
			return false
		}
	}
	return true
}

// shellHeaders are what every HTML page answers with. The rendering follows the reader's
// zone and language, so no cache may keep it: a stored pre-cookie copy would pin a reader
// to UTC, and the shell would not correct it — by then the cookie is already right
// (spec: web.md#shell).
func shellHeaders(w http.ResponseWriter) {
	head := w.Header()
	head.Set("Content-Type", "text/html; charset=utf-8")
	head.Set("Cache-Control", "no-store")
	head.Set("Vary", "Cookie, Accept-Language")
}

// shell is what every page carries around its content (templates/shell.html): the head,
// and the tabs at the top of the body. Rendering names what the head was built from, so
// the refresh script reloads a page whole rather than pair an old head with a new body;
// StalledNotice is what it shows when the page is not being refreshed (spec: web.md#live).
type shell struct {
	Locale        i18n.Locale
	Title         string
	Rendering     string
	StalledNotice string
	// Live says the page keeps itself current. A page whose content is a form does not:
	// nothing on it changes on its own, and a refresh would replace what the reader is
	// typing, or the refusal they are reading (spec: web.md#live, thresholds.md#form).
	Live bool
	Tabs []tabView
}

// tabView is one skin's tab. Skin is what a click on it stores in the browser.
type tabView struct {
	Skin  string
	Label string
	URL   string
	Open  bool
}

// shellOf is the shell of a page that keeps itself current. open names the skin the page
// is, or is empty on a page that is none of them.
func shellOf(printer *i18n.Printer, titleKey, open, lang string) shell {
	out := still(printer, titleKey, open, lang)
	out.Live = true
	return out
}

// still is the shell of a page that is not refreshed.
func still(printer *i18n.Printer, titleKey, open, lang string) shell {
	out := shell{
		Locale:        printer.Locale(),
		Title:         printer.T(titleKey),
		Rendering:     version.Current + " " + string(printer.Locale()),
		StalledNotice: printer.T("page.stalled"),
	}
	for _, one := range skins {
		out.Tabs = append(out.Tabs, tabView{
			Skin:  one.name,
			Label: printer.T(one.labelKey),
			URL:   pageLink(one.path, lang),
			Open:  one.name == open,
		})
	}
	return out
}
