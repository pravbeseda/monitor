package load

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// System returns the load source of the machine the agent runs on.
func System() Source {
	return func() ([3]float64, error) {
		raw, err := unix.SysctlRaw("vm.loadavg")
		if err != nil {
			return [3]float64{}, fmt.Errorf("sysctl vm.loadavg: %w", err)
		}
		return decode(raw)
	}
}

// decode reads struct loadavg: three 32-bit fixed-point averages, padding, and the 64-bit
// scale they are fixed to.
func decode(raw []byte) ([3]float64, error) {
	var averages [3]float64
	if len(raw) < 24 {
		return averages, fmt.Errorf("vm.loadavg: %d bytes, want 24", len(raw))
	}
	scale := float64(binary.LittleEndian.Uint64(raw[16:24]))
	if scale == 0 {
		return averages, errors.New("vm.loadavg: zero scale")
	}
	for i := range averages {
		averages[i] = float64(binary.LittleEndian.Uint32(raw[4*i:])) / scale
	}
	return averages, nil
}
