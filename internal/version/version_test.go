package version_test

import (
	"regexp"
	"testing"

	"github.com/pravbeseda/monitor/internal/version"
)

// spec: release.md#the-version-a-binary-reports — the development default is a version too.
func TestCurrentIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version.Current) {
		t.Errorf("version.Current = %q, want MAJOR.MINOR.PATCH", version.Current)
	}
}
