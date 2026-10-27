# VirelaiOS style bible (seed)

The taste rules that keep the virelai family coherent. Ground truth is the
shipped system — [VirelaiOS](https://github.com/drawmeanelephant/VirelaiOS):
`docs/ui-primitives.md` (chrome contract v2), `user/go/theme/theme.go`
(token hexes — the values below are copied, not eyeballed), `site/desktop.md`,
`image/apps.txt`. The two art fixtures were sampled directly:
`image/WALLPAPER.QOI` (640×360) and `user/src/lib/fixtures/qoi/mascot_64x64.qoi`
(64×64). Mirrors for scripts live in `work/virelai/vkit.py`.

## Two visual registers

1. **Chrome register** — the OS's own widgets: flat face + 1 px `Border`, no
   gradients on control faces, geometric single-weight **filled** silhouettes
   on a 16 px em, rounded terminals, readable at 12 px. This is the
   VirelaiChrome glyph language (PUA `U+E000`–`U+E01D`).
2. **Asset register** — our art: cel shading, thick warm-dark outlines, the
   mascot's world. App icons sit *above* chrome glyphs (same stroke family,
   fuller character, light cel shading). Filetype icons sit *between*:
   document silhouettes in the chrome stroke family, one accent hue per type.
   Cursors sit *at* chrome level: they must survive a 2-color mask.

## Ink and field

- **Never pure black.** Outlines are warm dark ink — the mascot fixture's
  outline samples at `#634B48`–`#7B5E5A`; the house ink `#503139` is in-family.
- **Navy field:** `#1E1E2E` — WALLPAPER.QOI's dominant color (93% of the
  canvas). The seat desktop gradient is `#1A1A2E` → `#0A0A14`
  (`WallpaperTop`/`WallpaperBot`); a flat `#1E1E2E` backdrop matches the
  shipped wallpaper exactly.

## Palette — shipped tokens

Dark is the boot default; light is the shipped alternate. Copy hexes from
`theme.go`, never invent a third palette — `theme.Set` refuses unknown names.

| role | dark | light | used for |
|---|---|---|---|
| `Bg` | `#182026` | `#F1F5F9` | window/app background |
| `Surface` | `#222D35` | `#FFFFFF` | chrome plates, title bars |
| `ChromeBg` / `Gutter` | `#11171C` / `#0B0E11` | `#E2E8F0` / `#E5E7EB` | recessed panels, inputs |
| `Border` / `Rule` | `#334155` / `#2E3A44` | `#CBD5E1` | 1 px borders and rules |
| `Text` / `Muted` | `#FFFFFF` / `#94A3B8` | `#0F172A` / `#64748B` | text roles |
| `Ink` / `InkMuted` | `#E6EDF3` / `#8B98A5` | `#0F172A` / `#64748B` | Go chrome extras |
| `Accent` | `#3B82F6` | `#2563EB` | selection, focus ring, caret |
| `OnAccent` | `#FFFFFF` | `#FFFFFF` | text on accent |
| `BtnIdle→Hover→Pressed` | `#2D3748→#4A5568→#1A202C` | `#E2E8F0→#CBD5E1→#94A3B8` | button state ramp |
| `Selection` | `#2A4460` | `#BFDBFE` | selected rows/text |
| `Success`/`Ok` | `#22C55E`/`#3FB950` | `#16A34A` | affirmative |
| `Danger` | `#EF4444` | `#DC2626` | errors, close-hover |
| `Warning` | `#F59E0B` | `#D97706` | caution |
| `Shadow` | `#000000` @25% peak | `#0F172A` | one shared floating shadow: dy 4, blur 16 |

## The mascot (sexiburger)

Sampled off the shipped fixtures — the character is fixed, poses are not
redesigns:

- Bun/body `#FDB845` (lit) → `#F5AC3C` (mid); shadow runs to `#DA761E` and
  `#9D4A0D` in the wallpaper's lighting.
- Gloves `#FFFFFF`; white-glove hands and the polka-dot bow tie are available
  whenever a pose wants readable hands.
- Anatomy: sesame bun, lettuce/tomato/cheese layers, ~6 tentacle limbs, big
  cartoon eyes, thick warm-dark outline, cel shading.
- The mark lives at Virelai Sans `U+E001` (do not draw over VirelaiChrome's
  own `U+E001 window-minimize` — separate font, separate cmap).

## Geometry and strokes

- Spacing scale `2 / 4 / 8 / 16` (`PadXS…PadLG`); composed values sum adjacent
  steps (`12 = 8+4`).
- Corner radii: controls `4`, windows/dialogs `8`, pills `h/2`. Radius clamps
  to `min(w,h)/2`.
- `BorderW = 1`, `FocusW = 2` (focus ring inside the bounds). Title bar 28;
  list rows 24.
- Asset strokes: ≥3 px at a 64 px master (thin-geometry rule); rounded caps
  and joins everywhere — the chrome glyph rule is "rounded terminals".
- Depth: floating plates only cast the shared shadow (dy 4, blur 16, 25%
  ink). Control faces never gradient; the one sanctioned gradient is the
  seat desktop itself. House rule still applies to our *art*: gradients and
  translucent shading inside a piece, never a flat fill pretending to be art.

## Type

- UI text: Inter, chrome nominal 13 (sizes in play: 11/13/15/17).
- Code/terminal: Fira Code 13.
- Display/wordmark: Virelai Sans — **no digit glyphs**, so any digits
  (version-free anyway per #1254) fall back to Source Sans 3.
- `add_text` takes no `opacity`/`style` kwargs — style the object after
  creation (`object.setProps`, `text.setStyle`, `object.rotate`).

## Surfaces

- Display target 1280×720 (virtio-gpu); art must read at 2× for retina.
- Seat chrome obscures the top ~80 px and a left/bottom rail region —
  wallpapers stay center-weighted, nothing important in corners.
- Assets paint on dark chrome AND light surfaces: transparent backgrounds,
  and prove both reads in the proof.

## Motif rules

- Register is **macabre-cute**: a guillotine is the wallpaper. Charming, not
  alarming — the error spot apologizes, it doesn't frighten.
- Spots are one subject, no scenes; they sit behind text.
- Wallpapers are a stage with one actor — mascot cameos stay small.
- No photorealism, no pure black, no OS-version text or trademarks on brand
  pieces — the plate must age well.
- Animation loops must be seamless: frame N leads into frame 0 with no pop.
