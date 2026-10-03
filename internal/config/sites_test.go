package config_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/config"
)

// Two web servers beside the laptop, and the sites node: synthetic names (ADR 0007).
const hosts = `
  server-b:
    class: server
    token_env: MONITOR_TOKEN_SERVER_B
  server-d:
    class: server
    token_env: MONITOR_TOKEN_SERVER_D
`

const sitesNode = `
  sites:
    class: sites
    sites:
      blog-a: { host: server-b, log: /var/log/nginx/blog-a.access.log }
      shop-c: { host: server-b, log: /var/log/nginx/shop-c.access.log }
`

func withHosts(t *testing.T) {
	t.Helper()
	t.Setenv("MONITOR_TOKEN_SERVER_B", strings.Repeat("synthetic-b-", 3))
	t.Setenv("MONITOR_TOKEN_SERVER_D", strings.Repeat("synthetic-d-", 3))
}

func loadSites(t *testing.T, body string) *config.Config {
	t.Helper()
	withHosts(t)
	return load(t, body)
}

// spec: site-traffic.md#configuration — a host receives its own sites, the sites node's
// name and its interval; a node that hosts none receives no access_log.
// spec: hub-config.md#resolution — beside its own sensors.
func TestSitesReachTheirHost(t *testing.T) {
	cfg := loadSites(t, minimal+hosts+sitesNode)
	for _, own := range []string{"disk", "load", "memory", "uptime", "systemd"} {
		if !node(t, cfg, "server-b").Agent.Sensors[own].Enabled {
			t.Errorf("server-b lost its own %s", own)
		}
	}

	got := node(t, cfg, "server-b").Agent.Sensors["access_log"]
	want := []config.Site{
		{Name: "blog-a", Log: "/var/log/nginx/blog-a.access.log"},
		{Name: "shop-c", Log: "/var/log/nginx/shop-c.access.log"},
	}
	if !got.Enabled || got.Interval != 5*time.Minute || got.Node != "sites" || !slices.Equal(got.Sites, want) {
		t.Errorf("server-b's access_log = %+v, want both sites for the sites node every 5m", got)
	}
	for _, other := range []string{"server-d", "laptop-a"} {
		if _, delivered := node(t, cfg, other).Agent.Sensors["access_log"]; delivered {
			t.Errorf("%s receives access_log, want it to host no site", other)
		}
	}
	// The host's own lane keeps the bound its own sensors set; the sites' series age by
	// the sites node's interval.
	if _, aged := node(t, cfg, "server-b").Target().Intervals["access_log"]; aged {
		t.Error("server-b ages series by access_log, want only the sites node to")
	}
}

// spec: site-traffic.md#the-file — the sites node has no token, the compiled-in window and
// the access_log interval its series age by.
// spec: hub-config.md#tokens — a sites node has no token, so no request authenticates as it.
func TestTheSitesNode(t *testing.T) {
	cfg := loadSites(t, minimal+hosts+sitesNode)
	sites := node(t, cfg, "sites")
	if sites.Token != "" {
		t.Error("the sites node holds a token, want none")
	}
	if sites.SilenceAfter != 30*time.Minute {
		t.Errorf("silence_after = %v, want 30m", sites.SilenceAfter)
	}
	if got := sites.Target().Intervals["access_log"]; got != 5*time.Minute {
		t.Errorf("access_log ages by %v, want 5m", got)
	}
	if host, ok := cfg.HostOf("blog-a"); !ok || host != "server-b" {
		t.Errorf("blog-a's host = %q (%t), want server-b", host, ok)
	}
	if host, ok := cfg.HostOf("news-e"); ok {
		t.Errorf("an unlisted site's host = %q, want none", host)
	}
}

// spec: site-traffic.md#configuration — the interval the sites node sets reaches every host.
func TestTheSitesIntervalReachesEveryHost(t *testing.T) {
	cfg := loadSites(t, minimal+hosts+sitesNode+"    sensors:\n      access_log: { interval: 10m }\n")
	if got := node(t, cfg, "server-b").Agent.Sensors["access_log"].Interval; got != 10*time.Minute {
		t.Errorf("server-b collects every %v, want 10m", got)
	}
}

