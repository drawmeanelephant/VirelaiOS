package pdfproof

import (
	"testing"
	"virelai/pdf"
	"virelai/vi"
)

func TestReceiptPauseWaitsForAcknowledgmentAndCloses(t *testing.T) {
	var ns uint64
	opens, closes, sleeps := 0, 0, 0
	fs := files{
		open: func(path string, mode uint32) (int64, int64) {
			if path != "/host/PDF/baseline.resume" || mode != vi.ModeRead {
				t.Fatal(path, mode)
			}
			opens++
			if opens < 3 {
				return -1, -vi.ErrENOENT
			}
			return 7, 7
		},
		read: func(h uint32, b []byte) (int, int64) {
			if h != 7 || len(b) != 2 {
				t.Fatal(h, len(b))
			}
			b[0] = '1'
			return 1, 1
		},
		close: func(h uint32) {
			if h != 7 {
				t.Fatal(h)
			}
			closes++
		},
	}
	var scratch [2]byte
	code := waitReceipt(fs, "/host/PDF/baseline.resume", scratch[:],
		func() uint64 { return ns }, func(ticks uint64) {
			if ticks != 1 {
				t.Fatal(ticks)
			}
			sleeps++
			ns += 1_000_000
		})
	if code != pdf.OK || opens != 3 || closes != 1 || sleeps != 2 {
		t.Fatal(code, opens, closes, sleeps)
	}
}

func TestReceiptPauseTimeoutAndFailureCleanup(t *testing.T) {
	for _, kind := range []string{"missing", "open", "read", "empty", "wrong", "excess", "late"} {
		t.Run(kind, func(t *testing.T) {
			var ns uint64
			closes, sleeps := 0, 0
			fs := files{
				open: func(string, uint32) (int64, int64) {
					if kind == "missing" {
						return -1, -vi.ErrENOENT
					}
					if kind == "open" {
						return -1, -vi.ErrEINVAL
					}
					return 7, 7
				},
				read: func(_ uint32, b []byte) (int, int64) {
					b[0] = '1'
					switch kind {
					case "read":
						return 0, -vi.ErrEINVAL
					case "empty":
						return 0, 0
					case "wrong":
						b[0] = '0'
					case "excess":
						return 2, 2
					case "late":
						ns = pdf.MaxNanos
					}
					return 1, 1
				},
				close: func(uint32) { closes++ },
			}
			var scratch [2]byte
			code := waitReceipt(fs, "ack", scratch[:], func() uint64 { return ns }, func(uint64) {
				sleeps++
				ns += pdf.MaxNanos / 5
			})
			want, closed := pdf.InvalidBuffer, 1
			switch kind {
			case "missing":
				want, closed = pdf.TimeLimit, 0
			case "open":
				want, closed = pdf.ReadFailed, 0
			case "read":
				want = pdf.ReadFailed
			case "late":
				want = pdf.TimeLimit
			}
			if code != want || closes != closed || (kind == "missing" && sleeps != 5) {
				t.Fatal(code, closes, sleeps)
			}
		})
	}
	if c := waitReceipt(files{}, "ack", nil, nil, nil); c != pdf.InvalidBuffer {
		t.Fatal(c)
	}
}

func TestReceiptPauseReusesScratchWithoutAllocation(t *testing.T) {
	fs := files{
		open:  func(string, uint32) (int64, int64) { return 7, 7 },
		read:  func(_ uint32, b []byte) (int, int64) { b[0] = '1'; return 1, 1 },
		close: func(uint32) {},
	}
	var scratch [2]byte
	allocs := testing.AllocsPerRun(100, func() {
		if c := waitReceipt(fs, "ack", scratch[:], func() uint64 { return 0 }, func(uint64) {}); c != pdf.OK {
			panic(c)
		}
	})
	if allocs != 0 {
		t.Fatal("pause allocated", allocs)
	}
}
