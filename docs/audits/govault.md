<!-- iface-review:v1 app=GOVAULT.ELF issue=3720 -->
# App audit: Vault (GOVAULT.ELF)

> Retired from polish audit issue #3720 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The secrets store — one unlock-gated vault file at `/host/GOVAULT/VAULT.BIN` (GVLT framing, PBKDF2-HMAC-SHA256 → AES-256-GCM, crash-safe `WriteFileSafe` writes, 0600 mode), behind a masked passphrase field. Manifest row `GOVAULT.ELF | Vault | $ | dock=false | v=2 | caps=file,window,timer | selection=primary | icon=ICONVLT` (image/apps.txt:66) — the `selection=primary` opt-in is what arms the copy path on the seat's primary selection.

## Interface today

Observed from `user/go/govault/{main,verbs,ui,model,session,totp}.go`:

- One window, native 512x384 at (32,32). A 24 px chrome strip reads `Vault   LOCKED|OPEN`; body is one of eight faces: **New vault** (passphrase + confirm, masked), **Locked** (one masked field), **Bad file** (dead-end face, esc quits), **List** (entry rows: name | `********`/revealed secret | live TOTP code+countdown for seeded entries | truncated note), **Add** (4 fields: name/secret/note/totp), **Receipts** (time+action+name audit rows, newest at bottom), **Export/Import** (bundle passphrase faces).
- Keyboard only — no pointer handling observed in verbs.go. Tab/Down advance fields, Up backs up, Enter submits the last field, printable runes insert. On the list: `r`/Enter reveal-hide, `c` copy (publishes to the seat primary selection, expires in 30 s → `[cleared]` overwrite), `a` add, `e` export / `i` import (GVLX bundles via the shared FilePicker modal), `v` receipts, `p` provision (slot-96 ask → seat consent), ctrl-l lock, Escape locks (the safe direction; quit lives on the locked face). Idle deadline locks automatically.
- Copy discipline: the kernel clipboard is seat/CAP_CLIPBOARD-only, so copy rides the M124e primary selection; expiry overwrites since empty publish is refused. Reveal/lock/copy/provision events append plaintext receipt rows to `RECEIPTS.TXT` — never secret material; serial markers carry only counts and rc codes.
- save_state blob carries only the vault path — a restored session still lands on the Locked face.

## Coverage

`tools/gate/specs/go-vault.spec` — ran `just gate go-vault` on this worktree (the spec is fixed K=3): **PASS, 27/27 runs** — the full create-add-reveal-copy-expire-lock-unlock arc, tamper legs, GVLX export/import merge, the slot-96 provision legs (GOMAIL+GOIRC consume the granted rows), receipts, idle lock, and the host cross-checked TOTP/clock probe all green on every iteration.

## Observed defects

- **User-facing label leak (cosmetic)**: the Add face's header literally reads `New vaultlib.Entry` (ui.go `drawAdd`) — the Go type name painted as the screen title. Cosmetic but user-visible.
- No functional defects observed — the gate was fully green.

## Improvement candidates

- Keyboard-only interface: the list rows could take pointer hit-testing (click select, double-click reveal) — every other shipped app is pointer-capable.
- No password generator — `a` add could offer a `g` verb that CSPRNG-fills the secret field (randFill seam exists).
- No entry edit/delete verbs at all — rename/rotate/delete is a bigger gap than cosmetics: today the only mutation is add + import merge.
- TOTP is a column, not a verb — `t` could copy the live code to the selection like `c` does secrets.
- The two-line hint footer is dense (`a add · r reveal · c copy · p provision / e export · i import · v receipts · ctrl-l lock`); an appkit menu bar or a `?` help face would scale better as verbs grow.
- Unlock has no "show passphrase" toggle; the masked field offers no strength feedback on create.
- `icon=ICONVLT` exists — check the tile actually decodes (no icon= raster issue like GOPAINT's absence).

## Evidence

- `just gate go-vault` → `artifacts/go-vault-report.txt`: `vgate go-vault: PASS (27/27 runs)` — incl. `govault: import added=3 skipped=0`, snapshot legs decoding `hunter3` + live TOTP codes, `SECRETS.TXT persisted: mail-gate->GOMAIL.ELF, irc-gate->GOIRC.ELF`, and serial-absent pins keeping every passphrase/secret off the wire.
- Code reads: `user/go/govault/{main,verbs,ui,model,session,totp,receipts,state,provision}.go`; manifest `image/apps.txt:66`; showcase `site/apps/govault.md`.
