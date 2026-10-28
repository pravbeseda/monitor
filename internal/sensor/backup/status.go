package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxSize       = 65536
	maxLabelRunes = 64
	maxCount      = 1 << 53
)

// status is one job's account of its latest run; a nil figure is one the file does not give.
type status struct {
	pair
	failed              bool
	lastSuccess         *time.Time
	archives            *uint64
	latestArchive       *time.Time
	storedBytes         *uint64
	latestOriginalBytes *uint64
}

var (
	timeForm  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
	countForm = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	bom       = []byte("\xef\xbb\xbf")
)

// parse reads a status file strictly: keys match exactly, a listed key appears once, and a
// value of the wrong form rejects the file rather than being read as something else.
func parse(data []byte) (status, error) {
	if bytes.HasPrefix(data, bom) {
		return status{}, errors.New("starts with a byte order mark")
	}
	if !utf8.Valid(data) {
		return status{}, errors.New("not UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return status{}, errors.New("not a JSON object")
	}
	values := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return status{}, fmt.Errorf("parse: %w", err)
		}
		key := tok.(string) // an object's next token, when More, is its key
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return status{}, fmt.Errorf("parse %q: %w", key, err)
		}
		if _, listed := readers[key]; !listed {
			continue
		}
		if _, twice := values[key]; twice {
			return status{}, fmt.Errorf("%q given twice", key)
		}
		values[key] = raw
	}
	if _, err := dec.Token(); err != nil {
		return status{}, fmt.Errorf("parse: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return status{}, errors.New("a second value follows the object")
	}

	var st status
	for key, read := range readers {
		raw, given := values[key]
		if given && string(raw) == "null" {
			given = false
		}
		if !given {
			if required[key] {
				return status{}, fmt.Errorf("%q missing", key)
			}
			continue
		}
		if err := read(&st, raw); err != nil {
			return status{}, fmt.Errorf("%q: %w", key, err)
		}
	}
	return st, nil
}

var required = map[string]bool{"backup": true, "job": true, "failed": true}

var readers = map[string]func(*status, json.RawMessage) error{
	"backup":       func(st *status, raw json.RawMessage) (err error) { st.backup, err = label(raw); return },
	"job":          func(st *status, raw json.RawMessage) (err error) { st.job, err = label(raw); return },
	"failed":       func(st *status, raw json.RawMessage) (err error) { st.failed, err = flag(raw); return },
	"last_success": func(st *status, raw json.RawMessage) (err error) { st.lastSuccess, err = when(raw); return },
	"archives":     func(st *status, raw json.RawMessage) (err error) { st.archives, err = count(raw); return },
	"latest_archive": func(st *status, raw json.RawMessage) (err error) {
		st.latestArchive, err = when(raw)
		return
	},
	"stored_bytes": func(st *status, raw json.RawMessage) (err error) { st.storedBytes, err = count(raw); return },
	"latest_archive_original_bytes": func(st *status, raw json.RawMessage) (err error) {
		st.latestOriginalBytes, err = count(raw)
		return
	},
}

func text(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", errors.New("not a string")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func label(raw json.RawMessage) (string, error) {
	s, err := text(raw)
	if err != nil {
		return "", err
	}
	if n := utf8.RuneCountInString(s); n == 0 || n > maxLabelRunes {
		return "", fmt.Errorf("%d characters, not 1 to %d", n, maxLabelRunes)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("holds a control character")
		}
	}
	return s, nil
}

func flag(raw json.RawMessage) (bool, error) {
	switch string(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, errors.New("not true or false")
}

func when(raw json.RawMessage) (*time.Time, error) {
	s, err := text(raw)
	if err != nil {
		return nil, err
	}
	if !timeForm.MatchString(s) {
		return nil, fmt.Errorf("%q is not YYYY-MM-DDThh:mm:ss with a zone", s)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func count(raw json.RawMessage) (*uint64, error) {
	if !countForm.Match(raw) {
		return nil, fmt.Errorf("%s is not a count in digits", raw)
	}
	n, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil || n > maxCount {
		return nil, fmt.Errorf("%s is over %d", raw, uint64(maxCount))
	}
	return &n, nil
}
