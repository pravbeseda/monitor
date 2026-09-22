package memory

import "testing"

const meminfo = `MemTotal:        8000000 kB
MemFree:          500000 kB
MemAvailable:    2000000 kB
Buffers:          100000 kB
`

// spec: host-sensors.md#memory — MemAvailable over MemTotal, kB being KiB.
func TestParseMeminfo(t *testing.T) {
	got, err := parse(meminfo)
	if err != nil || got != (Available{Bytes: 2048000000, Pct: 25}) {
		t.Fatalf("parse = %+v, %v; want 2048000000 bytes, 25%%", got, err)
	}
}

// spec: host-sensors.md#memory — a kernel without MemAvailable gets no estimate of ours.
func TestParseMeminfoWithoutMemAvailable(t *testing.T) {
	if got, err := parse("MemTotal: 8000000 kB\nMemFree: 500000 kB\n"); err == nil {
		t.Fatalf("parse = %+v, want an error", got)
	}
}

// spec: host-sensors.md#memory — a zero total is no share of anything.
func TestParseMeminfoWithoutTotal(t *testing.T) {
	if got, err := parse("MemTotal: 0 kB\nMemAvailable: 1 kB\n"); err == nil {
		t.Fatalf("parse = %+v, want an error", got)
	}
}
