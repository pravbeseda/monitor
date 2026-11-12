package systemd

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// spec: host-sensors.md#applicability — booted with systemd means the run directory is a
// directory, as sd_booted tests.
func TestBootedChecksTheRunDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	file := filepath.Join(root, "file")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{dir: true, file: false, filepath.Join(root, "missing"): false} {
		if got := (systemSource{runDir: path}).Booted(); got != want {
			t.Errorf("Booted with %s = %v, want %v", filepath.Base(path), got, want)
		}
	}
}

// spec: host-sensors.md#failed-units — the manager's own count, as systemctl prints it.
func TestParseFailedUnits(t *testing.T) {
	for text, want := range map[string]int{"0\n": 0, "3\n": 3} {
		if got, err := parse(text); err != nil || got != want {
			t.Errorf("parse(%q) = %v, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "three\n", "-1\n"} {
		if _, err := parse(text); err == nil {
			t.Errorf("parse(%q) succeeded, want an error", text)
		}
	}
}

// spec: host-sensors.md#failed-units — the failed units as list-units prints them: the first
// field of each line, a marker before it skipped, the root mount's `-.mount` included.
func TestParseFailedList(t *testing.T) {
	text := "backup-a.service loaded failed failed Backup of site A\n" +
		"● b.timer loaded failed failed Hourly B\n" +
		"-.mount loaded failed failed Root Mount\n\n"
	got, err := parseList(text)
	if want := []string{"backup-a.service", "b.timer", "-.mount"}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("parseList = %q, %v; want %q", got, err, want)
	}
	if got, err := parseList(""); err != nil || len(got) != 0 {
		t.Errorf("parseList of nothing = %q, %v; want none", got, err)
	}
	if _, err := parseList("UNIT LOAD ACTIVE SUB DESCRIPTION\n"); err == nil {
		t.Errorf("parseList read a line that names no unit")
	}
}

// spec: host-sensors.md#failed-units — the results as show prints them, whatever the order
// of the units or of their properties; an absent or empty Result is none.
func TestParseResults(t *testing.T) {
	text := "Result=timeout\nId=b.timer\n\nId=backup-a.service\nResult=exit-code\n\nId=c.target\n\nId=d.service\nResult=\n"
	got, err := parseShow(text)
	want := map[string]string{"b.timer": "timeout", "backup-a.service": "exit-code", "c.target": "", "d.service": ""}
	if err != nil || !maps.Equal(got, want) {
		t.Fatalf("parseShow = %v, %v; want %v", got, err, want)
	}
	if _, err := parseShow("Id=a.service\nnonsense\n"); err == nil {
		t.Errorf("parseShow read a line that is not a property")
	}
}
