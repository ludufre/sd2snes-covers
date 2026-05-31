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

// imageExts are the image file extensions handled by the "just convert to .cov"
// flow (all decodable by the cov package).
var imageExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".bmp":  true,
}

// FindImages walks root recursively and returns the sorted paths of all
// supported images (.png/.jpg/.jpeg/.bmp) found — used by the "just convert to
// .cov" flow, which turns each image into a .cov next to it without any DAT
// lookup. Unreadable directories are skipped rather than aborting the walk.
func FindImages(root string) ([]string, error) {
	var imgs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip entries we cannot read, keep walking
		}
		if d.IsDir() {
			return nil
		}
		if imageExts[strings.ToLower(filepath.Ext(path))] {
			imgs = append(imgs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(imgs)
	return imgs, nil
}
