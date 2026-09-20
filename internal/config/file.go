package config

import "gopkg.in/yaml.v3"

// The YAML file as written, before any layer is applied. Durations stay strings here so
// that a malformed one is reported against its key rather than as a parse failure.
//
// `rules` and `volumes` are no longer part of the file: a threshold is set on the page
// and stored beside the measurements (ADR 0032). They are still decoded, into nothing, so
// that a hub upgrading itself unattended starts on the file already on its disk instead
// of refusing it as an unknown key; startup says so once per key, and a later release
// drops them (docs/specs/hub-config.md#startup).

type file struct {
	BaseTick    string                `yaml:"base_tick"`
	AgentTarget agentTarget           `yaml:"agent_target"`
	Filesystems []string              `yaml:"filesystems"`
	SkipMounts  []string              `yaml:"skip_mounts"`
	Sensors     map[string]fileSensor `yaml:"sensors"`
	Rules       map[string]yaml.Node  `yaml:"rules"`
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
	Rules        map[string]yaml.Node  `yaml:"rules"`
}

type fileNode struct {
	Class       string                `yaml:"class"`
	TokenEnv    string                `yaml:"token_env"`
	AgentTarget agentTarget           `yaml:"agent_target"`
	BaseTick    string                `yaml:"base_tick"`
	Filesystems []string              `yaml:"filesystems"`
	SkipMounts  []string              `yaml:"skip_mounts"`
	Sensors     map[string]fileSensor `yaml:"sensors"`
	Rules       map[string]yaml.Node  `yaml:"rules"`
	Volumes     map[string]yaml.Node  `yaml:"volumes"`
}

type fileDigest struct {
	At       string `yaml:"at"`
	Timezone string `yaml:"timezone"`
}

type fileNotify struct {
	Channel string `yaml:"channel"`
	Locale  string `yaml:"locale"`
}

// agentTarget keeps the YAML node rather than its text: a key written with no value decodes
// as an absent one into a string, and absent means "no target" while an empty value is a
// mistake to refuse.
type agentTarget = yaml.Node
