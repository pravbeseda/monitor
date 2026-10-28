package backup_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/backup"
)

var collected = time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

const full = `{"backup": "site-a", "job": "create", "failed": false,
  "last_success": "2026-08-28T04:30:00Z", "archives": 14,
  "latest_archive": "2026-08-28T04:00:00Z", "stored_bytes": 4000000000,
  "latest_archive_original_bytes": 2500000000}`

const valid = `{"backup": "site-v", "job": "create", "failed": false}`

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func collectIn(t *testing.T, dir string) ([]sensor.Measurement, error) {
	t.Helper()
	return backup.New(dir, func() time.Time { return collected }).Collect(context.Background())
}

func collectOK(t *testing.T, dir string) []sensor.Measurement {
	t.Helper()
	got, err := collectIn(t, dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return got
}

// series renders each measurement as `metric backup/job=value`, sorted, after checking what
// every measurement shares.
func series(t *testing.T, got []sensor.Measurement) []string {
	t.Helper()
	var out []string
	for _, m := range got {
		if len(m.Labels) != 2 || !m.TS.Equal(collected) {
			t.Fatalf("unexpected measurement %+v", m)
		}
		out = append(out, fmt.Sprintf("%s %s/%s=%v", m.Metric, m.Labels["backup"], m.Labels["job"], m.Value))
	}
	slices.Sort(out)
	return out
}

func assertSeries(t *testing.T, got []sensor.Measurement, want ...string) {
	t.Helper()
	slices.Sort(want)
	if have := series(t, got); !slices.Equal(have, want) {
		t.Fatalf("series\n  %v\nwant\n  %v", have, want)
	}
}

var fullSeries = []string{
	"backup.failed site-a/create=0",
	"backup.success_age_seconds site-a/create=19800",
	"backup.archives site-a/create=14",
	"backup.latest_archive_age_seconds site-a/create=21600",
	"backup.stored_bytes site-a/create=4e+09",
	"backup.latest_archive_original_bytes site-a/create=2.5e+09",
}

const validSeries = "backup.failed site-v/create=0"

func assertErrorNames(t *testing.T, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one naming %v", names)
	}
	for _, name := range names {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q does not name %q", err, name)
		}
	}
}

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads what the permissions forbid")
	}
}

// spec: backup-sensor.md#files — no directory.
func TestCollectWithoutTheDirectory(t *testing.T) {
	assertSeries(t, collectOK(t, filepath.Join(t.TempDir(), "missing")))
}

// spec: backup-sensor.md#files — an empty directory.
func TestCollectAnEmptyDirectory(t *testing.T) {
	assertSeries(t, collectOK(t, t.TempDir()))
}

// spec: backup-sensor.md#files — a symbolic link to a directory holding a valid file.
func TestCollectThroughALinkedDirectory(t *testing.T) {
	target := t.TempDir()
	write(t, target, "a.json", valid)
	link := filepath.Join(t.TempDir(), "backup")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	assertSeries(t, collectOK(t, link), validSeries)
}

// spec: backup-sensor.md#files — a regular file where the directory should be.
func TestCollectAFileInPlaceOfTheDirectory(t *testing.T) {
	path := write(t, t.TempDir(), "backup", valid)
	got, err := collectIn(t, path)
	if err == nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want nothing and an error", got, err)
	}
}

// spec: backup-sensor.md#files — a directory that cannot be listed.
func TestCollectAnUnlistableDirectory(t *testing.T) {
	skipAsRoot(t)
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	got, err := collectIn(t, dir)
	if err == nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want nothing and an error", got, err)
	}
}

// spec: backup-sensor.md#files — one file, every key present: the six metrics.
func TestCollectEveryMetricOfAFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", full)
	assertSeries(t, collectOK(t, dir), fullSeries...)
}

// spec: backup-sensor.md#files — two files of one backup, labelled by their own jobs.
func TestCollectTwoJobsOfOneBackup(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "create.json", `{"backup": "site-a", "job": "create", "failed": false}`)
	write(t, dir, "copy.json", `{"backup": "site-a", "job": "copy", "failed": true}`)
	assertSeries(t, collectOK(t, dir), "backup.failed site-a/create=0", "backup.failed site-a/copy=1")
}

