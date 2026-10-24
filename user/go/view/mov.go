// mov.go is GOVIEW's MOV/MP4 demux (M180a, issue #3155; M202b, issue
// #3385): a bounded box walker over 32-bit sizes, 64-bit sizes (size ==
// 1) and to-end-of-file (size == 0, top level only), reading ftyp,
// moov/trak/tkhd and mdia/{mdhd,hdlr,minf/stbl/{stsd,stts,stsc,stsz,
// stco|co64}}. It takes the first `vide` track and resolves a sample
// table of (offset, size, duration). The first `soun` track resolves
// the same way: the PCM-family sample entries (sowt/twos/`raw `/lpcm)
// become a movAudio the app decodes through virelai/pcm, every other
// fourcc is named in the load receipt (`audio=skipped codec=mp4a`),
// and a missing or unplayable track is the honest `audio=skipped`.
// Edit lists are ignored; fragmented files (moof) are refused by name.
//
// The one accepted video codec is the `jpeg` sample entry — QuickTime
// Photo-JPEG, the common MJPEG tag — each sample decoding through
// webrender.DecodeImage. Every other fourcc refuses AFTER the demux has
// read it: mov-unsupported-codec-<fourcc>, an ErrImageUnsupported like
// jpegUnsupported, so the viewer reports an unsupported format rather
// than corrupt bytes.
//
// Like view.go this file is pure: no syscalls, so the host tests run the
// walker and the same hostile files the guest refuses.
package main

import (
	"math"

	"virelai/pcm"
	"virelai/webrender"
)

// The demux bounds (the card's fixed caps): a hostile file gets a named
// refusal, never an unbounded walk or allocation.
const (
	movDepthMax    = 8          // box nesting depth
	movBoxesMax    = 1024       // boxes read in one file
	movSamplesMax  = 600        // resolved video samples
	movSampleBytes = 256 * 1024 // one sample's byte cap
)

// movError is a named demux refusal — the "mov-<kind>" vocabulary the
// gate and the tests pin, matching webrender's imageError convention.
type movError string

func (e movError) Error() string { return string(e) }

const (
	errMOVInvalid    = movError("mov-invalid")    // the structure lies
	errMOVLimit      = movError("mov-limit")      // a bound was crossed
	errMOVFragmented = movError("mov-fragmented") // moof present
)

// movCodecError is the unsupported-codec refusal: it names the sample
// entry's fourcc, carries the container tag the report should print
// ("MP4" for an isom/mp4x brand, "MOV" otherwise), and IS an
// ErrImageUnsupported for errors.Is.
type movCodecError struct {
	tag    string
	fourcc string
}

func (e movCodecError) Error() string { return "mov-unsupported-codec-" + e.fourcc }
func (e movCodecError) Is(t error) bool {
	return t == webrender.ErrImageUnsupported
}

// movSample is one resolved video sample: a byte range inside the file
// (a whole JPEG for the `jpeg` codec) plus its stts duration in media
// timescale units.
type movSample struct {
	off  uint64
	size uint32
	dur  uint32
}

// movVideo is a demuxed video track: the tkhd dims, the media timescale
// and the sample table; data is the file the sample offsets slice into.
// audio is the first soun track when it demuxed to a playable PCM-family
// description; audioCodec names a soun codec the demux refused (mp4a and
// friends) so the receipt can say why — "" when there is nothing to name
// (no soun track, or a track whose tables never resolved).
type movVideo struct {
	w, h       int
	timescale  uint32
	brand      string // ftyp major brand, "" on an old ftyp-less file
	codec      string // stsd sample-entry fourcc
	samples    []movSample
	audio      *movAudio
	audioCodec string
	data       []byte
}

