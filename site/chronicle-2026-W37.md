---
title: 2026-W37 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W37: September 7–13, 27 selected PRs

## M43: USB device depth

M43 added the XHCI bulk path, a USB mass-storage/SCSI probe, and explicit rescan and detach behavior.

- [PR #1041: M43 U1+U6: XHCI bulk transfer engine + runner --usb-msd flag](https://github.com/drawmeanelephant/VirelaiOS/pull/1041) · Issues: [#1032](https://github.com/drawmeanelephant/VirelaiOS/issues/1032), [#1037](https://github.com/drawmeanelephant/VirelaiOS/issues/1037) · Labels: none
- [PR #1045: M43 U2 #1033: USB MSC Bulk-Only Transport + minimal SCSI probe](https://github.com/drawmeanelephant/VirelaiOS/pull/1045) · Issues: [#1033](https://github.com/drawmeanelephant/VirelaiOS/issues/1033) · Labels: none
- [PR #1052: M43 U4 (claim #1051): honest device lifecycle — polled rescan + administrative detach](https://github.com/drawmeanelephant/VirelaiOS/pull/1052) · Issues: [#1035](https://github.com/drawmeanelephant/VirelaiOS/issues/1035), [#1051](https://github.com/drawmeanelephant/VirelaiOS/issues/1051) · Labels: none
- [PR #1054: M43 closeout (claim #1053): retire umbrella #1031, milestone 30 done](https://github.com/drawmeanelephant/VirelaiOS/pull/1054) · Issues: [#1031](https://github.com/drawmeanelephant/VirelaiOS/issues/1031), [#1053](https://github.com/drawmeanelephant/VirelaiOS/issues/1053) · Labels: none

## M45 and M49: shell and daily-use flow

M45 moved the terminal and shell into userland, added shell scripting, and made the shell selectable as the default; M49 followed with a daily-use pass.

- [PR #1087: M45 SH1: userland terminal library lib/tty.zig + TTYED live smoke (#1077)](https://github.com/drawmeanelephant/VirelaiOS/pull/1087) · Issues: [#1077](https://github.com/drawmeanelephant/VirelaiOS/issues/1077) · Labels: none
- [PR #1089: M45 SH2: SH.BIN userland shell core — builtins + external apps (#1078)](https://github.com/drawmeanelephant/VirelaiOS/pull/1089) · Historical PR title; SH.BIN was later retired. · Issues: [#1078](https://github.com/drawmeanelephant/VirelaiOS/issues/1078) · Labels: none
- [PR #1095: M45 SH5: shell scripting — chains, if/fn/for/while, $(), $(( )), source (#1081)](https://github.com/drawmeanelephant/VirelaiOS/pull/1095) · Issues: [#1081](https://github.com/drawmeanelephant/VirelaiOS/issues/1081) · Labels: none
- [PR #1107: M45 SH8: default-shell flip (settings shell=sh|monitor) + polish (#1084)](https://github.com/drawmeanelephant/VirelaiOS/pull/1107) · Issues: [#1084](https://github.com/drawmeanelephant/VirelaiOS/issues/1084) · Labels: none
- [PR #1150: M49 SD1–SD5: a shell I'd use daily — monitor escape, unified startup, tool multicall, editing ergonomics, TERM depth (Closes #1127)](https://github.com/drawmeanelephant/VirelaiOS/pull/1150) · Issues: [#1127](https://github.com/drawmeanelephant/VirelaiOS/issues/1127) · Labels: none

## M46–M50: remote access, crypto, and trust

The remote-console client hook, crypto primitives, and process/file permission controls established the authenticated remote and trust surfaces.

- [PR #1145: M46 RC1–RC4: vgate client hook + authenticated net front-end + live-remote-console (#1108)](https://github.com/drawmeanelephant/VirelaiOS/pull/1145) · Issues: [#1108](https://github.com/drawmeanelephant/VirelaiOS/issues/1108) · Labels: none
- [PR #1148: M47: crypto primitives library — SHA-2/HMAC, ChaCha20-Poly1305, X25519, Ed25519, live-crypto](https://github.com/drawmeanelephant/VirelaiOS/pull/1148) · Issues: [#1113](https://github.com/drawmeanelephant/VirelaiOS/issues/1113) · Labels: none
- [PR #1152: M50/TS0: ADR 0024 — trust baseline (principals, permissions, authenticated remote) + scoping](https://github.com/drawmeanelephant/VirelaiOS/pull/1152) · Issues: [#1134](https://github.com/drawmeanelephant/VirelaiOS/issues/1134) · Labels: none
- [PR #1156: M50 TS2: file ownership/permissions on the host-share + file_table seam (#1136)](https://github.com/drawmeanelephant/VirelaiOS/pull/1156) · Issues: [#1136](https://github.com/drawmeanelephant/VirelaiOS/issues/1136) · Labels: none
- [PR #1162: M50 TS3: process privilege + syscall gating — kill gate + monitor admin spawn (#1137)](https://github.com/drawmeanelephant/VirelaiOS/pull/1162) · Issues: [#1137](https://github.com/drawmeanelephant/VirelaiOS/issues/1137) · Labels: none

## M48: browser-style tabs

M48 added browser-style tab operations to the desktop rail.

- [PR #1147: M48 BT1-BT6: browser-style tab depth on the left rail (#1120)](https://github.com/drawmeanelephant/VirelaiOS/pull/1147) · Issues: [#1120](https://github.com/drawmeanelephant/VirelaiOS/issues/1120) · Labels: none

## M51: SSH client and interoperability

M51 landed the client-first SSH transport and session flow, then added endpoint coverage and real-OpenSSH interoperability evidence.

- [PR #1173: M51 SSH0: ADR 0025 — client-first SSH transport + scoping (#1165)](https://github.com/drawmeanelephant/VirelaiOS/pull/1173) · Issues: [#1165](https://github.com/drawmeanelephant/VirelaiOS/issues/1165) · Labels: none
- [PR #1190: M51 SSH4: encrypted transport + session channel + SSH.BIN exec/shell (#1171)](https://github.com/drawmeanelephant/VirelaiOS/pull/1190) · Historical PR title; SSH.BIN was later retired. · Issues: [#1171](https://github.com/drawmeanelephant/VirelaiOS/issues/1171) · Labels: none
- [PR #1195: M51 SSH5: class-B endpoint gate + runner SSH responder (#1172)](https://github.com/drawmeanelephant/VirelaiOS/pull/1195) · Issues: [#1172](https://github.com/drawmeanelephant/VirelaiOS/issues/1172) · Labels: none
- [PR #1211: M51 interop: real-OpenSSH evidence + runner :relay byte proxy](https://github.com/drawmeanelephant/VirelaiOS/pull/1211) · Issues: [#1209](https://github.com/drawmeanelephant/VirelaiOS/issues/1209) · Labels: none

## GOOS=virelai: runtime breadth

The Go port gained its first kernel/toolchain path, thread and futex support, environment handling, broader runtime coverage, and hands-on exercises.

- [PR #1187: GOOS=virelai phase 0a: kernel enablement + gc toolchain fork — GO-HELLO VZ gate PASS (#1163)](https://github.com/drawmeanelephant/VirelaiOS/pull/1187) · Issues: [#1163](https://github.com/drawmeanelephant/VirelaiOS/issues/1163) · Labels: none
- [PR #1221: GOOS=virelai 0b round 2: sys_thread/sys_futex slots 73/74, proc.go deltas retired, argv-flake root cause](https://github.com/drawmeanelephant/VirelaiOS/pull/1221) · Issues: [#1214](https://github.com/drawmeanelephant/VirelaiOS/issues/1214) · Labels: none
- [PR #1230: GOOS=virelai envp half: exec envp + GOMAXPROCS override (Closes #1226)](https://github.com/drawmeanelephant/VirelaiOS/pull/1230) · Issues: [#1226](https://github.com/drawmeanelephant/VirelaiOS/issues/1226) · Labels: none
- [PR #1231: GOOS=virelai 0b breadth: GC/channel/timer/futex stress (Closes #1227)](https://github.com/drawmeanelephant/VirelaiOS/pull/1231) · Issues: [#1227](https://github.com/drawmeanelephant/VirelaiOS/issues/1227) · Labels: none
- [PR #1232: GOOS=virelai 0c: slot-75 sys_exnotify fault delivery to sigpanic (#1228)](https://github.com/drawmeanelephant/VirelaiOS/pull/1232) · Issues: [#1228](https://github.com/drawmeanelephant/VirelaiOS/issues/1228) · Labels: none
- [PR #1234: GOOS=virelai exercises: three hands-on labs verifying Go support](https://github.com/drawmeanelephant/VirelaiOS/pull/1234) · Issues: [#1233](https://github.com/drawmeanelephant/VirelaiOS/issues/1233) · Labels: none

## M-web: in-guest document rendering

M-web extended the in-guest renderer with images, navigation, fetch, and a published web-client path.

- [PR #1222: M-web S1: DOC.BIN renders oliver HTML in-guest](https://github.com/drawmeanelephant/VirelaiOS/pull/1222) · Historical PR title; DOC.BIN was later retired. · Issues: [#1202](https://github.com/drawmeanelephant/VirelaiOS/issues/1202) · Labels: none
- [PR #1224: M-web S3–S6: img, click-nav, fetch, and publish](https://github.com/drawmeanelephant/VirelaiOS/pull/1224) · Issues: [#1200](https://github.com/drawmeanelephant/VirelaiOS/issues/1200), [#1204](https://github.com/drawmeanelephant/VirelaiOS/issues/1204), [#1205](https://github.com/drawmeanelephant/VirelaiOS/issues/1205), [#1206](https://github.com/drawmeanelephant/VirelaiOS/issues/1206), [#1207](https://github.com/drawmeanelephant/VirelaiOS/issues/1207) · Labels: none
