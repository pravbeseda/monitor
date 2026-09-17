package config

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/pravbeseda/monitor/internal/evaluate"
)

// validate checks the whole file rather than the part some node happens to reference: a
// class or a sensor default nobody uses yet still has to be right, or the typo surfaces
// days later, on a running hub, the moment it is wired to a node. `digest` and `notify`
// are absent here on purpose: they have one layer, so what they say and what they must be
// are the same statement, and it is made where they are resolved.
func validate(f file) error {
	if err := validateLayer("", f.BaseTick, f.Filesystems, f.Sensors); err != nil {
		return err
	}
	if err := validateAgentTarget("", f.AgentTarget); err != nil {
		return err
	}
	if err := validateRuleLayer("", f.Rules); err != nil {
		return err
	}

	for _, name := range classNames(f) {
		if err := validateClass(f, name); err != nil {
			return err
		}
	}

	for _, name := range sorted(f.Nodes) {
		node := f.Nodes[name]
		where := fmt.Sprintf("node %s: ", name)
		if err := validateLayer(where, node.BaseTick, node.Filesystems, node.Sensors); err != nil {
			return err
		}
		if err := validateAgentTarget(where, node.AgentTarget); err != nil {
			return err
		}
		if err := validateRuleLayer(where, node.Rules); err != nil {
			return err
		}
		if err := validateVolumes(where, node.Volumes); err != nil {
			return err
		}
	}
	return nil
}

// validateClass checks a class whether or not a node uses it, down to the sensors its
// profile promises: a typo in a sensor name is what this catches, and it is the likeliest
// typo in the file.
func validateClass(f file, name string) error {
	builtin, custom, _ := classLayers(f, name)
	where := fmt.Sprintf("class %s: ", name)

	if err := validateLayer(where, custom.BaseTick, custom.Filesystems, custom.Sensors); err != nil {
		return err
	}
	if err := validateAgentTarget(where, custom.AgentTarget); err != nil {
		return err
	}
	if _, compiledIn := defaultClasses[name]; !compiledIn && custom.SilenceAfter == "" {
		return fmt.Errorf("%ssilence_after is required of a class the file introduces", where)
	}
	silenceAfter := last(builtin.SilenceAfter, custom.SilenceAfter)
	if silenceAfter != "" {
		if _, err := duration(where+"silence_after", silenceAfter); err != nil {
			return err
		}
	}

	// The node layer is missing here on purpose: a node adds its own sensors and is
	// checked when it is resolved. What the class alone promises has to hold already.
	//
	// The tick is not compared with the intervals here: a node may lower base_tick, so an
	// interval that looks too short at this layer can be right once the node is resolved.
	// That comparison belongs to resolve, where the tick is final.
	profile := lastList(builtin.Profile, custom.Profile)
	if _, err := sensorSettings(profile,
		defaultSensors, builtin.Sensors, f.Sensors, custom.Sensors); err != nil {
		return fmt.Errorf("%s%w", where, err)
	}

	// A class nobody uses yet is checked as written, so a threshold typo surfaces at
	// startup rather than on the day the class is wired to a node. Only what the class says
	// on its own terms is judged here: whether critical is stricter than warning depends on
	// layers above, so that check waits for a resolved node, like the base tick above.
	return validateRuleLayer(where, custom.Rules)
}

// classNames is every class the hub knows: the compiled-in ones and the file's own.
func classNames(f file) []string {
	names := map[string]bool{}
	for name := range defaultClasses {
		names[name] = true
	}
	for name := range f.Classes {
		names[name] = true
	}
	return sorted(names)
}