// spec: backup-sensor.md#files — only regular, visible `.json` files are read, and a pipe
// does not hold the collection up.
func TestCollectReadsOnlyVisibleRegularJSONFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	if err := os.Mkdir(filepath.Join(dir, "d.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := write(t, t.TempDir(), "b.json", `{"backup": "site-l", "job": "create", "failed": true}`)
	if err := os.Symlink(other, filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "p.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, dir, ".hidden.json", `{"backup": "site-h", "job": "create", "failed": true}`)
	write(t, dir, "X.JSON", `{"backup": "site-x", "job": "create", "failed": true}`)
	write(t, dir, "x.json.tmp", `{"backup": "site-t", "job": "create", "failed": true}`)
	assertSeries(t, collectWithin(t, dir), validSeries)
}

// collectWithin fails the test instead of hanging when a collection blocks.
func collectWithin(t *testing.T, dir string) []sensor.Measurement {
	t.Helper()
	done := make(chan []sensor.Measurement, 1)
	go func() {
		got, err := backup.New(dir, func() time.Time { return collected }).Collect(context.Background())
		if err != nil {
			t.Errorf("Collect: %v", err)
		}
		done <- got
	}()
	select {
	case got := <-done:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the collection blocked")
		return nil
	}
}

// spec: backup-sensor.md#files — a file deleted between the listing and the read.
func TestCollectSkipsAFileDeletedAfterTheListing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	gone := write(t, dir, "b.json", `{"backup": "site-b", "job": "create", "failed": true}`)
	s := backup.New(dir, func() time.Time { return collected })
	backup.BeforeRead(s, func(name string) {
		if name == "b.json" {
			_ = os.Remove(gone)
		}
	})
	got, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	assertSeries(t, got, validSeries)
}

// spec: backup-sensor.md#invariants — a file is checked to be regular as it is opened: one
// replaced by a pipe after the listing is not read and does not block.
func TestCollectSkipsAFileReplacedByAPipe(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	swapped := write(t, dir, "b.json", `{"backup": "site-b", "job": "create", "failed": true}`)
	s := backup.New(dir, func() time.Time { return collected })
	backup.BeforeRead(s, func(name string) {
		if name == "b.json" {
			_ = os.Remove(swapped)
			_ = syscall.Mkfifo(swapped, 0o644)
		}
	})
	done := make(chan []sensor.Measurement, 1)
	go func() {
		got, err := s.Collect(context.Background())
		if err != nil {
			t.Errorf("Collect: %v", err)
		}
		done <- got
	}()
	select {
	case got := <-done:
		assertSeries(t, got, validSeries)
	case <-time.After(5 * time.Second):
		t.Fatal("the collection blocked")
	}
}

// spec: backup-sensor.md#files — a file that cannot be read, beside a valid one.
func TestCollectAnUnreadableFile(t *testing.T) {
	skipAsRoot(t)
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	locked := write(t, dir, "locked.json", valid)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	got, err := collectIn(t, dir)
	assertErrorNames(t, err, "locked.json")
	assertSeries(t, got, validSeries)
}

// spec: backup-sensor.md#files — two valid files of one pair are both left out.
func TestCollectLeavesOutTwoFilesOfOnePair(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	write(t, dir, "one.json", `{"backup": "site-d", "job": "copy", "failed": false}`)
	write(t, dir, "two.json", `{"backup": "site-d", "job": "copy", "failed": true}`)
	got, err := collectIn(t, dir)
	assertErrorNames(t, err, "one.json", "two.json", "site-d", "copy")
	assertSeries(t, got, validSeries)
}

// spec: backup-sensor.md#files — only valid files make a pair.
func TestCollectPairsOnlyValidFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	write(t, dir, "bad.json", `{"backup": "site-v", "job": "create", "failed": "no"}`)
	got, err := collectIn(t, dir)
	assertErrorNames(t, err, "bad.json")
	if strings.Contains(err.Error(), "a.json") {
		t.Fatalf("error %q names the valid file", err)
	}
	assertSeries(t, got, validSeries)
}

