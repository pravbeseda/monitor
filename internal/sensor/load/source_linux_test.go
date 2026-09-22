package load

import "testing"

// spec: host-sensors.md#load — the averages as the kernel publishes them.
func TestParseLoadavg(t *testing.T) {
	got, err := parse("0.50 1.25 2.12 1/234 5678\n")
	if err != nil || got != [3]float64{0.5, 1.25, 2.12} {
		t.Fatalf("parse = %v, %v; want [0.5 1.25 2.12]", got, err)
	}
}

// spec: host-sensors.md#load — a file that is not load averages is an error.
func TestParseLoadavgRefusesGarbage(t *testing.T) {
	for _, text := range []string{"", "0.50 1.25", "a b c 1/2 3"} {
		if _, err := parse(text); err == nil {
			t.Errorf("parse(%q) succeeded, want an error", text)
		}
	}
}
