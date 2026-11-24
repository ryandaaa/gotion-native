package wallpaper

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const supported = true

// current is the file of the wallpaper: the one Settings set, or the copy
// Windows keeps of it (TranscodedWallpaper, a JPEG without an extension)
// when that file is gone or was never a path, as with Windows Spotlight.
func current() string {
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.QUERY_VALUE); err == nil {
		p, _, err := k.GetStringValue("WallPaper")
		k.Close()
		if err == nil && p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	t := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Themes", "TranscodedWallpaper")
	if _, err := os.Stat(t); err == nil {
		return t
	}
	return ""
}
