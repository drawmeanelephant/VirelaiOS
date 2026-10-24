package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"virelai/gpu3d"
	"virelai/infer"
	"virelai/tabapp"
	"virelai/vi"
)

// fakeHost is the platform seam's test double: console lines recorded,
// files in a map, compute sessions served by the kernel-emulating fake.
type fakeHost struct {
	console []string
	files   map[string][]byte
	dirs    map[string]bool
	devs    []*infer.FakeDev
	openErr error
	now     int64
}

func newFakeHost() *fakeHost {
	return &fakeHost{files: map[string][]byte{}, dirs: map[string]bool{}}
}

func (f *fakeHost) ConsoleLine(s string) { f.console = append(f.console, s) }

func (f *fakeHost) MkdirShare(p string) error { f.dirs[p] = true; return nil }

func (f *fakeHost) WriteShareFile(p string, b []byte) error {
	f.files[p] = append([]byte(nil), b...)
	return nil
}

func (f *fakeHost) FileExists(p string) bool {
	_, ok := f.files[p]
	return ok
}

type rcReader struct{ io.Reader }

func (rcReader) Close() error { return nil }

func (f *fakeHost) OpenRead(p string) (io.ReadCloser, error) {
	b, ok := f.files[p]
	if !ok {
		return nil, errors.New("open -2")
	}
	return rcReader{bytes.NewReader(b)}, nil
}

func (f *fakeHost) OpenComp() (infer.Dev, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	d := infer.NewFakeDev()
	f.devs = append(f.devs, d)
	return d, nil
}

// Nanos advances a fixed tick per call so tok/s fields are populated and
// deterministic without a real clock.
func (f *fakeHost) Nanos() int64 {
	f.now += 1e6
	return f.now
}

func (f *fakeHost) SelfMemBytes() uint64 { return 30 << 20 }

func (f *fakeHost) hasConsole(sub string) bool {
	for _, l := range f.console {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// --- the stub model on the emulated kernels ---------------------------------

func TestStubRunsDeterministicAndCoversKernels(t *testing.T) {
	h := newFakeHost()
	if err := run(h, []string{"stub"}); err != nil {
		t.Fatalf("run stub: %v", err)
	}
	// One journey, one compute session (the fixed-VA window makes a process
	// single-session anyway): the five runs share it and each frees its own
	// buffers.
	if len(h.devs) != 1 {
		t.Fatalf("sessions = %d want 1", len(h.devs))
	}
	d := h.devs[0]
	for _, k := range []uint32{vi.GpuKernelGEMM, vi.GpuKernelSoftmax, vi.GpuKernelDequant} {
		if d.Submits[k] == 0 {
			t.Fatalf("kernel %d never ran", k)
		}
	}
	if !h.hasConsole("goinfer: stub consistent K=5") {
		t.Fatalf("console: %v", h.console)
	}
	if !h.hasConsole("goinfer OK") {
		t.Fatalf("console: %v", h.console)
	}
	body := string(h.files[receiptTSV])
	if !strings.Contains(body, "stub\tconsistent\tK=5") {
		t.Fatalf("receipt:\n%s", body)
	}
	// Every run must be a full session: same id sequence, nonzero submits.
	var firstIds string
	for i, l := range h.console {
		if strings.HasPrefix(l, "goinfer: stub run k=") {
			j := strings.Index(l, "ids=") + 4
			k := strings.Index(l[j:], " ")
			ids := l[j : j+k]
			if firstIds == "" {
				firstIds = ids
			} else if ids != firstIds {
				t.Fatalf("run %d diverged: %s vs %s", i, ids, firstIds)
			}
		}
	}
	if firstIds == "" {
		t.Fatal("no stub run lines")
	}
}

func TestStubAnswerPinnedOnHost(t *testing.T) {
	// The host-emulated answer is pinned here; the gate pins the Metal
	// answer separately — both deterministic, possibly differing in the
	// last ulp of f16 rounding.
	h := newFakeHost()
	if err := run(h, []string{"stub"}); err != nil {
		t.Fatal(err)
	}
	for _, l := range h.console {
		if strings.HasPrefix(l, "goinfer: stub answer ") {
			t.Logf("stub answer line: %s", l)
			return
		}
	}
	t.Fatal("no answer line")
}

func TestModelJourneyReportsAbsentWeights(t *testing.T) {
	h := newFakeHost()
	err := run(h, []string{"model"})
	if err == nil || !h.hasConsole("goinfer: weights absent") {
		t.Fatalf("want weights-absent: err=%v console=%v", err, h.console)
	}
}

func TestModelJourneyRunsStagedVIW(t *testing.T) {
	h := newFakeHost()
	// Stage a stub image at the model path — the model journey only cares
	// that a real file arrived through the share.
	h.files[modelPath] = infer.SynthStub()
	if err := run(h, []string{"model"}); err != nil {
		t.Fatalf("model run: %v", err)
	}
	if !h.hasConsole("goinfer: model answer") {
		t.Fatalf("console: %v", h.console)
	}
	body := string(h.files[receiptTSV])
	if !strings.Contains(body, "model\tloaded\t") {
		t.Fatalf("receipt:\n%s", body)
	}
}

func TestUnsupportedNamesItself(t *testing.T) {
	h := newFakeHost()
	h.openErr = gpu3d.ErrComputeNoDevice
	if err := run(h, []string{"stub"}); err != nil {
		t.Fatalf("unsupported should exit clean: %v", err)
	}
	if !h.hasConsole("goinfer: unsupported") {
		t.Fatalf("console: %v", h.console)
	}
	if _, ok := h.files[receiptTSV]; ok {
		t.Fatal("a receipt landed on a device-less boot")
	}
}

func TestUsageError(t *testing.T) {
	h := newFakeHost()
	if err := run(h, []string{"bogus"}); !errors.Is(err, errUsage) {
		t.Fatalf("want usage: %v", err)
	}
}

// --- M195e (#3310): the envelope's refusal corpus -------------------------

// decodeState refuses whole on every corrupt shape — a torn or foreign
// blob must never land half-applied on the reopened face. The corpus is
// the app's own valid blob mutated each way: wrong fmtVer, nonzero
// flags, an unknown journey tag, a non-canonical path, truncation,
// trailing bytes.
func TestDecodeStateRefusals(t *testing.T) {
	src := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	src.modelPath = "/host/GOINFER/ALT.VIW"
	src.journey("model") // weights absent — armed anyway, log narrates
	good := src.encodeState()
	if _, ok := decodeState(good); !ok {
		t.Fatal("the app's own blob would not decode")
	}

	mut := func(f func(b []byte)) []byte {
		b := append([]byte(nil), good...)
		f(b)
		return b
	}
	cases := map[string][]byte{
		"empty":         {},
		"short":         good[:4],
		"bad fmtVer":    mut(func(b []byte) { b[0] = 2 }),
		"nonzero flags": mut(func(b []byte) { b[1] = 1 }),
		"bad journey":   mut(func(b []byte) { b[2] = 9 }),
		// A well-formed blob whose path is a relative name — the
		// canonical-path check's own refusal, not a length accident.
		"rel path": append(append([]byte{stateFmtVer, 0, 2, 7, 0},
			[]byte("ALT.VIW")...), 0),
		"truncated": good[:len(good)-3],
		"trailing":  append(append([]byte(nil), good...), 0),
	}
	for name, blob := range cases {
		if _, ok := decodeState(blob); ok {
			t.Fatalf("%s: corrupt blob decoded", name)
		}
	}
}
