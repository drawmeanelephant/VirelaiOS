// Package vector implements ADR 0041's bounded, caller-owned fill rasterizer.
package vector

type Point struct{ X, Y float64 }
type Affine struct{ A, B, C, D, E, F float64 }
type Verb uint8

const (
	Move Verb = iota
	Line
	Quad
	Cubic
	Close
)

type Command struct {
	Verb Verb
	P    [3]Point
}
type Rule uint8

const (
	NonZero Rule = iota
	EvenOdd
)

type Rect struct{ X0, Y0, X1, Y1 int }
type Paint struct {
	First, Count uint32
	Transform    Affine
	Clip         Rect
	Color        uint32
	Rule         Rule
}
type Scene struct {
	Commands []Command
	Paints   []Paint
}
type Target struct {
	Pix                   []uint32
	Width, Height, Stride int
}
type Storage struct {
	Commands []Command
	Paints   []Paint
}
type Workspace struct{ Bytes []byte }
type Budget struct{ Used, Max uint64 }
type Stats struct{ Work, Edges, ScratchBytes uint64 }
type Code uint8

const (
	OK Code = iota
	Malformed
	UnsupportedFeature
	UnsupportedText
	ExternalResource
	UnsupportedXML
	SourceLimit
	DepthLimit
	NodeLimit
	AttributeLimit
	TokenLimit
	CommandLimit
	ContourLimit
	SegmentLimit
	CurveLimit
	CoordinateLimit
	CanvasLimit
	SceneLimit
	ScratchLimit
	WorkLimit
	MemoryLimit
	TimeLimit
	InvalidBuffer
)

type Failure struct {
	Code                   Code
	Offset, Paint, Command int
}

func (f Failure) Error() string {
	switch f.Code {
	case OK:
		return ""
	case Malformed:
		return "malformed input"
	case UnsupportedFeature:
		return "unsupported feature"
	case UnsupportedText:
		return "unsupported text"
	case ExternalResource:
		return "external resource refused"
	case UnsupportedXML:
		return "unsupported XML"
	case SourceLimit:
		return "source limit"
	case DepthLimit:
		return "depth limit"
	case NodeLimit:
		return "node limit"
	case AttributeLimit:
		return "attribute limit"
	case TokenLimit:
		return "token limit"
	case CommandLimit:
		return "command limit"
	case ContourLimit:
		return "contour limit"
	case SegmentLimit:
		return "segment limit"
	case CurveLimit:
		return "curve limit"
	case CoordinateLimit:
		return "coordinate limit"
	case CanvasLimit:
		return "canvas limit"
	case SceneLimit:
		return "scene storage limit"
	case ScratchLimit:
		return "workspace limit"
	case WorkLimit:
		return "work limit"
	case MemoryLimit:
		return "memory limit"
	case TimeLimit:
		return "time limit"
	default:
		return "invalid buffer"
	}
}

const (
	MaxWork       = 64_000_000
	MaxCommands   = 8192
	MaxPaints     = 1024
	MaxContours   = 1024
	MaxEdges      = 8192
	MaxScene      = 1_048_576
	WorkspaceSize = 524_288
)

func failure(c Code, p, cmd int) Failure { return Failure{c, -1, p, cmd} }
func success() Failure                   { return failure(OK, -1, -1) }
