---
title: 2026-W38 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W38: September 14–20, 31 selected PRs

## M52–M53 and WMP: client lifetime and latency

M52 hardened cleanup when a client exits; M53 and WMP measured presentation latency and added reschedule-demand bookkeeping.

- [PR #1243: M52: client-death hardening — exit-path inventory, WM seat captures, surface teardown](https://github.com/drawmeanelephant/VirelaiOS/pull/1243) · Issues: [#1237](https://github.com/drawmeanelephant/VirelaiOS/issues/1237), [#1238](https://github.com/drawmeanelephant/VirelaiOS/issues/1238), [#1239](https://github.com/drawmeanelephant/VirelaiOS/issues/1239), [#1240](https://github.com/drawmeanelephant/VirelaiOS/issues/1240) · Labels: none
- [PR #1249: M53 card 1: measure the WM's present cadence and input latency on VZ](https://github.com/drawmeanelephant/VirelaiOS/pull/1249) · Issues: [#1247](https://github.com/drawmeanelephant/VirelaiOS/issues/1247) · Labels: none
- [PR #1251: WMP card 2 (#1250): present when an input burst ends — worst-case pointer latency 3.0 s → 1.0 s](https://github.com/drawmeanelephant/VirelaiOS/pull/1251) · Issues: [#1250](https://github.com/drawmeanelephant/VirelaiOS/issues/1250) · Labels: none
- [PR #1276: WMP card 3: land the reschedule-demand bookkeeping without the comparator pull (#1274)](https://github.com/drawmeanelephant/VirelaiOS/pull/1276) · Issues: [#1274](https://github.com/drawmeanelephant/VirelaiOS/issues/1274) · Labels: none

## TLS 1.3: guest HTTPS foundation

The TLS series added crypto and certificate verification, a TCP adapter, and a handshake that passed its guest-side responder path.

- [PR #1263: TLS13-C1: the crypto the shelf was missing, plus the RFC 8446 key schedule and record layer](https://github.com/drawmeanelephant/VirelaiOS/pull/1263) · Issues: [#1262](https://github.com/drawmeanelephant/VirelaiOS/issues/1262) · Labels: none
- [PR #1271: TLS13-C2: RSA and ECDSA signature verification](https://github.com/drawmeanelephant/VirelaiOS/pull/1271) · Issues: [#1264](https://github.com/drawmeanelephant/VirelaiOS/issues/1264) · Labels: none
- [PR #1273: TLS13-C4: certificate chain validation and the trust store](https://github.com/drawmeanelephant/VirelaiOS/pull/1273) · Issues: [#1266](https://github.com/drawmeanelephant/VirelaiOS/issues/1266) · Labels: none
- [PR #1275: TLS13-C5: TLS 1.3 handshake state machine — completes against the real internet](https://github.com/drawmeanelephant/VirelaiOS/pull/1275) · Issues: [#1267](https://github.com/drawmeanelephant/VirelaiOS/issues/1267) · Labels: none
- [PR #1280: TLS13-C7: the TCP seam adapter a guest consumer needs](https://github.com/drawmeanelephant/VirelaiOS/pull/1280) · Issues: [#1279](https://github.com/drawmeanelephant/VirelaiOS/issues/1279) · Labels: none
- [PR #1335: TLS13-C16: the guest handshake PASSES — two causes, both found by the gate](https://github.com/drawmeanelephant/VirelaiOS/pull/1335) · Issues: none · Labels: none

## M56–M59: Go apps and default desktop

The Go SDK and tab-app seam expanded into hosted apps and the boot-default Go seat, alongside Go networking and terminal support.

- [PR #1323: M56 (Go SDK): vi IPC/WM_RPC/hinted-mmap, widgets, and a Go tabapp — milestone 39](https://github.com/drawmeanelephant/VirelaiOS/pull/1323) · Issues: [#1311](https://github.com/drawmeanelephant/VirelaiOS/issues/1311), [#1314](https://github.com/drawmeanelephant/VirelaiOS/issues/1314), [#1315](https://github.com/drawmeanelephant/VirelaiOS/issues/1315), [#1316](https://github.com/drawmeanelephant/VirelaiOS/issues/1316), [#1319](https://github.com/drawmeanelephant/VirelaiOS/issues/1319) · Labels: none
- [PR #1330: M57b (#1317): GOTABWM manages its own Go windows — go-wm-seat VZ green](https://github.com/drawmeanelephant/VirelaiOS/pull/1330) · Issues: [#1317](https://github.com/drawmeanelephant/VirelaiOS/issues/1317) · Labels: none
- [PR #1331: M58a: Go file manager (full-viewport via tabapp)](https://github.com/drawmeanelephant/VirelaiOS/pull/1331) · Issues: [#1305](https://github.com/drawmeanelephant/VirelaiOS/issues/1305) · Labels: none
- [PR #1343: M59 (#1298): the boot default is the Go seat](https://github.com/drawmeanelephant/VirelaiOS/pull/1343) · Issues: [#1298](https://github.com/drawmeanelephant/VirelaiOS/issues/1298) · Labels: none
- [PR #1351: GOOS=virelai phase 2: real netpoll + fresh syscall/os/net + GONET.ELF (Closes #1350)](https://github.com/drawmeanelephant/VirelaiOS/pull/1351) · Issues: [#1350](https://github.com/drawmeanelephant/VirelaiOS/issues/1350) · Labels: none
- [PR #1363: M58c: Go terminal front-end (GOTERM.ELF) over the /dev/tty seam](https://github.com/drawmeanelephant/VirelaiOS/pull/1363) · Issues: [#1307](https://github.com/drawmeanelephant/VirelaiOS/issues/1307) · Labels: none

## M60–M65: app migration, self-test, and threads

The portfolio retired more Zig app binaries, added guest-run GOSELF receipts, and extended Go thread support with a 16-slot scheduler pool.

- [PR #1368: M60: no new Zig apps; delete leftover EDIT.BIN](https://github.com/drawmeanelephant/VirelaiOS/pull/1368) · Historical PR title; EDIT.BIN was deleted in this change. · Issues: [#1297](https://github.com/drawmeanelephant/VirelaiOS/issues/1297) · Labels: none
- [PR #1392: M61b (#1382): GOSELF.ELF — the guest runs the cases and writes the report](https://github.com/drawmeanelephant/VirelaiOS/pull/1392) · Issues: [#1382](https://github.com/drawmeanelephant/VirelaiOS/issues/1382) · Labels: none
- [PR #1411: M61f (#1386): share-equals / share-contains, and the go-selftest pilot](https://github.com/drawmeanelephant/VirelaiOS/pull/1411) · Issues: [#1386](https://github.com/drawmeanelephant/VirelaiOS/issues/1386) · Labels: none
- [PR #1427: M62h: delete CALC.BIN; Go seat hosts GOCALC.ELF](https://github.com/drawmeanelephant/VirelaiOS/pull/1427) · Historical PR title; CALC.BIN was deleted in this change. · Issues: [#1406](https://github.com/drawmeanelephant/VirelaiOS/issues/1406) · Labels: none
- [PR #1472: M65a: slot 73 sys_thread host proofs (create/exit/last-task-dies)](https://github.com/drawmeanelephant/VirelaiOS/pull/1472) · Issues: [#1439](https://github.com/drawmeanelephant/VirelaiOS/issues/1439) · Labels: none
- [PR #1475: M65d: grow max_tasks 13→16 for M:N seating](https://github.com/drawmeanelephant/VirelaiOS/pull/1475) · Issues: [#1442](https://github.com/drawmeanelephant/VirelaiOS/issues/1442) · Labels: none

## M66–M68: durable files, HTTPS, and GOSH

File semantics gained EL0 fsync and crash-safe settings; Go took HTTPS in-process and GOSH replaced the prior shell path.

- [PR #1479: M66a: file-semantics hardening — honest HF error rows, EL0 fsync, confirmed-count writes, GOSELF receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/1479) · Issues: [#1443](https://github.com/drawmeanelephant/VirelaiOS/issues/1443) · Labels: none
- [PR #1501: M67b+c: Go owns HTTPS in-process, pin go-fetch-https](https://github.com/drawmeanelephant/VirelaiOS/pull/1501) · Issues: [#1435](https://github.com/drawmeanelephant/VirelaiOS/issues/1435), [#1447](https://github.com/drawmeanelephant/VirelaiOS/issues/1447), [#1448](https://github.com/drawmeanelephant/VirelaiOS/issues/1448) · Labels: none
- [PR #1493: M68a: GOSH — the Go shell runs the M49 daily-use bar as a tab (exec, editing, history, vars, pipe, redirect, jobs/fg) — go-sh 3/3 (#1449)](https://github.com/drawmeanelephant/VirelaiOS/pull/1493) · Issues: [#1449](https://github.com/drawmeanelephant/VirelaiOS/issues/1449) · Labels: none
- [PR #1507: M68b: GOSH net handshake, retire SH.BIN](https://github.com/drawmeanelephant/VirelaiOS/pull/1507) · Historical PR title; SH.BIN was retired in this change. · Issues: [#1450](https://github.com/drawmeanelephant/VirelaiOS/issues/1450) · Labels: none

## M69–M70: dogfood and measured boundaries

The week included cross-process VZ restore evidence, a measured negative for JavaScript-in-WASM, the in-guest Go build loop, and default-seat dogfood/screenshot work.

- [PR #1490: M70g G3: restore beyond same-process — cross-process save/load + device verdicts (gpu/usb PASS, custom-virtio refused) (#1459)](https://github.com/drawmeanelephant/VirelaiOS/pull/1490) · Issues: [#1459](https://github.com/drawmeanelephant/VirelaiOS/issues/1459) · Labels: none
- [PR #1517: M70d: JS-in-WASM measurement is negative (closes #1456)](https://github.com/drawmeanelephant/VirelaiOS/pull/1517) · Issues: [#1456](https://github.com/drawmeanelephant/VirelaiOS/issues/1456) · Labels: none
- [PR #1552: M70c-S2 (#1544): the in-guest build loop, and the wall its third child hits](https://github.com/drawmeanelephant/VirelaiOS/pull/1552) · Issues: [#1544](https://github.com/drawmeanelephant/VirelaiOS/issues/1544) · Labels: none
- [PR #1553: M69a #1528: go-dogfood -- the default seat's daily-driver beat (GOSH+NOTE, then GOCALC+WEB)](https://github.com/drawmeanelephant/VirelaiOS/pull/1553) · Issues: [#1528](https://github.com/drawmeanelephant/VirelaiOS/issues/1528) · Labels: none
- [PR #1557: M69b: the screenshot corpus is five live captures, and the site/README stop describing August](https://github.com/drawmeanelephant/VirelaiOS/pull/1557) · Issues: [#1529](https://github.com/drawmeanelephant/VirelaiOS/issues/1529) · Labels: none
