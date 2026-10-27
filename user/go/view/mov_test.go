package main

// mov_test.go is the demux's class-A evidence: the committed fixture
// parses, its ten samples decode through our own JPEG decoder to pinned
// sums, the duration arithmetic is pinned, and every bound and lie the
// walker must refuse is refused BY NAME.
//
// The fixture itself is generated, never downloaded: GOVIEW_WRITE_FIXTURE=1
// runs TestWriteMOVFixture, which encodes ten frames through the host's
// image/jpeg (BSD-3-Clause, encoder only — see testdata/SOURCES.txt) and
// wraps them in the same moov/mdat box structure the demux reads. The
// normal tests read the committed bytes and never regenerate, because
// image/jpeg's output may drift across Go releases.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"math"
	"os"
	"testing"

	"virelai/pcm"
	"virelai/vi"
	"virelai/webrender"
)

const movFixture = "testdata/clip_160x120.mov"

// movFrameSums pins the FNV-1a-64 of each fixture frame as OUR decoder
// renders it — the same sums the live gate's run 02 asserts in the
// `gview: frame i/10` receipts (host and guest run the same decoder).
var movFrameSums = [10]uint64{
	0x841b31cc2db2c534, 0xf535f76c11a59fbc, 0xef521a8d3b1da9d4, 0x48d4cb3ca3e0ff48, 0xb822f8b9894c4bb5,
	0x1b34553fbeb17b7d, 0x1006bb4d92747eb1, 0x46ea77e6477c25d8, 0x4977773abca0c53c, 0xd86a2ac898451c12,
}

// ---------------------------------------------------------------------------
// Fixture generator (env-gated — see the header note and SOURCES.txt)
// ---------------------------------------------------------------------------

// movPalette is the band's 16 colours: distinct hues so a JPEG-decoded
// band stays a band, and a one-step slide keeps every frame's sum
// distinct — the same "moving band" idea play10_160x120.gif carries.
var movPalette = [16][3]byte{
	{0xd4, 0x20, 0x20}, {0xe0, 0x68, 0x18}, {0xe8, 0xa8, 0x18}, {0xc8, 0xc8, 0x20},
	{0x80, 0xd0, 0x28}, {0x28, 0xc8, 0x40}, {0x18, 0xd0, 0x90}, {0x18, 0xc8, 0xc8},
	{0x20, 0x90, 0xd8}, {0x28, 0x50, 0xe0}, {0x50, 0x30, 0xd8}, {0x90, 0x28, 0xd0},
	{0xd0, 0x28, 0xa0}, {0xd8, 0x28, 0x58}, {0xb0, 0x60, 0x30}, {0xf0, 0xf0, 0xf0},
}

// movGenFrame draws frame i: a diagonal 16-colour band (8 px stripes
// along x+y) slid i steps — the same one-step slide the GIF fixture
// plays, so the MOV leg's moving picture is the familiar shape.
func movGenFrame(i int) *image.RGBA {
	const w, h = 160, 120
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := movPalette[((x+y)/8+i)%16]
			o := img.PixOffset(x, y)
			img.Pix[o+0], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = c[0], c[1], c[2], 0xff
		}
	}
	return img
}

// TestWriteMOVFixture regenerates testdata/clip_160x120.mov: ten 160x120
// `jpeg` samples at 100 ms each (media timescale 1000), quality 75, in a
// moov-then-mdat QuickTime file carrying the `qt  ` brand. Skipped
// unless GOVIEW_WRITE_FIXTURE=1 — the gate and the class-A tests consume
// the committed bytes.
func TestWriteMOVFixture(t *testing.T) {
	if os.Getenv("GOVIEW_WRITE_FIXTURE") == "" {
		t.Skip("fixture generation is env-gated (GOVIEW_WRITE_FIXTURE=1)")
	}
	var samples [][]byte
	for i := 0; i < 10; i++ {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, movGenFrame(i), &jpeg.Options{Quality: 75}); err != nil {
			t.Fatalf("jpeg encode frame %d: %v", i, err)
		}
		samples = append(samples, buf.Bytes())
	}
	mov := movBuild(samples, 160, 120, 1000, 100, "jpeg")
	if err := os.WriteFile(movFixture, mov, 0o644); err != nil {
		t.Fatal(err)
	}
	// The generator's contract is the demux's: the bytes just written
	// must parse back through the same walker.
	v, err := demuxMOV(mov)
	if err != nil {
		t.Fatalf("generated fixture fails its own demux: %v", err)
	}
	if len(v.samples) != 10 || v.timescale != 1000 || v.codec != "jpeg" {
		t.Fatalf("generated demux = %d samples ts=%d codec=%s", len(v.samples), v.timescale, v.codec)
	}
	t.Logf("wrote %s: %d bytes, %d samples", movFixture, len(mov), len(v.samples))
}

// ---------------------------------------------------------------------------
// Host-side MOV writer — the generator's encoder and the hostile tests'
// honest file to mutate. Emits ftyp + moov + mdat, moov first so the
// header sniff finds tkhd inside readHead's 4 KiB.
// ---------------------------------------------------------------------------

