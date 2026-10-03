package config

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"time"
)

// SitesClass is the compiled-in class of the node every web site's series belong to, which
// the agent on each site's host reports for (ADR 0045). Like ServiceClass, the name is
// reserved and its node has no token.
const SitesClass = "sites"

// AccessLogSensor reads the sites' logs. It reaches a host only through the sites it serves.
const AccessLogSensor = "access_log"

// Site is one site as its host receives it.
type Site struct {
	Name string
	Log  string
}

type fileSite struct {
	Host string `yaml:"host"`
	Log  string `yaml:"log"`
}

// onlyThroughSites is why access_log may be named by the sites class and node alone.
const onlyThroughSites = "it reaches a host only through the sites it serves"

const sitesReserved = "class " + SitesClass + " is reserved for the node the hosts of its sites report for (ADR 0045); rename a machine class of that name"

// SitesNode says the hosts of this node's sites report for it, so it has no Token.
func (n Node) SitesNode() bool { return n.Class == SitesClass }

// HostOf returns the node that serves site, when the sites node lists it.
func (c *Config) HostOf(site string) (string, bool) {
	host, ok := c.siteHosts[site]
	return host, ok
}

// validateSites refuses access_log anywhere but the sites class and node, and a sites node
// whose sites could not all be read by an agent.
func validateSites(f file) error {
	if _, named := f.Sensors[AccessLogSensor]; named {
		return fmt.Errorf("the top level names %s: %s", AccessLogSensor, onlyThroughSites)
	}

	var sitesNodes []string
	for _, name := range sorted(f.Nodes) {
		entry := f.Nodes[name]
		if entry.Class != SitesClass {
			if _, named := entry.Sensors[AccessLogSensor]; named {
				return fmt.Errorf("node %s names %s: %s", name, AccessLogSensor, onlyThroughSites)
			}
			if entry.Sites != nil {
				return fmt.Errorf("node %s: sites belongs to a node of class %s", name, SitesClass)
			}
			continue
		}
		sitesNodes = append(sitesNodes, name)
		if err := validateSitesSensors("node "+name, entry.Sensors); err != nil {
			return err
		}
		if err := validateSiteList(f, name, entry.Sites); err != nil {
			return err
		}
	}
	if len(sitesNodes) > 1 {
		return fmt.Errorf("nodes %s are all of class %s; one groups every site", sitesNodes, SitesClass)
	}
	return nil
}

// validateSitesClass holds a class to what it may say of access_log: the sites class runs it
// alone, and every other class never names it.
func validateSitesClass(name string, builtin, custom fileClass) error {
	if name != SitesClass {
		if _, named := custom.Sensors[AccessLogSensor]; named || slices.Contains(custom.Profile, AccessLogSensor) {
			return fmt.Errorf("class %s names %s: %s", name, AccessLogSensor, onlyThroughSites)
		}
		return nil
	}
	if profile := lastList(builtin.Profile, custom.Profile); !slices.Equal(profile, []string{AccessLogSensor}) {
		return fmt.Errorf("class %s: its profile is %v, and must be [%s] alone", name, profile, AccessLogSensor)
	}
	return validateSitesSensors("class "+name, custom.Sensors)
}

// validateSitesSensors lets a sites layer set when access_log runs, and nothing else.
func validateSitesSensors(where string, sensors map[string]fileSensor) error {
	for _, sensor := range sorted(sensors) {
		if sensor != AccessLogSensor {
			return fmt.Errorf("%s names %s: a %s node runs %s alone", where, sensor, SitesClass, AccessLogSensor)
		}
		if on := sensors[sensor].Enabled; on != nil && !*on {
			return fmt.Errorf("%s disables %s, so its sites would never be read", where, AccessLogSensor)
		}
	}
	return nil
}

