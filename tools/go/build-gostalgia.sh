#!/usr/bin/env bash
#
# Build the archived Gostalgia pin, without overlays or modfile/cache edits.
# --audit builds every production package in ./... and its import closure.
# Raw runs, failing targets and the sorted, deduplicated breaks.tsv live in
# artifacts/gostalgia-audit/. Runtime assumptions still need a source audit.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
SOURCE="${GOSTALGIA_DIR:-$(dirname "$REPO")/gostalgia}"
PIN="${GOSTALGIA_PIN:-7ef7d23502a85fb891f78fc92f223e2ba8994f46}"
AUDIT=0
case "${1:-}" in
    --audit) AUDIT=1; shift ;;
    -h|--help)
        echo "usage: bash tools/go/build-gostalgia.sh [--audit]"
        exit 0 ;;
esac
[ "$#" -eq 0 ] || { echo "build-gostalgia: unknown argument: $*" >&2; exit 2; }

log() { printf 'build-gostalgia: %s\n' "$*"; }

if [ ! -d "$SOURCE" ] || ! git -C "$SOURCE" rev-parse --git-dir >/dev/null 2>&1; then
    log "missing checkout at $SOURCE"
    exit 1
fi
if ! COMMIT="$(git -C "$SOURCE" rev-parse --verify --end-of-options "$PIN^{commit}" 2>/dev/null)"; then
    log "missing pin: $PIN in $SOURCE"
    exit 1
fi
if [ ! -x "$FORK_DIR/bin/go" ]; then
    log "missing fork toolchain at $FORK_DIR/bin/go"
    log "provision it once with: bash tools/go/apply.sh && just go-toolchain"
    exit 1
fi

export GOROOT="$(cd "$FORK_DIR" && pwd)"
export PATH="$GOROOT/bin:$PATH"
export GOTOOLCHAIN=local CGO_ENABLED=0 GO111MODULE=on GOWORK=off GOFLAGS=
export GOOS=virelai GOARCH=arm64
export LC_ALL=C
# Bound host memory use even when cmd/go recompiles the whole closure.
export GOMAXPROCS="${GOMAXPROCS:-2}"

ROOT="$REPO/artifacts/gostalgia-audit"
STAGE="$REPO/.build/gostalgia-src"
OUT="$REPO/.build/go/GOSTALGIA.ELF"
mkdir -p "$ROOT" "$(dirname "$STAGE")" "$(dirname "$OUT")"
RUN="$(mktemp -d "$ROOT/run.XXXXXX")"
# Preserve prior generated inputs/products rather than deleting them. A failed
# build must not leave an old GOSTALGIA.ELF looking like a successful product.
if [ -e "$STAGE" ]; then mv "$STAGE" "$RUN/prior-src"; fi
if [ -e "$OUT" ]; then mv "$OUT" "$RUN/prior-GOSTALGIA.ELF"; fi
mkdir -p "$STAGE"
git -C "$SOURCE" archive "$COMMIT" | tar -x -C "$STAGE"
log "pin $COMMIT -> $STAGE"
log "raw evidence -> $RUN"

python3 - "$STAGE" "$OUT" "$ROOT" "$RUN" "$COMMIT" "$AUDIT" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

stage, out, root, run = map(Path, sys.argv[1:5])
commit, audit = sys.argv[5], sys.argv[6] == "1"
go = str(Path(os.environ["GOROOT"]) / "bin/go")


