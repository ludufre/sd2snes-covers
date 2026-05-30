// Package snes computes No-Intro-compatible checksums for Super Nintendo ROMs.
package snes

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// copierHeaderSize is the size of the legacy 512-byte SMC copier header that
// some SNES ROM dumps carry. No-Intro checksums are computed on the headerless
// ROM, so this header must be skipped before hashing.
const copierHeaderSize = 512

// CRC32Headerless computes the CRC32 (IEEE polynomial, same as zip) of a SNES
// ROM, skipping a 512-byte copier header when one is present.
//
// A headerless SNES ROM is always a multiple of 1 KiB, so a file whose size
// satisfies (size % 1024 == 512) carries a 512-byte copier header. The file is
// streamed into the hash rather than read entirely into memory.
func CRC32Headerless(path string) (crc uint32, hadCopierHeader bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, false, err
	}

	hadCopierHeader = info.Size()%1024 == copierHeaderSize
	if hadCopierHeader {
		if _, err = f.Seek(copierHeaderSize, io.SeekStart); err != nil {
			return 0, false, err
		}
	}

	h := crc32.NewIEEE()
	if _, err = io.Copy(h, f); err != nil {
		return 0, false, err
	}
	return h.Sum32(), hadCopierHeader, nil
}

// CRCHex formats a CRC32 as an 8-digit uppercase hex string, matching the way
// checksums appear in the No-Intro DAT.
func CRCHex(crc uint32) string {
	return fmt.Sprintf("%08X", crc)
}
