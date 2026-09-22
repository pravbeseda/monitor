package uptime

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// System returns the boot-time source of the machine the agent runs on.
func System() Source {
	return func() (time.Time, error) {
		boot, err := unix.SysctlTimeval("kern.boottime")
		if err != nil {
			return time.Time{}, fmt.Errorf("sysctl kern.boottime: %w", err)
		}
		return time.Unix(boot.Unix()), nil
	}
}