// movAudio is a demuxed soun track: the decode tuple its sound sample
// description resolved to, the media timescale and the sample table.
// tag carries a pcm format code (FormatPCM or FormatFloat) so the
// materialized stream feeds pcm.Pipe exactly like a parsed WAV does.
type movAudio struct {
	codec     string // stsd sample-entry fourcc
	tag       uint16 // pcm.FormatPCM or pcm.FormatFloat
	channels  uint16 // 1 or 2
	rate      uint32 // samples per second
	bits      uint16 // per channel
	be        bool   // big-endian sample words: swap to LE on decode
	bias      bool   // flip each sample's top bit (unsigned >8-bit, signed 8-bit)
	timescale uint32
	samples   []movSample
}

// sampleBytes returns sample i's bytes — the frame's whole JPEG.
func (v *movVideo) sampleBytes(i int) []byte {
	s := v.samples[i]
	return v.data[s.off : s.off+uint64(s.size)]
}

// movDelayNs is the playback arm for one sample: duration × 10⁹ /
// timescale, floored at 1 ms so a zero-length sample still lands a
// TIMER event on the deadline granularity.
func movDelayNs(dur uint64, timescale uint32) uint64 {
	if timescale == 0 {
		return 1_000_000
	}
	ns := dur * 1_000_000_000 / uint64(timescale)
	if ns < 1_000_000 {
		return 1_000_000
	}
	return ns
}

// movBrandTag is the tag the codec refusal prints: MP4 for an ISO-BMFF
// brand (isom or mp4x), MOV for the QuickTime brands and for an old file
// that carries no ftyp at all.
func movBrandTag(brand string) string {
	if brand == "isom" || (len(brand) == 4 && brand[:3] == "mp4") {
		return "MP4"
	}
	return "MOV"
}

// movBrandOK is the ftyp admission set: the QuickTime brand and the
// three ISO-BMFF brands the card names.
func movBrandOK(b string) bool {
	switch b {
	case "qt  ", "isom", "mp41", "mp42":
		return true
	}
	return false
}

// movTrak accumulates one trak's atoms during the walk.
type movTrak struct {
	w, h      uint32    // tkhd dims (already shifted out of 16.16)
	handler   string    // hdlr handler_type
	timescale uint32    // mdhd
	codec     string    // stsd first entry's format fourcc
	sound     *movSound // soun stsd's decode tuple (nil for vide traks)
	stts      [][2]uint32
	stsc      [][3]uint32
	sizes     []uint32 // stsz, expanded per-sample
	chunks    []uint64 // stco/co64

	haveTkhd, haveMdhd, haveHdlr, haveStsd bool
	haveStts, haveStsc, haveStsz, haveStco bool
}

// movParse is the walk's state: the box budget, the ftyp brands and the
// traks collected so far. cur is the trak the leaf parsers write into.
type movParse struct {
	data  []byte
	boxes int
	brand string
	traks []*movTrak
	cur   *movTrak
}

// movContainer reports whether the walk descends into a box type's
// payload. The set is the directed path the card reads plus edts/dinf,
// so a hostile file can still trip the depth bound anywhere in the
// container grammar.
func movContainer(typ string) bool {
	switch typ {
	case "moov", "trak", "mdia", "minf", "stbl", "edts", "dinf":
		return true
	}
	return false
}

// header reads one box header at pos inside [off, end) at the given
// depth, returning the box's total size (header + payload), its fourcc
// and the header length. size == 1 pulls the 64-bit size; size == 0 runs
// to the parent's end and is only legal at the top level.
func (p *movParse) header(pos, end int64, depth int) (size int64, typ string, hdr int64, err error) {
	if end-pos < 8 {
		return 0, "", 0, errMOVInvalid // a truncated box header
	}
	o := int(pos)
	n := int64(be32(p.data[o : o+4]))
	typ = string(p.data[o+4 : o+8])
	hdr = 8
	switch n {
	case 1:
		if end-pos < 16 {
			return 0, "", 0, errMOVInvalid
		}
		n64 := movBe64(p.data[o+8 : o+16])
		if n64 > 0x7fffffffffffffff {
			return 0, "", 0, errMOVInvalid // a size overflow
		}
		n, hdr = int64(n64), 16
		if n < 16 {
			return 0, "", 0, errMOVInvalid
		}
	case 0:
		if depth != 1 {
			return 0, "", 0, errMOVInvalid
		}
		n = end - pos
	}
	if n < hdr || pos+n > end {
		return 0, "", 0, errMOVInvalid // truncated inside the parent's span
	}
	return n, typ, hdr, nil
}

