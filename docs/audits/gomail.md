<!-- iface-review:v1 app=GOMAIL.ELF issue=3726 -->
# App audit: Mail (GOMAIL.ELF)

> Retired from polish audit issue #3726 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The smallest honest mail client — a **console** (non-windowed) app: IMAP4rev1 LIST/SELECT/FETCH for boxes, bounded headers and one readable body, SMTP submit for a single message, over `tls.Dial` with the plaintext-consent rule at the dial boundary. Manifest row `GOMAIL.ELF | Mail | @ | dock=false | v=2 | caps=file,net | icon=ICONMAIL` (image/apps.txt:67) — notably **no `window` cap**: it is a verb-driven CLI, not a desktop app.

## Interface today

Observed from `user/go/gomail/{main,imap,smtp,account,cache,folders,mime,dial,parse}.go`:

- Runs as `exec GOMAIL.ELF <verb> [args]` in the shell; prints one `gomail:` marker line per result and exits (0 clean / 1 usage / 2 refused-or-error). No window, no pointer, no keyboard UI — the "interface" is the verb grammar + account file.
- Verbs: `fetch <mbox> [seq…]` (LIST+SELECT+headers+body reads), `boxes`, `use <box>`, `headers [mbox]`, `read [mbox] <seq>`, `search <q>` (offline, over the bounded `/host/MAIL/` cache), `send --to A [--subject S --body T|--body-file P]`, `open <name>` (named attachment refusal), and a named refusal for each out-of-scope verb (idle notify copy move delete append store subscribe oauth render html).
- Config: `key=value` account file (default `/host/GOMAIL.TXT`) overridden by argv flags (`--host/--user/--from/--sni/--imap-port/--smtp-port/--tls/--plain/--max`); the credential never rides file or argv — `pass=` refuses, `--pass` refuses, the password resolves as `mail-<user>` from the slot-70 secret store.
- Output shape: `gomail: box <name>` / `gomail: hdr <seq> <from>: <subj>` / `gomail: text <line>` / `gomail: attach <seq> <name> <type>` + `gomail: refuse attachment`; html bodies render stripped (`gomail: html-stripped`). Dead transport falls back to the cache with `gomail: offline`/`cache-miss` — never fabricated mail.

## Coverage

`tools/gate/specs/go-mail.spec` — ran `just gate go-mail` on this worktree: **PASS, 16/16 runs** — IMAP fetch/boxes/use/headers/read over the pinned TLS chain, SMTP submit, plaintext-consent refusal+grant, attachment/html truth-in-bounds, the bounded `/host/MAIL/` cache (offline search + cache-miss legs), the contact-face send argv shape (c1–c3), and the M196b credential legs (store-resolved `mail-gate`, `pass=`/`--pass` refused, `secret_not_found` named).

## Observed defects

- None observed — all 16 legs green. (The bare-exec → usage shape noted under improvements is by design for a verb CLI, but is a UX dead-end from the launcher.)

## Improvement candidates

- This is the only one of the six audited apps with no window — a `GOMAIL.ELF --ui` face (the GOIRC precedent: console driver + tabapp front-end sharing the marker stream) is the obvious growth card: folder rail, message list, body pane.
- `send` is argv-only — no compose flow. A `--body-file -` stdin convention or a small compose face would close the loop with GOEDIT.
- `search` prints `hit` rows but there's no `read` from cache by hit — a `hit <i>` follow-up verb would make offline search actionable.
- Attachment handling is refuse-by-name with no save-to-share option — a `save <name>` verb writing under `/host/MAIL/ATT/` is an honest first step (still no render).
- `use <box>` remembers state in-process only across a single exec — a persisted last-box file would make `fetch`/`read` less verbose across invocations.
- The manifest shows `dock=false` and no argv — launching Mail from the launcher execs it bare → usage + exit (same shape as GOIRC's bare launch, but GOMAIL never opens a window). A usage-first launch is honest, but a dockable mail face is the real answer.

## Evidence

- `just gate go-mail` → `artifacts/go-mail-report.txt`: `vgate go-mail: PASS (16/16 runs)` — markers incl. `gomail: fetch INBOX` hdr/body rows, `gomail: sent bytes=`, `gomail: refuse plaintext`, `gomail: secret mail-gate`, `gomail: refuse pass argv`.
- Code reads: `user/go/gomail/*.go` (main.go is 723 lines of verb grammar + marker vocabulary); manifest `image/apps.txt:67`; showcase `site/apps/gomail.md`.
