package load

import (
	"encoding/binary"
	"testing"
)

func loadavg(scale uint64, averages ...uint32) []byte {
	raw := make([]byte, 24)
	for i, a := range averages {
		binary.LittleEndian.PutUint32(raw[4*i:], a)
	}
	binary.LittleEndian.PutUint64(raw[16:], scale)
	return raw
}

// spec: host-sensors.md#load — the kernel's fixed-point averages, divided by their scale.
func TestDecodeLoadavg(t *testing.T) {
	got, err := decode(loadavg(2048, 1024, 2560, 4352))
	if err != nil || got != [3]float64{0.5, 1.25, 2.125} {
		t.Fatalf("decode = %v, %v; want [0.5 1.25 2.125]", got, err)
	}
}

// spec: host-sensors.md#load — a short or unscaled answer is an error.
func TestDecodeLoadavgRefusesGarbage(t *testing.T) {
	if _, err := decode(make([]byte, 12)); err == nil {
		t.Error("decode of 12 bytes succeeded, want an error")
	}
	if _, err := decode(loadavg(0, 1, 2, 3)); err == nil {
		t.Error("decode with zero scale succeeded, want an error")
	}
}
