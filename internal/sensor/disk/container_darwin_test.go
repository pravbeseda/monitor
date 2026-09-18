package disk

import "testing"

// spec: disk-sensor.md#enumeration — only volumes that share a pool are collapsed, and on
// macOS that is APFS alone: two partitions of one disk are two pools. A mounted snapshot
// shares the pool of the volume it was taken from.
func TestContainerGroupsOnlyAPFSVolumes(t *testing.T) {
	tests := []struct {
		name       string
		filesystem string
		device     string
		want       string
	}{
		{"two volumes of one APFS container", "apfs", "/dev/disk3s1s1", "disk3"},
		{"the other volume of that container", "apfs", "/dev/disk3s5", "disk3"},
		{"an NTFS partition", "ntfs", "/dev/disk2s1", ""},
		{"the next NTFS partition of the same disk", "ntfs", "/dev/disk2s2", ""},
		{"a volume that is not on a device", "apfs", "map auto_home", ""},
		{"a local snapshot joins the container it was taken from", "apfs", "com.apple.TimeMachine.2026-01-01-120000.local@/dev/disk3s5", "disk3"},
		{"a backup snapshot joins its backup disk", "apfs", "com.apple.TimeMachine.2026-01-01-120000.backup@/dev/disk7s3", "disk7"},
		{"a source naming a device only before its @", "apfs", "/dev/disk3s5@snapshot", ""},
		{"a snapshot of a filesystem that is not APFS", "hfs", "snapshot@/dev/disk2s1", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := container(tc.filesystem, tc.device); got != tc.want {
				t.Errorf("container(%q, %q) = %q, want %q", tc.filesystem, tc.device, got, tc.want)
			}
		})
	}
}
