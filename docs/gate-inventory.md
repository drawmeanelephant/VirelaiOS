# VirelaiOS verification gate classes

> This file defines the A/B/C/D classification and the evidence policy.
> It is **not** an inventory: since M40 GF5 (issue #940) the single gate
> inventory is the generated
> [`gate-fleet-inventory.md`](gate-fleet-inventory.md) — the class-B fleet
> section there is exactly what `just gate-list` prints and what the
> `.github/workflows/vz-gates.yml` CI shards consume. Per-gate pass/fail
> status lives in [`docs/status.md`](status.md); historical per-gate
> evidence paragraphs and claim numbers live in
> [`archive/gate-inventory-detail.md`](archive/gate-inventory-detail.md)
> (frozen at GF5 — do not extend).

## Classes

- **A — portable / build CI.** Deterministic, no Apple silicon, no VZ VM.
  Runs in GitHub CI (`.github/workflows/ci.yml`) and as `just verify-portable`
  (CI splits it across an `ubuntu-latest` `verify-portable-core` lane and a
  `macos-latest` `verify-portable-swift` lane; `just verify-portable`
  composes both locally). A green CI badge means exactly these passed and
  nothing else.
- **B — Apple-silicon Virtualization.framework hardware gate.** Boots a real
  VZ VM on Apple silicon. GitHub-hosted CI does **not** run these and cannot
  prove them; the fleet runs in `.github/workflows/vz-gates.yml` (sharded
  from the spec dir) and locally via `just verify-vz`.
- **C — interactive / manual hardware gate.** Requires a human at the
  keyboard. Not automatable, not in CI.
- **D — diagnostic experiment** (claims 0017/0018/0020/0021/6460/7896);
  **not an acceptance gate**. Passing a diagnostic proves nothing about the
  milestone.

## Where every gate is registered

One source of truth, three generated views — none hand-edited:

| What | Where | Regenerate / check |
|---|---|---|
| Class-B fleet (specs + legacy scripts) | `docs/gate-fleet-inventory.md` snapshot (do not commit in a spec PR) | `just inventory-gates` locally; `--check` is spec-order + locale |
| Class-A + top-level script rows | same snapshot | same |
| CI shard input | `bash tools/gate/fleet.sh list` | derived from `tools/gate/specs/` live |

Run one class-B gate with `just gate <id>`, a pattern group with
`just gates <pattern>`, the whole fleet with `just verify-vz`. Adding a
spec under `tools/gate/specs/` registers it in all of the above with zero
list edits (the `--check` guard fails until the report is regenerated).
