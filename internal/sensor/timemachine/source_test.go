package timemachine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// A synthetic export in the shape the preferences service prints, with the keys the sensor
// does not read around the ones it does.
const exported = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>AutoBackup</key>
	<true/>
	<key>BackupAlias</key>
	<data>
	AAAAAAEAAgAA
	</data>
	<key>Destinations</key>
	<array>
		<dict>
			<key>AttemptDates</key>
			<array>
				<date>2026-09-21T23:00:00Z</date>
			</array>
			<key>BytesUsed</key>
			<integer>1000000000</integer>
			<key>LastKnownVolumeName</key>
			<string>Backups A</string>
			<key>SnapshotDates</key>
			<array>
				<date>2026-09-20T08:00:00Z</date>
				<date>2026-09-21T08:00:00Z</date>
			</array>
		</dict>
		<dict>
			<key>SnapshotDates</key>
			<array/>
			<key>LastKnownVolumeName</key>
			<string>Резервные копии — Mac</string>
		</dict>
		<dict>
			<key>SnapshotDates</key>
			<array>
				<date>2026-09-21T09:00:00Z</date>
			</array>
		</dict>
	</array>
	<key>RequiresACPower</key>
	<false/>
</dict>
</plist>
`

func at(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// spec: timemachine-sensor.md#measurements — each destination's name, byte for byte, and the
// backups it holds.
func TestParseReadsTheDestinations(t *testing.T) {
	got, err := parse([]byte(exported))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []Destination{
		{Name: "Backups A", Backups: []time.Time{at("2026-09-20T08:00:00Z"), at("2026-09-21T08:00:00Z")}},
		{Name: "Резервные копии — Mac"},
		{Backups: []time.Time{at("2026-09-21T09:00:00Z")}},
	}
	if len(got) != len(want) {
		t.Fatalf("destinations %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Name != want[i].Name || len(got[i].Backups) != len(want[i].Backups) {
			t.Fatalf("destination %d = %+v, want %+v", i, got[i], want[i])
		}
		for j := range want[i].Backups {
			if !got[i].Backups[j].Equal(want[i].Backups[j]) {
				t.Fatalf("destination %d backup %d = %v, want %v", i, j, got[i].Backups[j], want[i].Backups[j])
			}
		}
	}
}

// spec: timemachine-sensor.md#age — preferences without destinations hold none.
func TestParseWithoutDestinations(t *testing.T) {
	for name, body := range map[string]string{
		"no key":     `<plist version="1.0"><dict><key>AutoBackup</key><false/></dict></plist>`,
		"empty list": `<plist version="1.0"><dict><key>Destinations</key><array/></dict></plist>`,
	} {
		if got, err := parse([]byte(body)); err != nil || len(got) != 0 {
			t.Errorf("%s: parse = %+v, %v; want none and no error", name, got, err)
		}
	}
}

// spec: timemachine-sensor.md#age — an empty answer is unread preferences, not an empty list.
func TestParseRefusesAnEmptyDictionary(t *testing.T) {
	for _, body := range []string{
		`<plist version="1.0"><dict/></plist>`,
		`<plist version="1.0"><dict></dict></plist>`,
	} {
		if got, err := parse([]byte(body)); err == nil {
			t.Errorf("parse(%s) = %+v, want an error", body, got)
		}
	}
}

// spec: timemachine-sensor.md#age — preferences that cannot be parsed are an error.
func TestParseRefusesWhatItCannotRead(t *testing.T) {
	for name, body := range map[string]string{
		"not xml":             "defaults: domain not found",
		"no plist":            `<dict><key>Destinations</key><array/></dict>`,
		"truncated":           `<plist version="1.0"><dict><key>Destinations</key><array><dict>`,
		"destinations a map":  `<plist version="1.0"><dict><key>Destinations</key><dict/></dict></plist>`,
		"a date that is not":  `<plist version="1.0"><dict><key>Destinations</key><array><dict><key>SnapshotDates</key><array><date>yesterday</date></array></dict></array></dict></plist>`,
		"a root array":        `<plist version="1.0"><array/></plist>`,
		"an entry not a dict": `<plist version="1.0"><dict><key>Destinations</key><array><string>x</string></array></dict></plist>`,
		"a name not a string": `<plist version="1.0"><dict><key>Destinations</key><array><dict><key>LastKnownVolumeName</key><integer>1</integer></dict></array></dict></plist>`,
		"a string as a date":  `<plist version="1.0"><dict><key>Destinations</key><array><dict><key>SnapshotDates</key><array><string>2026-09-21T09:00:00Z</string></array></dict></array></dict></plist>`,
	} {
		if got, err := parse([]byte(body)); err == nil {
			t.Errorf("%s: parse = %+v, want an error", name, got)
		}
	}
}

// spec: timemachine-sensor.md#age — no preferences file means Time Machine was never set up,
// and the preferences service is not asked.
func TestPreferencesWithoutAFile(t *testing.T) {
	asked := false
	source := preferences{
		path:   filepath.Join(t.TempDir(), "missing.plist"),
		export: func(context.Context) ([]byte, error) { asked = true; return nil, nil },
	}
	if got, err := source.destinations(context.Background()); err != nil || len(got) != 0 || asked {
		t.Fatalf("got %+v, %v, asked %v; want nothing, no error, not asked", got, err, asked)
	}
}

// spec: timemachine-sensor.md#age — with the file present, what the service answers is parsed.
func TestPreferencesReadThroughTheService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.plist")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	answer := func(body string, err error) preferences {
		return preferences{path: path, export: func(context.Context) ([]byte, error) { return []byte(body), err }}
	}
	if got, err := answer(exported, nil).destinations(context.Background()); err != nil || len(got) != 3 {
		t.Fatalf("got %+v, %v; want three destinations", got, err)
	}
	if _, err := answer(`<plist version="1.0"><dict/></plist>`, nil).destinations(context.Background()); err == nil {
		t.Fatal("an empty answer beside an existing file succeeded, want an error")
	}
	if _, err := answer("", errors.New("exit status 1")).destinations(context.Background()); err == nil {
		t.Fatal("a failed export succeeded, want an error")
	}
	unreachable := preferences{path: filepath.Join(path, "below-a-file"), export: answer(exported, nil).export}
	if _, err := unreachable.destinations(context.Background()); err == nil {
		t.Fatal("a file that cannot be checked succeeded, want an error")
	}
}

// spec: timemachine-sensor.md#applicability — a Mac has a source and reads its own
// preferences; elsewhere there is none. The platform source is thin but easy to break, so
// this checks it against the machine the tests run on.
func TestSystemSourceOnThisMachine(t *testing.T) {
	source := System()
	if runtime.GOOS != "darwin" {
		if source != nil {
			t.Fatal("a source outside macOS, want none")
		}
		return
	}
	if source == nil {
		t.Fatal("no source on macOS")
	}
	if _, err := source(context.Background()); err != nil {
		t.Fatalf("reading this Mac's Time Machine preferences: %v", err)
	}
}
