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
}

// System returns the system manager of the machine the agent runs on.
func System() Source { return systemSource{runDir: "/run/systemd/system"} }

func (s systemSource) Booted() bool {
	info, err := os.Stat(s.runDir)
	return err == nil && info.IsDir()
}

// FailedUnits reads the manager's own NFailedUnits property: one read, no unit listing.
func (systemSource) FailedUnits(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "show", "--property=NFailedUnits", "--value").Output()
	if err != nil {
		return 0, fmt.Errorf("systemctl show: %w", err)
	}
	return parse(string(out))
}

// parse reads NFailedUnits as systemctl show --value prints it: "3".
func parse(text string) (int, error) {
	failed, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || failed < 0 {
		return 0, fmt.Errorf("NFailedUnits %q is not a count", text)
	}
	return failed, nil
}
