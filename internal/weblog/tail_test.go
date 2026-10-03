package weblog_test

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/weblog"
)

// logLine is a combined line at the given time whose target names it, so that a test can
// tell which lines were read.
func logLine(name string, at time.Time) string {
	return `203.0.113.7 - - [` + at.Format("02/Jan/2006:15:04:05 -0700") + `] "GET /` + name +
		` HTTP/1.1" 200 5 "-" "` + browser + `"` + "\n"
}

func appendTo(t *testing.T, path string, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeGzip(t *testing.T, path string, text string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// read collects the targets of the lines one Read hands over.
func read(t *testing.T, tail *weblog.Tail) []string {
	t.Helper()
	var got []string
	if err := tail.Read(func(line string) {
		r, ok := weblog.Parse(line)
		if !ok {
			t.Fatalf("handed an unparsable line %q", line)
		}
		got = append(got, strings.TrimPrefix(r.Target, "/"))
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func open(t *testing.T, path string) *weblog.Tail {
	t.Helper()
	tail, back := openBoth(t, path)
	back.Close()
	return tail
}

func openBoth(t *testing.T, path string) (*weblog.Tail, *weblog.Back) {
	t.Helper()
	tail, back, err := weblog.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(tail.Close)
	return tail, back
}

func same(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("read %q, want %q", got, want)
	}
}

// spec: site-traffic.md#rotation — reading starts from the end of the log as it was found.
func TestTailStartsAtTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, logLine("before", clock))
	tail := open(t, path)
	same(t, read(t, tail))
	appendTo(t, path, logLine("a", clock)+logLine("b", clock))
	same(t, read(t, tail), "a", "b")
	same(t, read(t, tail))
}

// spec: site-traffic.md#reading — a line written only in part waits for its end.
func TestTailWaitsForALineToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, "")
	tail := open(t, path)
	whole := logLine("a", clock)
	appendTo(t, path, whole[:20])
	same(t, read(t, tail))
	appendTo(t, path, whole[20:])
	same(t, read(t, tail), "a")
}

// spec: site-traffic.md#rotation — a renamed log is drained until a collection finds it
// unchanged with a new file in its place, then the new one is read from its start.
func TestTailFollowsARenamedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, "")
	tail := open(t, path)
	appendTo(t, path, logLine("a", clock))
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, logLine("new", clock))
	// nginx keeps writing to the renamed file until it is told to reopen.
	appendTo(t, path+".1", logLine("late", clock))
	same(t, read(t, tail), "a", "late", "new")
	appendTo(t, path+".1", logLine("later", clock))
	appendTo(t, path, logLine("next", clock))
	same(t, read(t, tail), "later", "next")
	same(t, read(t, tail))
}

// spec: site-traffic.md#rotation — a renamed file compressed and deleted while held open is
// still read through the open file.
func TestTailReadsARenamedFileAfterItIsDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, "")
	tail := open(t, path)
	appendTo(t, path, logLine("a", clock))
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path+".1", logLine("b", clock))
	if err := os.Remove(path + ".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, logLine("new", clock))
	same(t, read(t, tail), "a", "b", "new")
}

// spec: site-traffic.md#rotation — two rotations between collections: the renamed file is
// followed by what it is, not by its name.
func TestTailFollowsAFileThroughTwoRotations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, "")
	tail := open(t, path)
	appendTo(t, path, logLine("a", clock))
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, logLine("b", clock))
	if err := os.Rename(path+".1", path+".2"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, logLine("c", clock))
	same(t, read(t, tail), "a", "c")
}

// spec: site-traffic.md#rotation — a log truncated in place: what <log>.1 holds past the
// point read, then the file from its start.
func TestTailFollowsCopyTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, logLine("before", clock))
	tail := open(t, path)
	appendTo(t, path, logLine("a", clock))
	same(t, read(t, tail), "a")
	appendTo(t, path, logLine("copied", clock))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, logLine("new", clock))
	same(t, read(t, tail), "copied", "new")
}

// spec: site-traffic.md#reading — a missing log is an error naming its path.
func TestOpenNamesAMissingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.log")
	_, _, err := weblog.Open(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error %v does not name %s", err, path)
	}
}

// readBack reads back from a log and collects the targets it hands over.
func readBack(t *testing.T, back *weblog.Back, since time.Time) ([]string, time.Time) {
	t.Helper()
	var got []string
	reached, err := back.Read(since, func(r weblog.Request) {
		got = append(got, strings.TrimPrefix(r.Target, "/"))
	})
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	slices.Sort(got)
	return got, reached
}

