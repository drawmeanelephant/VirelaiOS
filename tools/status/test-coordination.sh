#!/usr/bin/env bash
#
# test-coordination.sh -- test suite for the GitHub-issue coordination gate
# (tools/status/verify-issue-coordination.sh, claim filing via
# tools/status/new-claim.sh) and the claim-staleness machinery. Claims live
# on the GitHub tracker as issues labeled `claim`; this suite exercises the
# gate's parsing/overlap/staleness logic, the weekly sweep's label decisions,
# and the real-time unlabel guard (tools/status/unlabel-guard.sh) OFFLINE via
# fixtures, so it needs no network and no gh.
#
# Two fixture shapes are used:
#   * gh issue list --json number,title,url,body,updatedAt,labels — for the
#     gate and the weekly sweep (issue-list records).
#   * GitHub webhook payloads in the shape of the issue_comment / issues
#     events — for the unlabel-fresh guard (webhook_event() below).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$ROOT/tools/status/verify-issue-coordination.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
failn=0

# run_case NAME EXPECT_RC -- runs the gate on a fixture; EXPECT_RC=0 pass, 1 fail
run_case() {
    local name="$1" expect="$2" rc=0 out=""
    out="$(bash "$GATE" --issues-json "$TMP/fixture.json" 2>&1)" || rc=$?
    if [ "$rc" -eq "$expect" ]; then
        pass=$((pass + 1))
        printf 'ok   %s\n' "$name"
    else
        failn=$((failn + 1))
        printf 'FAIL %s (rc=%d, expected %d)\n' "$name" "$rc" "$expect" >&2
        printf '%s\n' "$out" | sed 's/^/     /' >&2
    fi
}

# claim_json NUMBER BRANCH UPDATED_AT TOUCHES [STATUS [LABELS]]
claim_json() {
    local num="$1" branch="$2" upd="$3" touches="$4" status="${5:-}" labels="${6:-}"
    local status_line="" labels_json="[]"
    [ -z "$status" ] || status_line="- **Status:** $status"
    if [ -n "$labels" ]; then
        labels_json="$(printf '%s\n' "$labels" | tr ',' '\n' \
            | jq -R 'select(length > 0) | {name: .}' | jq -s '.')"
    fi
    jq -n \
        --argjson n "$num" \
        --arg title "claim fixture $num" \
        --arg url "https://github.com/x/y/issues/$num" \
        --arg upd "$upd" \
        --argjson labels "$labels_json" \
        --arg body "$(printf '%s\n' \
            "## Claim" \
            "" \
            "- **Owner:** agent (\`$branch\`)" \
            "- **Touches:** $touches" \
            "$status_line" \
            "" \
            "- **Notes:** fixture")" \
        '{number: $n, title: $title, url: $url, updatedAt: $upd, labels: $labels, body: $body}'
}

# --- case 1: disjoint touches, no overlap -> PASS ---------------------------

