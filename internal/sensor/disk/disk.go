// Package disk collects how much space each mounted volume has left
// (docs/specs/disk-sensor.md).
package disk

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
)

const (
	metricFreeBytes = "disk.free_bytes"
	metricFreePct   = "disk.free_pct"
)

// Mount is one entry of the machine's mount table.
type Mount struct {
	Path      string
	FS        string
	Removable bool
	// Container groups volumes that share one pool of free space: an APFS container, a
	// device with bind mounts. Empty means the mount stands alone.
	Container string
}

// Usage is what one volume reports about its size. Available excludes the blocks a
// filesystem reserves for root: they are not space the machine can use.
type Usage struct {
	TotalBytes uint64
	AvailBytes uint64
}

// Source is the platform-specific half of the sensor.
type Source interface {
	Mounts() ([]Mount, error)
	Usage(mount string) (Usage, error)
}

// Settings are the parts of the hub's configuration this sensor acts on.
type Settings struct {
	// Filesystems is the allow-list of filesystem types; nothing else is collected.
	Filesystems []string
	// SkipMounts are mount-point prefixes not worth watching.
	SkipMounts []string
}

// Sensor reads free space per volume. Its settings come from the hub and change while the
// agent runs, so they are read through a function rather than copied once.
type Sensor struct {
	source   Source
	settings func() Settings
	now      func() time.Time
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over a mount source, the settings the hub last delivered and the
// agent's clock.
func New(source Source, settings func() Settings, now func() time.Time) *Sensor {
	return &Sensor{source: source, settings: settings, now: now}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return "disk" }

// Applicable is true everywhere: every machine has at least one volume.
func (s *Sensor) Applicable() bool { return true }

// Collect returns what it could read: a volume that vanished or refused to answer costs
// only itself.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	mounts, err := s.source.Mounts()
	if err != nil {
		return nil, fmt.Errorf("read the mount table: %w", err)
	}

	settings := s.settings()

	at := s.now()
	var out []sensor.Measurement
	for _, mount := range representatives(watched(mounts, settings)) {
		usage, err := s.source.Usage(mount.Path)
		if err != nil || usage.TotalBytes == 0 {
			continue
		}
		labels := map[string]string{
			"mount":     mount.Path,
			"fs":        strings.ToLower(mount.FS),
			"removable": strconv.FormatBool(mount.Removable),
		}
		free := float64(usage.AvailBytes)
		percent := sensor.Round2(free / float64(usage.TotalBytes) * 100)
		out = append(out,
			measurement(metricFreeBytes, labels, free, at),
			measurement(metricFreePct, labels, percent, at))
	}
	return out, nil
}

// watched keeps the volumes worth reading: the filesystem type is on the hub's allow-list
// and the mount point is not under a skipped prefix.
func watched(mounts []Mount, settings Settings) []Mount {
	allowed := make(map[string]bool, len(settings.Filesystems))
	for _, fs := range settings.Filesystems {
		allowed[strings.ToLower(fs)] = true
	}

	kept := make([]Mount, 0, len(mounts))
	for _, mount := range mounts {
		if allowed[strings.ToLower(mount.FS)] && !skipped(mount.Path, settings.SkipMounts) {
			kept = append(kept, mount)
		}
	}
	return kept
}

// representatives keeps one volume per container: the shortest mount point, and the first
// alphabetically when two are equally short, so the choice is the same on every collection.
func representatives(mounts []Mount) []Mount {
	kept := make([]Mount, 0, len(mounts))
	byContainer := map[string]int{}
	for _, mount := range mounts {
		if mount.Container == "" {
			kept = append(kept, mount)
			continue
		}
		at, seen := byContainer[mount.Container]
		if !seen {
			byContainer[mount.Container] = len(kept)
			kept = append(kept, mount)
			continue
		}
		if shorter(mount.Path, kept[at].Path) {
			kept[at] = mount
		}
	}
	return kept
}

func skipped(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func shorter(candidate, current string) bool {
	if len(candidate) != len(current) {
		return len(candidate) < len(current)
	}
	return candidate < current
}

func measurement(metric string, labels map[string]string, value float64, at time.Time) sensor.Measurement {
	return sensor.Measurement{
		Metric: metric,
		Labels: maps.Clone(labels),
		Value:  value,
		TS:     at,
	}
}