func movBox(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

func movBox64(typ string, payload []byte) []byte {
	b := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint32(b[0:4], 1)
	copy(b[4:8], typ)
	binary.BigEndian.PutUint64(b[8:16], uint64(len(b)))
	copy(b[16:], payload)
	return b
}

func u32f(v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return b[:]
}

func u16f(v uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return b[:]
}

func movCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// movVisualEntry builds an 86-byte visual sample entry for the given
// codec fourcc — `jpeg` for the fixture, anything else for the codec
// refusal tests.
func movVisualEntry(codec string, w, h uint32) []byte {
	var e []byte
	e = append(e, make([]byte, 6)...)  // reserved
	e = append(e, u16f(1)...)          // data_reference_index
	e = append(e, make([]byte, 16)...) // version/revision/vendor/quality
	e = append(e, u16f(uint16(w))...)  // width
	e = append(e, u16f(uint16(h))...)  // height
	e = append(e, u32f(0x00480000)...) // hres 72dpi
	e = append(e, u32f(0x00480000)...) // vres
	e = append(e, u32f(0)...)          // reserved
	e = append(e, u16f(1)...)          // frame_count
	e = append(e, make([]byte, 32)...) // compressorname (pascal, empty)
	e = append(e, u16f(24)...)         // depth
	e = append(e, u16f(0xffff)...)     // pre_defined
	if len(e) != 78 {
		panic("visual entry size drift")
	}
	return movBox(codec, e)
}

// movBuild renders a complete MOV: ftyp (qt brand), moov (mvhd + one
// vide trak), mdat holding the samples back to back. timescale/dur are
// the mdhd timescale and each sample's stts delta. Extra traks may be
// appended through extra (already-boxed), e.g. a soun track.
func movBuild(samples [][]byte, w, h, timescale, dur uint32, codec string, extra ...[]byte) []byte {
	ftyp := movBox("ftyp", append(append([]byte("qt  "), u32f(0x200)...), []byte("qt  ")...))

	mvhd := movBox("mvhd", func() []byte {
		var b []byte
		b = append(b, u32f(0)...)                  // version+flags
		b = append(b, movCat(u32f(0), u32f(0))...) // creation/modification
		b = append(b, u32f(timescale)...)
		b = append(b, u32f(uint32(len(samples))*dur)...)
		b = append(b, u32f(0x00010000)...) // rate 1.0
		b = append(b, u16f(0x0100)...)     // volume 1.0
		b = append(b, make([]byte, 10)...)
		b = append(b, movUnityMatrix()...)
		b = append(b, make([]byte, 24)...)
		b = append(b, u32f(2)...) // next_track_id
		return b
	}())

	tkhd := movBox("tkhd", func() []byte {
		var b []byte
		b = append(b, u32f(0x000007)...)           // enabled | in_movie | in_preview
		b = append(b, movCat(u32f(0), u32f(0))...) // creation/modification
		b = append(b, u32f(1)...)                  // track_id
		b = append(b, u32f(0)...)
		b = append(b, u32f(uint32(len(samples))*dur)...)
		b = append(b, make([]byte, 8)...)
		b = append(b, movCat(u16f(0), u16f(0), u16f(0), u16f(0))...) // layer/alt/volume/reserved
		b = append(b, movUnityMatrix()...)
		b = append(b, u32f(w<<16)...)
		b = append(b, u32f(h<<16)...)
		return b
	}())

	mdhd := movBox("mdhd", func() []byte {
		var b []byte
		b = append(b, u32f(0)...)
		b = append(b, movCat(u32f(0), u32f(0))...)
		b = append(b, u32f(timescale)...)
		b = append(b, u32f(uint32(len(samples))*dur)...)
		b = append(b, u16f(0x55c4)...) // und language
		b = append(b, u16f(0)...)
		return b
	}())

	hdlr := movBox("hdlr", func() []byte {
		var b []byte
		b = append(b, u32f(0)...) // version+flags
		b = append(b, u32f(0)...) // pre_defined
		b = append(b, "vide"...)  // handler_type
		b = append(b, make([]byte, 12)...)
		return b
	}())

	vmhd := movBox("vmhd", movCat(u32f(0x000001), u16f(0), u16f(0), u16f(0), u16f(0)))
	dref := movBox("dref", movCat(u32f(0), u32f(1), movBox("url ", u32f(0x000001))))
	dinf := movBox("dinf", dref)

	stsd := movBox("stsd", movCat(u32f(0), u32f(1), movVisualEntry(codec, w, h)))

	stts := movBox("stts", movCat(u32f(0), u32f(1),
		u32f(uint32(len(samples))), u32f(dur)))

	stsc := movBox("stsc", movCat(u32f(0), u32f(1),
		u32f(1), u32f(uint32(len(samples))), u32f(1)))

	var stszBody []byte
	stszBody = append(stszBody, movCat(u32f(0), u32f(0), u32f(uint32(len(samples))))...)
	for _, s := range samples {
		stszBody = append(stszBody, u32f(uint32(len(s)))...)
	}
	stsz := movBox("stsz", stszBody)

	// stco is patched after moov is laid out: the samples live in mdat,
	// which follows moov, and moov's size does not depend on the value.
	stco := movBox("stco", movCat(u32f(0), u32f(1), u32f(0)))

	stbl := movBox("stbl", bytes.Join([][]byte{stsd, stts, stsc, stsz, stco}, nil))
	minf := movBox("minf", bytes.Join([][]byte{vmhd, dinf, stbl}, nil))
	mdia := movBox("mdia", bytes.Join([][]byte{mdhd, hdlr, minf}, nil))
	trak := movBox("trak", bytes.Join([][]byte{tkhd, mdia}, nil))

	moovChildren := [][]byte{mvhd, trak}
	moovChildren = append(moovChildren, extra...)
	moov := movBox("moov", bytes.Join(moovChildren, nil))

	// Patch the single stco entry: mdat's samples start right after its
	// 8-byte header, at len(ftyp)+len(moov)+8.
	mdatOff := uint32(len(ftyp) + len(moov) + 8)
	i := bytes.Index(moov, []byte("stco"))
	binary.BigEndian.PutUint32(moov[i+12:i+16], mdatOff)

	var mdat []byte
	for _, s := range samples {
		mdat = append(mdat, s...)
	}
	return bytes.Join([][]byte{ftyp, moov, movBox("mdat", mdat)}, nil)
}

func movUnityMatrix() []byte {
	return movCat(u32f(0x00010000), u32f(0), u32f(0),
		u32f(0), u32f(0x00010000), u32f(0),
		u32f(0), u32f(0), u32f(0x40000000))
}

// ---------------------------------------------------------------------------
// The committed fixture
// ---------------------------------------------------------------------------

// TestDemuxParsesTheFixture is the walker's happy path: ten `jpeg`
// samples at 100 ms on a 1000 Hz timescale, one vide track, qt brand.
func TestDemuxParsesTheFixture(t *testing.T) {
	data := readFixture(t, movFixture)
	if formatOfHeader(data) != fmtMOV {
		t.Fatal("fixture did not sniff as fmtMOV")
	}
	v, err := demuxMOV(data)
	if err != nil {
		t.Fatalf("demuxMOV: %v", err)
	}
	if len(v.samples) != 10 || v.timescale != 1000 || v.codec != "jpeg" {
		t.Fatalf("demux = %d samples ts=%d codec=%s, want 10/1000/jpeg",
			len(v.samples), v.timescale, v.codec)
	}
	if v.brand != "qt  " || v.w != 160 || v.h != 120 {
		t.Fatalf("demux = brand %q %dx%d, want qt/160x120", v.brand, v.w, v.h)
	}
	for i, s := range v.samples {
		if s.dur != 100 {
			t.Fatalf("sample %d dur = %d, want 100 (100ms)", i, s.dur)
		}
		if s.size > movSampleBytes || s.off+uint64(s.size) > uint64(len(data)) {
			t.Fatalf("sample %d escapes the file", i)
		}
	}
}

// TestMOVFixturePinsFrameSums decodes every sample through our own
// decoder and pins the FNV-1a of each frame — the numbers the live
// gate's run 02 asserts in the stepper receipts. decodeForView routes
// the file through the demux and the frame source lazily decodes.
func TestMOVFixturePinsFrameSums(t *testing.T) {
	data := readFixture(t, movFixture)
	im, src, err := decodeForView(data)
	if err != nil {
		t.Fatalf("decodeForView: %v", err)
	}
	if src == nil || len(src.frames) != 10 || src.video == nil {
		t.Fatalf("src = %v, want a 10-frame video source", src)
	}
	if im.Width != 160 || im.Height != 120 || im.Frames != 10 {
		t.Fatalf("shown = %dx%d frames=%d, want 160x120 x10", im.Width, im.Height, im.Frames)
	}
	sums := map[uint64]int{}
	for i := range src.frames {
		if src.frames[i].delayNs != 100_000_000 || src.frames[i].delayMs != 100 {
			t.Fatalf("frame %d dwell = %d ns/%d ms, want 100ms", i+1, src.frames[i].delayNs, src.frames[i].delayMs)
		}
		got := frameSum(src.pix(i))
		sums[got] = i
		if got != movFrameSums[i] {
			t.Fatalf("frame %d sum = %016x, want pinned %016x", i+1, got, movFrameSums[i])
		}
	}
	if len(sums) != 10 {
		t.Fatal("clip_160x120 must decode to ten distinct frames")
	}
}

// ---------------------------------------------------------------------------
// The named refusals
// ---------------------------------------------------------------------------

// TestDemuxRefusalsByName walks the card's hostile list: each corruption
// of an honest file — or a hostile table — must be refused by its
// refusal's NAME, never by panic, partial parse or silent clamp.
func TestDemuxRefusalsByName(t *testing.T) {
	good := movBuild([][]byte{[]byte("frame-one"), []byte("frame-two")}, 160, 120, 1000, 100, "jpeg")

	// deep nests moov inside moov so the innermost leaf sits at depth.
	deep := []byte("leaf")
	for i := 0; i < 9; i++ {
		deep = movBox("moov", deep)
	}

	// many tops the file up past the 1024-box budget with zero-payload
	// `free` boxes before the moov — the walk refuses the 1025th read.
	var many []byte
	for i := 0; i < 1025; i++ {
		many = append(many, movBox("free", nil)...)
	}
	many = append(many, movBox("ftyp", append(append([]byte("qt  "), u32f(0x200)...), []byte("qt  ")...))...)
	many = append(many, movBox("moov", nil)...)

	// patch rewrites the first size field of the box whose fourcc sits
	// at pos (the box starts at pos-4).
	patch := func(b []byte, pos int, size uint32) []byte {
		c := append([]byte(nil), b...)
		binary.BigEndian.PutUint32(c[pos-4:], size)
		return c
	}
	stsdAt := bytes.Index(good, []byte("stsd"))
	if stsdAt < 0 {
		t.Fatal("baseline fixture lost its stsd")
	}

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"truncated header", good[:5], errMOVInvalid},
		{"truncated box", patch(good, 4, uint32(len(good)+64)), errMOVInvalid},
		{"size overflow", patch(good, 4, 0x7fffffff), errMOVInvalid},
		{"depth 9", append(movBox("ftyp", append(append([]byte("qt  "), u32f(0x200)...), []byte("qt  ")...)), deep...), errMOVLimit},
		{"1025 boxes", many, errMOVLimit},
		{"moof", append(append([]byte(nil), movBox("ftyp", append(append([]byte("qt  "), u32f(0x200)...), []byte("qt  ")...))...), movBox("moof", nil)...), errMOVFragmented},
		{"nested size-0", movBox("moov", append(movBox("trak", nil), func() []byte {
			b := movBox("mdia", nil)
			binary.BigEndian.PutUint32(b[:4], 0)
			return b
		}()...)), errMOVInvalid},
	}
	for _, c := range cases {
		if _, err := demuxMOV(c.data); err != c.want {
			t.Errorf("%s: demuxMOV = %v, want %v", c.name, err, c.want)
		}
	}
}

