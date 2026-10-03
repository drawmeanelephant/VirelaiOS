# M87a — Human desktop launcher acceptance (superseded by M91)

> **Superseded (2026-10-03).** The owner closed [#1857](https://github.com/drawmeanelephant/VirelaiOS/issues/1857) as superseded by M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914), and closed M87's index [#1856](https://github.com/drawmeanelephant/VirelaiOS/issues/1856) with a pointer to #1914. The physical journey is now #1914's human milestone acceptance, run once after M91a–d land. This file keeps #1857's fixture procedure as the reference for that session. Do not file it.

## Coverage check

#1914's milestone acceptance already lists every #1857 step: Apps click with readable rows, pointer-only Terminal launch, filter/Escape/outside dismissal without old-app mutation, header/padding no-launch, an honest missing-binary state, a guest-written `/host` receipt with independent byte comparison, and real guest scanouts. It adds launch/close/relaunch, ordinary input and cursor behavior, flash-free interaction, a representative editor and receipt survival across relaunch. #1857's one note without another home, the baseline `go-dogfood` wide-rune snapshot failure, moves to M91d's read-only `go-dogfood` check.

## Reference fixture procedure (from #1857)

```bash
source tools/env-check.sh
set -o pipefail
ROOT="$(git rev-parse --show-toplevel)"
fixture="$(mktemp -d "$ROOT/artifacts/m91-desktop-acceptance.XXXXXX")"
mkdir -p "$fixture/share"
VIRELAI_SESSION_SHARE="$fixture/share" bash tools/session.sh 2>&1 | tee "$fixture/host.log"
```

No build-skip, alternate-seat flag, personal share, security/permission change or unrelated host capture. With the initial GOSH tab open, the human clicks Apps, reads the named rows and clicks Terminal by pointer alone. In the **guest Terminal tab**, write and read a uniquely named receipt under `/host`. After stopping only that session, compare its bytes independently on the host, then relaunch and compare again. Copy the session's serial log into the fixture evidence.

A real guest scanout, fixture-only before/after files, the byte comparisons, logs, revision and host capability, and the human's observed step results form the evidence. No automation, injected input, localhost result or hidden-window screenshot passes.

## Ownership and closure

- No card declares `docs/status.md`. After the human session, the owner lands one small status PR that rewrites the M87 row and adds or edits the M91 row (precedent #1913), then closes #1914 by hand with one evidence comment.
- A failed step returns to its owning M91 card with evidence; acceptance stays unpassed until that card's fix lands and is reverified.

## Sources read

#1857, “Remaining acceptance work,” all five human steps and verification/completion; #1914, milestone acceptance; #1856, both original journeys; `docs/status.md:144`. No human acceptance was performed.
