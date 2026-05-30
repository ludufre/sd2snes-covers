package snes

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// makePayload returns n deterministic bytes.
func makePayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestCRC32Headerless_NoHeader(t *testing.T) {
	// 1024 bytes -> 1024 % 1024 == 0 -> no header.
	payload := makePayload(1024)
	want := crc32.ChecksumIEEE(payload)

	path := writeTemp(t, "rom.sfc", payload)
	got, hadHeader, err := CRC32Headerless(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadHeader {
		t.Errorf("hadCopierHeader = true, want false")
	}
	if got != want {
		t.Errorf("crc = %08X, want %08X", got, want)
	}
}

func TestCRC32Headerless_WithHeader(t *testing.T) {
	// 512-byte header + 1024-byte payload -> 1536 % 1024 == 512 -> header.
	payload := makePayload(1024)
	want := crc32.ChecksumIEEE(payload) // CRC must match the headerless payload

	header := make([]byte, copierHeaderSize)
	for i := range header {
		header[i] = 0xAA // arbitrary header bytes that must be ignored
	}
	data := append(header, payload...)

	path := writeTemp(t, "rom.smc", data)
	got, hadHeader, err := CRC32Headerless(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hadHeader {
		t.Errorf("hadCopierHeader = false, want true")
	}
	if got != want {
		t.Errorf("crc = %08X, want %08X (header not skipped?)", got, want)
	}
}

func TestCRCHex(t *testing.T) {
	cases := map[uint32]string{
		0x00000000: "00000000",
		0x0000ABCD: "0000ABCD",
		0xB19ED489: "B19ED489",
		0x2D206BF7: "2D206BF7",
	}
	for in, want := range cases {
		if got := CRCHex(in); got != want {
			t.Errorf("CRCHex(%#x) = %q, want %q", in, got, want)
		}
	}
}