// TestDemuxTableRefusals mutates the sample tables: the bounds and
// count-mismatch refusals live in resolve(), past the box walk.
func TestDemuxTableRefusals(t *testing.T) {
	mk := func(samples [][]byte) []byte {
		return movBuild(samples, 160, 120, 1000, 100, "jpeg")
	}
	good := mk([][]byte{[]byte("s1"), []byte("s2")})

	setU32 := func(b []byte, at int, v uint32) []byte {
		c := append([]byte(nil), b...)
		binary.BigEndian.PutUint32(c[at:], v)
		return c
	}
	at := func(b []byte, fourcc string) int {
		i := bytes.Index(b, []byte(fourcc))
		if i < 0 {
			t.Fatalf("baseline lost %s", fourcc)
		}
		return i
	}

	// stsz declares 601 samples — the cap fires while the table parses.
	many := setU32(good, at(good, "stsz")+12, 601)

	// stsz count 1 vs stts sum 2 → the count mismatch refusal.
	mism := setU32(good, at(good, "stsz")+12, 1)

	// The single chunk's offset beyond the file end.
	far := setU32(good, at(good, "stco")+12, uint32(len(good)+4096))

	// Sample 2's size swells past the file end.
	big := setU32(good, at(good, "stsz")+16, uint32(len(good)))

	// mdhd timescale zeroed.
	zero := setU32(good, at(good, "mdhd")+16, 0)

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"601 samples", many, errMOVLimit},
		{"stsz/stts mismatch", mism, errMOVInvalid},
		{"chunk past EOF", far, errMOVLimit},
		{"sample past EOF", big, errMOVLimit},
		{"zero timescale", zero, errMOVInvalid},
	}
	for _, c := range cases {
		if _, err := demuxMOV(c.data); err != c.want {
			t.Errorf("%s: demuxMOV = %v, want %v", c.name, err, c.want)
		}
	}
}

// TestDemuxCodecRefusalNamesTheFourcc: a structurally honest file whose
// sample entry is not `jpeg` is refused AFTER the demux read it — the
// refusal carries the fourcc and rides ErrImageUnsupported, and the tag
// follows the container brand (MP4 for isom/mp4x, MOV otherwise).
func TestDemuxCodecRefusalNamesTheFourcc(t *testing.T) {
	data := movBuild([][]byte{[]byte("not-a-jpeg")}, 160, 120, 1000, 100, "avc1")
	_, err := demuxMOV(data)
	var mce movCodecError
	if !errors.As(err, &mce) {
		t.Fatalf("avc1 demux = %v, want a movCodecError", err)
	}
	if mce.fourcc != "avc1" || mce.tag != "MOV" {
		t.Fatalf("codec refusal = tag=%s fourcc=%s, want MOV/avc1", mce.tag, mce.fourcc)
	}
	if !errors.Is(err, webrender.ErrImageUnsupported) {
		t.Fatal("the codec refusal must satisfy errors.Is(ErrImageUnsupported)")
	}
	if err.Error() != "mov-unsupported-codec-avc1" {
		t.Fatalf("refusal named itself %q", err)
	}

	// An isom brand re-tags the same refusal MP4.
	isom := append([]byte(nil), data...)
	i := bytes.Index(isom, []byte("ftyp"))
	copy(isom[i+4:i+8], "isom")
	_, err = demuxMOV(isom)
	if mce2, ok := err.(movCodecError); !ok || mce2.tag != "MP4" {
		t.Fatalf("isom brand refusal = %v, want MP4 tag", err)
	}
}

