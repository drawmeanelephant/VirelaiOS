#!/usr/bin/env python3
"""adopt.py -- turn an f-artcraft drop into staged VirelaiOS rasters.

M203 (#3338) conveyor: f-artcraft delivers `<slug>-V1R00@2x.png` masters
(2x of ship size) under `art/virelai/<slug>/`; this tool writes the
checked-in QOI rasters under `image/art/<family>/<SHARE>.qoi` and the
provenance manifest `image/art/ART.TXT`. Share names are 8.3 uppercase on
the host share (frozen M69d #1531 D1); the repo-side file is the same
name lowercased, so the staging seam in tools/session.sh and
tools/lib/gate-run.sh copies `image/art/*/*.qoi` to `SHARE/ART/` with no
renaming beyond the case fold.

Usage:
    python3 tools/art/adopt.py <png-dir>            # write image/art/
    python3 tools/art/adopt.py <png-dir> --check    # verify, write nothing

<png-dir> holds the f-artcraft @2x.png masters, one per slug (the file
name is the slug). Fetch them with:

    gh api repos/drawmeanelephant/f-artcraft/contents/art/virelai/<slug>/<slug>-V1R00@2x.png -q .content | base64 -d > <slug>.png

The script is deterministic: same inputs -> same bytes -> same ART.TXT
shas. A row that disagrees with what's on disk fails --check.
"""

import hashlib
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
from png2qoi import encode_qoi  # noqa: E402

from PIL import Image  # noqa: E402

# slug -> (family, 8.3 share name, ship WxH, f-artcraft issue)
TABLE = [
    # #1249 cursor set (master 32x48, ships as kernel masks at 16x24;
    # hotspots in image/art/cursors/HOTSPOTS.TXT).
    ("cursor-arrow",       "cursors",   "CURS_PTR",  (32, 48),  1249),
    ("cursor-ibeam",       "cursors",   "CURS_TXT",  (32, 48),  1249),
    ("cursor-hand",        "cursors",   "CURS_LNK",  (32, 48),  1249),
    ("cursor-grab",        "cursors",   "CURS_GRB",  (32, 48),  1249),
    ("cursor-grabbing",    "cursors",   "CURS_GRG",  (32, 48),  1249),
    ("cursor-resize-x",    "cursors",   "CURS_RSX",  (32, 48),  1249),
    ("cursor-resize-y",    "cursors",   "CURS_RSY",  (32, 48),  1249),
    ("cursor-crosshair",   "cursors",   "CURS_XHR",  (32, 48),  1249),
    ("cursor-busy",        "cursors",   "CURS_BSY",  (32, 48),  1249),
    ("cursor-not-allowed", "cursors",   "CURS_NO",   (32, 48),  1249),
    # #1250 sexiburger pose set (master 64x64, must read at 24x24).
    ("mascot-wave",        "mascot",    "MSC_WAVE",  (64, 64),  1250),
    ("mascot-sleep",       "mascot",    "MSC_SLEP",  (64, 64),  1250),
    ("mascot-think",       "mascot",    "MSC_THNK",  (64, 64),  1250),
    ("mascot-celebrate",   "mascot",    "MSC_CELB",  (64, 64),  1250),
    ("mascot-oops",        "mascot",    "MSC_OOPS",  (64, 64),  1250),
    ("mascot-locked",      "mascot",    "MSC_LOCK",  (64, 64),  1250),
    # #1253 empty-state spots (master 160x120, must read at 80x60).
    ("spot-empty-folder",      "spots", "SPT_FOLD",  (160, 120), 1253),
    ("spot-no-results",        "spots", "SPT_NRES",  (160, 120), 1253),
    ("spot-offline",           "spots", "SPT_OFFL",  (160, 120), 1253),
    ("spot-error",             "spots", "SPT_ERR",   (160, 120), 1253),
    ("spot-no-notifications",  "spots", "SPT_BELL",  (160, 120), 1253),
    ("spot-blank-document",    "spots", "SPT_DOC",   (160, 120), 1253),
    ("spot-no-image",          "spots", "SPT_IMG",   (160, 120), 1253),
    ("spot-trash-empty",       "spots", "SPT_TRSH",  (160, 120), 1253),
    # #1254 brand plate (master 800x450 navy field + 256x256 emblem).
    ("brand-boot-plate",   "brand",     "BRD_PLT",   (800, 450), 1254),
    ("brand-about-emblem", "brand",     "BRD_EMB",   (256, 256), 1254),
    # #1255 spinner contact sheets (frames at 32x32, uniform 1-row grid).
    ("spinner-arc",        "spinner",   "SPN_ARC",   (384, 32),  1255),
    ("spinner-mascot",     "spinner",   "SPN_MASC",  (256, 32),  1255),
    # #1257 filetype family (master 32x32, primary read 16-20px).
    ("filetype-folder",      "filetypes", "FT_FOLD", (32, 32),   1257),
    ("filetype-folder-open", "filetypes", "FT_FOPE", (32, 32),   1257),
    ("filetype-text",        "filetypes", "FT_TEXT", (32, 32),   1257),
    ("filetype-image",       "filetypes", "FT_IMG",  (32, 32),   1257),
    ("filetype-audio",       "filetypes", "FT_AUD",  (32, 32),   1257),
    ("filetype-document",    "filetypes", "FT_DOC",  (32, 32),   1257),
    ("filetype-archive",     "filetypes", "FT_ARC",  (32, 32),   1257),
    ("filetype-binary",      "filetypes", "FT_BIN",  (32, 32),   1257),
    ("filetype-contact",     "filetypes", "FT_VCF",  (32, 32),   1257),
    ("filetype-calendar",    "filetypes", "FT_ICS",  (32, 32),   1257),
]