jq -s '.' \
    <(claim_json 101 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/a.zig, user/src/b.zig") \
    <(claim_json 102 "agent/b/two" "2026-09-02T00:00:00Z" "kernel/src/c.zig, tools/x.sh") \
    > "$TMP/fixture.json"
run_case "disjoint claims pass" 0

# --- case 2: overlapping touch, different branches -> FAIL ------------------

jq -s '.' \
    <(claim_json 201 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig, user/src/a.zig") \
    <(claim_json 202 "agent/b/two" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig") \
    > "$TMP/fixture.json"
out="$(bash "$GATE" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -qE "claims #20[12] .* and #20[12] .* both declare 'kernel/src/monitor.zig'"; then
    pass=$((pass + 1)); printf 'ok   overlap across branches fails (names both issues)\n'
else
    failn=$((failn + 1)); printf 'FAIL overlap message names both issues: %s\n' "$out" >&2
fi

# --- case 3: overlap but one claim is blocked (⛔) -> PASS -------------------

jq -s '.' \
    <(claim_json 301 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig") \
    <(claim_json 302 "agent/b/two" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig" "⛔ blocked") \
    > "$TMP/fixture.json"
run_case "blocked claim excluded from overlap" 0

# --- case 4: overlapping touch, SAME branch -> PASS -------------------------

jq -s '.' \
    <(claim_json 401 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig") \
    <(claim_json 402 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig") \
    > "$TMP/fixture.json"
run_case "same-branch overlap allowed (one editor)" 0

# --- case 5: prefix-glob overlap -> FAIL ------------------------------------

jq -s '.' \
    <(claim_json 501 "agent/a/one" "2026-09-02T00:00:00Z" "kernel/src/*.zig") \
    <(claim_json 502 "agent/b/two" "2026-09-02T00:00:00Z" "kernel/src/monitor.zig") \
    > "$TMP/fixture.json"
run_case "prefix-glob overlap fails" 1

# --- case 5b: generated artifact overlap across branches -> PASS ------------
# The generated inventory is re-rendered mechanically; two claims declaring
# it must NOT false-conflict (the recurring docs/gate-fleet-inventory.md bug).

jq -s '.' \
    <(claim_json 511 "agent/a/one" "2026-09-02T00:00:00Z" "docs/gate-fleet-inventory.md, kernel/src/a.zig") \
    <(claim_json 512 "agent/b/two" "2026-09-02T00:00:00Z" "docs/gate-fleet-inventory.md, user/src/b.zig") \
    > "$TMP/fixture.json"
run_case "generated inventory overlap across branches is exempt" 0

# --- case 5b2: the generated registry aggregate is exempt too -> PASS -------
# docs/component-registry.md compiles from docs/registry/*.md (issue
# #3676); claims declare the fragment, so the aggregate must not conflict.

jq -s '.' \
    <(claim_json 513 "agent/a/one" "2026-09-02T00:00:00Z" "docs/component-registry.md, docs/registry/a.md") \
    <(claim_json 514 "agent/b/two" "2026-09-02T00:00:00Z" "docs/component-registry.md, docs/registry/b.md") \
    > "$TMP/fixture.json"
run_case "generated registry overlap across branches is exempt" 0

# --- case 5c: the exemption does not mask a real overlap -> FAIL ------------

jq -s '.' \
    <(claim_json 521 "agent/a/one" "2026-09-02T00:00:00Z" "docs/gate-fleet-inventory.md, kernel/src/monitor.zig") \
    <(claim_json 522 "agent/b/two" "2026-09-02T00:00:00Z" "docs/gate-fleet-inventory.md, kernel/src/monitor.zig") \
    > "$TMP/fixture.json"
rc=0
out="$(bash "$GATE" --issues-json "$TMP/fixture.json" 2>&1)" || rc=$?
if [ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q "both declare 'kernel/src/monitor.zig'"; then
    pass=$((pass + 1)); printf 'ok   generated exemption does not mask a real overlap\n'
else
    failn=$((failn + 1)); printf 'FAIL generated exemption masked a real overlap (rc=%d): %s\n' "$rc" "$out" >&2
fi

# --- case 6: missing Owner branch -> FAIL -----------------------------------

printf '%s' '[
  {"number": 601, "title": "bad owner", "url": "https://github.com/x/y/issues/601",
   "updatedAt": "2026-09-02T00:00:00Z",
   "body": "## Claim\n\n- **Owner:** buffy\n- **Touches:** kernel/src/a.zig\n"}
]' > "$TMP/fixture.json"
run_case "missing backticked branch fails" 1

# --- case 7: stale claim (no update 20 days) -> warning, still PASS ---------

stale="$(date -u -v-20d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '20 days ago' +%Y-%m-%dT%H:%M:%SZ)"
jq -s '.' \
    <(claim_json 701 "agent/a/one" "$stale" "kernel/src/a.zig") \
    > "$TMP/fixture.json"
out="$(bash "$GATE" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -q "warn: claim #701"; then
    pass=$((pass + 1)); printf 'ok   stale claim warns\n'
else
    failn=$((failn + 1)); printf 'FAIL stale warning not emitted: %s\n' "$out" >&2
fi

# --- case 8: empty tracker (no open claims) -> PASS -------------------------

printf '[]' > "$TMP/fixture.json"
run_case "empty tracker passes" 0

# --- staleness sweep (tools/status/sweep-stale-claims.sh) -------------------

SWEEP="$ROOT/tools/status/sweep-stale-claims.sh"

# case 9: fresh claims -> nothing flagged

jq -s '.' \
    <(claim_json 901 "agent/a/one" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "kernel/src/a.zig") \
    > "$TMP/fixture.json"
out="$(bash "$SWEEP" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -qE '0 newly stale'; then
    pass=$((pass + 1)); printf 'ok   sweep: fresh claims not flagged\n'
else
    failn=$((failn + 1)); printf 'FAIL sweep flagged a fresh claim: %s\n' "$out" >&2
fi

# case 10: stale claim (no update 20 days) -> flagged with the claim number

stale="$(date -u -v-20d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '20 days ago' +%Y-%m-%dT%H:%M:%SZ)"
jq -s '.' \
    <(claim_json 902 "agent/a/one" "$stale" "kernel/src/a.zig") \
    <(claim_json 903 "agent/b/two" "$stale" "kernel/src/b.zig") \
    > "$TMP/fixture.json"
out="$(bash "$SWEEP" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -q '2 newly stale' \
    && printf '%s' "$out" | grep -q 'claim #902' \
    && printf '%s' "$out" | grep -q 'claim #903'; then
    pass=$((pass + 1)); printf 'ok   sweep: stale claims flagged (names both)\n'
else
    failn=$((failn + 1)); printf 'FAIL sweep missed a stale claim: %s\n' "$out" >&2
fi

# case 11: stale claim already carrying claim:stale -> not re-flagged

jq -s '.' \
    <(claim_json 911 "agent/a/one" "$stale" "kernel/src/a.zig" "" "claim:stale") \
    > "$TMP/fixture.json"
out="$(bash "$SWEEP" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -q 'skip claim #911' \
    && printf '%s' "$out" | grep -q '1 still flagged'; then
    pass=$((pass + 1)); printf 'ok   sweep: already-flagged stale claim skipped\n'
else
    failn=$((failn + 1)); printf 'FAIL sweep re-flagged a labeled claim: %s\n' "$out" >&2
fi

# case 12: fresh claim carrying claim:stale -> label removed

jq -s '.' \
    <(claim_json 912 "agent/a/one" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "kernel/src/a.zig" "" "claim:stale") \
    > "$TMP/fixture.json"
out="$(bash "$SWEEP" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -q 'unlabel claim #912' \
    && printf '%s' "$out" | grep -q '1 unlabeled'; then
    pass=$((pass + 1)); printf 'ok   sweep: updated claim unlabeled\n'
else
    failn=$((failn + 1)); printf 'FAIL sweep kept the stale label on an updated claim: %s\n' "$out" >&2
fi

# case 13: fresh claim with no label -> nothing happens

jq -s '.' \
    <(claim_json 913 "agent/a/one" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "kernel/src/a.zig") \
    > "$TMP/fixture.json"
out="$(bash "$SWEEP" --issues-json "$TMP/fixture.json" 2>&1 || true)"
if printf '%s' "$out" | grep -qE '0 newly stale.*0 unlabeled.*0 still flagged'; then
    pass=$((pass + 1)); printf 'ok   sweep: fresh unlabeled claim untouched\n'
else
    failn=$((failn + 1)); printf 'FAIL sweep acted on a fresh unlabeled claim: %s\n' "$out" >&2
fi

# --- real-time unlabel guard (tools/status/unlabel-guard.sh) ----------------
#
# The unlabel-fresh job in .github/workflows/claim-staleness.yml runs this
# guard on every issue_comment / issues event. These cases feed it webhook
# payloads (issue_comment = .issue + .comment.user + .comment.body; issues =
# .issue + .sender) and assert the bot-vs-human decision, so the real-time
# path is proven offline before it can fire in production.

GUARD="$ROOT/tools/status/unlabel-guard.sh"

# webhook_event EVENT NUM STATE LABELED LOGIN TYPE [BODY] -- a webhook
# payload in the issue_comment / issues shape. LABELED=1 gives the issue the
# claim + claim:stale labels; for issues events the comment key is omitted.
webhook_event() {
    local event="$1" num="$2" state="$3" labeled="$4" login="$5" type="$6" body="${7:-}"
    jq -n \
        --arg event "$event" \
        --argjson num "$num" \
        --arg state "$state" \
        --arg labeled "$labeled" \
        --arg login "$login" \
        --arg type "$type" \
        --arg body "$body" \
        '{ issue: { number: $num, state: $state,
                    labels: (if $labeled == "1" then
                                 [{name: "claim"}, {name: "claim:stale"}]
                             else [] end) } }
         | if $event == "issue_comment" then
               .comment = { user: { login: $login, type: $type }, body: $body }
           else . end
         | .sender = { login: $login, type: $type }'
}

