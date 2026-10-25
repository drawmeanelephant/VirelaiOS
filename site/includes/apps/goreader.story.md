The document reader — the `opens=document` handler: PDFs off
the share rendered through the bounded `user/go/pdf` engine
(ADR 0040, read-only), plus the product surface the engine never
had — page navigation, zoom, the shared find overlay, save_state.
The engine renders one 96 dpi page bitmap per call and re-checks
the whole document each time, so the app holds only the visible
page — that is the cache rule, not a shortcut. `go-reader.spec`
pages, zooms and finds through it; the tile is
[ICONREAD](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconread.qoi).