// validateLayer checks what a layer sets on its own terms only. A layer above may lower
// the base tick or replace a list, so nothing here compares one layer's value with
// another's: an intermediate layer is not required to stand alone.
func validateLayer(where, baseTick string, filesystems []string, sensors map[string]fileSensor) error {
	if baseTick != "" {
		if _, err := duration(where+"base_tick", baseTick); err != nil {
			return err
		}
	}
	if filesystems != nil && len(filesystems) == 0 {
		return fmt.Errorf("%sfilesystems is empty, so no volume would be collected", where)
	}
	for _, name := range sorted(sensors) {
		if declared := sensors[name].Interval; declared != "" {
			if _, err := duration(fmt.Sprintf("%ssensor %s: interval", where, name), declared); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateRuleLayer checks what a layer says about thresholds on its own terms: the rules
// it names, the shape of a backup branch, and that every size and ratio it writes can be
// read at all.
func validateRuleLayer(where string, rules map[string]fileRule) error {
	for _, name := range sorted(rules) {
		if _, known := evaluate.Lookup(name); !known {
			return fmt.Errorf("%srules.%s: no rule of that name is implemented", where, name)
		}
		declared := rules[name]
		key := fmt.Sprintf("%srules.%s", where, name)
		if err := validateThresholdText(key+".warning", declared.Warning); err != nil {
			return err
		}
		if err := validateThresholdText(key+".critical", declared.Critical); err != nil {
			return err
		}
		if declared.Backup == nil {
			continue
		}
		for _, level := range levelsOf(*declared.Backup) {
			if err := refuseBand(key+".backup."+level.name, level.threshold); err != nil {
				return err
			}
			if err := validateThresholdText(key+".backup."+level.name, level.threshold); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateVolumes judges what a node says about single volumes. A volume carries its role,
// so it writes thresholds directly, and a volume that is a backup writes no band: it is
// the very volume percentages say nothing about.
func validateVolumes(where string, volumes map[string]fileVolume) error {
	for _, mount := range sorted(volumes) {
		declared := volumes[mount]
		volumeWhere := fmt.Sprintf("%svolume %q: ", where, mount)
		if declared.Role != "" && declared.Role != roleBackup {
			return fmt.Errorf("%srole %q is unknown; the only role is %s", volumeWhere, declared.Role, roleBackup)
		}
		if err := validateRuleLayer(volumeWhere, declared.Rules); err != nil {
			return err
		}
		for _, name := range sorted(declared.Rules) {
			key := fmt.Sprintf("%srules.%s", volumeWhere, name)
			if declared.Rules[name].Backup != nil {
				return fmt.Errorf("%s: a volume carries its role, so it writes thresholds directly", key)
			}
			if declared.Role != roleBackup {
				continue
			}
			for _, level := range levelsOf(fileBackup{declared.Rules[name].Warning, declared.Rules[name].Critical}) {
				if err := refuseBand(key+"."+level.name, level.threshold); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// levelsOf names the two levels of a rule in a fixed order, so an error about a pair of
// them does not depend on map iteration.
func levelsOf(rule fileBackup) []struct {
	name      string
	threshold fileThreshold
} {
	return []struct {
		name      string
		threshold fileThreshold
	}{
		{"warning", rule.Warning},
		{"critical", rule.Critical},
	}
}

// validateThresholdText reads every value a layer writes, so that a size or a ratio nobody
// can parse is named here rather than surfacing as a merged rule two layers later.
func validateThresholdText(where string, declared fileThreshold) error {
	if declared.Floor != "" {
		if _, err := sizeValue(where+".floor", declared.Floor); err != nil {
			return err
		}
	}
	if declared.Ceiling != "" {
		if _, err := sizeValue(where+".ceiling", declared.Ceiling); err != nil {
			return err
		}
	}
	if declared.Ratio != nil {
		return checkRatio(where+".ratio", *declared.Ratio)
	}
	return nil
}

func refuseBand(where string, declared fileThreshold) error {
	if declared.Ratio != nil || declared.Ceiling != "" {
		return fmt.Errorf("%s: a backup rule is a floor, so it takes no ratio and no ceiling", where)
	}
	return nil
}

// agentTargetGrammar is hub.target's: latest, or a version whose components the installer's
// shell can still compare as numbers (docs/specs/installer.md).
var agentTargetGrammar = regexp.MustCompile(`^(latest|(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8}))$`)

// validateAgentTarget refuses a target the installer would refuse, a key with no value
// included; a key that is not written at all is no target.
func validateAgentTarget(where string, target agentTarget) error {
	if target.Kind == 0 {
		return nil
	}
	if target.Kind != yaml.ScalarNode || target.Tag == "!!null" || !agentTargetGrammar.MatchString(target.Value) {
		return fmt.Errorf("%sagent_target %q is neither latest nor a version such as 1.2.3", where, target.Value)
	}
	return nil
}
