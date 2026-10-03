package weblog

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Tail follows one log by the file it opened rather than by its name, so that the lines
// nginx appends to a renamed log before it reopens are read, and a line written only in
// part waits for its end.
type Tail struct {
	path string
	cur  *followed
	// old is the renamed file still being drained, until a read finds it unchanged.
	old *followed
}

type followed struct {
	file   *os.File
	offset int64
	// head is the file's first line as last seen: a truncation the file has grown back
	// past by the next read changes it, where the size alone no longer tells.
	head []byte
}

// headSpan is how much of the first line is compared: enough to hold its timestamp.
const headSpan = 256

// truncated reports whether the file no longer begins as it did. The first call only
// remembers how it begins, once it holds a complete line.
func (f *followed) truncated() (bool, error) {
	buf := make([]byte, headSpan)
	n, err := f.file.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	if f.head == nil {
		if end := bytes.IndexByte(buf[:n], '\n'); end >= 0 {
			f.head = append([]byte(nil), buf[:end+1]...)
		}
		return false, nil
	}
	return !bytes.HasPrefix(buf[:n], f.head), nil
}

// Back reads what was logged before a tail started. It holds the log through a descriptor
// of its own, so that it may run beside the tail, and closes it when it is done.
type Back struct {
	path string
	file *os.File
	end  int64
}