// spec: site-traffic.md#the-24-hour-window — reading back covers the day from the current
// log and the rotated ones, plain or compressed, and stops at the first that begins before.
func TestReadBackCoversTheDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	since := clock.Add(-24 * time.Hour)
	writeGzip(t, path+".3.gz", logLine("too-old", since.Add(-3*time.Hour)))
	writeGzip(t, path+".2.gz", logLine("old", since.Add(-time.Hour))+logLine("c", since.Add(time.Hour)))
	appendTo(t, path+".1", logLine("b", since.Add(5*time.Hour)))
	appendTo(t, path, logLine("a", clock.Add(-time.Hour)))
	tail, back := openBoth(t, path)
	got, reached := readBack(t, back, since)
	same(t, got, "a", "b", "c")
	if !reached.Equal(since) {
		t.Fatalf("reached %v, want %v", reached, since)
	}
	appendTo(t, path, logLine("after", clock))
	same(t, read(t, tail), "after")
}

// spec: site-traffic.md#the-24-hour-window — a compressed <log>.1 is read like a plain one.
func TestReadBackReadsACompressedFirstRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	since := clock.Add(-24 * time.Hour)
	writeGzip(t, path+".1.gz", logLine("old", since.Add(-time.Minute))+logLine("b", since.Add(time.Hour)))
	appendTo(t, path, logLine("a", clock))
	got, _ := readBack(t, back(t, path), since)
	same(t, got, "a", "b")
}

// spec: site-traffic.md#the-24-hour-window — rotated files that reach back only part of the
// day report how far back they reach.
func TestReadBackReportsHowFarItReached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	since := clock.Add(-24 * time.Hour)
	first := since.Add(2 * time.Hour)
	appendTo(t, path+".1", logLine("b", first))
	appendTo(t, path, logLine("a", clock))
	got, reached := readBack(t, back(t, path), since)
	same(t, got, "a", "b")
	if !reached.Equal(first) {
		t.Fatalf("reached %v, want %v", reached, first)
	}
}

// spec: site-traffic.md#the-log — the oldest file needed is searched, not read whole:
// a long log yields exactly the lines after the point.
func TestReadBackSearchesTheOldestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	since := clock.Add(-24 * time.Hour)
	var text strings.Builder
	for i := range 20_000 {
		text.WriteString(logLine("x", since.Add(time.Duration(i-10_000)*time.Second)))
	}
	appendTo(t, path, text.String())
	got, _ := readBack(t, back(t, path), since)
	if len(got) != 9_999 {
		t.Fatalf("read %d lines after the point, want 9999", len(got))
	}
}

func back(t *testing.T, path string) *weblog.Back {
	t.Helper()
	_, b := openBoth(t, path)
	return b
}

// spec: site-traffic.md#the-log — the search lands within a few pages before the point.
func TestSearchLandsNearThePoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	since := clock.Add(-24 * time.Hour)
	var text strings.Builder
	point := int64(0)
	for i := range 20_000 {
		if i == 10_001 {
			point = int64(text.Len())
		}
		text.WriteString(logLine("x", since.Add(time.Duration(i-10_000)*time.Second)))
	}
	appendTo(t, path, text.String())
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	got, err := weblog.Search(file, int64(text.Len()), since)
	if err != nil {
		t.Fatal(err)
	}
	if got > point || point-got > weblog.SearchSpan {
		t.Fatalf("search landed at %d, want within %d bytes before %d", got, weblog.SearchSpan, point)
	}
}

// spec: site-traffic.md#the-24-hour-window — the log renamed while reading back runs is not
// read a second time as <log>.1.
func TestReadBackSkipsTheLogRenamedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	appendTo(t, path, logLine("a", clock.Add(-time.Hour)))
	b := back(t, path)
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "")
	got, _ := readBack(t, b, clock.Add(-24*time.Hour))
	same(t, got, "a")
}

// spec: site-traffic.md#the-24-hour-window — the log renamed and compressed while reading
// back runs is not read a second time as <log>.1.gz.
func TestReadBackSkipsTheLogCompressedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	text := logLine("a", clock.Add(-time.Hour))
	appendTo(t, path, text)
	b := back(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeGzip(t, path+".1.gz", text)
	appendTo(t, path, "")
	got, _ := readBack(t, b, clock.Add(-24*time.Hour))
	same(t, got, "a")
}

// spec: site-traffic.md#the-24-hour-window — the log truncated in place while reading back
// runs: what it held is read from its copy, once.
func TestReadBackReadsTheCopyOfALogTruncatedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	text := logLine("a", clock.Add(-time.Hour))
	appendTo(t, path, text)
	b := back(t, path)
	if err := os.WriteFile(path+".1", []byte(text+logLine("after", clock)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	got, _ := readBack(t, b, clock.Add(-24*time.Hour))
	same(t, got, "a")
}
