---
title: 2026-W40 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W40: September 28–October 2, 33 selected PRs

## M82–M83: notifications, time, and keyboard input

Follow-ups completed notification history and authoring docs, persisted keyboard layouts, and exposed wall-clock, SNTP, timezone, and dead-key behavior.

- [PR #1830: M82d1: add seat-hosted notification center](https://github.com/drawmeanelephant/VirelaiOS/pull/1830) · Issues: [#1771](https://github.com/drawmeanelephant/VirelaiOS/issues/1771) · Labels: none
- [PR #1843: M82d2 (#1785): notifications do-not-disturb, and a history that survives a restart](https://github.com/drawmeanelephant/VirelaiOS/pull/1843) · Issues: [#1785](https://github.com/drawmeanelephant/VirelaiOS/issues/1785) · Labels: none
- [PR #1846: M82f (#1773): the app-platform chapters in the authoring guide](https://github.com/drawmeanelephant/VirelaiOS/pull/1846) · Issues: [#1773](https://github.com/drawmeanelephant/VirelaiOS/issues/1773) · Labels: none
- [PR #1831: M83d2: add persisted keyboard layouts](https://github.com/drawmeanelephant/VirelaiOS/pull/1831) · Issues: [#1786](https://github.com/drawmeanelephant/VirelaiOS/issues/1786) · Labels: none
- [PR #1838: M83a (#1774): expose existing wall clock to Go and GOSH](https://github.com/drawmeanelephant/VirelaiOS/pull/1838) · Issues: [#1774](https://github.com/drawmeanelephant/VirelaiOS/issues/1774) · Labels: none
- [PR #1842: M83b (#1775): SNTP sets the clock — time sync, slot 78, runner responder](https://github.com/drawmeanelephant/VirelaiOS/pull/1842) · Issues: [#1775](https://github.com/drawmeanelephant/VirelaiOS/issues/1775) · Labels: none
- [PR #1845: M83c (#1776): timezone setting + shared local formatting (fixed offsets first)](https://github.com/drawmeanelephant/VirelaiOS/pull/1845) · Issues: [#1776](https://github.com/drawmeanelephant/VirelaiOS/issues/1776) · Labels: none
- [PR #1839: M83e (#1778): dead keys, compose tables, and a staging buffer](https://github.com/drawmeanelephant/VirelaiOS/pull/1839) · Issues: [#1778](https://github.com/drawmeanelephant/VirelaiOS/issues/1778) · Labels: none

## M84: remote framebuffer closeout

M84 added the RFB wire codec and opt-in server, pinned viewer-failure cases, authenticated the macOS Screen Sharing bridge, and closed the over-cap connection path.

- [PR #1828: M84b: RFB wire codec and pinned vectors](https://github.com/drawmeanelephant/VirelaiOS/pull/1828) · Issues: [#1810](https://github.com/drawmeanelephant/VirelaiOS/issues/1810) · Labels: none
- [PR #1832: M84c: opt-in in-seat RFB server and hermetic live gate](https://github.com/drawmeanelephant/VirelaiOS/pull/1832) · Issues: [#1811](https://github.com/drawmeanelephant/VirelaiOS/issues/1811) · Labels: none
- [PR #1837: M84d: live-rfb negatives, viewer-death sweep, and the Screen Sharing tape script](https://github.com/drawmeanelephant/VirelaiOS/pull/1837) · Issues: [#1812](https://github.com/drawmeanelephant/VirelaiOS/issues/1812) · Labels: none
- [PR #1840: M84e: authenticate Screen Sharing at the host bridge](https://github.com/drawmeanelephant/VirelaiOS/pull/1840) · Issues: [#1835](https://github.com/drawmeanelephant/VirelaiOS/issues/1835) · Labels: none
- [PR #1844: M84f (#1836): refuse an over-cap SYN with RST+ACK in its own tx slot](https://github.com/drawmeanelephant/VirelaiOS/pull/1844) · Issues: [#1836](https://github.com/drawmeanelephant/VirelaiOS/issues/1836) · Labels: none

## M85–M86: sixel and UI drawing

The terminal gained cell-native sixel rendering and `imgcat`; the UI work added antialiased shapes and published canvas/dialog redraw support.

- [PR #1833: M85b (#1814): sixel decode + cell-native placement, pinned by corpus goldens](https://github.com/drawmeanelephant/VirelaiOS/pull/1833) · Issues: [#1814](https://github.com/drawmeanelephant/VirelaiOS/issues/1814) · Labels: none
- [PR #1834: M85c: imgcat and live terminal graphics pixels](https://github.com/drawmeanelephant/VirelaiOS/pull/1834) · Issues: [#1815](https://github.com/drawmeanelephant/VirelaiOS/issues/1815) · Labels: none
- [PR #1826: M86c: tiny AA shape renderer and goldens](https://github.com/drawmeanelephant/VirelaiOS/pull/1826) · Issues: [#1819](https://github.com/drawmeanelephant/VirelaiOS/issues/1819) · Labels: none
- [PR #1829: M86d: publish app chrome canvas and redraw appkit dialog](https://github.com/drawmeanelephant/VirelaiOS/pull/1829) · Issues: [#1820](https://github.com/drawmeanelephant/VirelaiOS/issues/1820) · Labels: none

## M87: first impressions and remote terminal

The desktop launcher and authenticated serial client landed, while M87d’s remote SSH-to-host path still lacks the distinct machine required for class-C acceptance.

- [PR #1861: M87a: make the default Go Apps launcher readable and pointer-first](https://github.com/drawmeanelephant/VirelaiOS/pull/1861) · Issues: [#1857](https://github.com/drawmeanelephant/VirelaiOS/issues/1857) · Labels: none
- [PR #1862: M87b: interactive authenticated serial client](https://github.com/drawmeanelephant/VirelaiOS/pull/1862) · Issues: [#1858](https://github.com/drawmeanelephant/VirelaiOS/issues/1858) · Labels: none
- [PR #1864: M87d (#1860): SSH-to-host remote terminal (class C blocked)](https://github.com/drawmeanelephant/VirelaiOS/pull/1864) · Issues: [#1860](https://github.com/drawmeanelephant/VirelaiOS/issues/1860) · Labels: none

## Bounded native Zig guest target: SDK and file contracts

Merged work accepted a bounded Zig guest target and added SDK, standard-stream, directory, metadata-containment, and staged-replacement pieces.

- [PR #1882: R1 (#1879): accept the bounded Zig guest target](https://github.com/drawmeanelephant/VirelaiOS/pull/1882) · Issues: [#1879](https://github.com/drawmeanelephant/VirelaiOS/issues/1879) · Labels: none
- [PR #1883: A2 (#1866): establish the pinned native Zig guest SDK](https://github.com/drawmeanelephant/VirelaiOS/pull/1883) · Issues: [#1866](https://github.com/drawmeanelephant/VirelaiOS/issues/1866) · Labels: none
- [PR #1885: A3 (#1867): own the Zig 0.16 native std boundary](https://github.com/drawmeanelephant/VirelaiOS/pull/1885) · Issues: [#1867](https://github.com/drawmeanelephant/VirelaiOS/issues/1867) · Labels: none
- [PR #1886: B1 (#1868): process-scoped standard streams and EOF](https://github.com/drawmeanelephant/VirelaiOS/pull/1886) · Issues: [#1868](https://github.com/drawmeanelephant/VirelaiOS/issues/1868) · Labels: none
- [PR #1888: B2 (#1869): complete, lossless directory enumeration](https://github.com/drawmeanelephant/VirelaiOS/pull/1888) · Issues: [#1869](https://github.com/drawmeanelephant/VirelaiOS/issues/1869) · Labels: none
- [PR #1884: B3 (#1870): add isolated filesystem metadata and no-follow containment core](https://github.com/drawmeanelephant/VirelaiOS/pull/1884) · Issues: [#1870](https://github.com/drawmeanelephant/VirelaiOS/issues/1870) · Labels: none
- [PR #1887: B4 (#1871): support safe staged replacement on both backends](https://github.com/drawmeanelephant/VirelaiOS/pull/1887) · Issues: [#1871](https://github.com/drawmeanelephant/VirelaiOS/issues/1871) · Labels: none
- [PR #1893: B3 (#1870): wire live metadata and pinned containment](https://github.com/drawmeanelephant/VirelaiOS/pull/1893) · Issues: [#1870](https://github.com/drawmeanelephant/VirelaiOS/issues/1870) · Labels: none

## Bounded native Zig guest target: workloads and limits

The week added Oliver, k4o, and fart-synth workload ports; the serial offline Boris effort reported its remaining blocker rather than claiming full closure.

- [PR #1891: C1 (#1875): refresh Oliver native proof and stage render/meta CLI](https://github.com/drawmeanelephant/VirelaiOS/pull/1891) · Issues: [#1875](https://github.com/drawmeanelephant/VirelaiOS/issues/1875) · Labels: none
- [PR #1890: C2 (#1876): port the bounded k4o render CLI](https://github.com/drawmeanelephant/VirelaiOS/pull/1890) · Issues: [#1876](https://github.com/drawmeanelephant/VirelaiOS/issues/1876) · Labels: none
- [PR #1889: C4 (#1878): port the actual fart-app synthesizer](https://github.com/drawmeanelephant/VirelaiOS/pull/1889) · Issues: [#1878](https://github.com/drawmeanelephant/VirelaiOS/issues/1878) · Labels: none
- [PR #1892: C3 (#1877): prove serial Boris no-libc closure and report blockers](https://github.com/drawmeanelephant/VirelaiOS/pull/1892) · Issues: [#1877](https://github.com/drawmeanelephant/VirelaiOS/issues/1877) · Labels: none

## KRN2: linked-ELF relocation fix

KRN2 repaired collection of absolute relocations in linked ELF files.

- [PR #1899: KRN2 (#1898): repair linked-ELF absolute relocation collection](https://github.com/drawmeanelephant/VirelaiOS/pull/1899) · Issues: [#1898](https://github.com/drawmeanelephant/VirelaiOS/issues/1898) · Labels: none
