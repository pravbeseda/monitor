package memory_test

import (
	"testing"

	"github.com/pravbeseda/monitor/internal/sensor/memory"
)

// The platform source is thin but easy to break; this checks it against the machine the
// tests run on, whichever of the two supported platforms that is.
func TestSystemSourceReadsThisMachine(t *testing.T) {
	got, err := memory.System()()
	if err != nil {
		t.Fatalf("System: %v", err)
	}
	if got.Pct < 0 || got.Pct > 100 || got.Bytes == 0 {
		t.Fatalf("System = %+v, want a share in 0..100 and some bytes", got)
	}
}