// spec: site-traffic.md#configuration — a site added or moved changes the versions of the
// hosts it reaches, and no other.
// spec: hub-config.md#configuration-version
func TestASiteChangesOnlyItsHostsVersions(t *testing.T) {
	before := loadSites(t, minimal+hosts+sitesNode)
	added := loadSites(t, minimal+hosts+sitesNode+
		"      news-e: { host: server-b, log: /var/log/nginx/news-e.access.log }\n")
	moved := loadSites(t, minimal+hosts+strings.Replace(sitesNode,
		"shop-c: { host: server-b", "shop-c: { host: server-d", 1))

	version := func(cfg *config.Config, name string) string { return node(t, cfg, name).Version }
	if version(added, "server-b") == version(before, "server-b") {
		t.Error("adding a site on server-b left its version unchanged")
	}
	if version(added, "server-d") != version(before, "server-d") {
		t.Error("adding a site on server-b changed server-d's version")
	}
	for _, host := range []string{"server-b", "server-d"} {
		if version(moved, host) == version(before, host) {
			t.Errorf("moving a site left %s's version unchanged", host)
		}
	}
}

// spec: site-traffic.md#startup — no interval and no window written, a host with a 15m
// tick: the compiled-in values are raised to fit it.
func TestCompiledInSitesValuesFollowTheSlowestHost(t *testing.T) {
	cfg := loadSites(t, minimal+strings.Replace(hosts, "    class: server\n    token_env: MONITOR_TOKEN_SERVER_B\n",
		"    class: server\n    token_env: MONITOR_TOKEN_SERVER_B\n    base_tick: 15m\n", 1)+sitesNode)
	sites := node(t, cfg, "sites")
	if got := sites.Target().Intervals["access_log"]; got != 15*time.Minute {
		t.Errorf("access_log every %v, want 15m", got)
	}
	if sites.SilenceAfter != 75*time.Minute {
		t.Errorf("silence_after = %v, want 75m", sites.SilenceAfter)
	}
	if got := node(t, cfg, "server-b").Agent.Sensors["access_log"].Interval; got != 15*time.Minute {
		t.Errorf("server-b collects every %v, want 15m", got)
	}
}

// spec: site-traffic.md#startup — a top-level sensor switched on never reaches the sites.
func TestATopLevelSensorNeverReachesTheSites(t *testing.T) {
	cfg := loadSites(t, "sensors:\n  load: { enabled: true }\n"+minimal+hosts+sitesNode)
	if _, delivered := node(t, cfg, "sites").Agent.Sensors["load"]; delivered {
		t.Error("the sites node runs load, want only access_log")
	}
}

