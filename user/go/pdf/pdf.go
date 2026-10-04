// Package pdf implements the closed classic-xref subset in ADR 0040.
// Render borrows all buffers. It neither acquires files nor owns a heap.
package pdf

import (
	"math"
	"unsafe"
	"virelai/vector"
)

const (
	ArenaBytes       = 9_437_184
	MaxSource        = 4_194_304
	MaxObjects       = 8192
	MaxDepth         = 32
	MaxGraphicsDepth = 16
	MaxExpansion     = 16_777_216
	MaxWork          = 256_000_000
	MaxReads         = 134_217_728
	MaxRewinds       = 32
	MaxPages         = 256
	MaxCommands      = 8192
	MaxPaints        = 1024
	MaxContours      = 1024
	MaxEdges         = 8192
	MaxOutput        = 6_291_472
	MaxReceipt       = 16_384
	MaxDiagnostics   = 256
	MaxNanos         = 5_000_000_000
)

type Code uint8

const (
	OK Code = iota
	Malformed
	MalformedContent
	MalformedStream
	Encrypted
	UnsupportedStructure
	UnsupportedPageGeometry
	UnsupportedStroke
	UnsupportedClip
	UnsupportedText
	UnsupportedFont
	UnsupportedGlyph
	UnsupportedImage
	UnsupportedFilter
	UnsupportedFeature
	ExternalResource
	UnsupportedOperation
	MissingResource
	MissingGlyph
	PageRange
	SourceLimit
	ObjectLimit
	DepthLimit
	GraphicsDepthLimit
	ExpansionLimit
	WorkLimit
	MemoryLimit
	CanvasLimit
	TokenLimit
	ContainerLimit
	PageLimit
	FontLimit
	SceneLimit
	SegmentLimit
	CurveLimit
	CoordinateLimit
	ReadLimit
	HandleLimit
	DecoderLimit
	ReceiptLimit
	TimeLimit
	InvalidBuffer
	ReadFailed
	SourceChanged
	OutputLimit
	WriteFailed
	OutputExists
	EngineSizeLimit
	MissingToolchain
	SourceDrift
)

var messages = [...]string{
	"", "Malformed", "MalformedContent", "MalformedStream", "Encrypted",
	"UnsupportedStructure", "UnsupportedPageGeometry", "UnsupportedStroke",
	"UnsupportedClip", "UnsupportedText", "UnsupportedFont", "UnsupportedGlyph",
	"UnsupportedImage", "UnsupportedFilter", "UnsupportedFeature", "ExternalResource",
	"UnsupportedOperation", "MissingResource", "MissingGlyph", "PageRange",
	"SourceLimit", "ObjectLimit", "DepthLimit", "GraphicsDepthLimit", "ExpansionLimit",
	"WorkLimit", "MemoryLimit", "CanvasLimit", "TokenLimit", "ContainerLimit",
	"PageLimit", "FontLimit", "SceneLimit", "SegmentLimit", "CurveLimit",
	"CoordinateLimit", "ReadLimit", "HandleLimit", "DecoderLimit", "ReceiptLimit",
	"TimeLimit", "InvalidBuffer", "ReadFailed", "SourceChanged", "OutputLimit",
	"WriteFailed", "OutputExists", "EngineSizeLimit", "MissingToolchain", "SourceDrift",
}

type Failure struct {
	Code                 Code
	Offset, Object, Page int
}

func (f Failure) Error() string {
	if int(f.Code) >= len(messages) {
		return "InvalidBuffer"
	}
	return messages[f.Code]
}
func fail(c Code) Failure { return Failure{c, -1, -1, -1} }

// Source.Length is the immutable length established by complete sequential
// admission (including a cap+1 probe). ReadAt writes only dst, must not retain
// it, and reports short reads/I/O explicitly. Native attempts, bytes/discards
// and backwards reopens charge this same Ledger, via Read and Rewind.
// The engine supplies the sole 64 KiB cache; no source shadow is permitted.
type Source interface {
	Length() int64
	ReadAt(dst []byte, offset int64, ledger *Ledger) (int, Code)
}