func validateSiteList(f file, node string, sites map[string]fileSite) error {
	if len(sites) == 0 {
		return fmt.Errorf("node %s lists no sites, so it would never store a measurement", node)
	}
	read := map[[2]string]string{}
	for _, name := range sorted(sites) {
		site := sites[name]
		where := fmt.Sprintf("node %s: site %s", node, name)
		switch {
		case site.Host == "":
			return fmt.Errorf("%s: host is required", where)
		case site.Log == "":
			return fmt.Errorf("%s: log is required", where)
		case !filepath.IsAbs(site.Log):
			return fmt.Errorf("%s: log %q is not an absolute path", where, site.Log)
		}
		host, listed := f.Nodes[site.Host]
		if !listed || host.Class == ServiceClass || host.Class == SitesClass {
			return fmt.Errorf("%s: host %s is not a node of the file that runs an agent", where, site.Host)
		}
		key := [2]string{site.Host, site.Log}
		if other, taken := read[key]; taken {
			return fmt.Errorf("node %s: sites %s and %s read the same log on %s, so each line would count twice",
				node, other, name, site.Host)
		}
		read[key] = name
	}
	return nil
}

// deliverSites hands each host its sites, at the interval the sites node resolves, and
// fits the sites node to its hosts: the compiled-in interval and window are raised to the
// slowest host's tick, and ones the file wrote too short are refused. It returns which host
// serves each site.
func deliverSites(f file, nodes map[string]Node) (map[string]string, error) {
	hostOf := map[string]string{}
	for _, name := range sorted(nodes) {
		sites := nodes[name]
		if !sites.SitesNode() {
			continue
		}
		entry := f.Nodes[name]
		_, custom, _ := classLayers(f, SitesClass)

		perHost := map[string][]Site{}
		var slowest time.Duration
		var slowestHost string
		for _, site := range sorted(entry.Sites) {
			listed := entry.Sites[site]
			perHost[listed.Host] = append(perHost[listed.Host], Site{Name: site, Log: listed.Log})
			hostOf[site] = listed.Host
			if tick := nodes[listed.Host].Agent.BaseTick; tick > slowest {
				slowest, slowestHost = tick, listed.Host
			}
		}

		reading := sites.Agent.Sensors[AccessLogSensor]
		if reading.Interval < slowest {
			if intervalWritten([]map[string]fileSensor{custom.Sensors, entry.Sensors}, AccessLogSensor) {
				return nil, fmt.Errorf("node %s: %s interval %v is below the base tick %v of %s, a host of its sites",
					name, AccessLogSensor, reading.Interval, slowest, slowestHost)
			}
			reading.Interval = slowest
		}
		if bound := 2*reading.Interval + 3*slowest; sites.SilenceAfter < bound {
			if custom.SilenceAfter != "" {
				return nil, fmt.Errorf("node %s: silence_after %v is shorter than %v, two of its %s interval and three of its hosts' slowest base tick, so one missed collection could make it fall silent",
					name, sites.SilenceAfter, bound, AccessLogSensor)
			}
			sites.SilenceAfter = bound
		}
		if err := sites.deliver(AccessLogSensor, reading); err != nil {
			return nil, err
		}
		// The sites' series are the sites node's, aged by its interval; a host's own target
		// leaves access_log out.
		sites.target.SilenceAfter = sites.SilenceAfter
		sites.target.Intervals = maps.Clone(sites.target.Intervals)
		sites.target.Intervals[AccessLogSensor] = reading.Interval
		nodes[name] = sites

		for host, list := range perHost {
			served := nodes[host]
			if err := served.deliver(AccessLogSensor, Sensor{
				Enabled: true, Interval: reading.Interval, Node: name, Sites: list,
			}); err != nil {
				return nil, err
			}
			nodes[host] = served
		}
	}
	return hostOf, nil
}

// deliver sets one sensor's entry, and the version that follows from it.
func (n *Node) deliver(sensor string, settings Sensor) error {
	n.Agent.Sensors = maps.Clone(n.Agent.Sensors)
	n.Agent.Sensors[sensor] = settings
	version, err := version(n.Agent)
	if err != nil {
		return fmt.Errorf("node %s: %w", n.Name, err)
	}
	n.Version = version
	return nil
}