// walk reads the boxes in [off, end) at the given depth, routing leaves
// by their full path and descending into container types. path carries
// the ancestor fourccs.
func (p *movParse) walk(off, end int64, depth int, path []string) error {
	pos := off
	for pos < end {
		p.boxes++
		if p.boxes > movBoxesMax {
			return errMOVLimit
		}
		if depth > movDepthMax {
			return errMOVLimit
		}
		n, typ, hdr, err := p.header(pos, end, depth)
		if err != nil {
			return err
		}
		if typ == "moof" || typ == "mfra" {
			return errMOVFragmented
		}
		body := p.data[pos+hdr : pos+n]
		path2 := append(path[:len(path):len(path)], typ) // copy-on-extend: siblings must not share the tail
		if err := p.visit(path2, body); err != nil {
			return err
		}
		if movContainer(typ) {
			if err := p.walk(pos+hdr, pos+n, depth+1, path2); err != nil {
				return err
			}
		}
		pos += n
	}
	return nil
}

// visit handles one box by its path. Only the exact paths the card reads
// are interpreted; a trak registers as the current trak only directly
// under moov, so a nested lookalike cannot smuggle its tables into the
// chosen track.
func (p *movParse) visit(path []string, body []byte) error {
	switch path[len(path)-1] {
	case "ftyp":
		if len(path) == 1 {
			return p.ftyp(body)
		}
	case "trak":
		if movPathEq(path, "moov", "trak") {
			p.cur = &movTrak{}
			p.traks = append(p.traks, p.cur)
		}
	case "tkhd":
		if p.cur != nil && movPathEq(path, "moov", "trak", "tkhd") {
			return p.tkhd(body)
		}
	case "mdhd":
		if p.cur != nil && movPathEq(path, "moov", "trak", "mdia", "mdhd") {
			return p.mdhd(body)
		}
	case "hdlr":
		if p.cur != nil && movPathEq(path, "moov", "trak", "mdia", "hdlr") {
			return p.hdlr(body)
		}
	case "stsd", "stts", "stsc", "stsz", "stco", "co64":
		if p.cur != nil && movPathEq(path, "moov", "trak", "mdia", "minf", "stbl", path[len(path)-1]) {
			return p.stbl(path[len(path)-1], body)
		}
	}
	return nil
}

func movPathEq(path []string, want ...string) bool {
	if len(path) != len(want) {
		return false
	}
	for i := range want {
		if path[i] != want[i] {
			return false
		}
	}
	return true
}

// ftyp reads the major brand and admits the file when the major brand —
// or any compatible brand — is one of the four the card names.
func (p *movParse) ftyp(body []byte) error {
	if len(body) < 8 {
		return errMOVInvalid
	}
	if p.brand != "" {
		return nil // a second ftyp keeps the first's brands
	}
	p.brand = string(body[:4])
	ok := movBrandOK(p.brand)
	for i := 8; i+4 <= len(body) && !ok; i += 4 {
		ok = movBrandOK(string(body[i : i+4]))
	}
	if !ok {
		return errMOVInvalid
	}
	return nil
}

// tkhd takes the track dims: the 16.16 fixed-point width/height are the
// last eight bytes of the box, whichever header version it carries.
// Zero dims are recorded, not refused here — a soun track honestly
// carries a 0x0 tkhd; the demux refuses the file only when the SELECTED
// vide track has no picture.
func (p *movParse) tkhd(body []byte) error {
	if len(body) < 8 {
		return errMOVInvalid
	}
	p.cur.w = be32(body[len(body)-8:]) >> 16
	p.cur.h = be32(body[len(body)-4:]) >> 16
	p.cur.haveTkhd = true
	return nil
}

