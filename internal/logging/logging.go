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

// inUTC moves the line's own timestamp; an attribute a caller passed is theirs, whatever it
// is called. The hook sees both, so the kind is what tells them apart: a caller's top-level
// "time" is as legal as any other key, and reading a string as an instant panics.
func inUTC(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == slog.TimeKey && attr.Value.Kind() == slog.KindTime {
		attr.Value = slog.TimeValue(attr.Value.Time().UTC())
	}
	return attr
}
