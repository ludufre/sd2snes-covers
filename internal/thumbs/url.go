// Package thumbs builds libretro thumbnail URLs and downloads boxart images.
package thumbs

import (
	"net/url"
	"strings"
)

// DefaultBoxartBase is the directory URL that holds the SNES Named_Boxarts on
// the libretro thumbnail server. It is configurable in the app.
const DefaultBoxartBase = "https://thumbnails.libretro.com/Nintendo%20-%20Super%20Nintendo%20Entertainment%20System/Named_Boxarts/"

// illegalReplacer mirrors libretro's thumbnail filename rule: the characters
// & * / : ` < > ? \ | " are replaced with '_'. Spaces and parentheses are kept.
var illegalReplacer = strings.NewReplacer(
	"&", "_",
	"*", "_",
	"/", "_",
	":", "_",
	"`", "_",
	"<", "_",
	">", "_",
	"?", "_",
	"\\", "_",
	"|", "_",
	"\"", "_",
)

// Sanitize converts a No-Intro game name into the libretro thumbnail base name.
func Sanitize(name string) string {
	return illegalReplacer.Replace(name)
}

// BoxartURL builds the boxart URL for a game name using the default repository.
func BoxartURL(gameName string) string {
	return BoxartURLFrom(DefaultBoxartBase, gameName)
}

// BoxartURLFrom builds the boxart URL for a game name under the given base
// directory URL. The sanitized name is appended and the path percent-encoded
// (spaces -> %20, parentheses -> %28/%29), which the server accepts.
func BoxartURLFrom(base, gameName string) string {
	name := Sanitize(gameName) + ".png"
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + name
		u.RawPath = "" // force String() to re-encode from Path
		return u.String()
	}
	// Fallback: naive join with an encoded segment.
	seg := (&url.URL{Path: name}).EscapedPath()
	return strings.TrimRight(base, "/") + "/" + seg
}
