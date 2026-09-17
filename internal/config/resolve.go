package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
)

// versionLength keeps the version short enough to read in a log line and long enough that
// two configurations do not collide.
const versionLength = 12

// resolve applies the layers of ADR 0010 — product default → node class → node — and
// returns the flat configuration for one node.
func resolve(f file, name string, entry fileNode) (Node, error) {
	builtin, custom, known := classLayers(f, entry.Class)
	if !known {
		return Node{}, fmt.Errorf("node %s: unknown class %q", name, entry.Class)
	}

	secret, err := token(entry.TokenEnv)
	if err != nil {
		return Node{}, fmt.Errorf("node %s: %w", name, err)
	}

	baseTick, err := duration("base_tick", last(defaultBaseTick, f.BaseTick, custom.BaseTick, entry.BaseTick))
	if err != nil {
		return Node{}, fmt.Errorf("node %s: %w", name, err)
	}
	silenceAfter, err := duration("silence_after", last(builtin.SilenceAfter, custom.SilenceAfter))
	if err != nil {
		return Node{}, fmt.Errorf("class %s: %w", entry.Class, err)
	}

	filesystems := lastList(defaultFilesystems, f.Filesystems, custom.Filesystems, entry.Filesystems)
	if len(filesystems) == 0 {
		return Node{}, fmt.Errorf("node %s: filesystems is empty, so no volume would be collected", name)
	}

	skipMounts := lastList(defaultSkipMounts, f.SkipMounts, custom.SkipMounts, entry.SkipMounts)

	profile := lastList(builtin.Profile, custom.Profile)
	sensors, err := resolveSensors(profile, baseTick,
		defaultSensors, builtin.Sensors, f.Sensors, custom.Sensors, entry.Sensors)
	if err != nil {
		return Node{}, fmt.Errorf("node %s: %w", name, err)
	}

	rules, volumes, err := resolveRules(f, custom, entry)
	if err != nil {
		return Node{}, fmt.Errorf("node %s: %w", name, err)
	}

	agent := Agent{BaseTick: baseTick, Filesystems: filesystems, SkipMounts: skipMounts, Sensors: sensors}
	version, err := version(agent)
	if err != nil {
		return Node{}, fmt.Errorf("node %s: %w", name, err)
	}

	// A sensor delivered as disabled collects nothing, so the rules that read it have no
	// subjects: an interval the agent never honours would only freeze them later.
	intervals := make(map[string]time.Duration, len(sensors))
	for sensor, settings := range sensors {
		if settings.Enabled {
			intervals[sensor] = settings.Interval
		}
	}

	return Node{
		Name:         name,
		Class:        entry.Class,
		Token:        secret,
		SilenceAfter: silenceAfter,
		AgentTarget:  last(f.AgentTarget.Value, custom.AgentTarget.Value, entry.AgentTarget.Value),
		Agent:        agent,
		Version:      version,
		target: evaluate.Target{
			Node:         name,
			SilenceAfter: silenceAfter,
			Intervals:    intervals,
			Rules:        rules,
			Volumes:      volumes,
		},
	}, nil
}

// classLayers returns the compiled-in class and the file's class of the same name. They
// are separate layers: the file's top-level sensor defaults sit between them.
func classLayers(f file, name string) (builtin, custom fileClass, known bool) {
	builtin, inCode := defaultClasses[name]
	custom, inFile := f.Classes[name]
	return builtin, custom, inCode || inFile
}

// resolveSensors is sensorSettings plus the one check that needs a final base tick: a
// sensor cannot collect faster than the tick that carries it. Only a fully resolved node
// has that tick, since every layer above may lower it.
func resolveSensors(profile []string, baseTick time.Duration, layers ...map[string]fileSensor) (map[string]Sensor, error) {
	sensors, err := sensorSettings(profile, layers...)
	if err != nil {
		return nil, err
	}
	for _, name := range sorted(sensors) {
		if sensors[name].Interval < baseTick {
			return nil, fmt.Errorf("sensor %s: interval %v is below the base tick %v",
				name, sensors[name].Interval, baseTick)
		}
	}
	return sensors, nil
}