// Open starts following the log at path from the end of its last complete line, and returns
// the Back that reads what lies before that point.
func Open(path string) (*Tail, *Back, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	end, err := lastLineEnd(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	back, err := reopen(file)
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	cur := &followed{file: file, offset: end}
	if _, err := cur.truncated(); err != nil {
		_ = file.Close()
		_ = back.Close()
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	return &Tail{path: path, cur: cur}, &Back{path: path, file: back, end: end}, nil
}

// Close releases every file the tail holds.
func (t *Tail) Close() {
	for _, f := range []*followed{t.cur, t.old} {
		if f != nil {
			_ = f.file.Close()
		}
	}
	t.cur, t.old = nil, nil
}

// Read hands over every complete line appended since the previous read.
func (t *Tail) Read(each func(line string)) error {
	if t.old != nil {
		read, err := t.old.drain(each)
		if err != nil {
			return fmt.Errorf("read the rotated %s: %w", t.path, err)
		}
		if read == 0 {
			_ = t.old.file.Close()
			t.old = nil
		}
	}

	named, err := os.Stat(t.path)
	if errors.Is(err, fs.ErrNotExist) {
		// Renamed, and nothing created in its place yet: nginx still writes to it.
		_, err := t.cur.drain(each)
		return err
	}
	if err != nil {
		return err
	}
	held, err := t.cur.file.Stat()
	if err != nil {
		return err
	}

	cut := named.Size() < t.cur.offset
	if !cut {
		if cut, err = t.cur.truncated(); err != nil {
			return err
		}
	}
	switch {
	case !os.SameFile(named, held):
		if _, err := t.cur.drain(each); err != nil {
			return fmt.Errorf("read the rotated %s: %w", t.path, err)
		}
		next, err := os.Open(t.path)
		if err != nil {
			return err
		}
		if t.old != nil {
			_ = t.old.file.Close()
		}
		t.old, t.cur = t.cur, &followed{file: next}
	case cut:
		// Truncated in place: what was appended before the copy is in <log>.1 past the
		// point already read.
		if err := readCopied(t.path+".1", t.cur.offset, each); err != nil {
			return err
		}
		t.cur.offset, t.cur.head = 0, nil
	}
	_, err = t.cur.drain(each)
	return err
}

// drain hands over the complete lines past the offset and moves it past them.
func (f *followed) drain(each func(line string)) (int, error) {
	count := 0
	consumed, err := lines(io.NewSectionReader(f.file, f.offset, math.MaxInt64-f.offset), func(line string) bool {
		count++
		each(line)
		return true
	})
	f.offset += consumed
	return count, err
}

func readCopied(path string, from int64, each func(line string)) error {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() < from {
		return nil
	}
	_, err = (&followed{file: file, offset: from}).drain(each)
	return err
}

// Read hands over every request logged after since: from the part of the log the tail
// started past, then from the rotated files — <log>.1 or <log>.1.gz, <log>.2.gz and on —
// until one begins at or before since. That last one, when plain, is searched for the point
// rather than read whole. It returns how far back the files reached: since itself, the
// first time of the oldest one when they all begin after it, or zero when none holds a
// request. It stops, returning the context's error, once ctx is done.
func (b *Back) Read(ctx context.Context, since time.Time, each func(Request)) (time.Time, error) {
	defer b.Close()
	keep := func(line string) bool {
		if ctx.Err() != nil {
			return false
		}
		if r, ok := Parse(line); ok && r.Time.After(since) {
			each(r)
		}
		return true
	}
	current, err := b.file.Stat()
	if err != nil {
		return time.Time{}, err
	}

	source, next := b.file, 1
	if current.Size() < b.end {
		// Truncated in place since the tail started, which reads what follows: what the
		// log held up to that point is in its copy.
		copied, err := os.Open(b.path + ".1")
		if errors.Is(err, fs.ErrNotExist) {
			return time.Time{}, nil
		}
		if err != nil {
			return time.Time{}, err
		}
		defer func() { _ = copied.Close() }()
		source, next = copied, 2
	}
	first, err := readPlain(source, b.end, since, keep)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil || reaches(first, since) {
		return reachedBy(first, since), err
	}
	reached := first
	for i := next; ; i++ {
		rotated := b.path + "." + strconv.Itoa(i)
		file, err := os.Open(rotated)
		if errors.Is(err, fs.ErrNotExist) {
			file, err = os.Open(rotated + ".gz")
		}
		if errors.Is(err, fs.ErrNotExist) {
			return reached, nil
		}
		if err != nil {
			return reached, err
		}
		first, err := readRotated(file, reached, since, keep)
		_ = file.Close()
		if err != nil {
			return reached, fmt.Errorf("read %s: %w", file.Name(), err)
		}
		if err := ctx.Err(); err != nil {
			return reached, err
		}
		if reaches(first, since) {
			return since, nil
		}
		if !first.IsZero() {
			reached = first
		}
	}
}

// Close releases a Back that will not be read.
func (b *Back) Close() { _ = b.file.Close() }

func reaches(first, since time.Time) bool { return !first.IsZero() && !first.After(since) }

func reachedBy(first, since time.Time) time.Time {
	if reaches(first, since) {
		return since
	}
	return first
}

// readRotated reads one rotated file, plain or compressed, and returns the time of its first
// request. One that does not begin before newer, the first time of the file read before it,
// is that file again — renamed, or compressed, while reading back ran — and is skipped, so
// no line counts twice.
func readRotated(file *os.File, newer, since time.Time, keep func(string) bool) (time.Time, error) {
	again := func(first time.Time) bool { return !newer.IsZero() && !first.Before(newer) }
	if !strings.HasSuffix(file.Name(), ".gz") {
		info, err := file.Stat()
		if err != nil {
			return time.Time{}, err
		}
		if first, _, err := timeAt(file, 0, info.Size()); err != nil || first.IsZero() || again(first) {
			return time.Time{}, err
		}
		return readPlain(file, info.Size(), since, keep)
	}
	zr, err := gzip.NewReader(file)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = zr.Close() }()
	var first time.Time
	skipped := false
	_, err = lines(zr, func(line string) bool {
		if r, ok := Parse(line); ok && first.IsZero() {
			first = r.Time
			if skipped = again(first); skipped {
				return false
			}
		}
		return keep(line)
	})
	if skipped {
		return time.Time{}, err
	}
	return first, err
}

// readPlain hands over the lines of file[0, end) and returns the time of its first request.
// A file that begins at or before since is searched for the point instead of being read
// from its start.
func readPlain(file *os.File, end int64, since time.Time, keep func(string) bool) (time.Time, error) {
	first, _, err := timeAt(file, 0, end)
	if err != nil || first.IsZero() {
		return first, err
	}
	from := int64(0)
	if !first.After(since) {
		if from, err = search(file, end, since); err != nil {
			return first, err
		}
	}
	_, err = lines(io.NewSectionReader(file, from, end-from), keep)
	return first, err
}

// searchSpan is where the search stops halving and reads on: a few pages of lines.
const searchSpan = 16 << 10

// search returns an offset in file[0, end) at or before the first line logged after since,
// within a few pages of it. Lines are close to time order, which is all halving needs.
func search(file *os.File, end int64, since time.Time) (int64, error) {
	lo, hi := int64(0), end
	for hi-lo > searchSpan {
		mid := lo + (hi-lo)/2
		at, start, err := timeAt(file, mid, end)
		if err != nil {
			return 0, err
		}
		if !at.IsZero() && !at.After(since) {
			lo = start
		} else {
			hi = mid
		}
	}
	return lo, nil
}

// timeAt is the time of the first request whose line starts at or after offset and before
// end, and where that line starts; zero when there is none.
func timeAt(file *os.File, offset, end int64) (time.Time, int64, error) {
	start, err := lineStart(file, offset)
	if err != nil || start >= end {
		return time.Time{}, start, err
	}
	var at time.Time
	skipped, err := lines(io.NewSectionReader(file, start, end-start), func(line string) bool {
		r, ok := Parse(line)
		at = r.Time
		return !ok
	})
	if !at.IsZero() {
		// The line that matched is not among those skipped past.
		return at, start + skipped, err
	}
	return time.Time{}, start, err
}

// lineStart is the offset of the first line that starts at or after offset.
func lineStart(file *os.File, offset int64) (int64, error) {
	if offset == 0 {
		return 0, nil
	}
	reader := bufio.NewReader(io.NewSectionReader(file, offset-1, math.MaxInt64-offset))
	skipped, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	return offset - 1 + int64(len(skipped)), nil
}

// lines hands each complete line to each until it returns false, and returns how many bytes
// the lines before the one that stopped it took. A last line without its newline is not
// handed over: it is still being written.
func lines(r io.Reader, each func(line string) bool) (int64, error) {
	reader := bufio.NewReader(r)
	var consumed int64
	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return consumed, nil
		}
		if err != nil {
			return consumed, err
		}
		if !each(strings.TrimRight(line, "\r\n")) {
			return consumed, nil
		}
		consumed += int64(len(line))
	}
}

// lastLineEnd is the offset just past the last newline: a line written only in part is
// read once it ends.
func lastLineEnd(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	buf := make([]byte, 4096)
	for end := info.Size(); end > 0; {
		from := max(0, end-int64(len(buf)))
		chunk := buf[:end-from]
		if _, err := file.ReadAt(chunk, from); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
			return from + int64(i) + 1, nil
		}
		end = from
	}
	return 0, nil
}

// reopen opens the file a descriptor holds, renamed or deleted since, as a descriptor of its
// own.
func reopen(file *os.File) (*os.File, error) {
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	var fd uintptr
	if err := conn.Control(func(raw uintptr) { fd = raw }); err != nil {
		return nil, err
	}
	return os.Open("/dev/fd/" + strconv.FormatUint(uint64(fd), 10))
}