// mdhd takes the media timescale — v0 at offset 12, v1 at 20. A zero
// timescale makes every sample duration uncomputable: refused.
func (p *movParse) mdhd(body []byte) error {
	if len(body) < 4 {
		return errMOVInvalid
	}
	var off int
	switch body[0] {
	case 0:
		off = 12
	case 1:
		off = 20
	default:
		return errMOVInvalid
	}
	if len(body) < off+8 {
		return errMOVInvalid
	}
	p.cur.timescale = be32(body[off:])
	if p.cur.timescale == 0 {
		return errMOVInvalid
	}
	p.cur.haveMdhd = true
	return nil
}

// hdlr takes the handler type — `vide` selects, `soun` is skipped and
// reported, everything else is ignored.
func (p *movParse) hdlr(body []byte) error {
	if len(body) < 12 {
		return errMOVInvalid
	}
	p.cur.handler = string(body[8:12])
	p.cur.haveHdlr = true
	return nil
}

// stbl routes one sample-table leaf into the current trak.
func (p *movParse) stbl(typ string, body []byte) error {
	switch typ {
	case "stsd":
		return p.cur.readStsd(body)
	case "stts":
		return p.cur.readStts(body)
	case "stsc":
		return p.cur.readStsc(body)
	case "stsz":
		return p.cur.readStsz(body)
	case "stco", "co64":
		return p.cur.readStco(body, typ == "co64")
	}
	return nil
}

// stsd reads the first sample entry's format fourcc — the codec. A
// sample description with no entries describes nothing to play. On a
// soun trak the entry is also parsed for its decode tuple; a handler
// that arrives later than its tables leaves the codec named and the
// decode unsynthesized, which the demux reports like any coded entry.
func (t *movTrak) readStsd(body []byte) error {
	if len(body) < 16 {
		return errMOVInvalid
	}
	count := be32(body[4:8])
	entry := be32(body[8:12])
	if count == 0 || entry < 8 || int64(entry) > int64(len(body))-8 {
		return errMOVInvalid
	}
	t.codec = string(body[12:16])
	if t.handler == "soun" {
		t.sound = movSoundEntry(body[8 : 8+entry])
	}
	t.haveStsd = true
	return nil
}

// The PCM family the card decodes plus the rate bound pcm shares
// (8 kHz..96 kHz — a MOV naming a rate outside it is skipped like a WAV
// refusing it).
const (
	movRateMin, movRateMax = 8000, 96000
)

// movSound is a soun sample entry resolved to the decode tuple the PCM
// family shares: a pcm format tag, the channel/rate/bits geometry, and
// the two byte transforms movAudio's pcmBytes applies — the endian swap
// for big-endian sample words and the top-bit bias that maps unsigned
// >8-bit and signed 8-bit encodings onto the decoder's vocabulary
// (8-bit unsigned, wider signed little-endian).
type movSound struct {
	codec    string // stsd sample-entry fourcc
	tag      uint16 // pcm.FormatPCM or pcm.FormatFloat
	channels uint16
	rate     uint32
	bits     uint16
	be       bool
	bias     bool
}