MANIFEST = ROOT / "image/art/ART.TXT"


def sha256(b: bytes) -> str:
    return hashlib.sha256(b).hexdigest()


def convert(src: Path, size):
    img = Image.open(src).convert("RGBA")
    if img.size != (size[0] * 2, size[1] * 2):
        raise SystemExit(
            f"adopt: {src.name} is {img.size[0]}x{img.size[1]}, "
            f"expected {size[0]*2}x{size[1]*2} (@2x of {size[0]}x{size[1]})"
        )
    img = img.resize(size, Image.LANCZOS)
    return encode_qoi(img)


def main():
    if len(sys.argv) not in (2, 3):
        sys.exit(f"usage: {sys.argv[0]} <png-dir> [--check]")
    srcdir = Path(sys.argv[1])
    check = len(sys.argv) == 3 and sys.argv[2] == "--check"
    if not srcdir.is_dir():
        sys.exit(f"adopt: {srcdir} is not a directory")

    fails, rows = [], []
    for slug, family, name, size, issue in TABLE:
        src = srcdir / f"{slug}.png"
        if not src.exists():
            fails.append(f"missing source {slug}.png")
            continue
        src_bytes = src.read_bytes()
        out = convert(src, size)
        rel = f"image/art/{family}/{name.lower()}.qoi"
        dst = ROOT / rel
        if check:
            if not dst.exists():
                fails.append(f"{rel} not generated")
            elif dst.read_bytes() != out:
                fails.append(f"{rel} differs from a fresh conversion")
        else:
            dst.parent.mkdir(parents=True, exist_ok=True)
            dst.write_bytes(out)
        rows.append(
            (name, f"{family}/{name.lower()}.qoi", slug,
             f"{size[0]}x{size[1]}", issue, sha256(src_bytes), sha256(out))
        )

    if fails:
        for f in fails:
            print(f"adopt: {f}", file=sys.stderr)
        sys.exit(1)
    if check:
        print(f"adopt: --check ok, {len(rows)} assets reproducible")
        return

    lines = [
        "# VirelaiOS staged art manifest (M203 #3338 conveyor).",
        "# Source: drawmeanelephant/f-artcraft art/virelai/<slug>/<slug>-V1R00@2x.png",
        "# Generated by tools/art/adopt.py -- do not hand-edit rows.",
        "#",
        "# SHARE    REPO FILE                       SOURCE SLUG            SHIP SIZE  ISSUE  SRC SHA256                                                       QOI SHA256",
    ]
    for name, rel, slug, size, issue, ssrc, sout in rows:
        lines.append(
            f"{name:<9} {rel:<30} {slug:<22} {size:<10} #{issue:<5} {ssrc} {sout}"
        )
    MANIFEST.parent.mkdir(parents=True, exist_ok=True)
    MANIFEST.write_text("\n".join(lines) + "\n")
    print(f"adopt: wrote {len(rows)} assets + {MANIFEST.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
