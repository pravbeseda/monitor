package memory

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// System returns the memory source of the machine the agent runs on.
func System() Source {
	return func() (Available, error) {
		level, err := unix.SysctlUint32("kern.memorystatus_level")
		if err != nil {
			return Available{}, fmt.Errorf("sysctl kern.memorystatus_level: %w", err)
		}
		memsize, err := unix.SysctlUint64("hw.memsize")
		if err != nil {
			return Available{}, fmt.Errorf("sysctl hw.memsize: %w", err)
		}
		return fromLevel(level, memsize)
	}
}

// fromLevel turns the kernel's free percentage into the two figures: the kernel answers a
// share only, so the size is derived from the installed memory.
func fromLevel(level uint32, memsize uint64) (Available, error) {
	if level > 100 {
		return Available{}, fmt.Errorf("kern.memorystatus_level %d is not a percentage", level)
	}
	if memsize == 0 {
		return Available{}, errors.New("hw.memsize reports no memory")
	}
	pct := float64(level)
	return Available{Bytes: uint64(pct / 100 * float64(memsize)), Pct: pct}, nil
}