// movSoundEntry parses one stsd sound sample entry into a decode tuple.
// The QT sound sample description and the ISO AudioSampleEntry share
// the channel/bits/rate offsets, so v0/v1 entries and isom entries read
// the same fields; the version word itself is QT's. nil means the entry
// is not one this demux synthesizes — a coded fourcc (mp4a, ulaw, …), a
// PCM-family member whose parameters are unplayable, or an entry too
// short to describe anything.
func movSoundEntry(entry []byte) *movSound {
	if len(entry) < 36 {
		return nil
	}
	s := &movSound{codec: string(entry[4:8]), tag: pcm.FormatPCM}
	channels := be16(entry[24:26])
	bits := be16(entry[26:28])
	rate := be32(entry[32:36]) >> 16
	switch s.codec {
	case "sowt":
		// Signed little-endian — WAV PCM's layout exactly; 8-bit signed
		// rides the u8 bias like the wider unsigned codecs.
		s.bias = bits == 8
	case "twos":
		// Signed big-endian.
		s.be = bits > 8
		s.bias = bits == 8
	case "raw ":
		// Offset binary: unsigned 8-bit is the decoder's own encoding;
		// wider words are big-endian and need the sign flip.
		s.be = bits > 8
		s.bias = bits > 8
	case "lpcm":
		// Only the QT v2 description carries the format flags that make
		// lpcm unambiguous — a v0/v1 lpcm stays refused like a coded
		// entry. The v2 extension overrides the base geometry: f64 rate,
		// u32 channels, the flags word.
		if be16(entry[16:18]) != 2 || len(entry) < 72 {
			return nil
		}
		const (
			lpcmFloat          = 1 << 0
			lpcmBE             = 1 << 1
			lpcmSigned         = 1 << 2
			lpcmPacked         = 1 << 3
			lpcmAlignedHigh    = 1 << 4
			lpcmNonInterleaved = 1 << 5
		)
		flags := be32(entry[60:64])
		if flags&(lpcmNonInterleaved|lpcmAlignedHigh) != 0 {
			return nil
		}
		if flags&lpcmPacked == 0 && bits > 8 {
			return nil // unpacked wide samples have no frame alignment to trust
		}
		s.be = flags&lpcmBE != 0
		if ch := be32(entry[48:52]); ch != 0 {
			channels = ch
		}
		if f := math.Float64frombits(movBe64(entry[40:48])); f >= 1 && f <= 0xffffffff {
			rate = uint32(f)
		}
		if bits == 0 {
			if bpp, fpp := be32(entry[64:68]), be32(entry[68:72]); bpp != 0 && fpp != 0 && channels != 0 {
				bits = bpp * 8 / (fpp * channels)
			}
		}
		if flags&lpcmFloat != 0 {
			s.tag = pcm.FormatFloat
		} else {
			s.bias = (flags&lpcmSigned != 0) == (bits == 8)
		}
	default:
		return nil
	}
	if channels < 1 || channels > 2 || rate < movRateMin || rate > movRateMax {
		return nil
	}
	if s.tag == pcm.FormatFloat {
		if bits != 32 {
			return nil
		}
	} else if bits != 8 && bits != 16 && bits != 24 && bits != 32 {
		return nil
	}
	s.channels, s.rate, s.bits = uint16(channels), rate, uint16(bits)
	return s
}

// stts reads the time-to-sample runs: (count, delta) pairs.
func (t *movTrak) readStts(body []byte) error {
	if len(body) < 8 {
		return errMOVInvalid
	}
	count := be32(body[4:8])
	if count > movSamplesMax {
		return errMOVLimit
	}
	if int64(len(body)) < 8+8*int64(count) {
		return errMOVInvalid
	}
	t.stts = make([][2]uint32, 0, count)
	for i := 8; i+8 <= len(body) && len(t.stts) < int(count); i += 8 {
		t.stts = append(t.stts, [2]uint32{be32(body[i:]), be32(body[i+4:])})
	}
	t.haveStts = true
	return nil
}

// stsc reads the sample-to-chunk table: (first_chunk, samples_per_chunk,
// description_index) triples. first_chunk is 1-based and the entries
// must ascend, or the run mapping is unresolvable.
func (t *movTrak) readStsc(body []byte) error {
	if len(body) < 8 {
		return errMOVInvalid
	}
	count := be32(body[4:8])
	if count > movSamplesMax {
		return errMOVLimit
	}
	if int64(len(body)) < 8+12*int64(count) {
		return errMOVInvalid
	}
	var prev uint32
	for i := 8; i+12 <= len(body) && len(t.stsc) < int(count); i += 12 {
		first, spc := be32(body[i:]), be32(body[i+4:])
		if first < 1 || first <= prev || spc == 0 {
			return errMOVInvalid
		}
		prev = first
		t.stsc = append(t.stsc, [3]uint32{first, spc, be32(body[i+8:])})
	}
	if len(t.stsc) != int(count) {
		return errMOVInvalid
	}
	t.haveStsc = true
	return nil
}