// spec: site-traffic.md#startup
// spec: hub-config.md#startup — a node of class sites goes without token_env.
func TestLoadRejectsMisconfiguredSites(t *testing.T) {
	site := func(entry string) string {
		return minimal + hosts + "  sites:\n    class: sites\n    sites:\n      blog-a: " + entry + "\n"
	}
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"a sites node with token_env", minimal + hosts + sitesNode + "    token_env: MONITOR_TOKEN_SERVER_D\n",
			[]string{"sites", "reserved"}},
		{"two sites nodes", minimal + hosts + sitesNode + strings.Replace(sitesNode, "  sites:\n", "  more:\n", 1),
			[]string{"sites", "more"}},
		{"no sites", minimal + hosts + "  sites:\n    class: sites\n", []string{"sites"}},
		{"an empty map of sites", minimal + hosts + "  sites:\n    class: sites\n    sites: {}\n", []string{"sites"}},
		{"a site without host", site("{ log: /var/log/a.log }"), []string{"blog-a", "host"}},
		{"a site without log", site("{ host: server-b }"), []string{"blog-a", "log"}},
		{"a host the file does not list", site("{ host: server-x, log: /var/log/a.log }"),
			[]string{"blog-a", "server-x"}},
		{"a host that is the sites node", site("{ host: sites, log: /var/log/a.log }"),
			[]string{"blog-a", "sites"}},
		{"a host that is a service node", strings.Replace(site("{ host: cloud, log: /var/log/a.log }"),
			"  sites:\n", "  cloud:\n    class: service\n    sensors:\n      gdrive: { enabled: true }\n  sites:\n", 1),
			[]string{"blog-a", "cloud"}},
		{"a relative log", site("{ host: server-b, log: logs/a.log }"), []string{"blog-a"}},
		{"two sites sharing a host and a log", minimal + hosts + strings.Replace(sitesNode,
			"shop-c.access.log", "blog-a.access.log", 1), []string{"blog-a", "shop-c"}},
		{"sites on a node of another class", minimal + strings.Replace(hosts,
			"    token_env: MONITOR_TOKEN_SERVER_D\n", "    token_env: MONITOR_TOKEN_SERVER_D\n    sites: {}\n", 1) + sitesNode,
			[]string{"server-d", "sites"}},
		{"the top level naming access_log", "sensors:\n  access_log: { interval: 10m }\n" + minimal + hosts + sitesNode,
			[]string{"access_log"}},
		{"a node naming access_log", minimal + "    sensors:\n      access_log: { enabled: false }\n" + hosts + sitesNode,
			[]string{"laptop-a", "access_log"}},
		{"a class naming access_log", minimal + hosts + sitesNode + "classes:\n  server:\n    profile: [disk, access_log]\n",
			[]string{"server", "access_log"}},
		{"the sites node enabling another sensor", minimal + hosts + sitesNode + "    sensors:\n      load: { enabled: true }\n",
			[]string{"sites", "load"}},
		{"the sites node disabling access_log", minimal + hosts + sitesNode + "    sensors:\n      access_log: { enabled: false }\n",
			[]string{"sites", "access_log"}},
		{"the sites class enabling another sensor", minimal + hosts + sitesNode +
			"classes:\n  sites:\n    sensors:\n      load: { enabled: true }\n", []string{"sites", "load"}},
		{"the sites class disabling access_log", minimal + hosts + sitesNode +
			"classes:\n  sites:\n    sensors:\n      access_log: { enabled: false }\n", []string{"sites", "access_log"}},
		{"the sites class without access_log", minimal + hosts + sitesNode + "classes:\n  sites:\n    profile: []\n",
			[]string{"sites", "access_log"}},
		{"an interval written below a host's tick", minimal + strings.Replace(hosts,
			"    token_env: MONITOR_TOKEN_SERVER_B\n", "    token_env: MONITOR_TOKEN_SERVER_B\n    base_tick: 10m\n", 1) +
			sitesNode + "    sensors:\n      access_log: { interval: 5m }\n", []string{"server-b"}},
		{"a window written below two intervals and three ticks", minimal + hosts + sitesNode +
			"classes:\n  sites:\n    silence_after: 20m\n", []string{"sites", "silence_after"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withHosts(t)
			withDrive(t)
			t.Setenv(tokenEnv, token)
			_, err := config.Load(write(t, tc.body))
			if err == nil {
				t.Fatalf("Load accepted %s", tc.name)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
		})
	}
}

// spec: site-traffic.md#startup — the sites interval answers to its hosts' ticks, never to
// the sites node's own: hosts ticking every minute may read every two.
func TestTheSitesIntervalAnswersToItsHostsOnly(t *testing.T) {
	cfg := loadSites(t, minimal+hosts+sitesNode+"    sensors:\n      access_log: { interval: 2m }\n"+
		"classes:\n  server:\n    base_tick: 1m\n")
	if got := node(t, cfg, "server-b").Agent.Sensors["access_log"].Interval; got != 2*time.Minute {
		t.Errorf("server-b collects every %v, want 2m", got)
	}
}
