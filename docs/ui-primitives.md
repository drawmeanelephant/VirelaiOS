# `user/go/draw` — UI primitives contract (chrome design language + API)

Status: **contract v1** · Date: 2026-09-27 · Owner: M86a (issue #1817) ·
Source of truth for: M86b (#1818, glyph inventory), M86c (#1819, shape
renderer), M86d (#1820, published API + adoption) ·
Arc index: M86 (#1816)

> A fresh author can implement or call every primitive from this document
> alone. §3 is the API — M86c implements it and M86d publishes it; neither
> renegotiates it. §2 is the taste — chrome drawn to any other geometry is
> off-contract. §4 is the glyph inventory — M86b draws exactly these.
> Values are tagged **(observed)** when measured from existing code and
> **(decision)** when this doc lands the choice.

This is the visual-design spec ADR 0008 deliberately deferred ("Colors,
spacing, and exact glyphs stay in the theme/Driving Award implementation;
this ADR fixes *behavior and discoverability*, not pixels"). ADR 0008 D4
("focus is always visible") stays binding on everything here.

## 0. What this is — and is not

- **Is:** the chrome design language (§2), the primitive API contract (§3),
  and the chrome glyph inventory (§4). Primitives, not a framework.
- **Is not:** a widget toolkit (the three `widgets` are the whole kit), a
  layout engine, SVG (explicit non-goal), a theme editor, or an ADR. The
  roadmap's standing guardrail applies: "someone lovingly architects
  GTKvirelai before anyone has clicked a button" — nothing in this contract
  grows.

## 1. Where primitives live — the buffer model

- **Package `user/go/draw`, userland only.** Pixel ownership is Seam B (M33):
  apps own their buffers and paint their own pixels; the seat composes. The
  engine is a pure raster library over a caller-owned pixel buffer — **zero
  kernel change**, no draw-slot work (ADR 0030/0034: the code is Go).
- **Pixel format (decision, matching observed reality):** colors are
  `uint32` **0xAARRGGBB** (alpha in the top byte; little-endian memory order
  B,G,R,A — the scanout's B8G8R8X8 and `user/go/ttf`'s blending format).
  Two hard rules: alpha `0x00` renders **transparent** (the scanout treats
  X/A=0 as transparent — hardware contract), and drawing with `rgb` alpha 0
  is a **no-op**, never an eraser.
- **`widgets.Canvas` widens, `FillRect` never changes.** Today the interface
  is one method (`widgets.go:53`): `FillRect(r Rect, rgb uint32)`, with tests
  supplying a recording canvas and the guest batching into `vi.Filler`. §3.2
  widens it to four methods; `FillRect`'s signature is frozen so the three
  widgets and every recording test stay green (the zero-regression fixed
  point).
- **Two canvas implementations ship (M86d):**
  - `draw.BufferCanvas{Pix []u32, W, H int}` — the real one; every AA
    guarantee in §3.3 holds.
  - `draw.FillerCanvas` — the legacy `vi.Filler` adapter: `FillRect` batches
    as today; every other method degrades to a hard-edged rect approximation
    (deterministic, documented). Apps that care about AA use the buffer.
- **Where an app gets `[]u32` (named gap):** the Seam-B window buffer — the
  GOWIN/M54 mmap path (`vi` already exposes the `sys_mmap` map flags,
  `vi.go:819`). The one call apps use is M86d's adapter deliverable; until it
  lands, `FillerCanvas` is the compatibility path. This gap is named, not
  hidden.

## 2. The chrome design language

### 2.1 Color roles — mapped onto `user/go/theme`

Roles come from `theme.Tokens` (**observed**: the M69c #1530 table; dark is
byte-identical to Zig `THEME_DARK`/`CHROME_DARK`). Apps carry no hex
(`theme.go`: "Tokens are data, not a framework").

| Role | Token | Notes |
|------|-------|-------|
| Window / app background | `Bg` | |
| Chrome plate (title bars, toolbars) | `Surface` | |
| Recessed panels (inputs, code, gutters) | `ChromeBg`, `Gutter` | |
| Borders / rules | `Border`, `Rule` | 1px |
| Text / secondary text | `Text`, `Muted`, `InkMuted` | |
| Accent (selection, active, caret) | `Accent`, `Caret`, `Selection` | |
| Text on accent | `OnAccent` | |
| Button faces | `BtnIdle` → `BtnHover` → `BtnPressed` | the state ramp |
| Status | `Success`/`Ok`, `Danger`, `Warning` | |
| Focus ring | `Accent` at `FocusW` | ADR 0008 D4 |

**Resolution order (decision):** `theme=custom` resolves
`palette_bg→Bg`, `palette_fg→Text`, `palette_accent→Accent` (+`OnAccent`
for contrast) **at paint** (the M73m rule); `theme=dark|light` selects the
table; the boot default is `dark`. `theme.Set` refuses unknown names — a
typo never invents a third palette.

### 2.2 Geometry

- **Spacing scale (observed):** `PadXS=2`, `PadSM=4`, `PadMD=8`, `PadLG=16`
  (`theme.Tokens`). All chrome insets come from this scale; a composed value
  may sum two adjacent steps (e.g. `12 = PadMD + PadSM`). Legacy chrome that
  predates the scale keeps its measured value until M86d redraws it (the
  capstone adopts the scale; e.g. `appkit/dialog.go`'s `Inset(12)`,
  `gotabwm/chrome.go`'s `chromePad=6`).
- **Corner radii (decision):** `radiusControl = 4` (buttons, inputs, list
  plates), `radiusWindow = 8` (dialogs, panels, popovers), `radiusPill =
  r.h/2` (capsules — the tab-pill rule). Radius clamps to `min(w,h)/2`.
- **Strokes (observed):** `BorderW = 1` for every border; `FocusW = 2` for
  focus rings, drawn **inside** the bounds (no layout shift).
- **Title bar (decision):** height **28** (a 16px face + 6px above and
  below — `gotabwm/chrome.go`'s face+`chromePad` composition, measured at
  the 8px face where `ChromeH=20`), title left-aligned at `PadMD` inset,
  window controls
  right-aligned in the order minimize · maximize · close (close rightmost),
  16×16 each with `PadSM` gaps.
- **Type:** UI text rides `user/go/ttf`; chrome labels at 13px (**observed**
  parity with the terminal's 13px FiraCode face); icons per §4.

### 2.3 States

Interactive chrome (buttons, list rows, tabs) has exactly these states:

1. **default** — face `BtnIdle` (or `Surface`), border `BorderW`/`Border`.
2. **hover** — face `BtnHover`.
3. **pressed** — face `BtnPressed`.
4. **disabled** — label `Muted`, face `BtnIdle` blended 50% toward `Surface`
   (integer blend, deterministic); never brighter than default.
5. **focused-visible** — the 2px `Accent` focus ring, inside bounds. Focus
   is always visible (ADR 0008 D4) and never color-only.
6. **selected** (lists, tabs) — fill `Selection` plus the **3px `Accent`
   left-edge marker** (**observed**: the Zig tab chrome's `3×22` accent bar).

### 2.4 Chrome anatomy

- **Title bar:** plate `Surface`, 1px `Border` bottom rule, title `Text`,
  controls per §2.2. Hovering **close** tints its glyph `Danger`; hover on
  the others tints `Muted`→`Text`.
- **Buttons:** `radiusControl` plate, `BorderW` border, the §2.3 ramp, label
  `Text` centered at `PadSM` horizontal padding; the default button in a
  dialog draws its `Accent` border + `OnAccent` label.
- **Dialogs:** `radiusWindow` plate (`Surface`), `BorderW` border, content
  inset `PadMD + PadSM`, title row, message, button row right-aligned with
  `PadSM` gaps. **Capstone surface for M86d** (§5).
- **Lists/rows:** row height 24 (**observed**: `appkit`'s input row `H: 24`),
  selection + hover per §2.3.

## 3. The primitive API contract

### 3.1 Types and engine functions (M86c implements exactly these)

```go
package draw

type Point struct{ X, Y int }

// BufferCanvas is a caller-owned pixel buffer. Pix has len W*H, row-major,
// stride W, pixels 0xAARRGGBB.
type BufferCanvas struct {
	Pix []uint32
	W, H int
}

// Geometry — all clip to the canvas, all allocate nothing (except the two
// polygon calls: bounded scratch, documented below).
func FillRect(c *BufferCanvas, r widgets.Rect, rgb uint32)
func FillRoundedRect(c *BufferCanvas, r widgets.Rect, radius int, rgb uint32)
func StrokeRoundedRect(c *BufferCanvas, r widgets.Rect, radius, weight int, rgb uint32)
func FillEllipse(c *BufferCanvas, r widgets.Rect, rgb uint32)
func StrokeEllipse(c *BufferCanvas, r widgets.Rect, weight int, rgb uint32)
func FillPolygon(c *BufferCanvas, pts []Point, rgb uint32)      // bounded scratch
func StrokePolyline(c *BufferCanvas, pts []Point, weight int, rgb uint32) // bounded scratch
func StrokeLine(c *BufferCanvas, x0, y0, x1, y1, weight int, rgb uint32)

// Compositing — the glyph/image path.
func BlendPixel(c *BufferCanvas, x, y int, rgb uint32)          // one AA pixel
func BlitTinted(c *BufferCanvas, src *BufferCanvas, x, y int, rgb uint32) // tint by src alpha
```

`widgets.Rect` (right/bottom exclusive — **observed** `widgets.go:25`) is
the one rectangle type; `draw` does not define a second. Dependency
direction is fixed: **`draw` imports `widgets`; `widgets` never imports
`draw`** — §3.2 takes raw sprite pixels precisely so the cycle cannot form.

### 3.2 The widened `widgets.Canvas` (M86d publishes)

```go
type Canvas interface {
	FillRect(r Rect, rgb uint32)                                  // frozen, unchanged
	FillRoundedRect(r Rect, radius int, rgb uint32)
	StrokeRoundedRect(r Rect, radius, weight int, rgb uint32)
	BlitTinted(src []uint32, srcW, srcH, x, y int, rgb uint32)     // raw sprite pixels; icon + image path
}
```

Exactly four methods — the chrome set widgets need. Ellipse, polygon, and
line stay `BufferCanvas`-only until a widget needs them (the contract grows
by revision, §6, never by accretion). Recorders implement the three new
methods as record-and-continue.

### 3.3 AA guarantees (normative — goldens pin every one)

1. **Coverage-based anti-aliasing.** Edge pixels blend in proportion to
   coverage (≥16-sample-equivalent for curves). **Integer/fixed-point math
   only** — the engine contains no floating point (guest determinism; the
   same rule `widgets.Scale` follows).
2. **Blending** is straight-alpha source-over into the destination, **no
   gamma correction** (documented; matches `user/go/ttf`).
3. **Determinism:** identical inputs produce bit-identical output on any
   host. No map iteration order, no goroutine races in the engine.
4. **Clipping:** every primitive clips to `[0,W)×[0,H)`; out-of-bounds draw
   calls are dropped, never panicked; empty rects (`W<=0 || H<=0`) are
   no-ops.
5. **Strokes** render **inside** the given bounds/path (matching `FocusW`'s
   inside rule); `weight` is in pixels and symmetric about the path.
6. **Radii** clamp to `[0, min(w,h)/2]`; `weight` clamps to `[0, min(w,h)]`
   and `weight 0` draws nothing. A bad argument is clamped deterministically
   (and refused loudly in tests) — never a guest crash.
7. **Hot-path allocation:** rect/rounded-rect/ellipse/line calls allocate
   nothing; `FillPolygon`/`StrokePolyline` use bounded scratch (≤ one
   scanline buffer per call).

**Behavior reference (observed prior art):** the Zig
`user/src/lib/ui/draw.zig:1019` `fill_rounded_rect_buf` — 4-way subpixel
quarter-circle AA, 99/99 geometry tests. Where §3.3 and that behavior
disagree, §3.3 wins, but the divergence is recorded in M86c's goldens.

## 4. Glyph inventory (M86b draws exactly these twenty)

**Rendering rule:** glyphs are font glyphs drawn through `user/go/ttf`
(outline font ⇒ any size), **tinted** via `BlitTinted`/`BlendPixel` with a
§2.1 role color. A glyph is never baked in a color. Style (**decision**):
geometric, single-weight, filled silhouettes over hairlines (they must read
at 12px), rounded terminals.

**Codepoints:** Private Use Area, `U+E000`–`U+E013` (20 slots). The table is
normative; M86b ships it as `user/go/icons` + the asset under
`image/fonts/` (with its license file — the Inter/Fira pattern).

| CP | Name | Shape | Primary use |
|----|------|-------|-------------|
| `U+E000` | `window-close` | ✕ | title bar close |
| `U+E001` | `window-minimize` | – | title bar minimize |
| `U+E002` | `window-maximize` | □ | title bar maximize |
| `U+E003` | `window-restore` | overlapping squares | restore from maximize |
| `U+E004` | `window-pin` | pin | always-on-top on |
| `U+E005` | `window-unpin` | open pin | always-on-top off |
| `U+E006` | `chevron-left` | ‹ | back / collapse |
| `U+E007` | `chevron-right` | › | forward / expand |
| `U+E008` | `chevron-up` | ^ | sort up / scroll |
| `U+E009` | `chevron-down` | v | sort down / dropdown |
| `U+E00A` | `check` | ✓ | confirm / selected |
| `U+E00B` | `plus` | + | new tab / add |
| `U+E00C` | `search` | magnifier | find |
| `U+E00D` | `settings-gear` | gear | settings |
| `U+E00E` | `folder` | folder | file manager |
| `U+E00F` | `file` | page | file manager |
| `U+E010` | `warning-triangle` | ⚠ | warnings |
| `U+E011` | `error-circle` | ✕ in circle | errors |
| `U+E012` | `info-circle` | i in circle | notices |
| `U+E013` | `lock` | padlock | trust / locked state |

**Sizes (decision):** designed on a **16px em grid**; rendered at 12, 16
(nominal), or 20 px. **All twenty shapes must read at 12px** — M86b's
golden QA bar.

## 5. Consumers and sequencing

| Card | Consumes | Delivers |
|------|----------|----------|
| M86c (#1819) | §3.1, §3.3 | the engine + goldens (class A) |
| M86b (#1818) | §4 | the asset + `user/go/icons` + per-glyph goldens (class A) |
| M86d (#1820) | §2, §3.2, §4 | the published interface, the `[]u32` adapter (§1), and the **capstone: `user/go/appkit` dialog chrome redrawn to §2** — plate, border, title, input, buttons, focus in one surface — pixel-gated by extending `live-wm-*-chrome` |

Follow-on adopters, in order: the seat's title/rail chrome
(`user/go/gotabwm`), then `user/go/widgets` faces. The recording canvas
keeps recording — widgets' zero-regression tests pass unchanged.

## 6. Enforcement and revision

- **Class A:** `cd user/go && go test ./...` — §3.3's guarantees and §2's
  composite surfaces are golden-pinned; widget regression tests are the
  fixed point.
- **Class B:** M86d's `live-wm-*-chrome` extension (extend the existing
  spec; the gate rule).
- **This file is the contract.** A change to §3's signatures or §4's
  codepoints is a **contract revision**: bump the status line and record the
  change here (the `wasm-import-contract.md` discipline). §2 taste values
  may be tuned by the same revision — chrome drawn outside the revision is
  off-contract drift.

## Revision history

- **v1** (2026-09-27, M86a #1817): initial contract — buffer model,
  widened `widgets.Canvas`, AA guarantees, chrome design language, twenty
  glyphs.