// stsz expands the sample sizes: a nonzero sample_size means every
// sample carries it, else the table carries one entry per sample. The
// count is bounded before the expansion so a hostile count cannot force
// an unbounded allocation.
func (t *movTrak) readStsz(body []byte) error {
	if len(body) < 12 {
		return errMOVInvalid
	}
	uniform := be32(body[4:8])
	count := be32(body[8:12])
	if count > movSamplesMax {
		return errMOVLimit
	}
	if uniform != 0 {
		if uniform > movSampleBytes {
			return errMOVLimit
		}
		t.sizes = make([]uint32, count)
		for i := range t.sizes {
			t.sizes[i] = uniform
		}
	} else {
		if int64(len(body)) < 12+4*int64(count) {
			return errMOVInvalid
		}
		t.sizes = make([]uint32, 0, count)
		for i := 12; i+4 <= len(body) && len(t.sizes) < int(count); i += 4 {
			t.sizes = append(t.sizes, be32(body[i:]))
		}
		if len(t.sizes) != int(count) {
			return errMOVInvalid
		}
	}
	t.haveStsz = true
	return nil
}

// stco reads the chunk-offset table — 32-bit entries, or 64-bit under
// the co64 name. A file may carry either, never both.
func (t *movTrak) readStco(body []byte, wide bool) error {
	if len(body) < 8 {
		return errMOVInvalid
	}
	count := be32(body[4:8])
	if count > movSamplesMax {
		return errMOVLimit
	}
	stride := int64(4)
	if wide {
		stride = 8
	}
	if int64(len(body)) < 8+stride*int64(count) {
		return errMOVInvalid
	}
	t.chunks = make([]uint64, 0, count)
	for i := int64(8); i+stride <= int64(len(body)) && len(t.chunks) < int(count); i += stride {
		if wide {
			t.chunks = append(t.chunks, movBe64(body[i:i+8]))
		} else {
			t.chunks = append(t.chunks, uint64(be32(body[i:i+4])))
		}
	}
	if len(t.chunks) != int(count) {
		return errMOVInvalid
	}
	t.haveStco = true
	return nil
}

// resolve turns the parsed tables into the sample list: stsc maps
// samples to chunks, stco gives each chunk's offset, stsz gives each
// sample's size and stts gives its duration. The three tables must agree
// on the sample count, every sample must land inside the file and under
// the byte cap, and the totals stay under the demux's bounds.
func (t *movTrak) resolve(data []byte) ([]movSample, error) {
	if !t.haveStsd || !t.haveStts || !t.haveStsc || !t.haveStsz || !t.haveStco {
		return nil, errMOVInvalid
	}
	if len(t.stts) == 0 || len(t.stsc) == 0 || len(t.chunks) == 0 {
		return nil, errMOVInvalid
	}
	n := len(t.sizes)
	total := 0
	for _, e := range t.stts {
		total += int(e[0])
	}
	if total != n {
		return nil, errMOVInvalid // stsz/stts count mismatch
	}
	if n == 0 {
		return nil, errMOVInvalid
	}
	if n > movSamplesMax {
		return nil, errMOVLimit
	}
	samples := make([]movSample, 0, n)
	si := 0
	run, left := 0, t.stts[0][0]
	for ci, coff := range t.chunks {
		spc := int64(-1)
		for _, e := range t.stsc {
			if uint64(e[0]) <= uint64(ci)+1 {
				spc = int64(e[1])
			}
		}
		if spc < 0 {
			return nil, errMOVInvalid
		}
		if spc > 0 && coff > uint64(len(data)) {
			return nil, errMOVLimit // a chunk offset past end of file
		}
		var inner uint64
		for j := int64(0); j < spc; j++ {
			if si >= n {
				return nil, errMOVInvalid // more chunk slots than samples
			}
			sz := t.sizes[si]
			if sz > movSampleBytes {
				return nil, errMOVLimit
			}
			off := coff + inner
			if off+uint64(sz) > uint64(len(data)) {
				return nil, errMOVLimit // a sample past end of file
			}
			if left == 0 {
				run++
				if run >= len(t.stts) {
					return nil, errMOVInvalid
				}
				left = t.stts[run][0]
			}
			samples = append(samples, movSample{off: off, size: sz, dur: t.stts[run][1]})
			left--
			inner += uint64(sz)
			si++
		}
	}
	if si != n {
		return nil, errMOVInvalid // the chunks ran out before the samples did
	}
	return samples, nil
}

