// Package api is the hub's wire format: the ingest contract the agent writes and the hub
// reads (docs/specs/ingest.md), and the pieces every /api/v1 endpoint shares.
package api

import (
	"regexp"
	"strings"
	"time"
)

// MetricID is the shape of every metric id on the wire: ingest refuses one that does not
// match, and a query cannot name one no request could have stored.
var MetricID = regexp.MustCompile(`^[a-z0-9_.]+$`)

// TimeLayout is how the API writes an instant: RFC 3339 in UTC, to the millisecond. It is
// the wire's own contract, free to differ from the resolution storage keeps.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// The wire format of docs/specs/ingest.md. Unknown fields are ignored, so an older hub
// accepts a newer agent's request.

// Request is what an agent posts on every tick.
type Request struct {
	Node          string         `json:"node"`
	AgentVersion  string         `json:"agent_version"`
	ConfigVersion string         `json:"config_version"`
	TS            string         `json:"ts"`
	Manifest      []SensorStatus `json:"manifest"`
	// A pointer tells an absent list from an empty one: empty is valid, absent is not.
	Measurements *[]Measurement `json:"measurements"`
}

// SensorStatus is one line of the manifest: a sensor the build contains.
type SensorStatus struct {
	Sensor     string `json:"sensor"`
	Applicable bool   `json:"applicable"`
}

// Measurement is one reading on the wire.
type Measurement struct {
	Metric string            `json:"metric"`
	Labels map[string]string `json:"labels"`
	Value  *float64          `json:"value"`
	TS     string            `json:"ts"`
	// Sensor names what produced this reading, and is what staleness is measured
	// against (docs/specs/evaluation.md#freezing). It is absent from an agent too old to
	// send it, which leaves that series aged by the longest interval among the sensors its
	// node runs rather than by its own (ADR 0034); a pointer tells that silence apart from
	// an empty name, which is refused.
	Sensor *string `json:"sensor,omitempty"`
}

// Response carries the configuration only when the agent's version differs from the
// hub's; otherwise both fields are omitted and the body is {}.
type Response struct {
	ConfigVersion string       `json:"config_version,omitempty"`
	Config        *AgentConfig `json:"config,omitempty"`
}

// AgentConfig is the flat configuration the agent applies as it arrives.
type AgentConfig struct {
	BaseTick    string                  `json:"base_tick"`
	Filesystems []string                `json:"filesystems"`
	SkipMounts  []string                `json:"skip_mounts"`
	Sensors     map[string]SensorConfig `json:"sensors"`
}

// SensorConfig is one sensor's slot in that configuration.
type SensorConfig struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
}

// ErrorBody is what every refused request answers with.
type ErrorBody struct {
	Error string `json:"error"`
}

// FormatDuration writes what an operator would write: 5m rather than 5m0s.
func FormatDuration(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
