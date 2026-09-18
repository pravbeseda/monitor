package disk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/disk"
)

var collected = time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

// fake is a mount table the test controls: usage is looked up by mount point, and a
// mount with no usage stands for one that vanished or cannot be read.
type fake struct {
	mounts []disk.Mount
	usage  map[string]disk.Usage
	err    error
}

func (f fake) Mounts() ([]disk.Mount, error) { return f.mounts, f.err }

func (f fake) Usage(mount string) (disk.Usage, error) {
	usage, ok := f.usage[mount]
	if !ok {
		return disk.Usage{}, errors.New("no such volume")
	}
	return usage, nil
}

func collect(t *testing.T, source disk.Source, allowed ...string) []sensor.Measurement {
	t.Helper()
	return collectWith(t, source, disk.Settings{Filesystems: allowed})
}

func collectWith(t *testing.T, source disk.Source, settings disk.Settings) []sensor.Measurement {
	t.Helper()
	s := disk.New(source, func() disk.Settings { return settings }, func() time.Time { return collected })
	got, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return got
}

func byMetric(ms []sensor.Measurement, metric string) []sensor.Measurement {
	var out []sensor.Measurement
	for _, m := range ms {
		if m.Metric == metric {
			out = append(out, m)
		}
	}
	return out
}

var internalRoot = fake{
	mounts: []disk.Mount{{Path: "/", FS: "apfs"}},
	usage:  map[string]disk.Usage{"/": {TotalBytes: 1000, AvailBytes: 250}},
}

// spec: disk-sensor.md#enumeration
func TestCollectsAllowedVolume(t *testing.T) {
	got := collect(t, internalRoot, "apfs")

	if len(got) != 2 {
		t.Fatalf("measurements = %+v, want free_bytes and free_pct", got)
	}
	bytes := byMetric(got, "disk.free_bytes")
	pct := byMetric(got, "disk.free_pct")
	if len(bytes) != 1 || bytes[0].Value != 250 {
		t.Errorf("free_bytes = %+v, want the available bytes", bytes)
	}
	if len(pct) != 1 || pct[0].Value != 25 {
		t.Errorf("free_pct = %+v, want 25", pct)
	}
	if !bytes[0].TS.Equal(collected) {
		t.Errorf("ts = %v, want the collection time", bytes[0].TS)
	}
}

// spec: disk-sensor.md#enumeration — a type outside the allow-list is skipped.
func TestSkipsFilesystemOutsideAllowList(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{{Path: "/", FS: "apfs"}, {Path: "/dev", FS: "devfs"}},
		usage:  map[string]disk.Usage{"/": {TotalBytes: 1000, AvailBytes: 250}, "/dev": {TotalBytes: 10, AvailBytes: 5}},
	}

	for _, m := range collect(t, source, "apfs") {
		if m.Labels["mount"] == "/dev" {
			t.Errorf("collected %+v, want devfs skipped", m)
		}
	}
}

// spec: disk-sensor.md#enumeration — a volume of zero total blocks has no percentage.
func TestSkipsEmptyVolume(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{{Path: "/empty", FS: "apfs"}},
		usage:  map[string]disk.Usage{"/empty": {TotalBytes: 0, AvailBytes: 0}},
	}

	if got := collect(t, source, "apfs"); len(got) != 0 {
		t.Errorf("measurements = %+v, want none for a volume of no size", got)
	}
}

// spec: disk-sensor.md#enumeration — one unreadable volume never costs the others.
func TestKeepsCollectingAfterAnUnreadableVolume(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{{Path: "/gone", FS: "apfs"}, {Path: "/", FS: "apfs"}},
		usage:  map[string]disk.Usage{"/": {TotalBytes: 1000, AvailBytes: 250}},
	}

	got := collect(t, source, "apfs")

	if len(got) != 2 {
		t.Fatalf("measurements = %+v, want the readable volume collected", got)
	}
	for _, m := range got {
		if m.Labels["mount"] != "/" {
			t.Errorf("collected %+v, want only the readable volume", m)
		}
	}
}