// audio resolves the soun track into a playable movAudio through the
// same resolve() the video track runs. The returned string is the
// refusal's name: the entry's fourcc whenever a sound description was
// read and refused (coded like mp4a, or PCM-family with unplayable
// parameters), "" for every quieter skip — no stsd at all, or a sample
// table that did not resolve. A bad sound track never fails the file:
// the video the demux already validated still plays.
func (t *movTrak) audio(data []byte) (*movAudio, string) {
	if t.sound == nil {
		return nil, t.codec
	}
	samples, err := t.resolve(data)
	if err != nil {
		return nil, ""
	}
	return &movAudio{
		codec: t.sound.codec, tag: t.sound.tag,
		channels: t.sound.channels, rate: t.sound.rate, bits: t.sound.bits,
		be: t.sound.be, bias: t.sound.bias,
		timescale: t.timescale, samples: samples,
	}, ""
}

// pcmBytes materializes the track's samples as one contiguous
// little-endian stream — the byte layout pcm.Pipe's decoder reads
// (little-endian signed for the wide words, unsigned for 8-bit, LE
// IEEE-754 for float). Big-endian sample words are swapped in place and
// the codec's sign bias flips each sample's top bit. A sample that does
// not hold a whole number of frames is not sound — nil, and the caller
// treats it like the table that never resolved.
func (a *movAudio) pcmBytes(data []byte) []byte {
	w := int(a.bits) / 8
	fb := w * int(a.channels)
	n := 0
	for _, s := range a.samples {
		n += int(s.size)
	}
	out := make([]byte, 0, n)
	for i := range a.samples {
		s := a.samples[i]
		b := data[s.off : s.off+uint64(s.size)]
		if len(b)%fb != 0 {
			return nil
		}
		out = append(out, b...)
	}
	if a.be && w > 1 {
		for i := 0; i+w <= len(out); i += w {
			for l, r := i, i+w-1; l < r; l, r = l+1, r-1 {
				out[l], out[r] = out[r], out[l]
			}
		}
	}
	if a.bias {
		for i := w - 1; i < len(out); i += w {
			out[i] ^= 0x80
		}
	}
	return out
}

// wavInfo dresses the materialized stream as a WavInfo — the same tuple
// a parsed WAV header negotiates — so NewPipe/Play run unchanged.
func (a *movAudio) wavInfo(data []byte) (pcm.WavInfo, []byte) {
	b := a.pcmBytes(data)
	fb := int(a.channels) * int(a.bits) / 8
	info := pcm.WavInfo{
		Tag: a.tag, Channels: a.channels, Rate: a.rate, Bits: a.bits,
		DataSize: uint64(len(b)),
	}
	if fb > 0 {
		info.Frames = uint64(len(b)) / uint64(fb)
	}
	return info, b
}

