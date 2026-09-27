package hub

import (
	"net/http"

	"github.com/pravbeseda/monitor/public"
)

// Public serves the committed public pages as they are: no shell, no language, no cookie.
func Public() http.Handler {
	files := http.StripPrefix("/public", http.FileServerFS(public.Files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Embedded files carry no modification time; no-cache states outright that a
		// browser must ask again, so an upgraded page shows on the next load.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
