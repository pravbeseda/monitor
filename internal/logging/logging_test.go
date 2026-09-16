package logging_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/logging"
)

// The host's own clock is pinned away from UTC, so a logger that followed it would be
// caught here rather than passing on a runner that happens to sit in UTC.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("HOST", 7*3600)
	os.Exit(m.Run())
}

// A log line carries its own instant in UTC (ADR 0026): diagnostics are read next to a
// measurement's timestamp and next to another host's log, and both of those are UTC.
func TestLogLineCarriesItsInstantInUTC(t *testing.T) {
	var out bytes.Buffer

	logging.New(&out).Error("read node states", "error", "database is locked")

	line := out.String()
	stamp, _, found := strings.Cut(strings.TrimPrefix(line, "time="), " ")
	if !found {
		t.Fatalf("log line = %q, want a leading time field", line)
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatalf("time field %q: %v", stamp, err)
	}
	if !strings.HasSuffix(stamp, "Z") {
		t.Errorf("time field = %q, want UTC", stamp)
	}
	if delta := time.Since(at); delta < 0 || delta > time.Minute {
		t.Errorf("time field = %q, which is %v away from now", stamp, delta)
	}
}

// The pairs the code already logs stay pairs: the handler changes the zone, nothing else.
func TestLogLineKeepsItsAttributes(t *testing.T) {
	var out bytes.Buffer

	logging.New(&out).Error("read node states", "error", "database is locked")

	line := out.String()
	for _, want := range []string{"level=ERROR", `msg="read node states"`, `error="database is locked"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line = %q, want %q in it", line, want)
		}
	}
}