# guard_case NAME EVENT PAYLOAD_FILE EXPECTED_SUBSTRING
#   runs the guard offline and asserts the decision text.
guard_case() {
    local name="$1" event="$2" payload="$3" want="$4" out=""
    out="$(bash "$GUARD" --payload "$payload" --event "$event" 2>&1 || true)"
    if printf '%s' "$out" | grep -qF "$want"; then
        pass=$((pass + 1)); printf 'ok   guard: %s\n' "$name"
    else
        failn=$((failn + 1)); printf 'FAIL guard: %s — wanted %q, got: %s\n' "$name" "$want" "$out" >&2
    fi
}

# case 14: human comment on an open labeled claim -> unlabel

webhook_event issue_comment 1401 open 1 alice User "progress update on the fix" > "$TMP/payload.json"
guard_case "human comment unlabels a stale claim" issue_comment "$TMP/payload.json" \
    "unlabel: human activity on stale-flagged claim #1401"

# case 15: the sweep's own warning (github-actions[bot]) -> skip

webhook_event issue_comment 1501 open 1 github-actions[bot] Bot "**Automated staleness check** — post a progress update" > "$TMP/payload.json"
guard_case "sweep warning (bot author) does not unlabel" issue_comment "$TMP/payload.json" \
    "skip: comment on #1501 is from bot 'github-actions[bot]'"

