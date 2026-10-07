package heap

import (
	"bytes"
	"testing"
	"time"

	"virelai/vi"
)

func TestPublishRetryReopensSameBoundedBodyAndStops(t *testing.T) {
	body := []byte(testSample(1).Row() + "\n")
	for _, tc := range []struct {
		name          string
		results       []int64
		calls, sleeps int
		want          int64
	}{
		{"success", []int64{0}, 1, 0, 0},
		{"lost handle then success", []int64{-vi.ErrEBADF, 0}, 2, 1, 0},
		{"persistent lost handle", []int64{-vi.ErrEBADF, -vi.ErrEBADF, -vi.ErrEBADF}, 3, 2, -vi.ErrEBADF},
		{"permission refuses immediately", []int64{-vi.ErrEACCES}, 1, 0, -vi.ErrEACCES},
		{"not found refuses immediately", []int64{vi.ErrFileNotFound}, 1, 0, vi.ErrFileNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, sleeps := 0, 0
			rc := writeRing("/host/HEAP/GOEDIT.ELF.TXT", body, func(path string, got []byte) int64 {
				if path != "/host/HEAP/GOEDIT.ELF.TXT" || !bytes.Equal(got, body) {
					t.Fatal("retry changed path or retained sample body")
				}
				result := tc.results[calls]
				calls++
				return result
			}, func(duration time.Duration) {
				if duration < time.Second {
					t.Fatal("retry did not sleep at least one second")
				}
				sleeps++
			})
			if rc != tc.want || calls != tc.calls || sleeps != tc.sleeps {
				t.Fatalf("rc=%d calls=%d sleeps=%d", rc, calls, sleeps)
			}
		})
	}
}
