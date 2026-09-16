// Package logging builds the logger both binaries install at startup. A log line is
// diagnostic: it is read beside a measurement's timestamp and beside another host's log,
// and both of those are UTC, so the line carries its own instant in UTC too (ADR 0026).
package logging

import (
	"io"
	"log/slog"
)

// New is the logger of a process. The caller installs it with slog.SetDefault, so that
// every package keeps logging through the standard functions and nothing here is hidden.
func New(out io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{ReplaceAttr: inUTC}))
}

// inUTC moves the line's own timestamp; an attribute a caller passed is theirs, including
// one that happens to be called "time" inside a group.
func inUTC(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == slog.TimeKey {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC())
	}
	return attr
}