type WorkKind uint8

const (
	Examine WorkKind = iota
	Copy
	Zero
	Hash
	Token
	Operand
	Object
	Reference
	Stack
	Record
	Transform
	Glyph
	InputBit
	Huffman
	Block
	DecodeByte
	Checksum
	Sample
	Pixel
	Background
	IO
	Vector
	WorkKinds
)

// Ledger belongs to the caller and begins once per page transaction, including
// any native source recheck before publication. Never reset it inside Render.
// Now must be a monotonic nanosecond clock on the guest. Nil disables only
// host timing diagnostics, not work limits. Deadline is an absolute timestamp.
type Ledger struct {
	Used, Max                    uint64
	Counts                       [WorkKinds]uint64
	Expanded, ReadBytes, Rewinds uint64
	Handles, Decoders            uint32
	Now                          func() uint64
	Deadline                     uint64
	nextClock                    uint64
	VectorBudget                 vector.Budget
}

func (l *Ledger) CheckTime() Code {
	if l.Now != nil && l.Now() > l.Deadline {
		return TimeLimit
	}
	return OK
}
func (l *Ledger) Charge(k WorkKind, n uint64) Code {
	if k >= WorkKinds || l.Max > MaxWork || l.Used > l.Max {
		return WorkLimit
	}
	if n > l.Max-l.Used {
		return WorkLimit
	}
	// Check at most 1024 charged operations apart, even for bulk charges.
	for n > 0 {
		step := min(n, 1024-l.nextClock)
		l.Used += step
		l.Counts[k] += step
		l.nextClock += step
		n -= step
		if l.nextClock == 1024 {
			l.nextClock = 0
			if c := l.CheckTime(); c != OK {
				return c
			}
		}
	}
	return OK
}
func (l *Ledger) Read(n uint64) Code {
	if l.ReadBytes > MaxReads || n > MaxReads-l.ReadBytes {
		return ReadLimit
	}
	if c := l.Charge(IO, 1); c != OK {
		return c
	}
	if c := l.Charge(Copy, n); c != OK {
		return c
	}
	l.ReadBytes += n
	return OK
}
func (l *Ledger) Rewind() Code {
	if l.Rewinds >= MaxRewinds {
		return ReadLimit
	}
	if c := l.Charge(IO, 1); c != OK {
		return c
	}
	l.Rewinds++
	return OK
}
func (l *Ledger) Open() Code {
	if l.Handles >= 2 {
		return HandleLimit
	}
	if c := l.Charge(IO, 1); c != OK {
		return c
	}
	l.Handles++
	return OK
}
func (l *Ledger) Close() Code {
	if l.Handles == 0 {
		return InvalidBuffer
	}
	c := l.Charge(IO, 1)
	l.Handles--
	return c
}
func (l *Ledger) expand() Code {
	if l.Expanded >= MaxExpansion {
		return ExpansionLimit
	}
	if c := l.Charge(DecodeByte, 1); c != OK {
		return c
	}
	l.Expanded++
	return OK
}

type Page struct {
	Pix                   []uint32
	Width, Height, Stride int
}
type Stats struct {
	Work, Expanded, Reads, Rewinds, VectorWork, Edges uint64
	Commands, Paints, Contours, Pages, Objects        uint32
	ArenaBytes                                        uint32
}
type xref struct {
	off, end                        uint32
	role, active, generation, spare uint16
}
type engine struct {
	src                      Source
	l                        *Ledger
	f                        Failure
	x                        []xref
	window                   []byte
	winStart, winN           int
	size, xoff, object, page int
	arena                    []byte
	cmd                      []vector.Command
	paint                    []vector.Paint
	clips                    []vector.Rect
	ws                       vector.Workspace
	pix                      []uint32
	dec                      []decoder
	parse                    []parseFrame
	path                     []vector.Command
	state                    *contentState
	stats                    Stats
	pages                    []pageDesc
	metadata                 uint32
}

