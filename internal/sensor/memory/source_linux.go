package memory

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const meminfoFile = "/proc/meminfo"

// System returns the memory source of the machine the agent runs on.
func System() Source {
	return func() (Available, error) {
		text, err := os.ReadFile(meminfoFile)
		if err != nil {
			return Available{}, err
		}
		return parse(string(text))
	}
}

// parse takes MemAvailable and MemTotal from /proc/meminfo, whose "kB" are KiB.
func parse(text string) (Available, error) {
	fields := map[string]uint64{}
	for line := range strings.Lines(text) {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || (name != "MemTotal" && name != "MemAvailable") {
			continue
		}
		kib, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return Available{}, fmt.Errorf("%s %s: %w", meminfoFile, name, err)
		}
		fields[name] = kib * 1024
	}
	available, ok := fields["MemAvailable"]
	if !ok {
		return Available{}, fmt.Errorf("%s has no MemAvailable: the kernel is older than 3.14", meminfoFile)
	}
	total := fields["MemTotal"]
	if total == 0 {
		return Available{}, fmt.Errorf("%s reports no MemTotal", meminfoFile)
	}
	return Available{Bytes: available, Pct: float64(available) / float64(total) * 100}, nil
}