# case 16: sweep warning run under a LOCAL token (human author + marker) -> skip

webhook_event issue_comment 1601 open 1 alice User "**Automated staleness check** — post a progress update. <!-- claim-staleness-bot -->" > "$TMP/payload.json"
guard_case "local-token sweep warning (marker) does not unlabel" issue_comment "$TMP/payload.json" \
    "skip: comment on #1601 is the sweep's own warning (marker present)"

# case 17: some other bot's comment (type Bot, no marker) -> skip

webhook_event issue_comment 1701 open 1 dependabot[bot] Bot "bump dependencies" > "$TMP/payload.json"
guard_case "third-party bot comment does not unlabel" issue_comment "$TMP/payload.json" \
    "skip: comment on #1701 is from bot 'dependabot[bot]'"

# case 18: issues event (edit) from a human -> unlabel

webhook_event issues 1801 open 1 alice User "" > "$TMP/payload.json"
guard_case "human issues edit unlabels a stale claim" issues "$TMP/payload.json" \
    "unlabel: human activity on stale-flagged claim #1801"

# case 19: issues event driven by github-actions[bot] -> skip

webhook_event issues 1901 open 1 github-actions[bot] Bot "" > "$TMP/payload.json"
guard_case "bot-driven issues event does not unlabel" issues "$TMP/payload.json" \
    "skip: bot-driven issue event on #1901"

# case 20: issues event driven by another bot (type Bot) -> skip

webhook_event issues 2001 open 1 renovate[bot] Bot "" > "$TMP/payload.json"
guard_case "third-party bot issues event does not unlabel" issues "$TMP/payload.json" \
    "skip: bot-driven issue event on #2001"