const (
	outputEnd    = 6_291_456
	sceneEnd     = outputEnd + 1_048_576
	workspaceEnd = sceneEnd + 524_288
	documentEnd  = workspaceEnd + 262_144
	decodeEnd    = documentEnd + 262_144
	stateEnd     = decodeEnd + 524_288
)

// Render returns no page on any failure, even if unpublished output was
// modified. The arena must be exactly 9 MiB, 8-byte aligned, caller owned and
// disjoint from source/ledger. It contains POD only and is retained by neither
// engine nor Source. Its populated capacity remains charged on white pages.
func Render(source Source, pageIndex int, arena []byte, ledger *Ledger) (Page, Stats, Failure) {
	var e engine
	e.src = source
	e.l = ledger
	e.f = fail(OK)
	e.object = -1
	e.page = -1
	e.winStart = -1
	if source == nil || ledger == nil {
		return Page{}, Stats{}, fail(InvalidBuffer)
	}
	if source.Length() < 0 {
		return Page{}, Stats{}, fail(ReadFailed)
	}
	if source.Length() > MaxSource {
		return Page{}, Stats{}, fail(SourceLimit)
	}
	if len(arena) != ArenaBytes {
		return Page{}, Stats{}, fail(MemoryLimit)
	}
	if uintptr(unsafe.Pointer(&arena[0]))&7 != 0 {
		return Page{}, Stats{}, fail(InvalidBuffer)
	}
	if ledger.Max > MaxWork || ledger.Used > ledger.Max || ledger.VectorBudget.Used > ledger.VectorBudget.Max ||
		ledger.VectorBudget.Max > vector.MaxWork {
		return Page{}, Stats{}, fail(WorkLimit)
	}
	if ledger.Expanded > MaxExpansion {
		return Page{}, Stats{}, fail(ExpansionLimit)
	}
	if ledger.ReadBytes > MaxReads || ledger.Rewinds > MaxRewinds {
		return Page{}, Stats{}, fail(ReadLimit)
	}
	if ledger.Handles > 2 {
		return Page{}, Stats{}, fail(HandleLimit)
	}
	if ledger.Decoders != 0 {
		return Page{}, Stats{}, fail(DecoderLimit)
	}
	if c := ledger.CheckTime(); c != OK {
		return Page{}, Stats{}, fail(c)
	}
	e.arena = arena
	e.pix = pod[uint32](arena[:outputEnd])
	e.cmd = pod[vector.Command](arena[outputEnd : outputEnd+MaxCommands*int(unsafe.Sizeof(vector.Command{}))])
	e.paint = pod[vector.Paint](arena[outputEnd+MaxCommands*int(unsafe.Sizeof(vector.Command{})) : sceneEnd])
	e.paint = e.paint[:MaxPaints]
	clipStart := outputEnd + MaxCommands*int(unsafe.Sizeof(vector.Command{})) + MaxPaints*int(unsafe.Sizeof(vector.Paint{}))
	e.clips = pod[vector.Rect](arena[clipStart:sceneEnd])[:MaxPaints]
	e.ws = vector.Workspace{Bytes: arena[sceneEnd:workspaceEnd]}
	e.x = pod[xref](arena[workspaceEnd : workspaceEnd+(MaxObjects+1)*16])
	e.window = arena[workspaceEnd+(MaxObjects+1)*16 : workspaceEnd+(MaxObjects+1)*16+65536]
	e.pages = pod[pageDesc](arena[workspaceEnd+(MaxObjects+1)*16+65536 : documentEnd])
	e.dec = pod[decoder](arena[documentEnd:decodeEnd])[:2]
	e.parse = pod[parseFrame](arena[decodeEnd : decodeEnd+int(unsafe.Sizeof(parseFrame{}))*32])
	start := decodeEnd + int(unsafe.Sizeof(parseFrame{}))*32
	e.state = (*contentState)(unsafe.Pointer(&arena[start]))
	start += int(unsafe.Sizeof(contentState{}))
	e.path = pod[vector.Command](arena[start:stateEnd])
	if len(e.pages) < MaxPages || len(e.path) < MaxCommands {
		return Page{}, Stats{}, fail(MemoryLimit)
	}
	e.run(pageIndex)
	if e.f.Code == OK {
		e.set(ledger.CheckTime())
	}
	e.stats.Work = ledger.Used
	e.stats.Expanded = ledger.Expanded
	e.stats.Reads = ledger.ReadBytes
	e.stats.Rewinds = ledger.Rewinds
	e.stats.VectorWork = ledger.VectorBudget.Used
	e.stats.ArenaBytes = ArenaBytes
	if e.f.Code != OK {
		return Page{}, e.stats, e.f
	}
	p := e.pages[pageIndex]
	return Page{e.pix[:int(p.width)*int(p.height)], int(p.width), int(p.height), int(p.width)}, e.stats, e.f
}
func pod[T any](b []byte) []T {
	var v T
	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), len(b)/int(unsafe.Sizeof(v)))
}
func (e *engine) set(c Code) {
	if c != OK && e.f.Code == OK {
		e.f = Failure{c, -1, e.object, e.page}
	}
}
func (e *engine) charge(k WorkKind, n uint64) bool { e.set(e.l.Charge(k, n)); return e.f.Code == OK }
func (e *engine) ignoreMetadata(n uint16) {
	if uint32(n) > 16384-e.metadata {
		e.set(TokenLimit)
		return
	}
	e.metadata += uint32(n)
}
func finite(x float64) bool { return !math.IsNaN(x) && math.Abs(x) <= 32768 }
func (e *engine) point(p vector.Point) bool {
	if !finite(p.X) || !finite(p.Y) {
		e.set(CoordinateLimit)
		return false
	}
	return true
}
func (e *engine) affine(a vector.Affine) bool {
	if !finite(a.A) || !finite(a.B) || !finite(a.C) || !finite(a.D) || !finite(a.E) || !finite(a.F) {
		e.set(CoordinateLimit)
		return false
	}
	return true
}

