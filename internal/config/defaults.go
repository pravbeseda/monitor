package config

import "github.com/pravbeseda/monitor/internal/i18n"

// Product defaults: how the tool behaves out of the box. They disclose nothing about any
// installation, so they live in code, and every layer of the file overrides them
// (ADR 0007 rule 1).

const defaultBaseTick = "5m"

var defaultFilesystems = []string{"apfs", "ext4", "xfs", "btrfs", "zfs", "ntfs"}

// Mount points nobody watches: the system volumes of a Mac and the simulator images.
// They describe the tool's behaviour out of the box, not any installation.
var defaultSkipMounts = []string{"/System/Volumes/", "/Library/Developer/CoreSimulator/"}

// The digest goes out at nine in the morning UTC: an hour names no installation, and UTC
// names no place. A deployment writes its own zone in its own file.
const (
	defaultDigestAt   = "09:00"
	defaultDigestZone = "UTC"
	defaultChannel    = ChannelLog
	defaultLocale     = i18n.English
)

var defaultSensors = map[string]fileSensor{
	"disk":    {Interval: "15m"},
	"load":    {Interval: "5m"},
	"memory":  {Interval: "5m"},
	"uptime":  {Interval: "15m"},
	"systemd": {Interval: "15m"},
}

var defaultClasses = map[string]fileClass{
	"laptop": {
		Profile:      []string{"disk", "load", "memory", "uptime"},
		SilenceAfter: "48h",
		Sensors:      map[string]fileSensor{"disk": {Interval: "1h"}},
	},
	"server": {
		Profile:      []string{"disk", "load", "memory", "uptime", "systemd"},
		SilenceAfter: "10m",
	},
}