// TestDemuxUnplayableSounTrackIsSkippedAndNamed: a soun track whose
// tables never resolve does not stop the vide track — the demux skips it
// and the load receipt names the bare audio=skipped (there is no codec
// to name when no stsd was read).
func TestDemuxUnplayableSounTrackIsSkippedAndNamed(t *testing.T) {
	soun := movBox("trak", movBox("mdia", bytes.Join([][]byte{
		movBox("hdlr", func() []byte {
			var b []byte
			b = append(b, u32f(0)...)
			b = append(b, u32f(0)...)
			b = append(b, "soun"...)
			b = append(b, make([]byte, 12)...)
			return b
		}()),
	}, nil)))
	v, err := demuxMOV(movBuild([][]byte{[]byte("s1")}, 160, 120, 1000, 100, "jpeg", soun))
	if err != nil {
		t.Fatalf("demuxMOV with a sound track: %v", err)
	}
	if v.audio != nil || v.audioCodec != "" {
		t.Fatalf("unresolvable soun track demuxed audio=%+v codec=%q", v.audio, v.audioCodec)
	}
	if got := movAudioReceipt(v); got != " audio=skipped" {
		t.Fatalf("load receipt token = %q, want the bare audio=skipped", got)
	}
}

// ---------------------------------------------------------------------------
// Durations and playback
// ---------------------------------------------------------------------------

// TestMovDelayNs pins duration × 10⁹ / timescale with its 1 ms floor:
// the fixture's 100/1000 → 100 ms, a sub-millisecond sample clamps up,
// and a zero-duration sample still lands a TIMER event.
func TestMovDelayNs(t *testing.T) {
	for _, c := range []struct {
		dur       uint64
		timescale uint32
		want      uint64
	}{
		{100, 1000, 100_000_000},
		{1, 1000, 1_000_000},
		{33, 1000, 33_000_000},
		{0, 1000, 1_000_000},
		{1, 1_000_000, 1_000_000},
		{100, 0, 1_000_000}, // unreachable — mdhd refuses a zero timescale — but the floor holds anyway
		{1500, 600, 2_500_000_000},
	} {
		if got := movDelayNs(c.dur, c.timescale); got != c.want {
			t.Errorf("movDelayNs(%d,%d) = %d, want %d", c.dur, c.timescale, got, c.want)
		}
	}
}

// TestMOVPlaybackTicksDriveTheSchedule is the frame-source playback
// check on the real fixture: the M170e machinery advances one sample per
// TIMER event, arms each sample's own duration, closes the loop on wrap
// and records the slowest sample decode on the injected clock — the same
// shape TestPlayTicksDriveTheDelaySchedule pins for GIF.
func TestMOVPlaybackTicksDriveTheSchedule(t *testing.T) {
	_, src, err := decodeForView(readFixture(t, movFixture))
	if err != nil {
		t.Fatal(err)
	}

	var arms []uint64
	var clock int64
	oldArm, oldCancel, oldNow := playArm, playCancel, playNow
	playArm = func(ns uint64) int64 { arms = append(arms, ns); return 0 }
	playCancel = func() int64 { return 1 }
	// Each playNow call adds 2 µs of fake wall time, so the lazy sample
	// decodes report a nonzero decode_us_max deterministically.
	playNow = func() int64 { clock += 2_000; return clock }
	defer func() { playArm, playCancel, playNow = oldArm, oldCancel, oldNow }()

	fire := func() { clock += int64(arms[len(arms)-1]) }

	a := &app{anim: src}
	if !a.togglePlay() || !a.playing {
		t.Fatal("togglePlay must start playback on the video source")
	}
	if len(arms) != 1 || arms[0] != 100_000_000 {
		t.Fatalf("first arm = %v, want [100ms]", arms)
	}
	// Nine TIMER events mid-loop: nine decodes each measured 2 µs on the
	// fake clock, so the loop's decode_us_max is already recorded.
	for i := 0; i < 9; i++ {
		fire()
		if !a.playTick() {
			t.Fatalf("tick %d did not advance", i+1)
		}
	}
	if a.anim.decMaxUs != 2 {
		t.Fatalf("decode_us_max = %d µs mid-loop, want 2 (the 2 µs fake-clock decode)", a.anim.decMaxUs)
	}
	// The tenth tick wraps the loop: the receipt prints decode_us_max
	// and the counter resets for the next loop.
	fire()
	if !a.playTick() || a.frame != 0 {
		t.Fatalf("tick 10 did not wrap: frame=%d", a.frame)
	}
	if a.anim.decMaxUs != 0 {
		t.Fatalf("decode_us_max = %d after the loop receipt, want reset", a.anim.decMaxUs)
	}
	if len(arms) != 11 {
		t.Fatalf("arms = %v, want one per frame plus the re-arm", arms)
	}
	for i, d := range arms {
		if d != 100_000_000 {
			t.Fatalf("arm %d = %d ns, want 100ms per sample", i, d)
		}
	}
	if a.playLastElapsed < 1_000_000_000 {
		t.Fatalf("loop elapsed = %d ns, want ≥1s of playing time", a.playLastElapsed)
	}
	// The stepper decodes on demand through the same path: stepping to
	// frame 2 shows sample 2's decode, pinned by the sum table.
	if !a.stepFrame(1) || a.frame != 1 {
		t.Fatal("stepping the video source did not land on frame 2")
	}
	if got := frameSum(a.img.Pix); got != movFrameSums[1] {
		t.Fatalf("frame 2 sum = %016x, want pinned %016x", got, movFrameSums[1])
	}
}

// TestMOVHeaderDimsFromFixture: the window sizing sniff finds the
// fixture's tkhd inside the 4 KiB head — a moov-first file sizes its own
// window at 160x120.
func TestMOVHeaderDimsFromFixture(t *testing.T) {
	data := readFixture(t, movFixture)
	if len(data) > headMax {
		data = data[:headMax]
	}
	iw, ih, ok := headerDims(data)
	if !ok || iw != 160 || ih != 120 {
		t.Fatalf("headerDims = %dx%d %v, want 160x120", iw, ih, ok)
	}
	if got := headerSize(data); got != (winSize{328, 264}) {
		t.Fatalf("headerSize = %+v, want 328x264", got)
	}
	// A head that ends before the moov yields no dims, not garbage.
	if _, _, ok := headerDims(data[:16]); ok {
		t.Fatal("a 16-byte head must not size the window")
	}
}

