# ADR 0041: Bounded SVG fills and a Go vector-raster contract

- Status: **PROPOSED for owner review**; acceptance takes effect on landing.
- Date: 2026-10-03 · Design card: M90a / #1919 · Index: #1917
- Related: [ADR 0030](0030-go-is-el0.md), [ADR 0038](0038-zig-guest-target.md),
  [UI primitives](../ui-primitives.md); downstream M89a / #1920.

## 1. Decision and evidence boundary

Choose an **owned, pure-Go fill renderer** under `user/go/svg` and
`user/go/vector`. SVG bytes become a bounded vector description; the
separately callable vector package produces pixels without parsing XML.
No third-party guest engine, cgo, C/Zig objects, libc, POSIX, new syscall,
viewer, WEB integration or boot-default change is authorized.

This follows ADR 0030 D2 without an amendment. **M89's engine must also be
Go to call this function-level contract in-process in the same static
ELF.** It cannot embed a native PDF engine and call this package through
FFI (D3). A file-format bridge would be a different, separately approved
contract, not an implementation of this decision. M89a must cite this
contract and declare its raster card blocked until M90b lands; there is
no implicit fallback and no dependency from M90 to M89.

The first subset is **solid filled shapes**, not general SVG. Stroke
offsetting, arcs, gradients, path clipping and text are deliberately
refused, not approximated away. This keeps the first renderer and its
hostile-input accounting reviewable. M89 can use filled paths, affine
transforms and integer rectangular clipping; fonts, image decode,
stroke-to-outline conversion and other PDF operations remain M89-owned
or explicitly excluded by its R1. A larger PDF subset does not enlarge
this contract.

**Observed source baseline:** VirelaiOS `03d929f25226d9055dfbae4571faa824b8d3ffeb`,
current `origin/main` fetched on 2026-10-03; live #1919 and #1917 bodies
and their empty comment lists read through GitHub REST. The index's
“PNG only/no vector rasterizer” baseline is not adopted: PNG and QOI
decoding, Go shape primitives, and Go/Zig glyph rasterizers already exist.
None is a general SVG renderer.

**Observed diagnostics:** host Go 1.27.1 `go -C user/go test ./ttf ./draw`
passed both packages. The pinned NanoSVG host object compiled and its
unresolved symbols were inspected (§2). These are prior-art tests and a
dependency audit, **not guest SVG pixels, renderer size, latency or memory
measurements**. All numerical values below are final design ceilings for
implementation acceptance, not measured results. Candidate source and
diagnostics remain ignored under `artifacts/m90-r1/`.

## 2. Source-backed route survey

| Route | Observed source, weight and license | Target fit and remaining work | Decision |
|---|---|---|---|
| Go, owned subset | `user/go/ttf/raster.go` (7,351 B) takes flattened contours, uses four vertical subrows, horizontal coverage and non-zero winding (`:37-53,67-191`). `outline.go` (10,742 B) flattens quadratics (`:303-413`). `user/go/draw/draw.go` (11,582 B) supplies shapes and proper straight-alpha source-over (`:52-74`). These three files total 29,675 B of prior art, **not renderer-added binary bytes**. In-tree code follows the repository license; Go runtime/std retain their upstream notices. `user/go/go.mod` adds no SVG dependency. | Already statically built as `GOOS=virelai`, `CGO_ENABLED=0`. Needs a strict XML/path front end, cubic curves, even-odd fill, general affine mapping, explicit work checks and caller-owned scratch. Existing TTF functions are private/glyph-specific: their full-bounds `int32` accumulator plus mask, insertion sort, silent empty-result bounds and depth-12 curve fallback are not acceptable general-renderer semantics. `draw` has a different shape sampler and is not a path engine. Runtime/collector pages and mmap growth require a separate allowance (§5). | **Select.** Reuse algorithmic ideas, not private state, a second toolkit or unbounded glyph allocation. Leave existing draw/TTF files untouched. No new guest module/download. |
| NanoSVG-class C embedding | Inspected `memononen/nanosvg` commit `239e102ec2c691f2902e20ace2ed36ee4a35cfe6`: `src/nanosvg.h` 88,980 B, `src/nanosvgrast.h` 39,398 B, `LICENSE.txt` 880 B. Two implementation headers total 128,378 B. Zlib license permits proprietary embedding with retained notice and plainly marked alterations. README says it is not actively maintained. Parser dispatch (`nanosvg.h:2836-2899`) includes basic shapes, paths, gradients and styles; it has no text/image renderer. Raster source actually implements strokes, gradients and both fill rules, despite README's older “flat filled shapes” summary. | **Not no-libc as shipped.** Active code uses allocation, string/numeric conversion, math, `qsort`, and `nsvgParseFromFile` stdio. A host `cc -std=c99 -O2 -c` object was 67,248 B; `nm -u` exposed `malloc/realloc/free/calloc`, `sscanf/strtol/strtoll`, string/memory functions, `fopen/fread/fseek/ftell/fclose`, trigonometry and host compiler helpers. This object is neither a guest ELF nor a binary-size estimate. A native port must remove file entry points, own bounded allocation/math/sorting, prevalidate unsupported syntax and replace silent omissions/failure returns (`nsvgRasterize` is `void`, `nanosvgrast.h:1375-1468`). Examples' GLFW/premake are not core dependencies. | Reject for v1: permissive license is fine, but adapting error semantics and bounding a maintained local C fork is real work, not a “single-header” shortcut. A native adapter also needs an owner-approved ADR 0030 amendment and cannot link into Go. |
| From-scratch native Zig subset | No existing complete SVG candidate is claimed. Actual native prior art: `user/src/lib/font_ttf.zig` 49,982 B, `user/src/lib/ui/draw.zig` 48,420 B, `user/zig/arena.zig` 9,097 B and `native.zig` 1,117 B, total 108,616 B. Glyph code uses static scratch and fixed four-piece quadratic subdivision, dropping segments when scratch is full (`font_ttf.zig:56-63,625-669`); `draw.zig:1019` handles AA rounded rectangles. SDK arena owns one bounded mapping (`arena.zig:1-35`); `native.zig` uses ADR 0007 directly. In-tree license; no third-party renderer license. | Technically plausible with Zig 0.16.0, the pinned ADR 0038 freestanding SDK/overlay and caller-owned allocation. Still needs XML, complete selected geometry, robust fills, AA, alpha, explicit errors and all tests. Existing glyph truncation/static scratch are not the new contract. Neither absence of GC nor source byte counts establishes a renderer footprint. ADR 0038's SDK does **not** extend ADR 0030 A1's closed four-workload allowlist (`0030:228-236,366`). | Reject for v1: duplicates language integration for the Go/PDF consumer and requires a new owner-approved native-workload amendment. The index's “fits the Zig portfolio” is not permission. |

