// Package backup reports what backup jobs say about their runs, read from the status files
// they write (docs/specs/backup-sensor.md).
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pravbeseda/monitor/internal/names"
	"github.com/pravbeseda/monitor/internal/sensor"
)

const (
	metricFailed           = "backup.failed"
	metricSuccessAge       = "backup.success_age_seconds"
	metricArchives         = "backup.archives"
	metricLatestArchiveAge = "backup.latest_archive_age_seconds"
	metricStored           = "backup.stored_bytes"
	metricLatestOriginal   = "backup.latest_archive_original_bytes"
)

// DirectoryOf is where the jobs of a system write their status files.
func DirectoryOf(goos string) string {
	if goos == "darwin" {
		return "/usr/local/var/lib/monitor-agent/backup"
	}
	return "/var/lib/monitor-agent/backup"
}

// SystemDirectory is the status directory of the system the agent runs on.
func SystemDirectory() string { return DirectoryOf(runtime.GOOS) }

// Sensor reports each job's status file by the agent's clock.
type Sensor struct {
	dir        string
	now        func() time.Time
	beforeRead func(name string)
}

var _ sensor.Sensor = (*Sensor)(nil)

// New builds the sensor over a status directory and the agent's clock.
func New(dir string, now func() time.Time) *Sensor {
	return &Sensor{dir: dir, now: now, beforeRead: func(string) {}}
}

// Name is the sensor id the configuration and the manifest use.
func (s *Sensor) Name() string { return "backup" }

// Names are what this sensor calls its metrics (docs/specs/metric-names.md).
func (s *Sensor) Names() names.Book {
	return sensor.Catalogued(metricFailed, metricSuccessAge, metricArchives,
		metricLatestArchiveAge, metricStored, metricLatestOriginal)
}

// Applicable is true on every system the agent is built for; a node without jobs simply
// has no directory.
func (s *Sensor) Applicable() bool { return true }

// Collect returns the metrics of every valid status file, and one error naming every file
// it left out for a reason worth fixing.
func (s *Sensor) Collect(context.Context) ([]sensor.Measurement, error) {
	if info, err := os.Stat(s.dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", s.dir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", s.dir)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", s.dir, err)
	}

	var errs []error
	read := map[string]status{}
	files := map[pair][]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		s.beforeRead(name)
		data, err := readRegular(filepath.Join(s.dir, name))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if data == nil {
			continue
		}
		st, err := parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		read[name] = st
		files[st.pair] = append(files[st.pair], name)
	}

	at := s.now()
	var out []sensor.Measurement
	for _, entry := range entries {
		st, ok := read[entry.Name()]
		if !ok {
			continue
		}
		if same := files[st.pair]; len(same) > 1 {
			if same[0] == entry.Name() {
				errs = append(errs, fmt.Errorf("%s all report backup %q, job %q",
					strings.Join(same, ", "), st.backup, st.job))
			}
			continue
		}
		out = append(out, st.measurements(at)...)
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("backup status files left out: %w", errors.Join(errs...))
	}
	return out, nil
}

// readRegular reads a file no larger than maxSize, or returns nil for one gone since the
// listing or no longer a regular file. The file is opened without blocking and without
// following a link, so a pipe or a link put in its place is never read.
func readRegular(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ELOOP) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only: closing cannot lose data
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	data := make([]byte, maxSize+1)
	n, err := io.ReadFull(f, data)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n > maxSize {
		return nil, fmt.Errorf("larger than %d bytes", maxSize)
	}
	return data[:n], nil
}
