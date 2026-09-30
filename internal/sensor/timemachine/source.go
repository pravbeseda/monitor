package timemachine

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// preferences reads Time Machine's preferences through the preferences service, which
// answers without Full Disk Access where a direct read of the file does not. The file's
// existence, which stat can check, tells a Mac that never set Time Machine up from one whose
// preferences could not be read.
type preferences struct {
	path   string
	export func(ctx context.Context) ([]byte, error)
}

func (p preferences) destinations(ctx context.Context) ([]Destination, error) {
	if _, err := os.Stat(p.path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", p.path, err)
	}
	out, err := p.export(ctx)
	if err != nil {
		return nil, fmt.Errorf("export the preferences: %w", err)
	}
	return parse(out)
}

// parse reads the destinations from an XML property list as the preferences service
// prints it.
func parse(plist []byte) ([]Destination, error) {
	var doc node
	if err := xml.Unmarshal(plist, &doc); err != nil {
		return nil, fmt.Errorf("parse the preferences: %w", err)
	}
	if doc.XMLName.Local != "plist" || len(doc.Nodes) != 1 || doc.Nodes[0].XMLName.Local != "dict" {
		return nil, errors.New("the preferences are not a property list holding a dictionary")
	}
	settings := doc.Nodes[0]
	// The service answers an empty dictionary for a domain it cannot read, too.
	if len(settings.Nodes) == 0 {
		return nil, errors.New("the preferences service answered no settings")
	}
	listed := settings.get("Destinations")
	if listed == nil {
		return nil, nil
	}
	if listed.XMLName.Local != "array" {
		return nil, fmt.Errorf("the destinations are a <%s>, not an array", listed.XMLName.Local)
	}
	out := make([]Destination, 0, len(listed.Nodes))
	for i, entry := range listed.Nodes {
		d, err := destination(entry)
		if err != nil {
			return nil, fmt.Errorf("destination %d: %w", i, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func destination(entry node) (Destination, error) {
	if entry.XMLName.Local != "dict" {
		return Destination{}, fmt.Errorf("a <%s>, not a dictionary", entry.XMLName.Local)
	}
	var d Destination
	if name := entry.get("LastKnownVolumeName"); name != nil {
		if name.XMLName.Local != "string" {
			return Destination{}, fmt.Errorf("the volume name is a <%s>, not a string", name.XMLName.Local)
		}
		d.Name = name.Text
	}
	dates := entry.get("SnapshotDates")
	if dates == nil {
		return d, nil
	}
	if dates.XMLName.Local != "array" {
		return Destination{}, fmt.Errorf("the snapshot dates are a <%s>, not an array", dates.XMLName.Local)
	}
	for _, date := range dates.Nodes {
		if date.XMLName.Local != "date" {
			return Destination{}, fmt.Errorf("a snapshot date is a <%s>, not a date", date.XMLName.Local)
		}
		parsed, err := time.Parse(time.RFC3339, date.Text)
		if err != nil {
			return Destination{}, fmt.Errorf("parse the snapshot date %q: %w", date.Text, err)
		}
		d.Backups = append(d.Backups, parsed)
	}
	return d, nil
}

// node is one element of an XML property list.
type node struct {
	XMLName xml.Name
	Text    string `xml:",chardata"`
	Nodes   []node `xml:",any"`
}

// get returns the value a dictionary holds under key, or nil. A dictionary's children
// alternate between a <key> and its value.
func (n node) get(key string) *node {
	for i := 0; i+1 < len(n.Nodes); i += 2 {
		if n.Nodes[i].XMLName.Local == "key" && n.Nodes[i].Text == key {
			return &n.Nodes[i+1]
		}
	}
	return nil
}