def snapshot():
    return {str(p.relative_to(stage)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in stage.rglob("*") if p.is_file()}


original = snapshot()
(run / "inputs.json").write_text(json.dumps({
    "pin": commit, "goroot": os.environ["GOROOT"], "files": original,
}, sort_keys=True, indent=2) + "\n")


def command(args, name, cwd=stage, env=None):
    result = subprocess.run([go, *args], cwd=cwd, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    (run / (name + ".stdout")).write_text(result.stdout)
    (run / (name + ".stderr")).write_text(result.stderr)
    (run / (name + ".exit")).write_text(str(result.returncode) + "\n")
    return result


def objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        yield value
        text = text[end:]


# Download only versions explicitly named by the archived go.sum. Run outside
# the module so even historical go.mod-only entries cannot alter its sums.
allowed = sorted({(fields[0], fields[1].removesuffix("/go.mod"))
                  for line in (stage / "go.sum").read_text().splitlines()
                  if (fields := line.split())})
download = command(["mod", "download", "-json",
                    *[path + "@" + version for path, version in allowed]],
                   "download", cwd=run)
if download.returncode:
    print(download.stderr or download.stdout, end="")
    sys.exit("build-gostalgia: module download failed (see raw evidence)")

# No implicit latest-version queries or downloads during discovery/builds.
# Graph-only module metadata must already be cached; do not fetch their source.
offline = dict(os.environ, GOPROXY="off")
modules = command(["list", "-mod=readonly", "-m", "all"], "modules", env=offline)
build = command(["build", "-mod=mod", "-p=2", "-ldflags", "-s -w",
                 "-o", str(out), "./cmd/gostalgia"], "build", env=offline)
if not audit:
    if snapshot() != original:
        sys.exit("build-gostalgia: staged source changed")
    print(build.stdout + build.stderr, end="")
    if build.returncode:
        sys.exit(build.returncode)
    print(f"build-gostalgia: wrote {out} ({out.stat().st_size} bytes)")
    sys.exit(0)

listing = command(["list", "-mod=mod", "-e", "-deps", "-json", "./..."],
                  "packages", env=offline)
packages = list(objects(listing.stdout))
# Normalize checkout, fork and cache paths to stable import-path locations.
locations = [(p["Dir"], p["ImportPath"]) for p in packages if p.get("Dir")]
locations += [(str(stage), "gostalgia"), (os.environ["GOROOT"] + "/src", "std"),
              (str(root.parent.parent), "<repo>")]
locations.sort(key=lambda item: len(item[0]), reverse=True)
cache = command(["env", "GOMODCACHE"], "modcache", env=offline).stdout.strip()


def normalize(text):
    for directory, name in locations:
        text = text.replace(directory + "/", name + "/")
        if text == directory:
            text = name
    if cache:
        text = text.replace(cache + "/", "")
    return text.replace("\t", " ").replace("\n", " ")


breaks = set()


def diagnostics(text, target):
    owner = target
    for line in text.splitlines():
        if line.startswith("# "):
            owner = line[2:].strip()
            continue
        if not line.strip():
            continue
        match = re.match(r"(.+?):(\d+)(?::(\d+))?: (.*)", line)
        if match:
            filename, number, column, message = match.groups()
            if message == "too many errors":
                # The ordinary linked build has the default diagnostic cap;
                # per-package -e builds supply the actual remaining errors.
                continue
            absolute = os.path.abspath(stage / filename)
            location = normalize(absolute) + ":" + number
            if column:
                location += ":" + column
            breaks.add((owner, location, normalize(message)))
        else:
            breaks.add((owner, "-", normalize(line.strip())))


for package in packages:
    for error in [package.get("Error"), *package.get("DepsErrors", [])]:
        if error:
            # DepsErrors repeat across dependents: the final stack entry is
            # the failing import, not every package blocked by that import.
            owner = (error.get("ImportStack") or [package["ImportPath"]])[-1]
            breaks.add((owner, normalize(error.get("Pos") or "-"),
                        normalize(error["Err"])))

if listing.returncode:
    diagnostics(listing.stderr or "go list failed without diagnostics", "<go-list>")
if modules.returncode:
    diagnostics(modules.stderr or "go list -m failed without diagnostics", "<modules>")
diagnostics(build.stderr, "gostalgia/cmd/gostalgia")

targets = []
for index, package in enumerate(sorted(packages, key=lambda p: p["ImportPath"])):
    target = package["ImportPath"]
    if not package.get("Error") and not package.get("GoFiles") and not package.get("CgoFiles") \
            and (package.get("TestGoFiles") or package.get("XTestGoFiles")):
        # ./... includes test-only directories; go build ./... skips them.
        # They are not a missing production implementation for this GOOS.
        continue
    # -e lists packages with no selected files too. Build those as well to
    # capture named build-constraint refusals, not just type-check errors.
    result = command(["build", "-mod=mod", "-p=2", "-gcflags=all=-e",
                      "-ldflags", "-s -w", "-o", os.devnull, target],
                     f"package-{index:03d}", env=offline)
    targets.append((target, str(result.returncode), f"package-{index:03d}"))
    if result.returncode:
        diagnostics(result.stderr or result.stdout or "build failed without diagnostics", target)

if snapshot() != original:
    breaks.add(("<recipe>", "-", "staged source changed"))

body = "".join("\t".join(row) + "\n" for row in sorted(breaks))
(run / "breaks.tsv").write_text(body)
(root / "breaks.tsv").write_text(body)
(run / "targets.tsv").write_text("".join("\t".join(row) + "\n" for row in targets))
failed = sum(status != "0" for _, status, _ in targets)
print(f"build-gostalgia: audit {len(targets)} production packages ({len(packages)} listed), "
      f"{failed} failing targets, "
      f"{len(breaks)} unique breaks -> {root / 'breaks.tsv'}")
# A successful audit means both discovery and the requested linked build
# succeeded, never that the raw logs happened to contain no recognized lines.
sys.exit(1 if breaks or failed or build.returncode or listing.returncode or modules.returncode else 0)
PY
