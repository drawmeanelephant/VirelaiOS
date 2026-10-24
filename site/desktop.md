---
title: The desktop
parent: capabilities
status: published
tags: [capabilities, desktop, seat]
---

# The desktop

The default boot does not land on a shell — it lands on a graphical
desktop. A single Go program, `GOTABWM.ELF`, registers itself as the
**seat**: the userland window manager that owns the scanout, hosts every
windowed application as a tab, and supplies the rail, the launcher, the
notifications surface, and the session file. This page is the seat's
story — what it is, the contract apps speak to it, and the chrome it
paints. [[apps]] catalogues the fleet that runs on it; [[media]] covers
the fonts and image decoders underneath.

## One seat, and a fallback kept honest

The seat is a **settings value**, not a compile-time choice. `wm` is a
schema-v2 key in `SETTINGS.TXT` with three legal values: `gotabwm` (the
compiled default — a boot with no persisted value autostarts
`GOTABWM.ELF`), `tabwm` (the Zig fallback seat, `TABWM.BIN`, retained
permanently by decision in ADR 0034), and `none` (an explicit shim-only
VM the class-B gates use when they want no seat at all).

The fallback is a contract, not a courtesy. Deleting `TABWM.BIN` would
be a schema migration plus a dozen gate re-hosts, so it stays — frozen
at bug-fix level, and exercised: `just gate go-wm-default` boots the
persisted `wm=tabwm` share and asserts the Zig seat's own markers. A
seat value nobody boots is how a fallback silently rots, and this one
does not.

<Aside kind="info">

**LIVE-GATED.** The seat matrix is pinned end to end: `go-wm-default`
covers the compiled default, the persisted fallback, and a corrupt
settings file healing byte-exact; `go-wm-seat` proves the WM_RPC wire;
`go-wm-tabs` the tab lifecycle; `go-wm-hid` real input; `go-wm-journey`
the snapshot/hard-kill/re-exec chain.

</Aside>

## How a pixel reaches the screen

Desktop policy lives in userland, not the kernel. ADR 0015 split the
machine: the kernel keeps surfaces, the blitter, and input fan-out — a
thin render server — while the seat registers syscall slot 65
(`sys_wmctl`) as its exclusive control surface and makes every policy
decision itself. ADR 0016 (the "Seam B" amendment) completed the split:
apps render into their **own** shared-anonymous mmap buffers, and the
seat asks the kernel for one composite per `COMPOSITE_TICK` event. There
are no per-rect fill syscalls on the modern path, no kernel-owned app
back-buffers, and no second compositor — ADR 0033 froze the rule that
GOTABWM deepens rather than forks the desktop.

The practical consequence: an app never calls `wmctl` and never paints
the scanout. It opens a window, owns a `[]u32` pixel buffer, and talks
to the seat.

## The WM_RPC contract

App-to-seat communication is a **38-byte frame** carried over the
ordinary mailbox IPC (slots 5/6) — `vi`'s `WmRpc` type packs a kind
byte, an 8-bit window id and requester, a sequence number, a 24-byte
title field, and a short bounded wait for the matching ack (replies set
the `0x80` bit on the kind). Every call is best-effort and honest: a
missing seat, a timeout, or an unimplemented kind returns a refusal the
app must handle, never a silent guess.

The vocabulary is additive — kinds are never renumbered:

