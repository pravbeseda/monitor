package load_test

import (
	"testing"

	"github.com/pravbeseda/monitor/internal/sensor/load"
)

// The platform source is thin but easy to break; this checks it against the machine the
// tests run on, whichever of the two supported platforms that is.
func TestSystemSourceReadsThisMachine(t *testing.T) {
	got, err := load.System()()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	for _, a := range got {
		if a < 0 {
			t.Errorf("average %v is negative", a)
		}
	}
}
