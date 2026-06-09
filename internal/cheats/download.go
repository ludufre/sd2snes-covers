// Package cheats fetches per-game cheat files (.yml) for the sd2snes+ firmware.
// Each cheat is located purely by the ROM's CRC32 at BaseURL/<CRC32>.yml,
// independent of the No-Intro/boxart DAT. The HTTP fetch lives here; deciding
// the output filename and handling name collisions is done by the pipeline
// (it needs the ROM's final name and a run-wide claim map).
package cheats

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Status is the per-ROM outcome of a cheat lookup/download. The zero value is
// StatusNone ("not requested") — unlike thumbs.Status, whose zero is OK — so a
// RowResult left untouched (cheats disabled) never renders as a success.
type Status int

const (
	StatusNone      Status = iota // not requested (zero value)
	StatusOK                      // .yml downloaded and written
	StatusNotFound                // no cheat on the server for this CRC (HTTP 404)
	StatusSkip                    // already present (same game) or overwrite off
	StatusCollision               // a different game already occupies this filename; not overwritten
	StatusError                   // network or IO failure
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusNotFound:
		return "Not found"
	case StatusSkip:
		return "Skipped"
	case StatusCollision:
		return "Collision"
	case StatusError:
		return "Error"
	default:
		return "-" // StatusNone — ASCII so it stays readable in the CSV
	}
}

// BaseURL is the cheat repository; the cheat for a ROM is BaseURL + CRC32 + ".yml".
const BaseURL = "https://sd2snes.ludufre.com/cheats/"

// maxCheatBytes caps the response read so a misconfigured server can't stream an
// unbounded body into memory. Cheat .yml files are tiny.
const maxCheatBytes = 1 << 20 // 1 MiB

// URL returns the cheat URL for an (uppercase) CRC32 hex, e.g. "A1B2C3D4".
func URL(crc string) string {
	return BaseURL + crc + ".yml"
}

// Fetch downloads the cheat file for crc. It returns (data, StatusOK) on success,
// (nil, StatusNotFound) on HTTP 404 (no cheat available — normal), and
// (nil, StatusError) on a network or IO failure. Network failures are retried once.
func Fetch(ctx context.Context, client *http.Client, crc string) ([]byte, Status) {
	u := URL(crc)
	for attempt := 0; attempt < 2; attempt++ {
		data, status, err := fetch(ctx, client, u)
		if err == nil {
			return data, status
		}
		if ctx.Err() != nil {
			break // do not retry a cancelled request
		}
	}
	return nil, StatusError
}

func fetch(ctx context.Context, client *http.Client, u string) ([]byte, Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, StatusError, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, StatusError, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to read
	case http.StatusNotFound:
		return nil, StatusNotFound, nil
	default:
		return nil, StatusError, fmt.Errorf("status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCheatBytes))
	if err != nil {
		return nil, StatusError, err
	}
	return data, StatusOK, nil
}

// NewClient returns an HTTP client suitable for bulk cheat downloads. The
// pipeline reuses thumbs.NewClient(); this exists for standalone/test use.
func NewClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
