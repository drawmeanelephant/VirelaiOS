package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func guardELF(size int) []byte {
	data := make([]byte, size)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], 183)
	binary.LittleEndian.PutUint64(data[24:], 0x10000)
	binary.LittleEndian.PutUint64(data[32:], 64)
	binary.LittleEndian.PutUint16(data[54:], 56)
	binary.LittleEndian.PutUint16(data[56:], 3)
	for i, flags := range []uint32{5, 4, 6} {
		at := 64 + i*56
		binary.LittleEndian.PutUint32(data[at:], 1)
		binary.LittleEndian.PutUint32(data[at+4:], flags)
		binary.LittleEndian.PutUint64(data[at+8:], uint64(i*4096))
		binary.LittleEndian.PutUint64(data[at+16:], uint64((i+1)*65536))
		binary.LittleEndian.PutUint64(data[at+32:], 4096)
		binary.LittleEndian.PutUint64(data[at+40:], 4096)
	}
	return data
}

func TestBuildGuardMatchesStreamedAndStagedLimits(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]byte)
		want string
	}{
		{"streamed past old staging bound", nil, ""},
		{"mapped overflow", func(b []byte) { binary.LittleEndian.PutUint64(b[64+2*56+40:], 64*1024*1024+1) }, "map_too_large"},
		{"truncated segment", func(b []byte) { binary.LittleEndian.PutUint64(b[64+8:], uint64(len(b)+1)) }, "truncated"},
		{"writable text", func(b []byte) { binary.LittleEndian.PutUint32(b[64+4:], 7) }, "writable text"},
		{"staged interp over limit", func(b []byte) {
			binary.LittleEndian.PutUint16(b[56:], 4)
			binary.LittleEndian.PutUint32(b[64+3*56:], 3)
		}, "staging_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := guardELF(2*1024*1024 + 160)
			if tc.edit != nil {
				tc.edit(data)
			}
			path := filepath.Join(t.TempDir(), "guard.elf")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("bash", "../../../tools/go/build-web.sh", "--check-elf", path).CombinedOutput()
			if tc.want == "" {
				if err != nil || !strings.Contains(string(out), "path=streamed") {
					t.Fatalf("valid streamed shape: %v %s", err, out)
				}
			} else if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("guard failed to refuse %q: %v %s", tc.want, err, out)
			}
		})
	}
}