# case 21: human comment on an open issue NOT carrying claim:stale -> skip

webhook_event issue_comment 2101 open 0 alice User "hello" > "$TMP/payload.json"
guard_case "unlabeled claim left alone" issue_comment "$TMP/payload.json" \
    "skip: issue #2101 does not carry the 'claim:stale' label"

# case 22: human comment on a CLOSED labeled issue -> skip

webhook_event issue_comment 2201 closed 1 alice User "wrapping this up" > "$TMP/payload.json"
guard_case "closed claim left alone" issue_comment "$TMP/payload.json" \
    "skip: issue #2201 is 'closed', not open"

# --- interface-review lane (tools/status/interface-review.py) --------------
#
# The lane rotates over the audit docs (docs/audits-style *.md fixtures),
# reads last-reviewed stamps from state-tracker comments, skips apps blocked
# by an open derived card or an active claim's Touches, and files one card
# or posts a no-drift stamp on the tracker. These cases run it fully
# offline: fixture audit files, tracker-comment JSON, a minimal .figcraft
# zip built here, and --dry-run so nothing posts.

LANE="$ROOT/tools/status/interface-review.py"

# audit_md NUM BIN APPNAME -- one docs/audits-style file into
# $TMP/lane-audits/<slug>.md: the iface-review header (app=, issue=), the
# App-audit H1, and a body naming a source path the way real audits do.
audit_md() {
    local num="$1" bin="$2" name="$3"
    local slug; slug="$(printf '%s' "$bin" | cut -d. -f1 | tr 'A-Z' 'a-z')"
    mkdir -p "$TMP/lane-audits"
    printf '%s\n' \
        "<!-- iface-review:v1 app=$bin issue=$num -->" \
        "# App audit: $name ($bin)" \
        "" \
        "Observed (code, \`user/go/$(printf '%s' "$name" | tr 'A-Z' 'a-z')/\`)." \
        > "$TMP/lane-audits/$slug.md"
}

# mk_figcraft PATH BIN... -- a minimal figcraft zip: a page + one empty
# `apps/<BIN>` frame per argument after the first.
mk_figcraft() {
    python3 - "$@" <<'PY'
import json, sys, zipfile
out, bins = sys.argv[1], sys.argv[2:]
nodes = {"1": {"id": 1, "name": "apps", "type": "page",
               "children": list(range(2, len(bins) + 2))}}
for i, b in enumerate(bins):
    nodes[str(i + 2)] = {"id": i + 2, "name": f"apps/{b}", "type": "frame",
                         "width": 100.0, "height": 128.0, "children": []}
with zipfile.ZipFile(out, "w") as z:
    z.writestr("document.json", json.dumps({"nodes": nodes}))
PY
}

# lane_run EXPECTED_SUBSTR [LANE_ARGS...] -- run the lane offline and assert
# one output line; claims + state fixtures are empty unless a case stages
# its own ($TMP/lane-claims.json / $TMP/lane-state.json).
lane_run() {
    local want="$1"; shift
    [ -f "$TMP/lane-claims.json" ] || printf '[]' > "$TMP/lane-claims.json"
    [ -f "$TMP/lane-state.json" ] || printf '[]' > "$TMP/lane-state.json"
    local out
    out="$(python3 "$LANE" --dry-run \
        --audits-dir "$TMP/lane-audits" \
        --state-json "$TMP/lane-state.json" \
        --claims-json "$TMP/lane-claims.json" "$@" 2>&1 || true)"
    if printf '%s' "$out" | grep -qF "$want"; then
        pass=$((pass + 1)); printf 'ok   lane: %s\n' "$want"
    else
        failn=$((failn + 1))
        printf 'FAIL lane: wanted %q, got:\n%s\n' "$want" "$out" >&2
    fi
}

mk_figcraft "$TMP/apps.figcraft" FAKEA.ELF FAKEB.ELF FAKEC.ELF

# case 23: rotation picks the first never-reviewed audit (name order)