// sensorSettings flattens the sensor layers, lowest first. A sensor is delivered when its
// class profile contains it or when some layer names it explicitly.
func sensorSettings(profile []string, layers ...map[string]fileSensor) (map[string]Sensor, error) {
	intervals := map[string]string{}
	enabled := map[string]bool{}
	for _, layer := range layers {
		for _, sensor := range sorted(layer) {
			if layer[sensor].Interval != "" {
				intervals[sensor] = layer[sensor].Interval
			}
			if on := layer[sensor].Enabled; on != nil {
				enabled[sensor] = *on
			}
		}
	}

	delivered := map[string]bool{}
	for _, sensor := range profile {
		delivered[sensor] = true
	}
	for sensor := range enabled {
		delivered[sensor] = true
	}

	out := make(map[string]Sensor, len(delivered))
	for _, sensor := range sorted(delivered) {
		interval, err := duration(fmt.Sprintf("sensor %s: interval", sensor), intervals[sensor])
		if err != nil {
			return nil, err
		}
		on, set := enabled[sensor]
		out[sensor] = Sensor{Enabled: !set || on, Interval: interval}
	}
	return out, nil
}

// version is the SHA-256 of the delivered configuration, so it changes exactly when what
// the agent receives changes and needs nothing stored between restarts.
func version(a Agent) (string, error) {
	type sensorJSON struct {
		Enabled  bool   `json:"enabled"`
		Interval string `json:"interval"`
	}
	payload := struct {
		BaseTick    string                `json:"base_tick"`
		Filesystems []string              `json:"filesystems"`
		SkipMounts  []string              `json:"skip_mounts"`
		Sensors     map[string]sensorJSON `json:"sensors"`
	}{
		BaseTick:    a.BaseTick.String(),
		Filesystems: a.Filesystems,
		SkipMounts:  a.SkipMounts,
		Sensors:     make(map[string]sensorJSON, len(a.Sensors)),
	}
	for name, sensor := range a.Sensors {
		payload.Sensors[name] = sensorJSON{Enabled: sensor.Enabled, Interval: sensor.Interval.String()}
	}

	// json.Marshal sorts map keys, so the same configuration always hashes the same.
	canonical, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode configuration: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:versionLength], nil
}

// Sizes are decimal — 10GB is ten thousand million bytes — as ADR 0012 writes its
// thresholds and as disks are sold. "b" comes last so that "10gb" matches "gb" first.
var sizeUnits = []struct {
	suffix string
	scale  float64
}{
	{"kb", 1e3},
	{"mb", 1e6},
	{"gb", 1e9},
	{"tb", 1e12},
	{"pb", 1e15},
	{"b", 1},
}

// sizeValue reads one size, naming its key the way duration does. A unit is required, Go's
// own literal spellings are not this file's grammar, and a value that is not finite and
// positive is refused: a threshold of NaN is never entered and never left, so a subject
// would latch at its level for good.
func sizeValue(key string, value size) (float64, error) {
	text := strings.ToLower(strings.TrimSpace(string(value)))
	for _, unit := range sizeUnits {
		if !strings.HasSuffix(text, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
		if strings.ContainsAny(number, "xXpP_ ") {
			break
		}
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			break
		}
		scaled := parsed * unit.scale
		if math.IsNaN(scaled) || math.IsInf(scaled, 0) || math.Signbit(scaled) {
			break
		}
		return scaled, nil
	}
	return 0, fmt.Errorf("%s: %q is not a size, which is a number with a unit such as 10GB", key, value)
}

// checkRatio guards the same way for a percentage, NaN included: it passes both ends of a
// range test while meaning nothing.
func checkRatio(key string, value float64) error {
	if math.IsNaN(value) || value < 0 || value > 100 {
		return fmt.Errorf("%s: %g is not a percentage between 0 and 100", key, value)
	}
	return nil
}

func duration(key, value string) (time.Duration, error) {
	if value == "" {
		return 0, fmt.Errorf("%s is set at no layer", key)
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration", key, value)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive duration", key, value)
	}
	return parsed, nil
}

// last returns the value of the most specific layer that sets one.
func last(layers ...string) string {
	var value string
	for _, layer := range layers {
		if layer != "" {
			value = layer
		}
	}
	return value
}

// lastList returns the list of the most specific layer that sets one; an empty list is a
// value, not an omission, so that it can be caught as a mistake.
func lastList(layers ...[]string) []string {
	var value []string
	for _, layer := range layers {
		if layer != nil {
			value = layer
		}
	}
	return value
}