// demuxMOV walks the box tree, takes the first `vide` track, resolves
// its sample table and applies the codec gate, then resolves the first
// `soun` track's audio the same bounded way. Named refusals:
// mov-invalid on structure, mov-limit on a bound, mov-fragmented on
// moof/mfra, and mov-unsupported-codec-<fourcc> once the table resolved
// — the demux reads the codec before it is refused.
func demuxMOV(data []byte) (*movVideo, error) {
	if len(data) < 8 {
		return nil, errMOVInvalid
	}
	p := &movParse{data: data}
	if err := p.walk(0, int64(len(data)), 1, nil); err != nil {
		return nil, err
	}
	v := &movVideo{data: data, brand: p.brand}
	var track, atrack *movTrak
	for _, t := range p.traks {
		switch t.handler {
		case "vide":
			if track == nil {
				track = t
			}
		case "soun":
			if atrack == nil {
				atrack = t
			}
		}
	}
	if track == nil || track.w == 0 || track.h == 0 {
		return nil, errMOVInvalid
	}
	samples, err := track.resolve(data)
	if err != nil {
		return nil, err
	}
	v.samples = samples
	v.timescale = track.timescale
	v.codec = track.codec
	v.w, v.h = int(track.w), int(track.h)
	if v.codec != "jpeg" {
		return nil, movCodecError{tag: movBrandTag(v.brand), fourcc: v.codec}
	}
	if atrack != nil {
		v.audio, v.audioCodec = atrack.audio(data)
	}
	return v, nil
}

// movHeaderDims is headerDims' MOV half: a best-effort, refusal-free
// walk over the head looking for the first nonzero tkhd dims — the
// window sizes before the file is demuxed, and a moov that ran past the
// head simply yields the default window.
func movHeaderDims(head []byte) (iw, ih uint32, ok bool) {
	pos := 0
	for pos+8 <= len(head) {
		n := int64(be32(head[pos:]))
		typ := string(head[pos+4 : pos+8])
		hdr := int64(8)
		switch n {
		case 1:
			if pos+16 > len(head) {
				return 0, 0, false
			}
			n, hdr = int64(movBe64(head[pos+8:pos+16])), 16
		case 0:
			n = int64(len(head) - pos)
		}
		if n < hdr || pos+int(n) > len(head) {
			return 0, 0, false
		}
		if typ == "moov" {
			if w, h, found := movTakDims(head[pos+int(hdr) : pos+int(n)]); found {
				return w, h, true
			}
		}
		pos += int(n)
	}
	return 0, 0, false
}

// movTakDims searches one moov payload for its traks' tkhd boxes and
// returns the first nonzero dims.
func movTakDims(moov []byte) (w, h uint32, ok bool) {
	pos := 0
	for pos+8 <= len(moov) {
		n := int64(be32(moov[pos:]))
		hdr := int64(8)
		if n == 1 {
			if pos+16 > len(moov) {
				return 0, 0, false
			}
			n, hdr = int64(movBe64(moov[pos+8:pos+16])), 16
		} else if n == 0 {
			n = int64(len(moov) - pos)
		}
		if n < hdr || pos+int(n) > len(moov) {
			return 0, 0, false
		}
		if string(moov[pos+4:pos+8]) == "trak" {
			if w, h, ok := movTkhdDims(moov[pos+int(hdr) : pos+int(n)]); ok {
				return w, h, true
			}
		}
		pos += int(n)
	}
	return 0, 0, false
}

// movTkhdDims pulls the dims out of a trak payload's first tkhd box.
func movTkhdDims(trak []byte) (w, h uint32, ok bool) {
	pos := 0
	for pos+8 <= len(trak) {
		n := int64(be32(trak[pos:]))
		hdr := int64(8)
		if n == 1 {
			if pos+16 > len(trak) {
				return 0, 0, false
			}
			n, hdr = int64(movBe64(trak[pos+8:pos+16])), 16
		} else if n == 0 {
			n = int64(len(trak) - pos)
		}
		if n < hdr || pos+int(n) > len(trak) {
			return 0, 0, false
		}
		if string(trak[pos+4:pos+8]) == "tkhd" {
			body := trak[pos+int(hdr) : pos+int(n)]
			if len(body) < 8 {
				return 0, 0, false
			}
			w, h = be32(body[len(body)-8:])>>16, be32(body[len(body)-4:])>>16
			if w == 0 || h == 0 {
				return 0, 0, false
			}
			return w, h, true
		}
		pos += int(n)
	}
	return 0, 0, false
}

func movBe64(b []byte) uint64 {
	return uint64(be32(b))<<32 | uint64(be32(b[4:]))
}
