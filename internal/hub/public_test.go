package hub_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func servePublic(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept-Language", "ru")
	req.AddCookie(&http.Cookie{Name: "skin", Value: "timeline"})
	rec := httptest.NewRecorder()
	routes(t).ServeHTTP(rec, req)
	return rec
}

func committed(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "public", name))
	if err != nil {
		t.Fatalf("read public/%s: %v", name, err)
	}
	return string(body)
}

// spec: web.md#public — each page is the committed file, whoever asks, in whatever language
// and with whatever query, asked for again on every load, and with none of the shell.
func TestThePublicPagesAreTheCommittedFiles(t *testing.T) {
	for _, tc := range []struct {
		target, file, contentType string
	}{
		{"/public/", "index.html", "text/html; charset=utf-8"},
		{"/public/privacy.html", "privacy.html", "text/html; charset=utf-8"},
		{"/public/terms.html", "terms.html", "text/html; charset=utf-8"},
		{"/public/style.css", "style.css", "text/css; charset=utf-8"},
		{"/public/?lang=ru", "index.html", "text/html; charset=utf-8"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			rec := servePublic(t, http.MethodGet, tc.target)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.target, rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tc.contentType)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache", got)
			}
			if rec.Body.String() != committed(t, tc.file) {
				t.Errorf("GET %s is not public/%s as committed", tc.target, tc.file)
			}
			if cookie := rec.Header().Get("Set-Cookie"); cookie != "" {
				t.Errorf("Set-Cookie = %q, want none", cookie)
			}
		})
	}
}

// spec: web.md#public — the prefix, the homepage's file name and any path not in clean
// form redirect to the clean address, which leaves /public/ only as an ordinary hub path.
func TestAPublicPathRedirectsToItsCleanForm(t *testing.T) {
	for _, tc := range []struct{ target, want string }{
		{"/public", "/public/"},
		{"/public/index.html", "/public/"},
		{"/public//privacy.html", "/public/privacy.html"},
		{"/public/./privacy.html", "/public/privacy.html"},
		{"/public/privacy.html/", "/public/privacy.html"},
		{"/public/../api/v1/state", "/api/v1/state"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.Path = tc.target // unnormalised, as a client sending it raw would
			rec := httptest.NewRecorder()
			routes(t).ServeHTTP(rec, req)

			if rec.Code < 300 || rec.Code > 399 {
				t.Fatalf("GET %s = %d, want a redirect", tc.target, rec.Code)
			}
			location, err := req.URL.Parse(rec.Header().Get("Location"))
			if err != nil || location.Path != tc.want {
				t.Errorf("GET %s redirects to %q, want %s", tc.target, rec.Header().Get("Location"), tc.want)
			}
		})
	}
}

// spec: web.md#public — nothing but the committed files, and never a listing.
func TestAnythingElseUnderPublicIsNotFound(t *testing.T) {
	for _, target := range []string{"/public/missing.html", "/public/public.go", "/public/sub/"} {
		if rec := servePublic(t, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
}

// spec: web.md#public — HEAD answers as GET does without the body; other methods are refused.
func TestThePublicPagesTakeOnlyGetAndHead(t *testing.T) {
	for _, target := range []string{"/public/", "/public/privacy.html", "/public/terms.html", "/public/style.css"} {
		get, head := servePublic(t, http.MethodGet, target), servePublic(t, http.MethodHead, target)
		if head.Code != get.Code || head.Body.Len() != 0 {
			t.Errorf("HEAD %s = %d with %d bytes, want %d and no body", target, head.Code, head.Body.Len(), get.Code)
		}
		for _, name := range []string{"Content-Type", "Cache-Control"} {
			if head.Header().Get(name) != get.Header().Get(name) {
				t.Errorf("HEAD %s %s = %q, GET has %q", target, name, head.Header().Get(name), get.Header().Get(name))
			}
		}
	}
	for _, target := range []string{"/public", "/public/privacy.html"} {
		rec := servePublic(t, http.MethodPost, target)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("POST %s = %d allowing %q, want 405 allowing GET, HEAD", target, rec.Code, rec.Header().Get("Allow"))
		}
	}
}

// spec: web.md#public — a page carries the hub pages' own icon inside it, so a reader
// without the proxy's credential is never asked for one by a request for it.
func TestThePublicPagesCarryTheirIcon(t *testing.T) {
	shell, err := os.ReadFile(filepath.Join("templates", "shell.html"))
	if err != nil {
		t.Fatalf("read the shell: %v", err)
	}
	var icon string
	for line := range strings.Lines(string(shell)) {
		if strings.Contains(line, `rel="icon"`) {
			icon = strings.TrimSpace(line)
		}
	}
	if icon == "" {
		t.Fatal("the shell carries no icon to compare with")
	}
	for _, target := range []string{"/public/", "/public/privacy.html", "/public/terms.html"} {
		if body := servePublic(t, http.MethodGet, target).Body.String(); !strings.Contains(body, icon) {
			t.Errorf("GET %s does not carry the hub pages' icon", target)
		}
	}
}