// ---------------------------------------------------------------------------
// The soun track (M202b #3385)
// ---------------------------------------------------------------------------

// movSoundSampleEntry builds one stsd sound sample entry: the classic
// 36-byte QT description for version 0/1, plus the 36-byte lpcm v2
// extension (f64 rate, u32 channels, the flags word and the packet
// constants) when version is 2.
func movSoundSampleEntry(codec string, ch, bits uint16, rate uint32, version uint16, flags uint32) []byte {
	var e []byte
	e = append(e, make([]byte, 6)...) // reserved
	e = append(e, u16f(1)...)         // data_reference_index
	e = append(e, u16f(version)...)
	e = append(e, u16f(0)...) // revision
	e = append(e, u32f(0)...) // vendor
	e = append(e, u16f(ch)...)
	e = append(e, u16f(bits)...)
	e = append(e, u16f(0)...) // compression id
	e = append(e, u16f(0)...) // packet size
	e = append(e, u32f(rate<<16)...)
	if version == 2 {
		var f [8]byte
		binary.BigEndian.PutUint64(f[:], math.Float64bits(float64(rate)))
		e = append(e, u32f(72)...) // sizeOfStructOnly
		e = append(e, f[:]...)
		e = append(e, u32f(uint32(ch))...)
		e = append(e, u32f(0x7f000000)...)
		e = append(e, u32f(8)...)
		e = append(e, u32f(flags)...)
		e = append(e, u32f(uint32(ch)*uint32(bits)/8)...) // bytes/packet
		e = append(e, u32f(1)...)                         // frames/packet
	}
	return movBox(codec, e)
}

// movSounTrak builds a complete soun trak — the 0x0 tkhd a real sound
// track carries, mdhd at the sample-rate timescale, hdlr soun and the
// stbl tables resolve() walks (stsz in its uniform-sample-size form,
// stco a placeholder the file assembler patches).
func movSounTrak(codec string, ch, bits uint16, rate uint32, version uint16, flags uint32,
	nsamp, sampBytes, sampDur uint32) []byte {
	tkhd := movBox("tkhd", movCat(
		u32f(0x000007), u32f(0), u32f(0), u32f(2), u32f(0),
		u32f(nsamp*sampDur),
		make([]byte, 8),
		u16f(0), u16f(0), u16f(0), u16f(0),
		movUnityMatrix(),
		u32f(0), u32f(0), // a sound track has no picture
	))
	mdhd := movBox("mdhd", movCat(u32f(0), u32f(0), u32f(0),
		u32f(rate), u32f(nsamp*sampDur), u16f(0x55c4), u16f(0)))
	hdlr := movBox("hdlr", movCat(u32f(0), u32f(0), []byte("soun"), make([]byte, 12)))
	smhd := movBox("smhd", movCat(u32f(0), u16f(0), u16f(0)))
	dinf := movBox("dinf", movBox("dref", movCat(u32f(0), u32f(1), movBox("url ", u32f(0x000001)))))
	stsd := movBox("stsd", movCat(u32f(0), u32f(1),
		movSoundSampleEntry(codec, ch, bits, rate, version, flags)))
	stts := movBox("stts", movCat(u32f(0), u32f(1), u32f(nsamp), u32f(sampDur)))
	stsc := movBox("stsc", movCat(u32f(0), u32f(1), u32f(1), u32f(nsamp), u32f(1)))
	stsz := movBox("stsz", movCat(u32f(0), u32f(sampBytes), u32f(nsamp)))
	stco := movBox("stco", movCat(u32f(0), u32f(1), u32f(0)))
	stbl := movBox("stbl", bytes.Join([][]byte{stsd, stts, stsc, stsz, stco}, nil))
	minf := movBox("minf", bytes.Join([][]byte{smhd, dinf, stbl}, nil))
	mdia := movBox("mdia", bytes.Join([][]byte{mdhd, hdlr, minf}, nil))
	return movBox("trak", bytes.Join([][]byte{tkhd, mdia}, nil))
}

// movWithAudio appends a soun trak and its PCM payload to a movBuild
// file: the audio bytes land at the end of mdat and both stco tables
// are repointed for the grown moov. The result is run through the demux
// so a test can never pin a fixture that does not parse.
func movWithAudio(t *testing.T, vsamples [][]byte, atrak []byte, audio []byte) []byte {
	base := movBuild(vsamples, 160, 120, 1000, 100, "jpeg")
	moovAt := bytes.Index(base, []byte("moov")) - 4
	mdatAt := bytes.Index(base, []byte("mdat")) - 4
	if moovAt < 0 || mdatAt < 0 || mdatAt < moovAt {
		t.Fatal("baseline mov lost its top-level boxes")
	}
	moov, mdatBody := base[moovAt:mdatAt], base[mdatAt+8:]
	newMoov := movBox("moov", movCat(moov[8:], atrak))
	newMdat := movBox("mdat", movCat(mdatBody, audio))
	voff := uint32(moovAt + len(newMoov) + 8)
	i := bytes.Index(newMoov, []byte("stco"))
	binary.BigEndian.PutUint32(newMoov[i+12:i+16], voff)
	j := bytes.LastIndex(newMoov, []byte("stco"))
	binary.BigEndian.PutUint32(newMoov[j+12:j+16], voff+uint32(len(mdatBody)))
	out := movCat(base[:moovAt], newMoov, newMdat)
	if _, err := demuxMOV(out); err != nil {
		t.Fatalf("movWithAudio built an undemuxable file: %v", err)
	}
	return out
}

// movAudioFixture is the card's pinned shape: the committed clip's ten
// JPEG samples beside a sowt s16 mono soun track — ten 4410-byte samples
// of 2205 frames at 22050 Hz, exactly the video loop's one second.
func movAudioFixture(t *testing.T) []byte {
	clip := readFixture(t, movFixture)
	cv, err := demuxMOV(clip)
	if err != nil {
		t.Fatalf("clip demux: %v", err)
	}
	var vs [][]byte
	for i := range cv.samples {
		vs = append(vs, cv.sampleBytes(i))
	}
	audio := make([]byte, 10*4410)
	for i := range audio {
		audio[i] = byte(i / 7) // a nonzero, non-constant pattern
	}
	return movWithAudio(t, vs,
		movSounTrak("sowt", 1, 16, 22050, 0, 0, 10, 4410, 2205), audio)
}

