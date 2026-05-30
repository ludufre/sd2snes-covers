// Package dat downloads, caches and parses the libretro No-Intro DAT for SNES.
package dat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is the libretro No-Intro DAT for Super Nintendo.
const DefaultURL = "https://raw.githubusercontent.com/ludufre/sd2snes-covers/refs/heads/main/dats/libretro-custom.dat"

// Index maps an uppercase 8-hex CRC32 to a No-Intro game name.
type Index map[string]string

var crcRe = regexp.MustCompile(`(?i)\bcrc\s+([0-9a-f]+)`)

// cachePath returns the on-disk location of the cached DAT, creating its parent
// directory. It falls back to the OS temp dir when no user cache dir is set.
func cachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "sd2snes-covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "snes.dat"), nil
}

// Load returns the CRC index, downloading and caching the DAT from datURL on
// first use (an empty datURL uses DefaultURL). When forceRefresh is true the
// cached file is re-downloaded.
func Load(ctx context.Context, datURL string, forceRefresh bool) (Index, error) {
	path, err := cachePath()
	if err != nil {
		return nil, err
	}
	if forceRefresh {
		_ = os.Remove(path)
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		if err := download(ctx, datURL, path); err != nil {
			return nil, fmt.Errorf("downloading DAT: %w", err)
		}
	} else if statErr != nil {
		return nil, statErr
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// download fetches the DAT from datURL to dst, writing atomically via a temp
// file. An empty datURL uses DefaultURL.
func download(ctx context.Context, datURL, dst string) error {
	if datURL == "" {
		datURL = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, datURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// Parse reads a clrmamepro-format DAT and builds a CRC -> game name index.
func Parse(r io.Reader) (Index, error) {
	idx := make(Index, 5000)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var name string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "game ("):
			name = ""
		case strings.HasPrefix(line, "rom ("):
			if name == "" {
				continue
			}
			m := crcRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v, err := strconv.ParseUint(m[1], 16, 32)
			if err != nil {
				continue
			}
			idx[fmt.Sprintf("%08X", uint32(v))] = name
		case strings.HasPrefix(line, "name "):
			if q := quoted(line); q != "" {
				name = q
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return idx, nil
}

// quoted returns the contents of the first double-quoted substring, or "".
func quoted(s string) string {
	i := strings.IndexByte(s, '"')
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
