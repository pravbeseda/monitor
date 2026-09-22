package uptime

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const uptimeFile = "/proc/uptime"

// System returns the boot-time source of the machine the agent runs on.
func System() Source {
	return func() (time.Time, error) {
		text, err := os.ReadFile(uptimeFile)
		if err != nil {
			return time.Time{}, err
		}
		up, err := parse(string(text))
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(-up), nil
	}
}

// parse reads the first field of /proc/uptime, seconds since boot including suspend:
// "266400.90 1000000.00".
func parse(text string) (time.Duration, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s is empty", uptimeFile)
	}
	up, err := time.ParseDuration(fields[0] + "s")
	if err != nil {
		return 0, fmt.Errorf("%s: %w", uptimeFile, err)
	}
	return up, nil
}
