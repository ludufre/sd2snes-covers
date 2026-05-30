package thumbs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Status is the per-ROM outcome of a boxart lookup/download.
type Status int

const (
	StatusOK       Status = iota // boxart downloaded
	StatusNotFound               // in DAT but no art on the server (HTTP 404)
	StatusSkip                   // destination already exists, not overwritten
	StatusNoMatch                // CRC not present in the DAT
	StatusError                  // network or IO failure
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusNotFound:
		return "404"
	case StatusSkip:
		return "Skipped"
	case StatusNoMatch:
		return "No match"
	default:
		return "Error"
	}
}

// NewClient returns an HTTP client suitable for bulk thumbnail downloads.
func NewClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// Download fetches the boxart for gameName (from the repository at base) and
// writes it to destPNG.
//
// It returns StatusSkip when destPNG already exists and overwrite is false,
// StatusNotFound on HTTP 404 (no art available), and StatusError on a network
// or IO failure. Network failures are retried once. An empty base uses
// DefaultBoxartBase.
func Download(ctx context.Context, client *http.Client, base, gameName, destPNG string, overwrite bool) (Status, error) {
	if !overwrite {
		if _, err := os.Stat(destPNG); err == nil {
			return StatusSkip, nil
		}
	}

	if base == "" {
		base = DefaultBoxartBase
	}
	u := BoxartURLFrom(base, gameName)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		status, err := fetch(ctx, client, u, destPNG)
		if err == nil {
			return status, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break // do not retry a cancelled request
		}
	}
	return StatusError, lastErr
}

func fetch(ctx context.Context, client *http.Client, u, destPNG string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return StatusError, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return StatusError, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to save
	case http.StatusNotFound:
		return StatusNotFound, nil
	default:
		return StatusError, fmt.Errorf("status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPNG), 0o755); err != nil {
		return StatusError, err
	}
	tmp := destPNG + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return StatusError, err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return StatusError, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return StatusError, err
	}
	if err := os.Rename(tmp, destPNG); err != nil {
		os.Remove(tmp)
		return StatusError, err
	}
	return StatusOK, nil
}