// TestDemuxSounTrackDecodes is the card's happy path: a soun track with
// a sowt PCM entry demuxes beside the vide track — the decode tuple,
// the resolved sample table, and the receipt's `audio=sowt` token.
func TestDemuxSounTrackDecodes(t *testing.T) {
	v, err := demuxMOV(movAudioFixture(t))
	if err != nil {
		t.Fatalf("demuxMOV: %v", err)
	}
	a := v.audio
	if a == nil {
		t.Fatal("soun track did not demux to a movAudio")
	}
	if a.codec != "sowt" || a.tag != pcm.FormatPCM || a.channels != 1 ||
		a.rate != 22050 || a.bits != 16 || a.be || a.bias {
		t.Fatalf("audio tuple = %+v, want sowt pcm16 mono 22050 LE", a)
	}
	if len(a.samples) != 10 || a.timescale != 22050 {
		t.Fatalf("audio table = %d samples ts=%d", len(a.samples), a.timescale)
	}
	for i, s := range a.samples {
		if s.size != 4410 || s.dur != 2205 {
			t.Fatalf("audio sample %d = %d B dur %d, want 4410/2205", i, s.size, s.dur)
		}
	}
	if got := movAudioReceipt(v); got != " audio=sowt ch=1 rate=22050" {
		t.Fatalf("receipt token = %q", got)
	}
	// The materialized stream is the declared 22050 frames of LE s16.
	info, b := a.wavInfo(v.data)
	if info.Frames != 22050 || info.DataSize != 44100 || len(b) != 44100 {
		t.Fatalf("wavInfo = frames %d data %d len %d", info.Frames, info.DataSize, len(b))
	}
}