// spec: backup-sensor.md#files — two invalid files give one error naming both.
func TestCollectNamesEveryInvalidFileInOneError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", valid)
	write(t, dir, "bad1.json", "not json")
	write(t, dir, "bad2.json", "[]")
	got, err := collectIn(t, dir)
	assertErrorNames(t, err, "bad1.json", "bad2.json")
	assertSeries(t, got, validSeries)
}

// spec: backup-sensor.md#content
func TestCollectChecksTheContent(t *testing.T) {
	label := func(n int, r string) string { return strings.Repeat(r, n) }
	pad := func(size int) string {
		body := `{"backup": "site-p", "job": "create", "failed": false, "pad": ""}`
		return strings.Replace(body, `""`, `"`+strings.Repeat("x", size-len(body))+`"`, 1)
	}
	cases := []struct {
		name    string
		content string
		want    []string // the file's own series; nil means an error naming it
	}{
		{"exactly 65 536 bytes", pad(65536), []string{"backup.failed site-p/create=0"}},
		{"65 537 bytes", pad(65537), nil},
		{"not JSON", "backup: site-a", nil},
		{"an array", "[]", nil},
		{"a string", `"site-a"`, nil},
		{"a number", "14", nil},
		{"trailing whitespace", valid2 + "\n  \n", []string{"backup.failed site-w/create=1"}},
		{"a second object", valid2 + valid2, nil},
		{"invalid UTF-8", "{\"backup\": \"site-\xff\", \"job\": \"create\", \"failed\": true}", nil},
		{"a byte order mark", "\xef\xbb\xbf" + valid2, nil},
		{"backup missing", `{"job": "create", "failed": true}`, nil},
		{"job missing", `{"backup": "site-w", "failed": true}`, nil},
		{"failed missing", `{"backup": "site-w", "job": "create"}`, nil},
		{"backup null", `{"backup": null, "job": "create", "failed": true}`, nil},
		{"job null", `{"backup": "site-w", "job": null, "failed": true}`, nil},
		{"failed null", `{"backup": "site-w", "job": "create", "failed": null}`, nil},
		{"failed a string", `{"backup": "site-w", "job": "create", "failed": "false"}`, nil},
		{"backup empty", `{"backup": "", "job": "create", "failed": true}`, nil},
		{"backup of 65 characters", `{"backup": "` + label(65, "a") + `", "job": "create", "failed": true}`, nil},
		{"backup of 64 Cyrillic characters", `{"backup": "` + label(64, "д") + `", "job": "create", "failed": true}`,
			[]string{"backup.failed " + label(64, "д") + "/create=1"}},
		{"job holding a newline", `{"backup": "site-w", "job": "cre\nate", "failed": true}`, nil},
		{"backup carried byte for byte", `{"backup": "Сайт — A", "job": "create", "failed": true}`, []string{"backup.failed Сайт — A/create=1"}},
		{"archives 14.0", withArchives("14.0"), nil},
		{"archives 14.5", withArchives("14.5"), nil},
		{"archives 1e3", withArchives("1e3"), nil},
		{"archives -1", withArchives("-1"), nil},
		{"archives a string", withArchives(`"14"`), nil},
		{"archives 2^53+1", withArchives("9007199254740993"), nil},
		{"archives 2^53", withArchives("9007199254740992"),
			[]string{"backup.failed site-w/create=1", "backup.archives site-w/create=9.007199254740992e+15"}},
		{"a space for the T", withSuccess("2026-08-28 04:30:00Z"), nil},
		{"no zone", withSuccess("2026-08-28T04:30:00"), nil},
		{"an offset without a colon", withSuccess("2026-08-28T04:30:00+0300"), nil},
		{"a word", withSuccess("yesterday"), nil},
		{"failed twice", `{"backup": "site-w", "job": "create", "failed": true, "failed": false}`, nil},
		{"a key of another case", `{"backup": "site-w", "job": "create", "Failed": true, "failed": false}`,
			[]string{"backup.failed site-w/create=0"}},
		{"an unlisted key", `{"backup": "site-w", "job": "create", "failed": true, "host": "b"}`, []string{"backup.failed site-w/create=1"}},
		{"figures null", `{"backup": "site-w", "job": "create", "failed": true, "archives": null, "stored_bytes": null}`,
			[]string{"backup.failed site-w/create=1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "a.json", valid)
			write(t, dir, "case.json", c.content)
			got, err := collectIn(t, dir)
			if c.want == nil {
				assertErrorNames(t, err, "case.json")
				assertSeries(t, got, validSeries)
				return
			}
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			assertSeries(t, got, append([]string{validSeries}, c.want...)...)
		})
	}
}