No Rust route is evaluated. No native route is selected, so this card
neither claims nor edits `docs/decisions/0030-go-is-el0.md`, held by M88a
(#1921). A future native reversal requires owner approval, a sequenced
0030 amendment with that owner, and a revised M89 boundary.

## 3. SVG byte-to-description contract

All allowlists are closed. Unsupported content causes **whole-document,
bounded refusal**, including content inside an otherwise invisible group.
No best-effort image or partial scene is returned. “Ignore” below is the
only graceful degradation; it affects non-rendering metadata, never paint.

### 3.1 Document, numbers and attributes

UTF-8 XML only, with an optional UTF-8 BOM and XML 1.0 declaration
(`version="1.0"`, optional `encoding="UTF-8"` and `standalone="yes|no"`).
Accept one unprefixed `svg` root with absent namespace or exactly
`xmlns="http://www.w3.org/2000/svg"`. Matching tags, quoted attributes,
unique attribute names and legal UTF-8/XML characters are required.
Comments and whitespace are allowed. No DTD/entity definitions, CDATA,
other processing instructions, namespace prefixes or extra roots.
Only the five predefined XML entities and legal numeric character
references are decoded, once, within the same byte/work caps.

Numbers use SVG decimal syntax: optional sign, digits with optional
decimal point, optional `e|E` and signed exponent. Require at least one
digit, finite result, complete consumption and at most 32 bytes/token.
Accept comma/ASCII-whitespace separators and path's sign-separated
numbers; reject doubled/trailing commas and incomplete argument groups.
Lengths are unitless or `px`, except path/points/transform/viewBox
numbers, which are unitless only. Percentages and physical/font units
are refused. Local coordinates and accumulated relative coordinates
must be within ±32,768; positive lengths/viewBox extents ≤32,768.
All bounds are checked before rounding or allocation.

| Element / placement | Exact additional attributes and behavior | Test family |
|---|---|---|
| `svg`, root only | Required `width`, `height`, positive integral px, each ≤1,024. Optional `viewBox="minX minY width height"` with positive extents; absent means `0 0 width height`. `preserveAspectRatio` absent or `xMidYMid meet` centers uniform scale; `none` scales axes independently. Other modes refused. Root viewport is the fixed output size, clipped to it; no external size inference. | `viewport` |
| `g`, child of root/group | Inherited presentation and local `transform`; no separate viewport or compositing layer. Groups may contain groups or drawable leaves. | `group` |
| `path`, leaf | `d` required, possibly empty. Commands in §3.2. Non-whitespace children refused. | `path` |
| `rect`, leaf | `x`,`y` default 0; required nonnegative `width`,`height`; optional nonnegative `rx`,`ry`. Neither radius means square; one copies to the other; clamp to half the respective extent. Zero extent paints nothing. Rounded corners are elliptical, expanded within the common tolerance/segment limits. | `rect` |
| `circle`, leaf | `cx`,`cy` default 0; required nonnegative `r`; zero radius paints nothing. | `ellipse` |
| `ellipse`, leaf | `cx`,`cy` default 0; required nonnegative `rx`,`ry`; either zero paints nothing. | `ellipse` |
| `polygon`, leaf | Required `points`, possibly empty, ordered coordinate pairs; implicit closing edge. Fewer than three points paint nothing; odd coordinate count is malformed. | `polygon` |
| `title`,`desc`, root/group children | Text-only content; validated and ignored, with bounded metadata counters. No attributes except `id`. They do not supply fallback text or layout. | `metadata` |

Common attributes on `svg`, `g` and drawable leaves are **only**
`id`, `fill`, `fill-rule`, `fill-opacity`, `transform`, and `stroke="none"`.
`id` is opaque metadata, ≤64 bytes, never resolved. Default fill is opaque
black; `fill` accepts `none`, `#RGB`, `#RRGGBB`, `black`, `white`, `red`,
`green` (0,128,0), or `blue`. No other CSS colors or color functions.
`fill-rule` is `nonzero` (default) or `evenodd`. `fill-opacity` is a
unitless number in [0,1], converted to 8-bit alpha by nearest rounding,
halves upward. These three fill properties inherit; a local value replaces
the inherited value. `fill="none"`/zero alpha means no paint, not erasure;
geometry and excluded syntax still validate. `stroke` is absent or
`none` only, with no other stroke attributes.

`transform` accepts a whitespace/comma-separated list of `matrix(a b c d e f)`,
`translate(tx [ty])` and `scale(sx [sy])`; defaults are ty=0 and sy=sx.
Rotation/shear can be represented by `matrix`; named `rotate`, `skewX`
and `skewY` are refused. For column points,
`x'=a*x+c*y+e`, `y'=b*x+d*y+f`. A list `T1 T2` is `T1*T2`,
so T2 acts first; inherited mapping is `parent*local`; final mapping
includes the viewport transform on the left. Matrix coefficients
a,b,c,d and each composed coefficient have magnitude ≤256; e,f ≤32,768.
All transformed control/end points must remain within ±32,768 pixels.
Singular matrices are legal collapsed geometry, not an inverse-map
exception. Geometry outside the target is clipped, not a larger allocation.

### 3.2 Paths, shapes and fills

Accept absolute/relative **M L H V C S Q T Z**, with SVG repeated argument
groups, extra M pairs becoming L, and reflected controls for S/T only
after the corresponding curve family (otherwise current point).
Every nonempty path starts with M. Z closes to the subpath start; a
following drawing command continues from that start. Open contours close
implicitly **for filling**, without changing the parser's current point.
Empty/zero-area contours are successful no-paint geometry.

Unsupported A/a is refused, never replaced by a straight line. Normalize
H/V to L, S to C and T to Q. Expand all shapes/closures and flatten Q/C
after the final transform with de Casteljau subdivision. Endpoint/control
distance from the finite chord must be ≤1/16 output pixel, including
degenerate/backtracking chords. Depth is at most 12; failure to reach
tolerance refuses (`CurveLimit`), unlike TTF's best-effort depth fallback.
Circle/ellipse and rounded corners must satisfy that same geometric
tolerance against the analytic ellipse, not an unchecked four-cubic
approximation. The SVG front end adaptively samples these analytic arcs
using a conservative affine-image error bound, emitting target-coordinate
Move/Line/Close records with identity Paint.Transform. This avoids passing
an inaccurate cubic approximation into the public curve flattener.
All other paths keep Q/C commands for vector preflight; expansion edges
and the generated normalized commands each have their own global cap.

Fill compound contours in one operation, non-zero signed winding or
even-odd parity. Self-intersections, oppositely oriented holes and
reflections are legal. Scanline crossings include the low-y endpoint and
exclude high-y; horizontal edges do not change winding. Equal-x crossings
are aggregated before filling. Use four vertical samples at
`y+1/8,3/8,5/8,7/8` with horizontal span overlap coverage; accumulate a
row, round its final normalized coverage to an 8-bit mask, then blend once
per paint/pixel. No full-canvas mask or per-sample compositing.
Document-order paints use straight-alpha source-over, no gamma correction.
This is the vector sampler, not a change to `draw`'s 4×4 primitive contract.

### 3.3 Exclusions and deterministic outcomes

| Excluded case (including use on hidden/zero-alpha content) | Policy | Test family |
|---|---|---|
| `<text>`, `tspan`, `textPath`, font attributes, embedded fonts | `UnsupportedText`; no substitution, glyph loading or silent omission | `text-refusal` |
| `line`, `polyline`, any non-`none` stroke; stroke width/caps/joins/dashes, markers, `vector-effect` | `UnsupportedFeature`; caller must author filled outlines | `stroke-refusal` |
| A/a, unsupported transform names, unsupported units/colors/aspect modes | `UnsupportedFeature`, naming the construct, never guessed geometry | `syntax-subset` |
| Gradients/stops, patterns, `defs`, `use`, `symbol`, nested `svg`, arbitrary `clipPath`, masks | `UnsupportedFeature`; even unused definitions are refused | `paint-refusal` |
| `image`, `foreignObject`, `a`, links, any `href`/`xlink:href`, `url(...)`, data/file/network URLs, resource attributes | `ExternalResource`; no resolver callback, I/O, fetch or resource decoding | `resource-refusal` |
| Filters and their attributes/elements | `UnsupportedFeature`; never render as though filter absent | `filter-refusal` |
| Animation/SMIL, `animate*`, `set`, CSS animation | `UnsupportedFeature`; never pick a made-up frame | `animation-refusal` |
| `script`, event attributes `on*` | `UnsupportedFeature`; no execution | `script-refusal` |
| `style` element/attribute, `class`, CSS cascade, `currentColor`, `inherit`, `opacity` (including group opacity), display/visibility, blend modes/paint order | `UnsupportedFeature`; no CSS parser, hidden-content shortcut or incorrect per-child group opacity | `style-refusal` |
| Unknown elements/attributes, foreign namespace/prefix, unsupported root version attributes | `UnsupportedFeature`; reject rather than ignore paint-affecting content | `unknown-refusal` |
| Comments, whitespace, `id`, text-only `title`/`desc` | Validated bounded ignore, the only graceful degradation | `metadata` |
| DTD/entity definitions, external entities, CDATA, processing instructions beyond the initial declaration | `UnsupportedXML`; do not expand/load | `xml-subset` |
| Bad XML/UTF-8/entities, duplicate attributes, mismatched tags, invalid path groups, negative radii/extents, nonfinite/out-of-range numbers | `Malformed`, with bounded offset; range exceeding a numeric cap is `CoordinateLimit` | `malformed` |
| Source/depth/node/attribute/token/command/contour/edge/work/memory/time limits | The specific limit error; no truncated success | `limits` |

**Text decision:** refuse every text element. Observed `image/fonts/`
ships Inter regular/bold/italic, Fira Code regular and VirelaiChrome,
not Virelai Sans. `user/go/icons/generate.py:2-9` uses an external
Virelai Sans pipeline to generate the icon font, not a shipped body face.
`user/go/ttf` is the in-tree Inter/Fira Code text path, but SVG text
placement, shaping and font selection are outside v1. Already outlined
letters are ordinary accepted paths, without a font dependency.

## 4. Public M89-facing vector contract, version 1

The following **Go API is normative**, not code landed by R1. M90b owns
`virelai/vector`; consumers import only its exported API. No SVG parser
types, global renderer, serialized pointers or language-neutral ABI.

```go
package vector

type Point struct{ X, Y float64 }
type Affine struct{ A, B, C, D, E, F float64 } // zero is singular, not identity
type Verb uint8
const (
    Move Verb = iota // P[0]: new contour start
    Line            // P[0]: endpoint
    Quad            // P[0]: control; P[1]: endpoint
    Cubic           // P[0],P[1]: controls; P[2]: endpoint
    Close           // no points; close current contour
)
type Command struct { Verb Verb; P [3]Point }
type Rule uint8
const ( NonZero Rule = iota; EvenOdd )
type Rect struct{ X0, Y0, X1, Y1 int } // target pixels, half-open
type Paint struct {
    First, Count uint32 // contiguous range in Commands; independent path
    Transform Affine
    Clip Rect          // target-coordinate integer rectangular clip
    Color uint32       // straight-alpha 0xAARRGGBB
    Rule Rule
}
type Scene struct { Commands []Command; Paints []Paint }
type Target struct { Pix []uint32; Width, Height, Stride int } // words
type Storage struct { Commands []Command; Paints []Paint }
type Workspace struct { Bytes []byte } // caller-owned, reusable, no pointers
type Budget struct { Used, Max uint64 } // cumulative work, never reset by callee
type Stats struct { Work, Edges, ScratchBytes uint64 } // this call's counts
type Code uint8 // named constants listed below; not kernel errno
const OK Code = 0
type Failure struct { Code Code; Offset, Paint, Command int }
func (Failure) Error() string // static message, empty for OK

// Validates everything, including expansion/work plan, without touching dst.
// Scratch requirement <= 524,288 B; validates all borrowed slice ranges.
func Rasterize(s Scene, dst Target, ws Workspace, b *Budget) (Stats, Failure)
```

The SVG adapter uses caller-owned scene storage and the same cumulative
budget, rather than a second renderer:

```go
package svg // imports virelai/vector
type Canvas struct{ Width, Height int }
func Parse(src []byte, out vector.Storage, b *vector.Budget) (vector.Scene, Canvas, vector.Failure)
```

### 4.1 Representation and supported operations

Commands are absolute local coordinates; required points follow their
verb and unused points must be zero. Every paint's path is independently
well-formed, starts with Move unless empty, and may have multiple contours.
Paint ranges must be disjoint, ordered, and together cover Commands;
no alias-based path reuse or scene graph. Rule/verb unknown values fail.
Scene is immutable during a call. Empty scene is a successful no-op.
Stats.Work is this call's Budget.Used delta, including preflight and
replay. Stats.Edges is the unique planned flattened-edge count (not
doubled by replay); Stats.ScratchBytes is the highest workspace offset
used. Replaying an edge charges work again, not the geometry capacity.

Solid fill, Q/C curves, general affine transform and per-paint integer
rectangular clip are the whole operation set. No implicit strokes, text,
image blits, gradients, save/restore stack or PDF parser. The caller
flattens its graphics-state stack into Paint records and supplies outlines
for any accepted PDF glyphs/strokes, charging that preparation to M89's
budget. Rect is intersected with target bounds; equal endpoints are an
empty clip, reversed endpoints are invalid. There is no implicit “zero
Rect means full target”; callers pass `{0,0,w,h}`.

Coordinates are output pixel edges, x right/y down, origin top-left,
with fractional geometry supported. M89 converts its page units and
y-up coordinates explicitly, e.g. at 96 dpi a points-to-pixels transform
is `{A:96/72, D:-96/72, F:pageHeightPixels}` before page offsets.
Scale/offset/rounding of the PDF page box is M89's decision. The local,
transformed and affine bounds in §3 apply equally to direct consumers.
No fast-math; finite float64 geometry with specified subdivision/order
and integer final blending, deterministic for identical inputs on the
supported arm64 host/guest. Curve capacity exhaustion is an error.

**Target maximum: 1,024 × 1,536**, not the SVG front end's 1,024 square.
No tiling is required for M89a's maximum page. Width/height must be
positive; Stride is in `uint32` words, Width ≤ Stride ≤1,040; validate
`len(Pix) >= Stride*Height` using checked multiplication. The full
page-rounded backing slice counts toward consumer memory; only Width
pixels/row are written and row padding/tail remain unchanged.

Pixels are top-down **straight-alpha 0xAARRGGBB**, little-endian B,G,R,A,
matching the in-tree buffer convention, **not premultiplied**. Source-over
weights destination RGB by destination alpha, as `draw.go:52-74`;
coverage scales source alpha with nearest rounding. Result alpha zero
has RGB zero for written pixels. No gamma/color-space conversion.
For coverage byte c, source alpha becomes `sa=(A*c+127)/255`.
With destination alpha da, `den=sa*255+da*(255-sa)`; resulting alpha
is `(den+127)/255`, and each channel is
`(src*sa*255+dst*da*(255-sa)+den/2)/den`. sa=0 is an unchanged
destination; den=0 produces zero. This specifies rounding, rather than
importing TTF's opaque-destination blend.
The caller initializes output (transparent for SVG, usually opaque white
for PDF); Rasterize does not clear it. Display scanout may ignore/use the
A byte differently; publishing a window is outside this API.

### 4.2 Ownership, errors and budgets

Caller owns source, commands, paints, output and Workspace. Parse borrows
src for the call only; returned Scene is a prefix of Storage and retains
no source/string slices. Capacity is checked before every write;
insufficient Storage is `SceneLimit`, no allocation fallback. Rasterize
allocates **zero heap objects**, keeps no references after return and
spawns no goroutines. Workspace needs 8-byte alignment and 524,288 B,
irrespective of height; smaller workspace is `ScratchLimit`. It contains
only pointer-free edges, crossing/sort arrays, row accumulation and
bounded subdivision/planning state. No conversion of untrusted addresses.
The caller must keep all backing allocations alive, prohibit overlap
among writable buffers and immutable inputs, and use a separate Workspace
for concurrency. No cache/hidden full-surface bitmap.

Failure codes are `Malformed`, `UnsupportedFeature`, `UnsupportedText`,
`ExternalResource`, `UnsupportedXML`, `SourceLimit`, `DepthLimit`,
`NodeLimit`, `AttributeLimit`, `TokenLimit`, `CommandLimit`, `ContourLimit`,
`SegmentLimit`, `CurveLimit`, `CoordinateLimit`, `CanvasLimit`,
`SceneLimit`, `ScratchLimit`, `WorkLimit`, `MemoryLimit`, `TimeLimit`,
and `InvalidBuffer`, declared as successive Code constants starting at 1
in the listed order. Success is Failure with Code `OK`; indexes are -1.
Return Failure **by value**, not boxed into a Go `error` interface, so
indexed diagnostics need no heap allocation. SVG failures carry a byte Offset; direct vector
failures carry Paint/Command indexes; unavailable indexes are -1.
No panic, infinite loop, format-specific success substitution or diagnostic
echo of user data. Static error text is ≤128 bytes. MemoryLimit/TimeLimit
are caller/adapter admission or post-call errors, not hidden raster
allocations or an asynchronous rollback promise.
For multiple defects, source admission limits precede parsing; thereafter
report the first offending token in document order. Direct calls validate
Budget, Target and Workspace, then scene counts and paints/commands in
index order before expansion/work. An unrecognized resource-bearing
attribute is ExternalResource, not the generic unknown-attribute error.

Parse validates the entire document, then returns a scene or zero scene/
canvas and an error; written Storage prefixes are invalid on error.
Rasterize uses a deterministic preflight pass to validate commands,
flattening, scratch capacity and reserve its exact subsequent work cost.
On validation/limit failure **dst is unchanged**. Preflight and replay
both count; Budget.Used records work actually performed even on failure.
Reject invalid budgets (`Used > Max` or Max above the hard cap).
After reservation the deterministic raster pass cannot exhaust work/
scratch mid-paint. Buffer aliasing/concurrent mutation is a caller error,
not a supported rollback case. A caller enforcing wall-time can abort
only between calls; a timed-out result is discarded/unpublished.

M89 owns its document, stream decode, outline conversion, background
initialization, output and process-wide memory/time budgets. It passes a
cumulative vector Budget and counts Stats within its page work ledger;
it may set a lower Max, never a higher one. SVG Parse and Rasterize share
one Budget. Repeated calls do not reset it. A lower limit may refuse a
valid scene. M89 never edits M90 files, copies private raster state or
assumes PDF operators map to SVG syntax.

## 5. Final budgets and accounting

All limits are inclusive; reject the first operation exceeding one.
Dimensions are capability ceilings, not a promise that every combination
of maximum source/complexity succeeds. The finite accepted corpus must
succeed; hostile valid inputs may exhaust work. No limit is TBD.

| Required budget | Final ceiling | Workload / accounting |
|---|---:|---|
| Renderer-added binary bytes | **1,048,576 B** | Difference in complete stripped static guest ELF file sizes between full SVG+vector adapter and equivalent empty adapter; same pinned fork/compiler, SDK, transport, output writer, fixed buffers, `CGO_ENABLED=0`, `-trimpath`, `-ldflags="-s -w"`, normal optimized Go build. Empty adapter omits only engine calls/packages. Report full size and PT_LOAD filesz/memsz too; no source/object-size proxy. |
| Typical icon parse+raster time | **100 ms** | Maximum single call sequence for every §6 icon at 64×64, source preloaded and buffers supplied; includes Parse, output background initialization, vector preflight/replay and collector interruptions. Excludes VM startup, source transport, allocation setup and output write. |
| SVG peak raster memory | **8,388,608 B** | Page-rounded capacity of all retained source, scene, output, scratch, adapter/staging buffers, metadata/alignment and unused arena space, not just live logical slices. No uncharged copy. Runtime headroom is separately inside the process limit below. |
| SVG canvas | **1,024 × 1,024 px** | Root dimensions; max output 4,194,304 B at packed 32 bpp. Larger/zero/nonintegral dimensions refuse. |
| SVG source | **1,048,576 B** | Complete uncompressed bytes including BOM, whitespace/metadata; no gzip/SVGZ. Detect cap+1 before parsing. |
| XML nesting | **32** | Root counts as depth 1; metadata counts too. Check before push. |
| Expanded path segments | **8,192** | Whole render: every emitted straight edge after shape expansion, affine mapping, curve subdivision and explicit/implicit closure, including degenerate/clipped/no-paint geometry. No per-path reset. |
| Work units per render | **64,000,000** | One cumulative Parse+preflight+replay ledger, defined below. Replaces draft 1,000,000: even a square output initialization has 1,048,576 pixels, and sampled fill/preflight costs more; the smaller value cannot exercise maximum canvas honestly. |

Additional hard bounds: **4,096 XML elements**, **16 attributes/element**,
**32,768 total attributes**, **64 B name/id**, **32 B numeric token**,
**16 transforms/element**, **256 transforms/document**, **8,192 normalized
commands**, **1,024 contours**, **1,024 paints**, **1,048,576 B scene
storage**, **524,288 B Workspace**, **12 curve-subdivision levels**.
Metadata shares source/node/attribute/work bounds. Direct vector callers
use the same command/contour/paint/edge/scene/work limits without XML.
Both preflight and raster loops are iterative or depth-bounded.

One work unit is charged **before** each source-byte examination (including
rescans/entity/numeric handling), normalized command/paint validation,
point transform, subdivision-node visit, emitted edge, scanline edge test/
crossing, crossing sort comparison or move, winding/span update,
per-pixel coverage accumulation, destination pixel blend, or caller's
destination clear. Copies/zeroing charge each byte; fixed-size record
operations count as one, bulk memory operations do not bypass the ledger.
Arithmetic inside one listed operation is bounded. Count planning and
replay, offscreen tests and unsuccessful attempts. A deterministic bounded
sort replaces TTF's unchecked insertion-sort cost. M90b publishes exact
counter tests for each category; optimizations must preserve a conservative
charge. Input transport/output serialization are excluded from render
work/time but their retained buffers still count as memory.

Reference time hardware: **Apple M4, 16 GiB host RAM, macOS 27.2,
Apple VZ, 2 vCPUs, 256 MiB guest RAM**. Use `vi.Nanos`'s guest monotonic
counter (`vi_guest.go:36-41`), store integer nanoseconds, report milliseconds
to 0.001 ms without rounding for pass/fail. Record counter frequency/
resolution, toolchain/build mode, host load and VM settings; do not use
whole-second wall time. One cold render and 20 repeats per icon, report
the maximum including the cold sample, never only the mean. Maximum SVG
canvas and direct 1,024×1,536 page fixtures must each finish within
**5,000 ms/call**; no blanket 100 ms promise for maximum-complexity input.

### 5.1 Allocation plan and Go runtime margin

Use one retained **8 MiB private anonymous SVG arena** via `vi.MmapAnon`,
populated and partitioned once, then reused for 100 success/refusal cycles:

| Partition | Maximum capacity |
|---|---:|
| Source bytes, never copied into a Go string/DOM | 1,048,576 B |
| Packed maximum SVG output | 4,194,304 B |
| Pointer-free Command/Paint storage | 1,048,576 B |
| Vector Workspace, row rather than full-canvas mask | 524,288 B |
| Adapter I/O staging, state, alignment/accounting | 524,288 B |
| Unassigned arena margin, still charged | 1,048,576 B |
| **Total retained arena** | **8,388,608 B** |

Go slices may view aligned mapped POD storage, but no Go pointers/strings/
interfaces may be stored in it. The owner retains its mapping until exit,
uses bounded SDK staging for file I/O and never maps per render. Scene
headers and bounded stack locals live in ordinary Go memory and count
against runtime margin. No general XML DOM, `encoding/xml` allocation
defaults, font cache, `fmt` diagnostic allocation or per-render `append`
growth is permitted.

`process.zig:124-130` limits **all** recorded demand-backed pages to
4,096 (16,777,216 B) and mmap regions to 16. This is not 256 MiB of
available heap. The SVG plan reserves **6,291,456 B (1,536 pages)** for
runtime/collector metadata, stacks, heap growth/retained free spans,
wrappers and other demand-page use, so arena+runtime ≤**14,680,064 B
(3,584 pages)**, leaving **2,097,152 B (512 pages)** below the hard cap.
No doubling the 8 MiB live arena under GC. Use ≤**12 occupied mmap regions**
(one renderer arena plus at most eleven runtime/other), leaving four spare.
These are acceptance limits, **not observed runtime sufficiency**.

The raw arena removes large GC-managed buffers, not runtime initialization
costs. `runtime.MemStats.HeapAlloc` alone cannot prove the cap; the sbrk
runtime grows via mmap (`tools/go/overlay/runtime/os_virelai.go:34-39`).
M90b must audit the pinned runtime's touched/resident demand-page backing
and maximum region growth, including collector cycles and failed inputs;
conservatively charged mapping extents can prove an upper bound, but must
not substitute virtual heap reservation or ELF `datapages` for actual
dynamic-page accounting. `vi.ProcRow` does not expose those counters.
M90c requires a reproducible conservative upper-bound ledger from the
runtime mapping/touch paths plus allocation receipts; if that cannot prove
the limits without kernel/runtime changes, **block acceptance and request
a separately owned diagnostic**, not an unclaimed SDK edit or a guessed
“runtime fits.” No cap increase is part of M90.

The vector target's maximum backing is **6,389,760 B** at stride 1,040
and height 1,536 (packed stride 1,024 is 6,291,456 B). Direct raster input
scene ≤1 MiB plus Workspace 0.5 MiB and that backing is ≤**7,962,624 B**,
before caller staging; preflight/replay needs no second output. This is
the vector library's live caller-buffer requirement, not a process arena
reservation guarantee.
The direct-consumer proof uses the same 8 MiB arena, no SVG source, packed
stride at maximum page. This proves capability, not M89's memory plan:
a 4 MiB PDF plus page and runtime may not fit. M89a must budget/stream
resident PDF data and runtime separately within its own limits and the
same 16 MiB process cap; this ADR grants no extra arena or GC headroom.

## 6. Corpus, independent references and acceptance

M90b pins small, authored engine vectors under
`tests/fixtures/svg/engine/`; M90c owns the independently checked
acceptance corpus under `tests/fixtures/svg/acceptance/`. Never generate
references from this renderer and call that independent verification.

**Finite icon corpus: 12 fixed 64×64 SVGs**, each ≤16,384 B, ≤128 normalized
commands, ≤512 expanded edges and ≤16 paints:

| Fixture ID | Required geometry/behavior |
|---|---|
| `rect` | square/rounded rect, rx-only/ry-only copying and clamp |
| `ellipse` | circle and noncircular ellipse at fractional positions |
| `polygon` | concave polygon, implicit close and empty geometry |
| `lines` | M/L/H/V, repeated groups, relative signs and multiple contours |
| `quadratic` | Q/T reflection and degenerate/backtracking chord |
| `cubic` | C/S reflection and inflection |
| `nonzero` | same/opposite-oriented compound contours and hole |
| `evenodd` | self-intersection and parity hole |
| `affine` | translate/scale/matrix, reflection and singular no-paint |
| `group` | nested inheritance and noncommuting transform order |
| `viewport` | offset viewBox, centered meet and nonuniform none |
| `alpha` | overlapping translucent fills over transparent/opaque pixels |

Engine parameterized tests additionally exhaust every allowlisted
attribute/default, color spelling, command case/arity/repetition and
element placement in §3, not just the 12 icons. Degenerate geometry,
offscreen controls, huge bounds, matrix composition and both fill rules
must not disappear behind the small corpus.

**Maximum fixtures:** one 1,024-square SVG with solid full-canvas fill;
one with clipped transformed cubic/compound fills; direct-consumer
1,024×1,536 filled page with a bottom-row feature proving height is not
silently truncated; a padded-stride case with unchanged sentinel padding.
Do not commit multi-megabyte golden files: use small feature references,
analytic checks and streaming reference generation with pinned hashes.

For independent shape semantics, M90c pins **host-only CairoSVG 2.8.2**
(`Kozea/CairoSVG` tag source inspected at
`9e8c6ede00dd1c4495fca4809b4cabd628a85eb9`) and its Cairo/Python
dependency versions/checksums and notices, producing PNG/RGBA
at exact target dimensions with no font/resource access. This is a
Python/Cairo reference oracle, **not a guest dependency or engine route**.
Its LGPL-3.0-or-later license applies to the host tool, not the authored
input fixtures; retain tool notices and record reference-image provenance.
No Rust toolchain or renderer is selected. Build/download is reference
preparation only, never during a guest build.
Record tool version/hash, SVG hash, invocation, background and conversion
from RGBA to the stated BGRA words in the corpus manifest. No unpinned
browser screenshot, undisclosed font or network-derived golden. If that
version cannot be acquired/verified, reference generation is blocked;
no silently substituted oracle. Run the authored accepted fixtures from
bytes with `unsafe=False`, a denying resource fetcher and no font elements;
out-of-subset negatives never go through the oracle. `setup.cfg` and
`cairosvg/VERSION` confirm the release/dependencies, and `LICENSE` confirms
LGPL-3.0 (package declaration: LGPL-3.0-or-later). Source acquisition was
observed, but a working host oracle and its dependency environment were
not installed or executed by R1.

Comparison accounts for different AA samplers without hiding wrong
geometry: exact dimensions and fully covered flat-color/interior pixels;
exact expected no-paint/metadata invariance and analytic alpha tests.
For curve/edge pixels only, compare **premultiplied RGBA** converted from
straight output, max channel difference ≤**64/255**, mean channel
difference across the whole image ≤**1/255**, and no nonzero discrepancy
outside a **one-pixel Chebyshev dilation** of the oracle's edge mask.
Edge mask includes nonopaque coverage pixels and adjacent pixels whose
premultiplied color differs; derive it from the independent oracle,
never the tested output. Exact same-algorithm host/guest Go output must
be bit-identical. Tolerances are fixed here; a failing curve must be fixed
or explicitly reviewed, not accommodated by widening thresholds.

Negative families are the §3.3 table. Each exclusion gets a separate
fixture, including in a hidden/zero-alpha group. Test every numeric bound
at limit and limit+1 (or nearest meaningful float), exponent overflow,
integer product overflow, malformed UTF-8/entity/depth/commands, failure
recovery and no partial output. A within-limit boundary can yield another
specific resource refusal, never a fabricated boundary success; provide
separate low-work fixtures proving supported capacities.

The independent Go consumer imports **only `virelai/vector`**, constructs
Commands/Paints itself (no SVG parse, no PDF implementation), and proves
Q/C geometry, winding/parity, affine/y-axis conversion, rectangular clip,
alpha over existing pixels, 1,536-row page, padded stride, immutable source,
borrowed storage/scratch reuse, zero heap allocation during calls,
zero-scene success, all errors and dst-unchanged preflight failures.
Analytic rect/alpha expectations plus independently generated filled-path
references establish consumer pixels. Repeating a renderer's own checksum
is not the consumer test.

## 7. Landable implementation cards and gate ownership

These **finalize the held M90b/M90c shapes**, including their corrected
Go homes and isolated gate. They are not newly filed issues. Owner reviews
this split before claims/implementation; if effort exceeds the declared
16–24 / 12–16 agent-hour scopes, re-scope complete interfaces, not parser-
only prerequisites. Each card's landing PR applies only to its own card;
the index remains open for all four acceptance conditions.

| Card | Gate-syntax `Touches` reservations (comma-separated) | Complete deliverable and verification |
|---|---|---|
| **M90b: SVG + vector implementation**, after M90a | `user/go/svg/*, user/go/vector/*, tools/svg-engine/*, tests/fixtures/svg/engine/*` | All of §3 byte-to-scene plus §4 direct vector interface, limits/work/preflight/row scratch, offline build recipe, empty/full adapters, artifact/delta-size check and engine feature/boundary/golden/allocation tests. Build both guest artifacts with the pinned Go fork and inspect static no-FFI loader/dependency shape. Run `go -C user/go test ./svg ./vector ./ttf ./draw`, owned tool tests and `git diff --check`; prove the conservative runtime/mmap ledger, without changing the runtime. A parser or compiled library without the whole declared contract does not finish this card. |
| **M90c: independent consumer + VZ acceptance**, after M90b | `user/go/svgproof/*, user/go/vectorconsumer/*, tools/svg-proof/*, tests/fixtures/svg/acceptance/*, tools/gate/specs/live-svg-raster.spec` | Minimal guest adapter, independent consumer, frozen oracle/provenance/comparator, whole corpus including exclusions and max page, timing/memory/work receipts and 100-cycle reuse/reclamation tests. Run host consumer/comparator tests, `just gate live-svg-raster`, `bash tools/inventory-gates.sh --check`, `just verify-coordination`, `git diff --check`. Engine files remain read-only. Missing renderer/oracle/bitmap/receipt, timeout or unverifiable runtime bounds are failures, not skips. |

Reservations use exact files or terminal `dir/*` prefix globs, never
recursive glob syntax. M89 reserves its own Go parser/adapter/test paths
and only reads/calls `vector`; no overlap with either card. M88a, M91a/b/c
remain independent. Shared Go draw/font/SDK/runtime, PDF files, root build,
manifests, ADRs and `docs/status.md` are outside both ownership sets;
unavoidable changes need explicit owner sequencing before a revised claim.

**Downstream gate: `live-svg-raster`, one new declarative spec** in
`tools/gate/specs/live-svg-raster.spec`, following `tools/gate/SPEC.md`.
Source inspection found no isolated SVG bitmap gate; `live-web.spec:49`
puts existing image decode in `live-web-ttf`, neither tests SVG.
`go-r3d` is a triangle/window proof, not this contract. Do not retarget
those specs, add a verification shell gate or commit the generated fleet
inventory. Adding the spec registers the gate through discovery.

M90c's spec stages the prebuilt no-UI SVG proof ELF and consumer ELF with
`vgate_setup_python`, checks independent reference/artifact pins, and uses
one invocation per boot, ending on a marker the **program** prints after
all output closes. Separate boots cover icons, negative/reuse suite,
maximum SVG and direct-vector page. `vgate_assert ... python` independently
compares shared bitmaps and checks receipts; it copies asserted share
outputs into ignored evidence before share teardown. Require zero
unexpected process exit, no `[EXC] parking:`, no outstanding outputs on
failure and post-exit allocation reclamation. Standard gate logs live in
`artifacts/live-svg-raster-*`, detail in ignored `artifacts/m90-acceptance/`;
only small pinned source/reference fixtures are committed.

M90 has no PDF-implementation dependency. M89a's review must acknowledge
Go, this exported API, filled-only operations, target size, its own
resident-document/runtime budgets and the M90b dependency before it
claims a renderer card. Its independent consumer proof is already M90c's
job. The owner records final index acceptance for #1917 only after SVG
pixels within all budgets, every exclusion behaving exactly as declared,
consumer-side contract acceptance and evidence for each implementation card.

## 8. R1 verification and precise non-results

This design maps parent R1 answers 1–6 to §§2, 3, 5, 3.3, 4 and 7,
and card answers 1–6 to §§2, 3, 5, 3.3, 4 and 7 respectively.
Owner review covers the route, exact subset, exclusions, text decision,
all numbers, M89 language/size boundary and disjoint closable split.
Run `git diff --check` and `just verify-coordination` on this one-file PR.

No guest renderer exists on this design card, so no SVG/PDF raster run
or numerical-budget diagnostic was performed. The worktree-default
`../go-virelai/bin/go` fork executable was absent when checked:
the downstream **offline guest cross-build** needs an explicit existing
`GO_FORK_DIR` or the separately prepared pinned fork, not automatic
download/build in a gate. No claim that cross-build failed or that guest
runtime fits is made. Oracle generation, general-renderer compilation,
actual guest pixels and budget acceptance belong to M90b/c, not these
host prior-art diagnostics.