// TestDemuxSounRefusalsByName: every soun entry the demux will not
// synthesize is named — the coded fourccs by their own name, a
// PCM-family member with unplayable parameters the same way, and a
// track whose tables never resolved with the bare audio=skipped.
func TestDemuxSounRefusalsByName(t *testing.T) {
	vs := [][]byte{[]byte("v1")}
	audio := make([]byte, 4*4) // 4 stereo s16 frames, content irrelevant
	mk := func(codec string, ch, bits uint16, rate uint32, version uint16, flags uint32, sampBytes uint32, n uint32) []byte {
		return movWithAudio(t, vs,
			movSounTrak(codec, ch, bits, rate, version, flags, n, sampBytes, 100), audio[:sampBytes*n])
	}
	cases := []struct {
		name       string
		data       []byte
		wantCodec  string // the named codec, or "" for the bare skip
		wantPlayOK bool
	}{
		{"mp4a named", mk("mp4a", 1, 16, 22050, 0, 0, 4, 4), "mp4a", false},
		{"ulaw named", mk("ulaw", 1, 16, 22050, 0, 0, 4, 4), "ulaw", false},
		{"lpcm v0 named", mk("lpcm", 1, 16, 22050, 0, 0, 4, 4), "lpcm", false},
		{"sowt 12-bit named", mk("sowt", 1, 12, 22050, 0, 0, 4, 4), "sowt", false},
		{"sowt 3ch named", mk("sowt", 3, 16, 22050, 0, 0, 4, 4), "sowt", false},
		{"sowt slow rate named", mk("sowt", 1, 16, 4000, 0, 0, 4, 4), "sowt", false},
		{"twos plays", mk("twos", 2, 16, 22050, 0, 0, 4, 4), "", true},
		{"raw 8 plays", mk("raw ", 1, 8, 22050, 0, 0, 4, 4), "", true},
		{"lpcm v2 s16be plays", mk("lpcm", 1, 16, 22050, 2, 0x0a, 4, 4), "", true},
	}
	for _, c := range cases {
		v, err := demuxMOV(c.data)
		if err != nil {
			t.Fatalf("%s: demuxMOV = %v", c.name, err)
		}
		if c.wantPlayOK {
			if v.audio == nil {
				t.Errorf("%s: audio did not demux", c.name)
			}
			continue
		}
		if v.audio != nil || v.audioCodec != c.wantCodec {
			t.Errorf("%s: audio=%+v codec=%q, want refused %q", c.name, v.audio, v.audioCodec, c.wantCodec)
		}
		if got := movAudioReceipt(v); got != " audio=skipped codec="+c.wantCodec {
			t.Errorf("%s: receipt = %q", c.name, got)
		}
	}
	// A soun table that never resolves is the bare skip, no codec name:
	// stsz declares four samples but the mdat payload holds three.
	short := movWithAudio(t, vs, movSounTrak("sowt", 2, 16, 22050, 0, 0, 4, 4, 100), audio[:12])
	v, err := demuxMOV(short)
	if err != nil {
		t.Fatalf("short audio table: %v", err)
	}
	if v.audio != nil || v.audioCodec != "" {
		t.Fatalf("unresolvable soun table: audio=%+v codec=%q", v.audio, v.audioCodec)
	}
	if got := movAudioReceipt(v); got != " audio=skipped" {
		t.Fatalf("receipt = %q, want bare skipped", got)
	}
	// And a video-only file keeps the bare skip — the honest no-track case.
	v, err = demuxMOV(movBuild(vs, 160, 120, 1000, 100, "jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if v.audio != nil || v.audioCodec != "" {
		t.Fatalf("video-only demux: audio=%+v codec=%q", v.audio, v.audioCodec)
	}
	if got := movAudioReceipt(v); got != " audio=skipped" {
		t.Fatalf("video-only receipt = %q", got)
	}
}

// TestMOVSounEntryParams unit-pins the decode tuple each PCM-family
// codec resolves to — the endian swap, the sign bias and the lpcm flag
// semantics — plus the refusals the parser must make.
func TestMOVSounEntryParams(t *testing.T) {
	mk := func(codec string, ch, bits uint16, rate uint32, version uint16, flags uint32) []byte {
		// The parser sees the whole sample entry — size, fourcc, body.
		return movSoundSampleEntry(codec, ch, bits, rate, version, flags)
	}
	// lpcm flag bits, mirroring mov.go's constants.
	const (
		fFloat = 1 << 0
		fBE    = 1 << 1
		fSig   = 1 << 2
		fPack  = 1 << 3
		fNonI  = 1 << 5
	)
	cases := []struct {
		name       string
		entry      []byte
		want       *movSound
		wantNilMsg bool
	}{
		{"sowt s16", mk("sowt", 1, 16, 44100, 0, 0),
			&movSound{codec: "sowt", tag: pcm.FormatPCM, channels: 1, rate: 44100, bits: 16}, false},
		{"sowt s8 biased", mk("sowt", 2, 8, 22050, 0, 0),
			&movSound{codec: "sowt", tag: pcm.FormatPCM, channels: 2, rate: 22050, bits: 8, bias: true}, false},
		{"twos s16 be", mk("twos", 2, 16, 48000, 0, 0),
			&movSound{codec: "twos", tag: pcm.FormatPCM, channels: 2, rate: 48000, bits: 16, be: true}, false},
		{"raw u8 direct", mk("raw ", 1, 8, 8000, 0, 0),
			&movSound{codec: "raw ", tag: pcm.FormatPCM, channels: 1, rate: 8000, bits: 8}, false},
		{"raw u16 be+bias", mk("raw ", 1, 16, 22050, 0, 0),
			&movSound{codec: "raw ", tag: pcm.FormatPCM, channels: 1, rate: 22050, bits: 16, be: true, bias: true}, false},
		{"lpcm s16be v2", mk("lpcm", 1, 16, 44100, 2, fSig|fPack|fBE),
			&movSound{codec: "lpcm", tag: pcm.FormatPCM, channels: 1, rate: 44100, bits: 16, be: true}, false},
		{"lpcm f32le v2", mk("lpcm", 2, 32, 48000, 2, fFloat|fPack),
			&movSound{codec: "lpcm", tag: pcm.FormatFloat, channels: 2, rate: 48000, bits: 32}, false},
		{"lpcm u16le v2 biased", mk("lpcm", 1, 16, 22050, 2, fPack),
			&movSound{codec: "lpcm", tag: pcm.FormatPCM, channels: 1, rate: 22050, bits: 16, bias: true}, false},
		{"lpcm noninterleaved refused", mk("lpcm", 2, 16, 22050, 2, fSig|fPack|fNonI), nil, true},
		{"lpcm v1 refused", mk("lpcm", 1, 16, 22050, 1, 0), nil, true},
		{"lpcm f64 refused", mk("lpcm", 1, 64, 44100, 2, fFloat|fPack|fBE), nil, true},
		{"mp4a refused", mk("mp4a", 2, 16, 44100, 0, 0), nil, true},
		{"in24 refused", mk("in24", 1, 24, 44100, 0, 0), nil, true},
		{"sowt rate low refused", mk("sowt", 1, 16, 4000, 0, 0), nil, true},
		{"sowt 3ch refused", mk("sowt", 3, 16, 22050, 0, 0), nil, true},
		{"short entry refused", mk("sowt", 1, 16, 22050, 0, 0)[:20], nil, true},
	}
	for _, c := range cases {
		got := movSoundEntry(c.entry)
		if c.wantNilMsg {
			if got != nil {
				t.Errorf("%s: parsed %+v, want refused", c.name, got)
			}
			continue
		}
		if got == nil || *got != *c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestMOVAudioPCMBytesTransforms pins the byte munging the materialized
// stream applies: big-endian word swap, the sign bias, and the
// whole-frames check on each sample.
func TestMOVAudioPCMBytesTransforms(t *testing.T) {
	mk := func(bits uint16, be, bias bool, b ...byte) *movAudio {
		return &movAudio{codec: "x", tag: pcm.FormatPCM, channels: 1, bits: bits,
			be: be, bias: bias, samples: []movSample{{off: 0, size: uint32(len(b))}}}
	}
	cases := []struct {
		name string
		a    *movAudio
		data []byte
		want []byte
	}{
		{"sowt direct", mk(16, false, false, 1, 2, 3, 4), []byte{1, 2, 3, 4}, []byte{1, 2, 3, 4}},
		{"twos swap", mk(16, true, false, 0x12, 0x34, 0x56, 0x78), []byte{0x12, 0x34, 0x56, 0x78}, []byte{0x34, 0x12, 0x78, 0x56}},
		{"sowt8 bias", mk(8, false, true, 0x00, 0x80, 0x7f, 0xff), []byte{0x00, 0x80, 0x7f, 0xff}, []byte{0x80, 0x00, 0xff, 0x7f}},
		{"raw16 swap+bias", mk(16, true, true, 0x80, 0x00), []byte{0x80, 0x00}, []byte{0x00, 0x00}},
		{"raw8 direct", mk(8, false, false, 0x80, 0xff), []byte{0x80, 0xff}, []byte{0x80, 0xff}},
		{"f32 be swap", mk(32, true, false, 0x3f, 0x80, 0x00, 0x00), []byte{0x3f, 0x80, 0x00, 0x00}, []byte{0x00, 0x00, 0x80, 0x3f}},
		{"u16le bias only", mk(16, false, true, 0x00, 0x80), []byte{0x00, 0x80}, []byte{0x00, 0x00}},
	}
	for _, c := range cases {
		if got := c.a.pcmBytes(c.data); !bytes.Equal(got, c.want) {
			t.Errorf("%s: pcmBytes = %x, want %x", c.name, got, c.want)
		}
	}
	// A sample that is not a whole number of frames is not sound.
	bad := mk(16, false, false, 1, 2, 3)
	if got := bad.pcmBytes([]byte{1, 2, 3}); got != nil {
		t.Fatalf("partial-frame sample decoded %x, want nil", got)
	}
}

// fakeSeat is the host-test mixer seat: a bounded period queue that
// answers EAGAIN like the kernel and lets the test drain the device.
type fakeSeat struct {
	queued    int
	cap       int
	submitted uint64
	completed uint64
	underruns uint64
	started   bool
	aborted   bool
}

func (f *fakeSeat) Submit(p []byte) (int, error) {
	if f.queued >= f.cap {
		return 0, vi.ErrAudioBusy
	}
	f.queued++
	f.submitted += uint64(len(p))
	return len(p), nil
}
func (f *fakeSeat) Start() error { f.started = true; return nil }
func (f *fakeSeat) End() error   { return nil }
func (f *fakeSeat) Abort() error { f.aborted = true; return nil }
func (f *fakeSeat) Status() (vi.AudioStreamStatus, error) {
	return vi.AudioStreamStatus{Completed: f.completed, Underruns: f.underruns}, nil
}
func (f *fakeSeat) Generation() uint64 { return 7 }

// drain releases every queued period into the completed count — the
// device's real-time consumption, on demand.
func (f *fakeSeat) drain() { f.queued = 0 }

// movAudioTestSeams installs the fake audio seams and returns them plus
// a restore func. The device negotiates f32/48k/stereo like the VZ
// sound device does.
func movAudioTestSeams(seat *fakeSeat) func() {
	oldQ, oldO := audioQuery, audioOpen
	audioQuery = func() (vi.AudioInfo, error) {
		return vi.AudioInfo{Ready: 1, Format: vi.AudioFmtFloat, Rate: 7, Channels: 2}, nil
	}
	audioOpen = func(p vi.AudioStreamParams) (audioSeat, error) {
		if p.Format != vi.AudioFmtFloat || p.Rate != 7 || p.Channels != 2 {
			return nil, errors.New("seat opened for the wrong tuple")
		}
		return seat, nil
	}
	return func() { audioQuery, audioOpen = oldQ, oldO }
}

// TestMOVAudioPlaybackPumpsAndDrift is the playback half: a MOV with a
// soun track decodes+resamples at play start, the pump fills the fake
// queue, and the wrap's drift math measures the mixer's completed clock
// against the video loop's.
func TestMOVAudioPlaybackPumpsAndDrift(t *testing.T) {
	_, src, err := decodeForView(movAudioFixture(t))
	if err != nil {
		t.Fatalf("decodeForView: %v", err)
	}
	seat := &fakeSeat{cap: 4}
	restore := movAudioTestSeams(seat)
	defer restore()

	var arms []uint64
	var clock int64
	oldArm, oldCancel, oldNow := playArm, playCancel, playNow
	playArm = func(ns uint64) int64 { arms = append(arms, ns); return 0 }
	playCancel = func() int64 { return 1 }
	playNow = func() int64 { return clock }
	defer func() { playArm, playCancel, playNow = oldArm, oldCancel, oldNow }()

	a := &app{anim: src}
	if !a.togglePlay() || !a.playing {
		t.Fatal("togglePlay did not start playback")
	}
	if a.audio == nil || a.seat == nil {
		t.Fatal("audio did not build and open a seat at play start")
	}
	// 22050 mono s16 resampled to 48000 stereo f32: exactly one second.
	if want := 48000 * 8; len(a.audio.buf) != want {
		t.Fatalf("audio buffer = %d bytes, want %d", len(a.audio.buf), want)
	}
	if !seat.started || seat.submitted != 4*vi.AudioPeriodBytes {
		t.Fatalf("seat primed with %d bytes started=%v, want 4 periods", seat.submitted, seat.started)
	}
	// The queue's own EAGAIN is the pace: a fifth period is refused and
	// the pump idles rather than erroring.
	before := a.audioPos
	a.audioPump()
	if a.audioPos != before {
		t.Fatal("the pump submitted past a full queue")
	}
	// One full video loop: ten 100 ms ticks, the device draining each
	// period between them, the wrap printing the drift receipt.
	fire := func() {
		clock += int64(arms[len(arms)-1])
		seat.completed = seat.submitted - 8192 // one queue's backlog behind
		seat.drain()
		a.audioPump()
	}
	for i := 0; i < 10; i++ {
		fire()
		if !a.playTick() {
			t.Fatalf("tick %d did not advance", i+1)
		}
	}
	if a.frame != 0 || a.playTotalNs == 0 {
		t.Fatalf("loop did not wrap: frame=%d playTotalNs=%d", a.frame, a.playTotalNs)
	}
	// The drift measure: completed lags playing time by the one-queue
	// backlog (8192 B = 1024 f32stereo frames ≈ 21 ms at 48 kHz).
	frames, driftMs := a.audioDrift(mustStatus(t, seat))
	if frames != seat.completed/8 {
		t.Fatalf("audioDrift frames = %d, want %d", frames, seat.completed/8)
	}
	wantDrift := uint64(0)
	if playedNs := frames*1_000_000_000/48000 + a.audioSkipNs; playedNs < a.playTotalNs {
		wantDrift = (a.playTotalNs - playedNs) / 1_000_000
	} else {
		wantDrift = (playedNs - a.playTotalNs) / 1_000_000
	}
	if driftMs != wantDrift {
		t.Fatalf("audioDrift = %d ms, want %d", driftMs, wantDrift)
	}
	// #3778: the fake device lags the video clock by more than the resync
	// bound, so the pump must have dropped content forward — the skip is
	// what holds the drift under the starvation instead of past it.
	if a.audioSkipNs == 0 {
		t.Fatal("audioSkipNs = 0: the starved feed never resynced")
	}
	// A file swap abandons the seat and the decoded buffer.
	a.stopPlay()
	if !seat.aborted || a.seat != nil || a.audio != nil || a.audioPos != 0 || a.playTotalNs != 0 {
		t.Fatal("stopPlay did not reset the audio side")
	}
}

func mustStatus(t *testing.T, s audioSeat) vi.AudioStreamStatus {
	st, err := s.Status()
	if err != nil {
		t.Fatalf("seat status: %v", err)
	}
	return st
}

// TestMOVAudioNoDevicePlaysSilent: a soundless boot still plays the
// video — the refusal names itself once and the file is silent forever.
func TestMOVAudioNoDevicePlaysSilent(t *testing.T) {
	_, src, err := decodeForView(movAudioFixture(t))
	if err != nil {
		t.Fatalf("decodeForView: %v", err)
	}
	oldQ, oldO := audioQuery, audioOpen
	audioQuery = func() (vi.AudioInfo, error) {
		return vi.AudioInfo{Ready: 0, Format: vi.AudioFmtNone}, nil
	}
	openCalled := false
	audioOpen = func(p vi.AudioStreamParams) (audioSeat, error) {
		openCalled = true
		return nil, vi.ErrNoAudioDevice
	}
	defer func() { audioQuery, audioOpen = oldQ, oldO }()

	oldArm := playArm
	playArm = func(ns uint64) int64 { return 0 }
	defer func() { playArm = oldArm }()

	a := &app{anim: src}
	if !a.togglePlay() || !a.playing {
		t.Fatal("playback must still start on a soundless boot")
	}
	if a.audioErr != "no device" || a.seat != nil || openCalled {
		t.Fatalf("audioErr=%q seat=%v openCalled=%v, want named silent",
			a.audioErr, a.seat, openCalled)
	}
	// A second toggle cycle does not retry — the refusal is named once.
	a.pausePlay()
	a.audioStart()
	if a.audioErr != "no device" {
		t.Fatalf("audioErr = %q after a retry", a.audioErr)
	}
}

// TestMOVAudioMixerFullNamesIt: the open-time ENOSPC is a named refusal,
// not a silent skip.
func TestMOVAudioMixerFullNamesIt(t *testing.T) {
	_, src, err := decodeForView(movAudioFixture(t))
	if err != nil {
		t.Fatalf("decodeForView: %v", err)
	}
	oldQ, oldO := audioQuery, audioOpen
	audioQuery = func() (vi.AudioInfo, error) {
		return vi.AudioInfo{Ready: 1, Format: vi.AudioFmtFloat, Rate: 7, Channels: 2}, nil
	}
	audioOpen = func(p vi.AudioStreamParams) (audioSeat, error) {
		return nil, vi.ErrAudioStreamFull
	}
	defer func() { audioQuery, audioOpen = oldQ, oldO }()
	oldArm := playArm
	playArm = func(ns uint64) int64 { return 0 }
	defer func() { playArm = oldArm }()

	a := &app{anim: src}
	a.togglePlay()
	if a.audioErr != "mixer full" || a.seat != nil {
		t.Fatalf("audioErr=%q seat=%v, want the named mixer-full refusal", a.audioErr, a.seat)
	}
}
