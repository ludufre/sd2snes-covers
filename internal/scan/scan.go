// Package scan finds Super Nintendo ROM files on disk.
package scan

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// romExts are the loose ROM file extensions handled by the app.
var romExts = map[string]bool{
	".sfc": true,
	".smc": true,
}

// FindROMs walks root recursively and returns the sorted paths of all .sfc/.smc
// files found. Unreadable directories are skipped rather than aborting the walk.
func FindROMs(root string) ([]string, error) {
	var roms []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip entries we cannot read, keep walking
		}
		if d.IsDir() {
			return nil
		}
		if romExts[strings.ToLower(filepath.Ext(path))] {
			roms = append(roms, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(roms)
	return roms, nil
}