audit_md 9301 FAKEA.ELF Fakea
audit_md 9302 FAKEB.ELF Fakeb
printf '[]' > "$TMP/lane-open.json"
lane_run "review FAKEA.ELF" \
    --open-json "$TMP/lane-open.json" --figcraft "$TMP/apps.figcraft"

# case 24: a never-reviewed audit outranks a reviewed one (tracker stamp)

jq -n '[{author: {login: "lane"}, createdAt: "2026-10-27T00:00:00Z",
         body: "<!-- iface-review:v1 app=FAKEA.ELF last=2026-10-26T00:00:00Z -->"}]' \
    > "$TMP/lane-state.json"
lane_run "review FAKEB.ELF" \
    --open-json "$TMP/lane-open.json" --figcraft "$TMP/apps.figcraft"
printf '[]' > "$TMP/lane-state.json"

# case 25: an open derived card (binary named in title) skips the app;
# a second card citing the audit doc path counts too

jq -n '[{number: 9401, title: "FAKEA.ELF: drift fix", body: "", labels: []},
        {number: 9403, title: "fix the fakea face",
          body: "From app audit `docs/audits/fakea.md`.", labels: []}]' \
    > "$TMP/lane-open.json"
lane_run "skip FAKEA.ELF (docs/audits/fakea.md): open derived card(s) #9401" \
    --open-json "$TMP/lane-open.json" --figcraft "$TMP/apps.figcraft"

# case 26: an active claim touching the app's files skips it

jq -s '.' \
    <(claim_json 9501 "agent/other/lane" "2026-10-26T00:00:00Z" "user/go/fakea") \
    > "$TMP/lane-claims-full.json"
printf '[]' > "$TMP/lane-open.json"
out="$(python3 "$LANE" --dry-run \
    --audits-dir "$TMP/lane-audits" \
    --state-json "$TMP/lane-state.json" \
    --claims-json "$TMP/lane-claims-full.json" \
    --open-json "$TMP/lane-open.json" \
    --figcraft "$TMP/apps.figcraft" 2>&1 || true)"
if printf '%s' "$out" | grep -qF "skip FAKEA.ELF (docs/audits/fakea.md): active claim(s) #9501" \
    && printf '%s' "$out" | grep -qF "review FAKEB.ELF"; then
    pass=$((pass + 1)); printf 'ok   lane: claim-touched app skipped, next reviewed\n'
else
    failn=$((failn + 1))
    printf 'FAIL lane: claim-touches skip/review — got:\n%s\n' "$out" >&2
fi

# case 27: every app blocked -> nothing filed, clean exit

jq -n '[{number: 9401, title: "FAKEA.ELF: drift fix", body: "", labels: []},
        {number: 9402, title: "FAKEB.ELF: drift fix", body: "", labels: []}]' \
    > "$TMP/lane-open.json"
lane_run "interface-review: no eligible app this pass" \
    --open-json "$TMP/lane-open.json" --figcraft "$TMP/apps.figcraft"

# case 28: --app reviews a specific audit out of rotation order

printf '[]' > "$TMP/lane-open.json"
out="$(python3 "$LANE" --dry-run --app FAKEB.ELF \
    --audits-dir "$TMP/lane-audits" \
    --state-json "$TMP/lane-state.json" \
    --claims-json "$TMP/lane-claims.json" \
    --open-json "$TMP/lane-open.json" \
    --figcraft "$TMP/apps.figcraft" 2>&1 || true)"
if printf '%s' "$out" | grep -qF "review FAKEB.ELF" \
    && printf '%s' "$out" | grep -qF "comment (dry-run) on state tracker"; then
    pass=$((pass + 1)); printf 'ok   lane: --app reviews out of rotation\n'
else
    failn=$((failn + 1))
    printf 'FAIL lane: --app — got:\n%s\n' "$out" >&2
fi

echo
if [ "$failn" -eq 0 ]; then
    echo "test-coordination: $pass/$((pass + failn)) PASS"
    exit 0
else
    echo "test-coordination: FAILED ($failn failing)" >&2
    exit 1
fi