const valid2 = `{"backup": "site-w", "job": "create", "failed": true}`

func withArchives(raw string) string {
	return `{"backup": "site-w", "job": "create", "failed": true, "archives": ` + raw + `}`
}

func withSuccess(at string) string {
	return `{"backup": "site-w", "job": "create", "failed": true, "last_success": "` + at + `"}`
}

// spec: backup-sensor.md#values
func TestCollectValues(t *testing.T) {
	at := func(d time.Duration) string { return collected.Add(d).Format(time.RFC3339Nano) }
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"failed true", `{"backup": "b", "job": "j", "failed": true}`, []string{"backup.failed b/j=1"}},
		{"failed false", `{"backup": "b", "job": "j", "failed": false}`, []string{"backup.failed b/j=0"}},
		{"whole seconds since the last success", `{"backup": "b", "job": "j", "failed": false, "last_success": "` + at(-(26*time.Hour + 900*time.Millisecond)) + `"}`,
			[]string{"backup.failed b/j=0", "backup.success_age_seconds b/j=93600"}},
		{"an offset", `{"backup": "b", "job": "j", "failed": false, "last_success": "2026-08-28T12:00:00+03:00"}`,
			[]string{"backup.failed b/j=0", "backup.success_age_seconds b/j=3600"}},
		{"an age of 0", `{"backup": "b", "job": "j", "failed": false, "last_success": "` + at(0) + `"}`,
			[]string{"backup.failed b/j=0", "backup.success_age_seconds b/j=0"}},
		{"never succeeded", `{"backup": "b", "job": "j", "failed": true}`, []string{"backup.failed b/j=1"}},
		{"an empty count beside a date", `{"backup": "b", "job": "j", "failed": false, "archives": 0, "latest_archive": "` + at(-3*time.Hour) + `"}`,
			[]string{"backup.failed b/j=0", "backup.archives b/j=0", "backup.latest_archive_age_seconds b/j=10800"}},
		{"dates ahead of the clock", `{"backup": "b", "job": "j", "failed": false, "archives": 3, "last_success": "` + at(500*time.Millisecond) + `", "latest_archive": "` + at(24*time.Hour) + `"}`,
			[]string{"backup.failed b/j=0", "backup.archives b/j=3"}},
		{"sizes unchanged", `{"backup": "b", "job": "j", "failed": false, "stored_bytes": 4000000000, "latest_archive_original_bytes": 2500000000}`,
			[]string{"backup.failed b/j=0", "backup.stored_bytes b/j=4e+09", "backup.latest_archive_original_bytes b/j=2.5e+09"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "a.json", c.content)
			assertSeries(t, collectOK(t, dir), c.want...)
		})
	}
}

// spec: backup-sensor.md#values — the file's modification time is not the job's date.
func TestCollectAgesByTheJobsDateNotTheFiles(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "a.json", `{"backup": "b", "job": "j", "failed": false, "last_success": "`+
		collected.Add(-72*time.Hour).Format(time.RFC3339)+`"}`)
	if err := os.Chtimes(path, collected.Add(-time.Second), collected.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	assertSeries(t, collectOK(t, dir), "backup.failed b/j=0", "backup.success_age_seconds b/j=259200")
}

// spec: backup-sensor.md#applicability
func TestApplicableOnLinuxAndMacOS(t *testing.T) {
	s := backup.New(t.TempDir(), time.Now)
	if s.Name() != "backup" || !s.Applicable() {
		t.Fatalf("sensor %q applicable %v, want backup and true", s.Name(), s.Applicable())
	}
}

// spec: backup-sensor.md#the-status-file — the directory of each system.
func TestDirectoryOfEachSystem(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":  "/var/lib/monitor-agent/backup",
		"darwin": "/usr/local/var/lib/monitor-agent/backup",
	} {
		if got := backup.DirectoryOf(goos); got != want {
			t.Errorf("DirectoryOf(%s) = %q, want %q", goos, got, want)
		}
	}
}
