package systemd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// systemSource asks the running system manager through systemctl.
type systemSource struct {
	runDir string
	// systemctl runs systemctl with its arguments and returns what it printed.
	systemctl func(ctx context.Context, args ...string) ([]byte, error)
}

// System returns the system manager of the machine the agent runs on.
func System() Source {
	return systemSource{runDir: "/run/systemd/system", systemctl: func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "systemctl", args...).Output()
	}}
}

func (s systemSource) Booted() bool {
	info, err := os.Stat(s.runDir)
	return err == nil && info.IsDir()
}

// FailedUnits reads the manager's own NFailedUnits property: one read, no unit listing.
func (s systemSource) FailedUnits(ctx context.Context) (int, error) {
	out, err := s.systemctl(ctx, "show", "--property=NFailedUnits", "--value")
	if err != nil {
		return 0, fmt.Errorf("systemctl show: %w", err)
	}
	return parse(string(out))
}

// ListFailed lists the failed units, then reads every one's Result in a single call: two
// reads however many failed. `--plain`, `--no-legend` and `--full` keep the list to one unit per
// line with its name in full, and `--` lets a name such as `-.mount` through as a name.
func (s systemSource) ListFailed(ctx context.Context) ([]Unit, error) {
	out, err := s.systemctl(ctx, "list-units", "--state=failed", "--plain", "--no-legend", "--full", "--no-pager")
	if err != nil {
		return nil, fmt.Errorf("systemctl list-units: %w", err)
	}
	names, err := parseList(string(out))
	if err != nil || len(names) == 0 {
		return nil, err
	}
	out, err = s.systemctl(ctx, append([]string{"show", "--property=Id,Result", "--"}, names...)...)
	if err != nil {
		return nil, fmt.Errorf("systemctl show: %w", err)
	}
	results, err := parseShow(string(out))
	if err != nil {
		return nil, err
	}
	units := make([]Unit, len(names))
	for i, name := range names {
		units[i] = Unit{Name: name, Result: results[name]}
	}
	return units, nil
}

// parse reads NFailedUnits as systemctl show --value prints it: "3".
func parse(text string) (int, error) {
	failed, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || failed < 0 {
		return 0, fmt.Errorf("NFailedUnits %q is not a count", text)
	}
	return failed, nil
}

// parseList reads the unit names list-units prints, one to a line before its load, active
// and sub states. A bullet before the name, which systemctl prints on a terminal, is skipped.
func parseList(text string) ([]string, error) {
	var names []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] == "●" || fields[0] == "*") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		// Every unit name ends in its type: `.service`, `.timer`, `.mount`.
		if !strings.Contains(fields[0], ".") {
			return nil, fmt.Errorf("list-units line %q names no unit", line)
		}
		names = append(names, fields[0])
	}
	return names, nil
}

// parseShow reads the Id and Result of each unit as show prints them: a block of
// `key=value` lines per unit, blocks parted by a blank line, in no promised order.
func parseShow(text string) (map[string]string, error) {
	results := map[string]string{}
	for _, block := range strings.Split(text, "\n\n") {
		var id, result string
		for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
			if line == "" {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if !found {
				return nil, fmt.Errorf("show line %q is not a property", line)
			}
			switch key {
			case "Id":
				id = value
			case "Result":
				result = value
			}
		}
		if id != "" {
			results[id] = result
		}
	}
	return results, nil
}
