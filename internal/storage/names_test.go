package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/names"
)

func named(kind NameKind, key string, locale i18n.Locale, text string) Name {
	return Name{Kind: kind, Key: key, Locale: locale, Text: text}
}

// spec: metric-names.md#ingest — a declared name is kept; a later one replaces it locale by
// locale, from any node; a request without names leaves them.
func TestSaveIngestKeepsTheNewestNamePerLocale(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	first := ingest("server-b", collected, free("/", 1))
	first.Names = []Name{
		named(MetricName, "disk.free_bytes", i18n.English, "Free on {mount}"),
		named(MetricName, "disk.free_bytes", i18n.Russian, "Свободно {mount}"),
		named(FamilyName, "disk", i18n.English, "Disk"),
	}
	if err := db.SaveIngest(ctx, first); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	later := ingest("laptop-a", collected, free("/", 2))
	later.Names = []Name{named(MetricName, "disk.free_bytes", i18n.Russian, "Свободно на {mount}")}
	if err := db.SaveIngest(ctx, later); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if err := db.SaveIngest(ctx, ingest("server-b", collected, free("/", 3))); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}

	book, err := db.Names(ctx)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	want := names.Text{i18n.English: "Free on {mount}", i18n.Russian: "Свободно на {mount}"}
	if got := book.Metrics["disk.free_bytes"]; len(got) != 2 || got[i18n.English] != want[i18n.English] || got[i18n.Russian] != want[i18n.Russian] {
		t.Errorf("metric name = %v, want %v", got, want)
	}
	if got := book.Family(i18n.English, "disk"); got != "Disk" {
		t.Errorf("family name = %q, want Disk", got)
	}
}

// spec: metric-names.md#ingest — the names declared before a restart are still known.
func TestNamesSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	in := ingest("server-b", collected, free("/", 1))
	in.Names = []Name{named(MetricName, "disk.free_bytes", i18n.English, "Free on {mount}")}
	if err := db.SaveIngest(context.Background(), in); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	book, err := reopened.Names(context.Background())
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if got, _ := book.Metrics["disk.free_bytes"].In(i18n.English); got != "Free on {mount}" {
		t.Errorf("after a restart the name is %q", got)
	}
}

// spec: metric-names.md#groups — a family's group label is kept, replaced by a later one
// from any node, left by a request without one, and still known after a restart.
func TestSaveIngestKeepsTheNewestGroupLabel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitor.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	first := ingest("server-b", collected, free("/", 1))
	first.Names = []Name{named(GroupLabel, "disk", "", "fs")}
	later := ingest("laptop-a", collected, free("/", 2))
	later.Names = []Name{named(GroupLabel, "disk", "", "mount")}
	for _, in := range []Ingest{first, later, ingest("server-b", collected, free("/", 3))} {
		if err := db.SaveIngest(ctx, in); err != nil {
			t.Fatalf("SaveIngest: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	book, err := reopened.Names(ctx)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if got := book.Groups["disk"]; got != "mount" {
		t.Errorf("group label = %q, want mount", got)
	}
	if len(book.Families) != 0 {
		t.Errorf("families = %v: a group label is not a family's name", book.Families)
	}
}
