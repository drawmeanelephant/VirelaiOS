The browser — Go chrome, navigation, history and fetch over the
project's own `webrender` engine: HTML parse, a bounded CSS
cascade, box layout, native paint. It is a deliberately
Dillo-tier subset — HTTP/1.1 keep-alive with chunked and gzip
bodies, no JavaScript and no POST (ADR 0028's permanent refusal)
— with a real product surface on top: history dropdown,
bookmarks, find-in-page, per-tab zoom and cancellable downloads
(PDFs dispatch to GOREADER rather than navigating). `live-web.spec`
pins the rendering byte-exact: frozen reference pages'
marker-geometry crops SHA-256-match their approved goldens. The
tile is [ICONWEB](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconweb.qoi); the
[front-page shot](https://github.com/drawmeanelephant/VirelaiOS/blob/main/site/index.assets/web.png) shows a real
in-guest render.
