package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// The YAML file as written, before any layer is applied. Durations stay strings here so
// that a malformed one is reported against its key rather than as a parse failure.

type file struct {
	BaseTick    string                `yaml:"base_tick"`
	AgentTarget agentTarget           `yaml:"agent_target"`
	Filesystems []string              `yaml:"filesystems"`
	SkipMounts  []string              `yaml:"skip_mounts"`
	Sensors     map[string]fileSensor `yaml:"sensors"`
	Rules       map[string]fileRule   `yaml:"rules"`
	Digest      fileDigest            `yaml:"digest"`
	Notify      fileNotify            `yaml:"notify"`
	Classes     map[string]fileClass  `yaml:"classes"`
	Nodes       map[string]fileNode   `yaml:"nodes"`
}

type fileSensor struct {
	Enabled  *bool  `yaml:"enabled"`
	Interval string `yaml:"interval"`
}

type fileClass struct {
	Profile      []string              `yaml:"profile"`
	SilenceAfter string                `yaml:"silence_after"`
	AgentTarget  agentTarget           `yaml:"agent_target"`
	BaseTick     string                `yaml:"base_tick"`
	Filesystems  []string              `yaml:"filesystems"`
	SkipMounts   []string              `yaml:"skip_mounts"`
	Sensors      map[string]fileSensor `yaml:"sensors"`
	Rules        map[string]fileRule   `yaml:"rules"`
}

type fileNode struct {
	Class       string                `yaml:"class"`
	TokenEnv    string                `yaml:"token_env"`
	AgentTarget agentTarget           `yaml:"agent_target"`
	BaseTick    string                `yaml:"base_tick"`
	Filesystems []string              `yaml:"filesystems"`
	SkipMounts  []string              `yaml:"skip_mounts"`
	Sensors     map[string]fileSensor `yaml:"sensors"`
	Rules       map[string]fileRule   `yaml:"rules"`
	Volumes     map[string]fileVolume `yaml:"volumes"`
}

// Thresholds as written. Sizes stay text for the same reason durations do — a malformed
// one is reported against its key — and a bare YAML number decodes here too, so that a
// size without a unit is refused with that same message rather than as a type error.

type fileRule struct {
	Warning  fileThreshold `yaml:"warning"`
	Critical fileThreshold `yaml:"critical"`
	Backup   *fileBackup   `yaml:"backup"`
}

type fileBackup struct {
	Warning  fileThreshold `yaml:"warning"`
	Critical fileThreshold `yaml:"critical"`
}

type fileThreshold struct {
	Floor   size     `yaml:"floor"`
	Ratio   *float64 `yaml:"ratio"`
	Ceiling size     `yaml:"ceiling"`
}

type fileDigest struct {
	At       string `yaml:"at"`
	Timezone string `yaml:"timezone"`
}

type fileNotify struct {
	Channel string `yaml:"channel"`
	Locale  string `yaml:"locale"`
}

type fileVolume struct {
	Role  string              `yaml:"role"`
	Rules map[string]fileRule `yaml:"rules"`
}

// agentTarget keeps the YAML node rather than its text: a key written with no value decodes
// as an absent one into a string, and absent means "no target" while an empty value is a
// mistake to refuse.
type agentTarget = yaml.Node

type size string

func (s *size) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Value == "" || node.Tag == "!!null" {
		return fmt.Errorf("line %d: a size is one value with a unit, such as 10GB", node.Line)
	}
	*s = size(node.Value)
	return nil
}
