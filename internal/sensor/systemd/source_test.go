package systemd

import (
	"os"
	"path/filepath"
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
