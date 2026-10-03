package ingest_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/ingest"
)

// Two web servers and the sites node, every name synthetic (ADR 0007).
const sitesConfig = `
nodes:
  laptop-a:
    class: laptop
    token_env: MONITOR_TOKEN_LAPTOP_A
  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
  server-d:
    class: server
    token_env: MONITOR_TOKEN_SERVER_D
  cloud:
    class: service
    sensors:
      gdrive: { enabled: true }
  sites:
    class: sites
    sites:
      blog-a: { host: server-b, log: /var/log/nginx/blog-a.access.log }
      shop-c: { host: server-d, log: /var/log/nginx/shop-c.access.log }
`

var serverB = strings.Repeat("synthetic-b-", 3)

func newSitesHandler(t *testing.T) (http.Handler, *spy) {
	t.Helper()
	t.Setenv("MONITOR_TOKEN_SERVER_B", serverB)
	t.Setenv("MONITOR_TOKEN_SERVER_D", strings.Repeat("synthetic-d-", 3))
	for _, name := range []string{"MONITOR_GDRIVE_CLIENT_ID", "MONITOR_GDRIVE_CLIENT_SECRET", "MONITOR_GDRIVE_REFRESH_TOKEN"} {
		t.Setenv(name, "synthetic")
	}
	store := &spy{}
	return ingest.NewHandler(loadConfig(t, sitesConfig), store, func() time.Time { return received }), store
}

// fromServerB posts server-b's report carrying the given measurements.
func fromServerB(t *testing.T, h http.Handler, measurements ...string) int {
	t.Helper()
	body := fmt.Sprintf(`{"node": "server-b", "agent_version": "0.1.0", "config_version": "",
		"ts": "2026-08-28T10:00:00Z", "manifest": [], "measurements": [%s]}`, strings.Join(measurements, ","))
	return post(t, h, "Bearer "+serverB, body).Code
}

func site(node, labels string) string {
	field := ""
	if node != "" {
		field = `"node": ` + node + `, `
	}
	return `{` + field + `"metric": "site.pageviews_24h", "labels": ` + labels + `, "sensor": "access_log", "value": 3}`
}

const load = `{"metric": "load.avg_1m", "labels": {}, "value": 0.5}`

// spec: site-traffic.md#ingest — a host's measurement of a site it serves is stored under
// the sites node, beside the host's own.
func TestAHostReportsForTheSitesNode(t *testing.T) {
	h, store := newSitesHandler(t)
	if code := fromServerB(t, h, site(`"sites"`, `{"site": "blog-a"}`), load); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	saved := store.saved[0]
	if saved.Node != "server-b" || len(saved.Measurements) != 2 {
		t.Fatalf("saved %+v", saved)
	}
	if got := saved.Measurements[0].Node; got != "sites" {
		t.Errorf("the site's measurement belongs to %q, want sites", got)
	}
	if got := saved.Measurements[1].Node; got != "" {
		t.Errorf("the host's own measurement belongs to %q, want the host", got)
	}
}

// spec: site-traffic.md#ingest — a measurement of a site the host does not serve is dropped
// alone and logged, and the configuration is still delivered.
// spec: ingest.md#storage — a valid request stores all but such a measurement.
func TestASiteTheHostDoesNotServeIsDropped(t *testing.T) {
	for name, c := range map[string]struct{ labels, logged string }{
		"another host's site": {`{"site": "shop-c"}`, "site=shop-c"},
		"an unknown site":     {`{"site": "news-e"}`, "site=news-e"},
		"no site label":       {`{}`, "labelled=false"},
	} {
		t.Run(name, func(t *testing.T) {
			logged := captureLog(t)
			h, store := newSitesHandler(t)
			rec := post(t, h, "Bearer "+serverB, fmt.Sprintf(`{"node": "server-b", "config_version": "",
				"ts": "2026-08-28T10:00:00Z", "measurements": [%s, %s]}`, site(`"sites"`, c.labels), load))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", rec.Code)
			}
			if got := store.saved[0].Measurements; len(got) != 1 || got[0].Metric != "load.avg_1m" {
				t.Errorf("stored %+v, want the host's own reading alone", got)
			}
			if !strings.Contains(rec.Body.String(), `"config"`) {
				t.Errorf("body %s, want the configuration delivered", rec.Body)
			}
			if text := logged.String(); !strings.Contains(text, "node=server-b") || !strings.Contains(text, c.logged) {
				t.Errorf("logged %q, want it to name server-b and %s", text, c.logged)
			}
		})
	}
}

// spec: site-traffic.md#ingest — a measurement naming another agent's node, a service node
// or no node of the file refuses the whole request.
// spec: ingest.md#authentication
func TestAnotherNodeIsRefused(t *testing.T) {
	for _, other := range []string{`"server-d"`, `"cloud"`, `"nowhere"`} {
		h, store := newSitesHandler(t)
		if code := fromServerB(t, h, site(other, `{"site": "blog-a"}`), load); code != http.StatusForbidden {
			t.Errorf("node %s: status %d, want 403", other, code)
		}
		if len(store.saved) != 0 {
			t.Errorf("node %s: something was stored", other)
		}
	}
}

// spec: site-traffic.md#ingest — naming the requesting node is as without the field.
func TestNamingItselfIsAsWithoutTheField(t *testing.T) {
	h, store := newSitesHandler(t)
	if code := fromServerB(t, h, site(`"server-b"`, `{"site": "blog-a"}`)); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if got := store.saved[0].Measurements[0].Node; got != "" {
		t.Errorf("stored under %q, want the requesting node", got)
	}
}

// spec: site-traffic.md#ingest — a node that is not a string, or is empty, is a bad request.
// spec: ingest.md#validation
func TestAMalformedNodeIsABadRequest(t *testing.T) {
	for _, node := range []string{`""`, `5`, `["sites"]`} {
		h, store := newSitesHandler(t)
		if code := fromServerB(t, h, site(node, `{"site": "blog-a"}`)); code != http.StatusBadRequest {
			t.Errorf("node %s: status %d, want 400", node, code)
		}
		if len(store.saved) != 0 {
			t.Errorf("node %s: something was stored", node)
		}
	}
}
