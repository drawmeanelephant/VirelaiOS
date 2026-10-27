<!-- iface-review:v1 app=GOIRC.ELF issue=3709 -->
# App audit: IRC (GOIRC.ELF)

> Retired from polish audit issue #3709 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The IRC client — an RFC 1459/2812 core (register/join/PRIVMSG/NOTICE, 433 nick-retry, reconnect with backoff) with a console driver plus a `--ui` tabapp front-end. TLS is the default dial (`tls.Dial`, 6697-style); plaintext refuses unless `--plain` or a stored UI consent answers; SASL resolves `irc-<acct>` from the kernel secret store and `--sasl acct:pass` is refused outright. Manifest row `GOIRC.ELF | IRC | / | dock=false | v=2 | caps=file,window,net | icon=ICONIRC` (image/apps.txt:65); no `argv=` — see defects.

## Interface today

Two faces (observed from `user/go/goirc/main.go`, `ui.go`, and gate serial):

- **Console driver** (default with argv): runs in the shell, prints one marker line per event (`goirc: registered/join/msg/notice/drop/backoff/quit`). Flags: `--host/--port/--nick/--user/--realname/--join/--tls/--sni/--sasl/--plain/--retries/--backoff-ms/--idle-ms/--pong-ms/--max-ms`, or positional `<host> <nick> [#chan …]`.
- **`--ui` window** (640x400 native): a `nick @ host:port (state)` header strip, a 132 px channel rail (server buffer `*` plus one row per channel; `-` suffix on open-but-unjoined buffers), the scrollback pane (12 px rows; msg/notice/join/error each get a glyph prefix; mention/highlight lines carry a 3 px accent bar), and a single-line input. PgUp/PgDn scroll, Up/Down move the rail focus, pointer clicks select a buffer. Slash verbs: `/join /part /msg /nick /quit /hl /log`. A mention or armed `/hl` word posts a seat toast whose click-through lands the user on the calling buffer. The first plaintext dial raises an appkit Confirm dialog (No is default); the answer rides save_state and is never re-asked. Session blob carries server/port/TLS/nick, buffers+scroll, focus, consent, and `/hl` rules.
- **Bare launch**: a zero-argv exec falls into `runUI` to honour seat re-open/session restore — but with no staged blob there is no host/nick, so the window opens, prints usage to the console, and exits (ui.go:813-819).

## Coverage

`tools/gate/specs/go-irc.spec` — ran `BOOTS=1 just gate go-irc` on this worktree: **8/11 runs PASS**. Protocol core (register/join/drop/reconnect/rejoin, plaintext refusal, 433 retry), UI legs (consent dialog No/Yes, mention toast click-through), the K-journey (TLS first then consent-stored plaintext + snapshot), the restore boot, `/hl`+`/log`, and both M196c SASL legs — runs 04, 07 and 08 failed; see defects.

## Observed defects

- **Run 07/08: boot autostart restores stale SESSION.TABS before the script's `vf rm`** — serial shows `session load n=1 mode=restore` + `session exec GOIRC.ELF` ahead of the first script line, so run 07's snapshot recorded 3 tabs (`expected 2 tab records, got 3`) and run 08 restored `n=3` instead of `n=2`. Filed as #3708.
- **Run 04 ordering race** (`vgate_assert 04 python`): the chain expects `paint → blur → dismiss → tab focus → focus #other`; observed `paint → toast → dismiss → tab focus → blur → focus #other` — toast click and alt-tab armed on the same marker raced, and the click landed in the kernel FIFO first. UX chain still completed. Noted on #3708 (same spec).
- **Bare launch has no first-run UI**: clicking the IRC row in the launcher (no `argv=` in the manifest) opens a window that immediately exits with usage — there is no connect dialog (ui.go:813-819). Observed in code; UX impact inferred.

## Improvement candidates

- Add a first-run connect form (host/port/TLS/nick, SASL acct picker fed from `SECRETS.TXT` names) so a launcher launch is usable; it would also give the seat's crash re-exec a defined landing when the blob is stale.
- The rail is 132 px fixed and rows are 12 px of 8 px text — cramped; wider rail + resizable split (drag the divider) would match GOEDIT-era chrome depth.
- No nick list / member roster pane — NAMES results only print to serial. A right-hand roster column is the biggest missing IRC surface.
- `/log` writes bounded per-channel files but there's no in-app "open log" gesture — a `/log open` or GOFILES reveal verb would close the loop.
- Scrollback rows are 12 px with no timestamps; optional `HH:MM` prefixes (clockfmt already in tree) would aid catch-up reading.
- Unread-badge count on the rail for unfocused buffers (the accent bar exists only on the focused buffer's mention line).

## Evidence

- `BOOTS=1 just gate go-irc` → `artifacts/go-irc-report.txt`: runs 01–03, 05, 06, 09–11 PASS; run 04 `python` ordering FAIL (chain marker `gotabwm: notify dismiss id=` present but earlier than the chain allows: dismiss@137985, tab focus@137986, blur@139642, focus@139675); run 07 `python` FAIL (`expected 2 tab records, got 3`); run 08 FAIL (`session load n=2` printed `n=3`, `restored-tail` never arrived).
- Console/UI code reads: `user/go/goirc/{main,ui,client,dial,state,highlight,log,backlog}.go`; manifest `image/apps.txt:65`; showcase `site/apps/goirc.md`.
