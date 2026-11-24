//go:build !windows

package wallpaper

// Only Windows is asked: macOS's glass picks its own contrast, and Linux
// shows no glass.
const supported = false

func current() string { return "" }
