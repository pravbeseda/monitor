// Package config reads the hub's YAML file and resolves it into the per-node
// configuration that the ingest response delivers (ADR 0010).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/pravbeseda/monitor/internal/evaluate"
)

// minTokenLength keeps the security of ADR 0007 rule 5 on the token, not on obscurity.
const minTokenLength = 32

// Sensor is one sensor as the agent receives it.
type Sensor struct {
	Enabled  bool
	Interval time.Duration
}

// Agent is the flat configuration an agent applies: the hub resolves the layers, the
// agent merges nothing.
type Agent struct {
	BaseTick    time.Duration
	Filesystems []string
	// SkipMounts are mount-point prefixes the disk sensor leaves alone.
	SkipMounts []string
	Sensors    map[string]Sensor
}

// Node is everything the hub knows about one node.
type Node struct {
	Name         string
	Class        string
	Token        string
	SilenceAfter time.Duration
	// AgentTarget is the version this node's updater installs — latest or MAJOR.MINOR.PATCH
	// — or empty when no layer names one (ADR 0028). It never reaches the agent itself.
	AgentTarget string
	Agent       Agent
	// Version identifies Agent, not the node: identical configurations share it.
	Version string
	// target is what evaluation reads of this node. It travels apart from Agent because
	// none of it ever reaches one: thresholds are the hub's business (ADR 0012).
	target evaluate.Target
}

// Target is what evaluation reads of this node (ADR 0015): the silence window, the
// interval of every sensor the node actually runs, and the thresholds.
func (n Node) Target() evaluate.Target { return n.target }

// String keeps every token out of a debug print of the whole configuration (ADR 0007).
func (n Node) String() string {
	return fmt.Sprintf("node %s of class %s, configuration %s", n.Name, n.Class, n.Version)
}

// Config is the resolved file: one entry per listed node, plus the hub-wide settings that
// belong to no node.
type Config struct {
	nodes  map[string]Node
	digest evaluate.Schedule
	notify Notify
}

// String keeps tokens out of a debug print, whatever verb is used on the configuration.
func (c *Config) String() string {
	return fmt.Sprintf("%d nodes, digest %02d:%02d %s, notify %s in %s",
		len(c.nodes), c.digest.Hour, c.digest.Minute, c.digest.Location, c.notify.Channel, c.notify.Locale)
}

// Digest is when the daily digest goes out.
func (c *Config) Digest() evaluate.Schedule { return c.digest }

// Notify is how notifications leave the hub, secrets included.
func (c *Config) Notify() Notify { return c.notify }

// Load reads the file at path, validates it and resolves every listed node. Deployment
// settings have no defaults, so an incomplete file is an error rather than a fallback.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	var f file
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(f.Nodes) == 0 {
		return nil, fmt.Errorf("%s: nodes is missing or empty, so the hub would serve nobody", path)
	}
	announceLegacyKeys(path, f)
	if err := validate(f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	digest, err := resolveDigest(f.Digest)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	notify, err := resolveNotify(f.Notify)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	nodes := make(map[string]Node, len(f.Nodes))
	owner := make(map[string]string, len(f.Nodes))
	holder := make(map[string]string, len(f.Nodes))
	for _, name := range sorted(f.Nodes) {
		entry := f.Nodes[name]
		if err := claimToken(owner, name, entry.TokenEnv); err != nil {
			return nil, err
		}
		node, err := resolve(f, name, entry)
		if err != nil {
			return nil, err
		}
		// The token is what tells the hub which node asks, so it has to name exactly one.
		if other, taken := holder[node.Token]; taken {
			return nil, fmt.Errorf("nodes %s and %s hold the same token; each node needs its own", other, name)
		}
		holder[node.Token] = name
		nodes[name] = node
	}
	return &Config{nodes: nodes, digest: digest, notify: notify}, nil
}

// Node returns the resolved configuration of one node.
func (c *Config) Node(name string) (Node, bool) {
	node, ok := c.nodes[name]
	return node, ok
}

// Targets is every node as evaluation reads it, ordered by name: what a tick judges, and
// none of it ever reaching an agent.
func (c *Config) Targets() []evaluate.Target {
	out := make([]evaluate.Target, 0, len(c.nodes))
	for _, node := range c.Nodes() {
		out = append(out, node.Target())
	}
	return out
}

// Nodes returns every configured node, ordered by name.
func (c *Config) Nodes() []Node {
	out := make([]Node, 0, len(c.nodes))
	for _, name := range sorted(c.nodes) {
		out = append(out, c.nodes[name])
	}
	return out
}

// claimToken reads the node's token from its environment variable, refusing a variable
// two nodes share, one that is unset, and one holding a token too short to be a secret.
func claimToken(owner map[string]string, node, env string) error {
	if env == "" {
		return fmt.Errorf("node %s: token_env is required and has no default", node)
	}
	if other, taken := owner[env]; taken {
		return fmt.Errorf("nodes %s and %s share the token variable %s", other, node, env)
	}
	owner[env] = node
	return nil
}

func token(env string) (string, error) {
	value, err := envValue(env, "the node's token has no default")
	if err != nil {
		return "", err
	}
	if len(value) < minTokenLength {
		return "", fmt.Errorf("%s holds fewer than %d characters", env, minTokenLength)
	}
	return value, nil
}

// envValue reads one deployment secret, naming the variable it wanted and never its value.
func envValue(env, because string) (string, error) {
	value := os.Getenv(env)
	if value == "" {
		return "", fmt.Errorf("%s is unset: %s", env, because)
	}
	return value, nil
}

func sorted[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// announceLegacyKeys says once per key that a threshold in the file does nothing. The
// keys are tolerated for one release so that a hub upgrading itself unattended starts on
// the file already on its disk (ADR 0032); silence would leave an operator believing the
// numbers still judge something.
func announceLegacyKeys(path string, f file) {
	say := func(where string) {
		slog.Warn("a threshold in the configuration file is ignored",
			"file", path, "key", where,
			"how", "thresholds are set per series on the hub's /thresholds page")
	}
	if len(f.Rules) > 0 {
		say("rules")
	}
	for _, name := range sorted(f.Classes) {
		if len(f.Classes[name].Rules) > 0 {
			say("classes." + name + ".rules")
		}
	}
	for _, name := range sorted(f.Nodes) {
		if len(f.Nodes[name].Rules) > 0 {
			say("nodes." + name + ".rules")
		}
		if len(f.Nodes[name].Volumes) > 0 {
			say("nodes." + name + ".volumes")
		}
	}
}
