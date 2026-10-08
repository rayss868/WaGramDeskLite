//go:build linux

package audio

// Linux MVP leaves per-account volume control to the desktop audio mixer.
func SetVolume(percent int)                        {}
func StartLabeler(label string, volumePercent int) {}
