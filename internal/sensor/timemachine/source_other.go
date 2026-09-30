//go:build !darwin

package timemachine

// System returns no source: Time Machine exists on macOS only.
func System() Source { return nil }
