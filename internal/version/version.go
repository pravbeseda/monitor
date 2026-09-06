// Package version carries the build version both binaries report.
package version

// Current is the version an agent sends as agent_version on every ingest request. A
// variable rather than a constant because a release stamps it with -ldflags -X, which the
// linker ignores on a constant (docs/specs/release.md); the value here is the development
// default.
var Current = "0.1.0"