// spec: disk-sensor.md#enumeration — the mount table itself failing is an error.
func TestReportsUnreadableMountTable(t *testing.T) {
	s := disk.New(fake{err: errors.New("no mount table")},
		func() disk.Settings { return disk.Settings{Filesystems: []string{"apfs"}} }, time.Now)

	got, err := s.Collect(context.Background())

	if err == nil {
		t.Fatalf("Collect returned %+v, want an error", got)
	}
	if len(got) != 0 {
		t.Errorf("measurements = %+v, want none", got)
	}
}

// spec: disk-sensor.md#enumeration — nothing allowed means nothing collected.
func TestCollectsNothingWhenNoVolumeIsAllowed(t *testing.T) {
	if got := collect(t, internalRoot, "ext4"); len(got) != 0 {
		t.Errorf("measurements = %+v, want none", got)
	}
}

// spec: disk-sensor.md#labels
func TestLabelsIdentifyTheVolume(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{{Path: "/Volumes/backup-a", FS: "APFS", Removable: true}},
		usage:  map[string]disk.Usage{"/Volumes/backup-a": {TotalBytes: 2000, AvailBytes: 1000}},
	}

	for _, m := range collect(t, source, "apfs") {
		want := map[string]string{"mount": "/Volumes/backup-a", "fs": "apfs", "removable": "true"}
		for label, value := range want {
			if m.Labels[label] != value {
				t.Errorf("%s label %s = %q, want %q", m.Metric, label, m.Labels[label], value)
			}
		}
		if len(m.Labels) != len(want) {
			t.Errorf("%s labels = %v, want exactly %v", m.Metric, m.Labels, want)
		}
	}
}

// spec: disk-sensor.md#labels — an internal volume says so rather than omitting the label.
func TestInternalVolumeIsLabelledNotRemovable(t *testing.T) {
	got := collect(t, internalRoot, "apfs")

	if got[0].Labels["removable"] != "false" {
		t.Errorf("removable = %q, want \"false\"", got[0].Labels["removable"])
	}
}

// spec: disk-sensor.md#applicability
func TestSensorIsApplicableEverywhere(t *testing.T) {
	s := disk.New(internalRoot, func() disk.Settings { return disk.Settings{} }, time.Now)

	if s.Name() != "disk" || !s.Applicable() {
		t.Errorf("manifest entry = %q applicable=%v, want disk applicable", s.Name(), s.Applicable())
	}
}

func TestFreePercentIsRoundedToTwoDecimals(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{{Path: "/", FS: "ext4"}},
		usage:  map[string]disk.Usage{"/": {TotalBytes: 3000, AvailBytes: 1000}},
	}

	pct := byMetric(collect(t, source, "ext4"), "disk.free_pct")

	if len(pct) != 1 || pct[0].Value != 33.33 {
		t.Errorf("free_pct = %+v, want 33.33", pct)
	}
}

// spec: disk-sensor.md#enumeration — the hub's skip list names what is not worth watching.
func TestSkipsMountsUnderASkippedPrefix(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{
			{Path: "/", FS: "apfs", Container: "disk3"},
			{Path: "/System/Volumes/Hardware", FS: "apfs", Container: "disk1"},
		},
		usage: map[string]disk.Usage{
			"/":                        {TotalBytes: 1000, AvailBytes: 250},
			"/System/Volumes/Hardware": {TotalBytes: 100, AvailBytes: 50},
		},
	}

	got := collectWith(t, source, disk.Settings{
		Filesystems: []string{"apfs"},
		SkipMounts:  []string{"/System/Volumes/"},
	})

	if len(got) != 2 {
		t.Fatalf("measurements = %+v, want only the root volume", got)
	}
	for _, m := range got {
		if m.Labels["mount"] != "/" {
			t.Errorf("collected %+v, want the skipped prefix left out", m)
		}
	}
}

