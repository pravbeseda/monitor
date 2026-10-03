package weblog

// Search and SearchSpan expose the search reading back uses, so a test can tell a search
// from a full read that filters by time.
var (
	Search     = search
	SearchSpan = int64(searchSpan)
)
