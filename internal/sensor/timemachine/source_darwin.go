package timemachine

import (
	"context"
	"os/exec"
)

const domain = "/Library/Preferences/com.apple.TimeMachine"

// System returns the source of the Mac the agent runs on.
func System() Source {
	return preferences{path: domain + ".plist", export: export}.destinations
}

func export(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "/usr/bin/defaults", "export", domain, "-").Output()
}