// spec: disk-sensor.md#enumeration — volumes of one container report the same free space,
// so the shortest mount point stands for all of them.
func TestKeepsOneVolumePerContainer(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{
			{Path: "/Volumes/backup-a", FS: "apfs", Container: "disk5", Removable: true},
			{Path: "/Volumes/data-a", FS: "apfs", Container: "disk5", Removable: true},
			{Path: "/", FS: "apfs", Container: "disk3"},
		},
		usage: map[string]disk.Usage{
			"/Volumes/backup-a": {TotalBytes: 4000, AvailBytes: 130},
			"/Volumes/data-a":   {TotalBytes: 4000, AvailBytes: 130},
			"/":                 {TotalBytes: 1000, AvailBytes: 250},
		},
	}

	got := collectWith(t, source, disk.Settings{Filesystems: []string{"apfs"}})

	mounts := map[string]bool{}
	for _, m := range got {
		mounts[m.Labels["mount"]] = true
	}
	if len(mounts) != 2 || !mounts["/"] || !mounts["/Volumes/data-a"] {
		t.Errorf("mounts = %v, want the root and the shortest mount of the container", mounts)
	}
}

// A container whose volumes cannot be told apart by length is still resolved the same way
// on every collection, or a series would change identity between ticks.
func TestContainerChoiceIsStable(t *testing.T) {
	source := fake{
		mounts: []disk.Mount{
			{Path: "/Volumes/bbb", FS: "apfs", Container: "disk5"},
			{Path: "/Volumes/aaa", FS: "apfs", Container: "disk5"},
		},
		usage: map[string]disk.Usage{
			"/Volumes/bbb": {TotalBytes: 4000, AvailBytes: 130},
			"/Volumes/aaa": {TotalBytes: 4000, AvailBytes: 130},
		},
	}

	for range 3 {
		got := collectWith(t, source, disk.Settings{Filesystems: []string{"apfs"}})
		if len(got) != 2 || got[0].Labels["mount"] != "/Volumes/aaa" {
			t.Fatalf("measurements = %+v, want /Volumes/aaa every time", got)
		}
	}
}

// spec: disk-sensor.md#enumeration — a mounted snapshot is one more member of its container:
// it loses to a shorter watched mount point, and stands for the container when it is the
// only one watched.
func TestSnapshotCountsAsAMemberOfItsContainer(t *testing.T) {
	const localSnapshot = "/Volumes/com.apple.TimeMachine.localsnapshots/Backups.backupdb/laptop-a/2026-01-01-120000/Data"
	const backupSnapshot = "/Volumes/.timemachine/00000000-0000-0000-0000-000000000000/2026-01-01-120000.backup"
	// The backup disk's own volume is skipped, leaving the snapshot its container's only member.
	source := fake{
		mounts: []disk.Mount{
			{Path: "/", FS: "apfs", Container: "disk3"},
			{Path: localSnapshot, FS: "apfs", Container: "disk3"},
			{Path: "/System/Volumes/backup-b", FS: "apfs", Container: "disk7"},
			{Path: backupSnapshot, FS: "apfs", Container: "disk7"},
		},
		usage: map[string]disk.Usage{
			"/":                        {TotalBytes: 1000, AvailBytes: 250},
			localSnapshot:              {TotalBytes: 1000, AvailBytes: 250},
			"/System/Volumes/backup-b": {TotalBytes: 4000, AvailBytes: 130},
			backupSnapshot:             {TotalBytes: 4000, AvailBytes: 130},
		},
	}

	got := collectWith(t, source, disk.Settings{
		Filesystems: []string{"apfs"},
		SkipMounts:  []string{"/System/Volumes/"},
	})

	mounts := map[string]bool{}
	for _, m := range got {
		mounts[m.Labels["mount"]] = true
	}
	if len(mounts) != 2 || !mounts["/"] || !mounts[backupSnapshot] {
		t.Errorf("mounts = %v, want the root and the snapshot standing alone for its container", mounts)
	}
}
