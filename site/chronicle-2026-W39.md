---
title: 2026-W39 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W39: September 21–27, 38 selected PRs

## M71: seat honesty and app succession

M71 moved more shipped desktop functions to their Go successors and corrected the seat/scanout evidence to match the current default.

- [PR #1585: M71k (#1570): FETCHS.BIN retires; live-tls13 probe moves to live-web boot 12](https://github.com/drawmeanelephant/VirelaiOS/pull/1585) · Historical PR title; FETCHS.BIN was retired. · Issues: [#1570](https://github.com/drawmeanelephant/VirelaiOS/issues/1570) · Labels: none
- [PR #1588: M71l (#1571): GOHTTPD.ELF serves the share; HTTPD.BIN retires](https://github.com/drawmeanelephant/VirelaiOS/pull/1588) · Historical PR title; HTTPD.BIN was retired. · Issues: [#1571](https://github.com/drawmeanelephant/VirelaiOS/issues/1571) · Labels: none
- [PR #1591: M71b (#1561): a seated scanout carries the seat's chrome, not kernel console ink](https://github.com/drawmeanelephant/VirelaiOS/pull/1591) · Issues: [#1561](https://github.com/drawmeanelephant/VirelaiOS/issues/1561) · Labels: none
- [PR #1606: M71f (#1565): Go settings panel GOSET.ELF; SETTINGS.BIN deleted](https://github.com/drawmeanelephant/VirelaiOS/pull/1606) · Issues: [#1565](https://github.com/drawmeanelephant/VirelaiOS/issues/1565) · Labels: none
- [PR #1618: M71n (#1573): retire Zig PING.BIN for Go GOPING.ELF, and name the net CLIs in GOSH help](https://github.com/drawmeanelephant/VirelaiOS/pull/1618) · Historical PR title; PING.BIN was retired. · Issues: [#1573](https://github.com/drawmeanelephant/VirelaiOS/issues/1573) · Labels: none

## M72: Charm TUI in a bound window

M72 split initialized-image and mapped-memory limits, then ran Bubble Tea through the window terminal and captured the real scanout.

- [PR #1587: M72a (#1579): split the exec bound — load_max on initialized bytes, map_max on mapped](https://github.com/drawmeanelephant/VirelaiOS/pull/1587) · Issues: [#1579](https://github.com/drawmeanelephant/VirelaiOS/issues/1579) · Labels: none
- [PR #1593: M72b: add bounded ANSI window tty presentation](https://github.com/drawmeanelephant/VirelaiOS/pull/1593) · Issues: [#1580](https://github.com/drawmeanelephant/VirelaiOS/issues/1580) · Labels: none
- [PR #1594: M72c: run Bubble Tea on the bound window tty](https://github.com/drawmeanelephant/VirelaiOS/pull/1594) · Issues: [#1581](https://github.com/drawmeanelephant/VirelaiOS/issues/1581) · Labels: none
- [PR #1595: M72d: add host Charm scanout tape](https://github.com/drawmeanelephant/VirelaiOS/pull/1595) · Issues: [#1582](https://github.com/drawmeanelephant/VirelaiOS/issues/1582) · Labels: none

## M73–M74: terminal depth and Go utilities

The terminal gained a reusable shell core, mouse reporting, scrollback/search and theming; Go added a Git TUI and help browser.

- [PR #1647: M73b #1626: extract the GOSH shell core into importable package user/go/shlib](https://github.com/drawmeanelephant/VirelaiOS/pull/1647) · Issues: [#1626](https://github.com/drawmeanelephant/VirelaiOS/issues/1626) · Labels: none
- [PR #1650: M73c #1627: GOTERM runs a real gosh session through virelai/shlib](https://github.com/drawmeanelephant/VirelaiOS/pull/1650) · Issues: [#1627](https://github.com/drawmeanelephant/VirelaiOS/issues/1627) · Labels: none
- [PR #1656: M73i: mouse reporting into the bound tty — ?1000/1002/1003/1006, selection precedence, wheel chord (#1635)](https://github.com/drawmeanelephant/VirelaiOS/pull/1656) · Issues: [#1635](https://github.com/drawmeanelephant/VirelaiOS/issues/1635) · Labels: none
- [PR #1659: M73k: scrollback depth + Ctrl+Shift+F search (#1637)](https://github.com/drawmeanelephant/VirelaiOS/pull/1659) · Issues: [#1637](https://github.com/drawmeanelephant/VirelaiOS/issues/1637) · Labels: none
- [PR #1664: M74c: GOHELP.ELF — Bubble Tea help browser, gate go-help (#1646)](https://github.com/drawmeanelephant/VirelaiOS/pull/1664) · Issues: [#1646](https://github.com/drawmeanelephant/VirelaiOS/issues/1646) · Labels: none
- [PR #1665: M74b #1645: in-guest Git TUI over a local .git reader (GOGITUI.ELF + gitread)](https://github.com/drawmeanelephant/VirelaiOS/pull/1665) · Issues: [#1645](https://github.com/drawmeanelephant/VirelaiOS/issues/1645) · Labels: none
- [PR #1667: M73m (#1662): palette theming — the user chooses the colours](https://github.com/drawmeanelephant/VirelaiOS/pull/1667) · Issues: [#1662](https://github.com/drawmeanelephant/VirelaiOS/issues/1662) · Labels: none

## M75–M78: site truth, first boot, and retirement

M75 added the site truth-up and docs-gate guard, M76 pinned the first-boot handoff, and M78 retired obsolete Zig user programs.

- [PR #1686: M75: docs & site truth-up — site sweep, archive ballast, docs-gate truth guard](https://github.com/drawmeanelephant/VirelaiOS/pull/1686) · Issues: [#1670](https://github.com/drawmeanelephant/VirelaiOS/issues/1670), [#1671](https://github.com/drawmeanelephant/VirelaiOS/issues/1671), [#1672](https://github.com/drawmeanelephant/VirelaiOS/issues/1672) · Labels: none
- [PR #1687: M76a: splash-to-seat handoff with an honest bounded timeout](https://github.com/drawmeanelephant/VirelaiOS/pull/1687) · Issues: [#1674](https://github.com/drawmeanelephant/VirelaiOS/issues/1674) · Labels: none
- [PR #1693: M76d: pin first-boot splash-to-shell chain](https://github.com/drawmeanelephant/VirelaiOS/pull/1693) · Issues: [#1677](https://github.com/drawmeanelephant/VirelaiOS/issues/1677) · Labels: none
- [PR #1702: M78d: retire obsolete Zig user programs](https://github.com/drawmeanelephant/VirelaiOS/pull/1702) · Issues: [#1685](https://github.com/drawmeanelephant/VirelaiOS/issues/1685) · Labels: none

## M79–M80: navigation, sessions, and terminal controls

The Go seat gained back/forward navigation, persisted sessions and notifications; terminal follow-ups added font zoom and reset/scrollback chords.

- [PR #1758: M79e (#1708): the Go seat honours nav — kinds 9/10, a back/forward chord, and the first live adopter](https://github.com/drawmeanelephant/VirelaiOS/pull/1758) · Issues: [#1708](https://github.com/drawmeanelephant/VirelaiOS/issues/1708) · Labels: none
- [PR #1759: M79g (#1718): SESSION.TABS records what the user did](https://github.com/drawmeanelephant/VirelaiOS/pull/1759) · Issues: [#1718](https://github.com/drawmeanelephant/VirelaiOS/issues/1718) · Labels: none
- [PR #1784: M79k (#1720): the notify seam — WM_RPC kind 12, a bounded toast queue, GOFILES as the adopter](https://github.com/drawmeanelephant/VirelaiOS/pull/1784) · Issues: [#1720](https://github.com/drawmeanelephant/VirelaiOS/issues/1720) · Labels: none
- [PR #1791: M80i (#1725): font zoom — the bound grid leaves 8×16](https://github.com/drawmeanelephant/VirelaiOS/pull/1791) · Issues: [#1725](https://github.com/drawmeanelephant/VirelaiOS/issues/1725) · Labels: none
- [PR #1756: M80k (#1727): the terminal hygiene chords — clear scrollback, soft reset, full RIS](https://github.com/drawmeanelephant/VirelaiOS/pull/1756) · Issues: [#1727](https://github.com/drawmeanelephant/VirelaiOS/issues/1727) · Labels: none

## M81: file-platform behavior

M81 added crash-safe publication, a shared MIME table, portable snapshots, recoverable deletion, and file watching.

- [PR #1789: M81e1 (#1765): publish git, browser-store and calc writes crash-safe](https://github.com/drawmeanelephant/VirelaiOS/pull/1789) · Issues: [#1765](https://github.com/drawmeanelephant/VirelaiOS/issues/1765) · Labels: none
- [PR #1792: M81b (#1762): one mime table, and GOFILES stops guessing what a file is](https://github.com/drawmeanelephant/VirelaiOS/pull/1792) · Issues: [#1762](https://github.com/drawmeanelephant/VirelaiOS/issues/1762) · Labels: none
- [PR #1798: M81g (#1767): a snapshot bundle you can carry, and the drill that restores it](https://github.com/drawmeanelephant/VirelaiOS/pull/1798) · Issues: [#1767](https://github.com/drawmeanelephant/VirelaiOS/issues/1767) · Labels: none
- [PR #1804: M81a: make file deletes recoverable](https://github.com/drawmeanelephant/VirelaiOS/pull/1804) · Issues: [#1761](https://github.com/drawmeanelephant/VirelaiOS/issues/1761) · Labels: none
- [PR #1822: M81f (#1766): file watching — probe, vi subscribe seam, GOFILES on the feed](https://github.com/drawmeanelephant/VirelaiOS/pull/1822) · Issues: [#1766](https://github.com/drawmeanelephant/VirelaiOS/issues/1766) · Labels: none

## M82–M83: app services and restorable files

The app platform gained a versioned manifest, settings pub/sub and global shortcuts; standard VirtioFS/FUSE supplied the opt-in restorable `/host` transport.

- [PR #1793: M82a (#1768): a versioned, additive APPS.TXT — and the three readers stop truncating it](https://github.com/drawmeanelephant/VirelaiOS/pull/1793) · Issues: [#1768](https://github.com/drawmeanelephant/VirelaiOS/issues/1768) · Labels: none
- [PR #1799: M82c (#1770): global shortcuts registry — one owner per chord](https://github.com/drawmeanelephant/VirelaiOS/pull/1799) · Issues: [#1770](https://github.com/drawmeanelephant/VirelaiOS/issues/1770) · Labels: none
- [PR #1801: M82b (#1769): settings pub/sub reaches live apps](https://github.com/drawmeanelephant/VirelaiOS/pull/1801) · Issues: [#1769](https://github.com/drawmeanelephant/VirelaiOS/issues/1769) · Labels: none
- [PR #1805: M83g (#1780): use VirtioFS for restorable host files](https://github.com/drawmeanelephant/VirelaiOS/pull/1805) · Issues: [#1780](https://github.com/drawmeanelephant/VirelaiOS/issues/1780) · Labels: none

## M84–M86: remote display, terminal images, UI primitives

The week began the remote-framebuffer design, sixel image intake, and the public UI-primitives contract with its chrome glyph set.

- [PR #1821: M84a (#1809): remote-display ADR — seams measured, serving shape, auth posture](https://github.com/drawmeanelephant/VirelaiOS/pull/1821) · Issues: [#1809](https://github.com/drawmeanelephant/VirelaiOS/issues/1809) · Labels: none
- [PR #1824: M85a (#1813): image-sequence intake — DCS/sixel capture, sixel picked primary](https://github.com/drawmeanelephant/VirelaiOS/pull/1824) · Issues: [#1813](https://github.com/drawmeanelephant/VirelaiOS/issues/1813) · Labels: none
- [PR #1823: M86a (#1817): the UI primitives contract — design language, API, glyphs](https://github.com/drawmeanelephant/VirelaiOS/pull/1823) · Issues: [#1817](https://github.com/drawmeanelephant/VirelaiOS/issues/1817) · Labels: none
- [PR #1827: M86b (#1818): ship the twenty chrome glyphs](https://github.com/drawmeanelephant/VirelaiOS/pull/1827) · Issues: [#1818](https://github.com/drawmeanelephant/VirelaiOS/issues/1818) · Labels: none