| Kind | Name | What it does |
|-----:|------|--------------|
| 1 | `raise` | focus/raise the tab |
| 5 / 6 | `attach` / `detach` | join or leave the strip |
| 7 | `cycle` | advance tab focus |
| 8 | `declare_fullscreen` | register a tab and propose the full viewport |
| 9 / 10 | `nav_declare` / `nav_poll` | per-tab navigation history the seat steps with `ctrl+shift+[`/`]` |
| 11 | `set_title` | retitle a rail cell |
| 12 | `notify` | post a toast (the 24-byte title is the whole message budget) |
| 13–15 | `settings_*` | subscribe to / publish settings keys; kind 15 is the seat's broadcast |
| 16 | `windows_query` | enumerate the strip (AGENTD's `ui.windows`) |
| 17 / 18 | `gpu3d` / `share_surface` | the opt-in Metal render path and surface handoff |
| 20 / 21 | `save_state` / `restore_state` | the per-app session protocol (the frame carries a rendezvous, not the blob) |
| 22 / 23 | `progress` / `progress_cancel` | cancelable progress rows in the notifications center |
| 24–26 | `drag_*` | cross-app drag-and-drop: begin, drop, cancel |
| 27 / 28 | `sel_publish` / `sel_paste` | the primary selection |
| 29 / 30 | `agent_input` / `agent_geom` | AGENTD-only injected HID events and geometry reads |

Most apps never see the wire. `tabapp` wraps it: `tabapp.Init` performs
the kind-8 declaration, `tabapp.Notify` posts kind 12, and the
`SaveState`/`RestoreState`/`DragSource`/`DropTarget` hooks answer kinds
20–26 from inside the app's own event loop.

## Writing an app: `tabapp`, `appkit`, `widgets`

Three layers sit between an author and the seat, all in
[`user/go`](https://github.com/drawmeanelephant/VirelaiOS/tree/main/user/go):

- **`tabapp`** owns the window/WM handshake — init, dispatch, resize
  re-viewing of the bound surface, drag hooks, state hooks. An app's
  `Dispatch` turns raw events into actions (`ActionClosed`,
  `ActionResized`, `ActionUndo`, …).
- **`appkit`** is the application framework proper: `Loop` (present
  once, dispatch, repaint only on real state change), `Dialog`,
  `MenuBar`/`TreeController`/`TableController` (the controls v2 — a menu
  bar with `&` mnemonics, a lazy outline, a keyboard-driven virtualized
  grid), `FilePicker` (the one shared open/save modal), `ErrorBanner`
  (in-frame failure reporting with action buttons), `Progress` (bars
  that post to the notifications center and take Cancel back on kind
  23), `ChordHints` (the `?` overlay, generated from the live chord
  registry), and `EmptyState` (a zero-content surface is still a
  surface).
- **`widgets`** and **`draw`** are the kit and the raster contract:
  `Text`, `Button`, `List`, `TextInput`, `Table`, `Tree`, plus the
  `Canvas` interface painting through `draw.BufferCanvas` — coverage-AA
  shapes, gradients, shadows, scaled blits and a fixed clip stack over a
  caller-owned `0xAARRGGBB` pixel buffer, all integer math.

The discipline underneath: apps own their buffers and their event
loops; a control is a pure state machine plus a `Draw`, so the same
model drives a canvas app and a bound-tty TUI.

## Chrome: what the seat paints

The seat's own pixels are deliberately few, all painted on the composite
tick:

- **The rail** — the tab strip across the top: up to 16 cells, one per
  hosted tab, with pin, reorder (click or `ctrl+shift+PgUp`/`PgDn`),
  close, a frozen badge, and a focus badge that names the focused app.
  More than one space draws the live space's name at the rail's right
  edge. The strip *is* the running set — a cell exists only while its
  tab lives, so there is no separate dock and no running dot to paint.
  What a cell carries instead is attention and action: an unread badge
  counts that tab's undismissed notifications-center entries posted
  since it last took focus (it clears when the tab takes focus, not
  when the toast fades), and a right press opens a four-row menu —
  Focus / Duplicate / Pin-or-Unpin / Quit — aimed at that cell's tab
  through the same seams the chords run, chrome-modal like the power
  menu (#3238).
- **The clock panel** — bottom-right, with an honest source chain:
  kernel wall time (slot 66) first, the session launcher's
  `/host/.clock` next, the seat's own uptime last, and a one-shot
  `clock-source` marker saying which won.
- **The start surface** — an empty strip is not a blank screen: a
  centered plate names the seat and points at the launcher.
- **The launcher** — `ctrl+space` summons the docked-strip grid over the
  `APPS.TXT` catalogue; the same engine's second face is the
  **command palette** (`ctrl+shift+p`), which subsequence-ranks open
  tabs, apps, recent files, settings keys, help topics and the whole
  chord registry in one modal input. A non-empty filter also ranks
  bounded filename hits under the share, and every row carries a verb
  affordance — Tab or the horizontal arrows step between open, reveal
  in GOFILES, and one open-with row per manifest `opens=` candidate for
  the hit's mime spelling. Content search stays a hand-off: a "Search
  files for" row sends the query to GOFILES's own walk rather than
  doing it inline (#3243).
- **The god menu and PWR buttons** — small seat glyphs on the bottom
  band for system actions and the power menu: three rows, Suspend,
  Snapshot (launches or focuses GOSNAP's snapshot window), and Lock.
- **Screenshots** — the guest photographs itself, no host recorder
  involved: `ctrl+shift+3` publishes the composed scanout and
  `ctrl+shift+4` the focused window's rect of it, as a PNG to
  `/host/SHOTS/SHOT-<unix>.png` through a banded scanout read and a
  crash-safe share write. Every refusal names itself twice — the
  serial marker and a toast: `no_host`, `no_window`, `no_clock`,
  `bad_rect`, `io_<errno>` (#3237).
- **The clipboard-history menu** — `ctrl+shift+i` summons the seat's
  paste menu over the rail: a ≤16-entry ring of every clip the kernel
  buffer has carried this boot, FIFO-evicted, a re-copy bumping to the
  head, memory-only — never serialized to the share or the session.
  Each row shows the mime the seat classified the bytes as plus a
  preview; Enter (or a row press) writes the pick back through the same
  slot-38 seam every copy takes and forwards the app's own paste chord.
  The primary selection is excluded by construction — it never touches
  the shared buffer — and host-clipboard applies join the ring like any
  other clip (#3236).
- **The lock curtain** — `ctrl+shift+l`, or the PWR menu's Lock row,
  drops a full-opacity veil holding one passphrase field: a seat-owned
  input sink takes kernel focus, so no hosted app receives a keystroke
  while it is up. Unlock re-authenticates the passphrase against the
  GOVAULT credential through the same `vaultlib` codec GOVAULT opens —
  and with no readable credential the lock refuses by name
  (`no_credential`, `bad_credential`, `no_sink`), never a silent no-op
  (#3240).

<Aside kind="note">

**VERIFIED.** Seat chrome is pinned in pixels, not just markers:
`go-dogfood` asserts real scanout content — a hosted tab must not
paint blank — and its keyboard-only acceptance boot drives the hints
plate, the palette strip and the focus badge with pixel asserts at
each step.

</Aside>

The clipboard the paste menu fronts is the kernel's one bounded
machine-global buffer — 512 bytes behind slots 38/39 — and every
writer meets it the same way, so a terminal copy chord, the monitor's
`clip`, a capability holder's set, a signed host-lane apply and the
menu's own pick are indistinguishable to its observers. Terminal
owners speak it through real escape sequences: an OSC 52
`c;<base64>` string from a bound terminal's owner queues a copy the
session pump lands in the shared buffer — a `?` read-back is refused
outright, and malformed base64 refuses whole rather than landing a
partial clip — while DECSET 2004 (**bracketed paste**) wraps a
chord-pushed paste in `\e[200~` … `\e[201~` so the receiving editor
keeps the bytes literal (#1715, #1629).

Menus and find are app chrome with one shared shape. The appkit
`MenuBar` — a top strip of titled menus with `&` mnemonics, engaged
by F10 or a bare Alt — is the fleet's menu surface: GOEDIT, GOFILES
and WEB share their title row with it, and GOTERM's tty-bound
front-end engages the same bar on the alternate screen, each row
firing the code path its named chord takes (#3239). The appkit `Find`
overlay is likewise one bar — a bottom-docked strip with a pattern
field, a live match count, and Enter / Shift-Enter cycling over the
app's own corpus seam — adopted by NOTE, GOREADER and GOTERM (#3244).
GOTERM's face of it is the scrollback find: `ctrl+shift+g` (BEL on
the tty wire — the kernel encoder folds shift out of ctrl-letters)
opens an alternate-screen bar over the capture ring re-derived into
plain rows. The kernel owns tty presentation and exposes no readback,
so the searchable corpus is the app's own mirror of grid plus
history — searching it searches scrollback and the visible grid
whole, and sixel image rows are named, never searched as text
(#3242).

Chrome also answers the accessibility rows: `a11y.contrast`
recomposes every `theme.ApplySettings` palette including the seat's
own, and `a11y.reduce_motion` holds the toast ramp's fade (#2983).

## Tabs, panes, and spaces

A **tab** is one hosted window. The strip supports pin, drag- and
chord-reorder, reopen-last-closed (`ctrl+shift+t`), duplicate
(`ctrl+shift+d`), close (`ctrl+shift+w`), a frozen badge, and Alt-Tab /
Ctrl-Tab / `ctrl+1..9` focus — the browser-style depth the seat carried
forward from the Zig seat's tab lifecycle.

A **split** divides a tab's viewport into a fixed-pool binary **pane
tree**: internal nodes split `v`/`h` at a sash, leaves host tabs, with
bounds of 8 leaves and depth 3 and a 160×120 pane minimum enforced at
mutation. `ctrl+shift+v` cycles the focused pane none → v → h → none;
`ctrl+alt+arrows` move focus by rect geometry. Panes follow tabs across
reorders and persist in a `SESSION.PANES` sidecar — the `.tabs` v2
record format is frozen, so the tree rides its own file.

A **space** is a named tab strip plus its pane tree — the seat's answer
to workspaces, bounded at `MaxSpaces` = 4. `ctrl+alt+PgDn` steps forward
and *creates* the next space at the end; `ctrl+alt+PgUp` clamps at the
first; the palette moves a focused tab between spaces wholesale.
Dormant windows keep running (input simply cannot reach them), and the
space name persists in the session record's `group` field.

## Notifications

`tabapp.Notify` posts a kind-12 toast — 24 bytes of text, click-to-focus
back to the sending tab, dropped with its sender when the app dies. The
seat keeps a bounded 4-deep toast queue bottom-left, and behind it a
**notifications center**: an 8-row history panel with a do-not-disturb
toggle (`notify_dnd`) and crash-safe persistence across a seat restart.
DND is honest: the seat still acks the notice — an app must not learn
the user is away — but holds the toast off the strip while the notice
lands in the center. Kind-22 progress posts become live rows there with
a Cancel button that answers the app on kind 23.

## Chords: one owner per dispatch point

Every global shortcut is a row in `user/go/chords` — the one table that
names every chord, its owner, and its **dispatch point**:
`kernel-terminal` (frozen kernel chrome), `seat`, or `app/<binary>`.
Because focus is exclusive, `ctrl+s` legitimately belongs to both
GOEDIT.ELF and NOTE.ELF at different dispatch points — but two owners of
one chord at one point trip `Validate()`'s named `ErrChordConflict`,
which fails the host tests and makes the seat's boot prologue refuse
the seeded conflict fixture rather than surprise a person.

The registry is also user-facing: GOSET's shortcuts view
(`ctrl+shift+h`) renders the table, the palette execs seat rows, the
`?` hints overlay in a hints-aware app lists the chords that answer
while it is focused, and the focus badge names what would get the next
key. The kernel's own chrome chords are the frozen `kernel-terminal`
rows — copy/paste/search (`ctrl+shift+c`/`v`/`f`), scrollback
(`ctrl+shift+k` clears, `ctrl+shift+r` soft-resets,
`ctrl+shift+alt+r` is full RIS, PgUp/PgDn/Shift+arrows scroll) — and
`ctrl+z` is a seat chord that routes undo to the focused tab.

## The session keeps itself

Sessions persist in `.tabs` v2 — a header plus 69-byte records, at most
16 tabs, flags `pinned|frozen|dock`, a `group` field carrying the space
name — written crash-safe to `/host/SESSION.TABS`. On boot the seat
**live re-execs** each record's binary, and a `SESSION.PANES` sidecar
rehydrates the pane trees.

Per-app state rides the kind-20/21 protocol: WM_RPC frames are too small
for blobs, so the seat asks at lifecycle points (snapshot, tab close,
crash) and the app stages a ≤64 KiB versioned blob at
`/host/APPSTATE.<id>` — consume-on-use, so a stale blob is never
delivered twice. The blob rides an `AS` trailer inside `.tabs` v2, and a
closed tab's blob re-delivers on reopen. `ctrl+shift+s` takes a whole
**snapshot bundle** (settings + session + a docs selection) to
`/host/SNAPSHOT.BUNDLE`; GOSNAP parks full machine images as named
`/host/SNAPSHOTS/*.vzsave` files behind consent.

A crash is not data loss: the seat's released-window mirror catches a
client death, re-collects every live app's state, rewrites the session
with the dead row's last blob, parks a rotated crash snapshot, re-execs
the crashed binary in-place (bounded once per binary per boot), and the
next boot posts a **recovery toast** offering the parked machine image.

<Aside kind="info">

**LIVE-GATED.** Session fidelity is pinned by `go-wm-journey` (snapshot
→ hard kill → `session reexec` with panes and spaces intact),
`go-fileman` (per-app save/restore across a session kill),
`live-session-persistence`, and `go-wm-default`'s corrupt-session
heal legs.

</Aside>

## Idle and input posture

The seat owns **idle dim**: the `idle_minutes` settings key arms a
visual curtain that fades the desktop after inactivity and wakes on the
next input — seat-owned, so the kernel's composite path stays dumb.
Keyboard layout, dead keys, and translated-key delivery are likewise
seat-side (`keyboard_layout`, the M83 compose tables), and kernel chrome
chords remain the kernel's frozen claim — the registry records them so
nothing else binds their spellings.

<Aside kind="warning">

**LIMITATION.** The desktop is single-display and 2D-only; accelerated
3D exists only as the opt-in custom-virtio Metal device. Under
`wm=none` there is no seat at all — apps get the bare shim surface and
none of this page applies.

</Aside>

## See also

- [[apps]] — the shipping fleet the seat hosts.
- [[media]] — typography, image decode, PDF/SVG, sixel.
- [[programs]] — the runtimes (WASM, QuickJS, Bubble Tea, native Zig).
- [[graphics]] — the kernel-side framebuffer and compositor seam.
- [[input]] — how keyboard and pointer events reach the seat.
- [[development]] — the authoring guide this page summarizes.