var identity = vector.Affine{A: 1, D: 1}

func (e *engine) multiply(a, b vector.Affine) vector.Affine {
	e.charge(Transform, 1)
	c := vector.Affine{A: a.A*b.A + a.C*b.B, B: a.B*b.A + a.D*b.B, C: a.A*b.C + a.C*b.D, D: a.B*b.C + a.D*b.D, E: a.A*b.E + a.C*b.F + a.E, F: a.B*b.E + a.D*b.F + a.F}
	e.affine(c)
	return c
}
func (e *engine) mapped(a vector.Affine, p vector.Point) vector.Point {
	e.charge(Transform, 1)
	e.point(p)
	q := vector.Point{X: a.A*p.X + a.C*p.Y + a.E, Y: a.B*p.X + a.D*p.Y + a.F}
	e.point(q)
	return q
}
func vectorCode(c vector.Code) Code {
	switch c {
	case vector.OK:
		return OK
	case vector.Malformed:
		return MalformedContent
	case vector.UnsupportedFeature:
		return UnsupportedFeature
	case vector.UnsupportedText:
		return UnsupportedText
	case vector.ExternalResource:
		return ExternalResource
	case vector.UnsupportedXML:
		return UnsupportedFeature
	case vector.SourceLimit:
		return SourceLimit
	case vector.DepthLimit:
		return DepthLimit
	case vector.NodeLimit, vector.AttributeLimit, vector.TokenLimit:
		return ContainerLimit
	case vector.CommandLimit, vector.ContourLimit, vector.SceneLimit:
		return SceneLimit
	case vector.SegmentLimit:
		return SegmentLimit
	case vector.CurveLimit:
		return CurveLimit
	case vector.CoordinateLimit:
		return CoordinateLimit
	case vector.CanvasLimit:
		return CanvasLimit
	case vector.ScratchLimit, vector.MemoryLimit:
		return MemoryLimit
	case vector.WorkLimit:
		return WorkLimit
	case vector.TimeLimit:
		return TimeLimit
	default:
		return InvalidBuffer
	}
}
