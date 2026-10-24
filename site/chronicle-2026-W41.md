---
title: 2026-W41 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W41: October 3–11, 99 selected PRs

This page's range opens on October 3 — the Saturday/Sunday tail of ISO W40 that the previous chronicle's corpus cut off at October 2 — and runs through Sunday, October 11. 110 PRs merged in the range; the unlisted ones are status-row and card-drafting bookkeeping.

## Bounded native Zig guest target: closeout

The bounded-Zig arc landed its last cards: publication primitives, TLS threads, owned sockets and DNS, continuous PCM playback, and the serial-offline Boris port.

- [PR #1906: B5 (#1895): pinned-directory publication primitives and SDK bridge](https://github.com/drawmeanelephant/VirelaiOS/pull/1906) · Issues: [#1895](https://github.com/drawmeanelephant/VirelaiOS/issues/1895) · Labels: none
- [PR #1907: Static TLS preparation: bounded layout and pinned compiler regressions](https://github.com/drawmeanelephant/VirelaiOS/pull/1907) · Issues: none · Labels: none
- [PR #1908: B7 (#1874): activate continuous bounded PCM playback](https://github.com/drawmeanelephant/VirelaiOS/pull/1908) · Issues: [#1874](https://github.com/drawmeanelephant/VirelaiOS/issues/1874) · Labels: none
- [PR #1909: B5 (#1872): bounded native Zig TLS threads and coarse futex waits](https://github.com/drawmeanelephant/VirelaiOS/pull/1909) · Issues: [#1872](https://github.com/drawmeanelephant/VirelaiOS/issues/1872) · Labels: none
- [PR #1910: B6 (#1873): complete owned native preview sockets and DNS](https://github.com/drawmeanelephant/VirelaiOS/pull/1910) · Issues: [#1873](https://github.com/drawmeanelephant/VirelaiOS/issues/1873) · Labels: none
- [PR #1911: C3 (#1877): ship serial-offline Boris with native publication](https://github.com/drawmeanelephant/VirelaiOS/pull/1911) · Issues: [#1877](https://github.com/drawmeanelephant/VirelaiOS/issues/1877) · Labels: none

## M88–M91: QuickJS, PDF, vector, and the supported host

Four milestone families landed in one burst. M88 built a bounded offline QuickJS runtime and product but reported blocked strict guest acceptance; M89 shipped the bounded Go PDF engine with CoreGraphics as the oracle after ADR 0040 A2 replaced Poppler — and the week also banned GPL-family code outright; M90 landed the bounded Go SVG/vector engine with kernel page and mmap receipts; M91 gave the macOS host a supported front door, lifecycle, HID completions, and desktop-readiness polish, closing the M87 desktop acceptance along the way.

- [PR #1912: M87d (#1860): reconcile accepted remote-terminal evidence](https://github.com/drawmeanelephant/VirelaiOS/pull/1912) · Issues: [#1860](https://github.com/drawmeanelephant/VirelaiOS/issues/1860) · Labels: none
- [PR #1927: M90a (#1919): choose Go SVG fills and publish vector contract](https://github.com/drawmeanelephant/VirelaiOS/pull/1927) · Issues: [#1919](https://github.com/drawmeanelephant/VirelaiOS/issues/1919) · Labels: none
- [PR #1928: M88a (#1921): design bounded standalone QuickJS runtime](https://github.com/drawmeanelephant/VirelaiOS/pull/1928) · Issues: [#1921](https://github.com/drawmeanelephant/VirelaiOS/issues/1921) · Labels: none
- [PR #1929: M91a: supported macOS host front door and VM lifecycle](https://github.com/drawmeanelephant/VirelaiOS/pull/1929) · Issues: [#1922](https://github.com/drawmeanelephant/VirelaiOS/issues/1922) · Labels: none
- [PR #1931: M91c: recognizable cursor and exclusive completed-frame presentation](https://github.com/drawmeanelephant/VirelaiOS/pull/1931) · Issues: [#1924](https://github.com/drawmeanelephant/VirelaiOS/issues/1924) · Labels: none
- [PR #1932: M91b (#1923): preserve shared HID completions and native report formats](https://github.com/drawmeanelephant/VirelaiOS/pull/1932) · Issues: [#1923](https://github.com/drawmeanelephant/VirelaiOS/issues/1923) · Labels: none
- [PR #1933: Docs: record owner approval of ADR 0039 and A2; mark ADR 0041 accepted](https://github.com/drawmeanelephant/VirelaiOS/pull/1933) · Issues: none · Labels: none
- [PR #1936: M89a (#1920): design the bounded Go PDF subset](https://github.com/drawmeanelephant/VirelaiOS/pull/1936) · Issues: [#1920](https://github.com/drawmeanelephant/VirelaiOS/issues/1920) · Labels: none
- [PR #1937: M90b: bounded Go SVG and vector engine (runtime proof blocked)](https://github.com/drawmeanelephant/VirelaiOS/pull/1937) · Issues: [#1935](https://github.com/drawmeanelephant/VirelaiOS/issues/1935) · Labels: none
- [PR #1938: M91d: source-fresh default desktop workflow readiness](https://github.com/drawmeanelephant/VirelaiOS/pull/1938) · Issues: [#1925](https://github.com/drawmeanelephant/VirelaiOS/issues/1925) · Labels: none
- [PR #1942: M88b (#1934): complete offline native QuickJS runtime](https://github.com/drawmeanelephant/VirelaiOS/pull/1942) · Issues: [#1934](https://github.com/drawmeanelephant/VirelaiOS/issues/1934) · Labels: none
- [PR #1943: M90d (#1939): kernel peak page and mmap-region receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/1943) · Issues: [#1939](https://github.com/drawmeanelephant/VirelaiOS/issues/1939) · Labels: none
- [PR #1944: M89b: complete bounded Go PDF engine](https://github.com/drawmeanelephant/VirelaiOS/pull/1944) · Issues: [#1940](https://github.com/drawmeanelephant/VirelaiOS/issues/1940) · Labels: none
- [PR #1951: M90e (#1947): fix SVG exclusion classification](https://github.com/drawmeanelephant/VirelaiOS/pull/1951) · Issues: [#1947](https://github.com/drawmeanelephant/VirelaiOS/issues/1947) · Labels: none
- [PR #1953: M90f (#1945): reclaim every demand-backed process page](https://github.com/drawmeanelephant/VirelaiOS/pull/1953) · Issues: [#1945](https://github.com/drawmeanelephant/VirelaiOS/issues/1945) · Labels: none
- [PR #1954: M89c: PDF source transport and M89-PDF1 acceptance harness](https://github.com/drawmeanelephant/VirelaiOS/pull/1954) · Issues: [#1950](https://github.com/drawmeanelephant/VirelaiOS/issues/1950) · Labels: none
- [PR #1955: M88c (#1946): QJS product and blocked strict guest acceptance](https://github.com/drawmeanelephant/VirelaiOS/pull/1955) · Issues: [#1946](https://github.com/drawmeanelephant/VirelaiOS/issues/1946) · Labels: none
- [PR #1960: M89d (#1959): fix PDF acceptance engine defects](https://github.com/drawmeanelephant/VirelaiOS/pull/1960) · Issues: [#1959](https://github.com/drawmeanelephant/VirelaiOS/issues/1959) · Labels: none
- [PR #1963: QuickJS (#1956): preserve usable slack across bridge realloc](https://github.com/drawmeanelephant/VirelaiOS/pull/1963) · Issues: [#1956](https://github.com/drawmeanelephant/VirelaiOS/issues/1956) · Labels: none
- [PR #1964: Docs: ADR 0040 A1, oracle rulings and stream-boundary tokens](https://github.com/drawmeanelephant/VirelaiOS/pull/1964) · Issues: none · Labels: none
- [PR #1966: M90c (#1941): independent SVG and vector acceptance](https://github.com/drawmeanelephant/VirelaiOS/pull/1966) · Issues: [#1941](https://github.com/drawmeanelephant/VirelaiOS/issues/1941) · Labels: none
- [PR #1967: M89d (#1959): enforce A1.1 PDF stream token boundaries](https://github.com/drawmeanelephant/VirelaiOS/pull/1967) · Issues: [#1959](https://github.com/drawmeanelephant/VirelaiOS/issues/1959) · Labels: none
- [PR #1970: Docs: ban GPL-family code; ADR 0040 A2 replaces Poppler with CoreGraphics](https://github.com/drawmeanelephant/VirelaiOS/pull/1970) · Issues: none · Labels: none
- [PR #1975: M89e (#1971): CoreGraphics references and live runtime receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/1975) · Issues: [#1971](https://github.com/drawmeanelephant/VirelaiOS/issues/1971) · Labels: none
- [PR #1979: M91f (#1976): remove kernel chrome from seat-hosted tabs](https://github.com/drawmeanelephant/VirelaiOS/pull/1979) · Issues: [#1976](https://github.com/drawmeanelephant/VirelaiOS/issues/1976) · Labels: none
- [PR #1980: M91e (#1973): align console-ink with the first-boot workspace](https://github.com/drawmeanelephant/VirelaiOS/pull/1980) · Issues: [#1973](https://github.com/drawmeanelephant/VirelaiOS/issues/1973) · Labels: none

## M92: manifest-driven services

INIT.ELF became the boot path: declarative service manifests, deterministic dependency order, supervised restarts with jittered retries, live settings with fail-closed reload, and a pinned mid-boot kill recovery.

- [PR #2018: Svcgraph: resolve service dependencies deterministically (M92b)](https://github.com/drawmeanelephant/VirelaiOS/pull/2018) · Issues: [#1987](https://github.com/drawmeanelephant/VirelaiOS/issues/1987) · Labels: none
- [PR #2019: M92a: Service manifests, validator and ADR 0042](https://github.com/drawmeanelephant/VirelaiOS/pull/2019) · Issues: [#1986](https://github.com/drawmeanelephant/VirelaiOS/issues/1986) · Labels: none
- [PR #2023: M92c (#1988): supervise flat services with jittered retries and receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/2023) · Issues: [#1988](https://github.com/drawmeanelephant/VirelaiOS/issues/1988) · Labels: none
- [PR #2028: M92c fixup: align svfixture healthy-evidence pids](https://github.com/drawmeanelephant/VirelaiOS/pull/2028) · Issues: [#2027](https://github.com/drawmeanelephant/VirelaiOS/issues/2027) · Labels: none
- [PR #2032: M92e: boot through INIT.ELF with single-seat fallback](https://github.com/drawmeanelephant/VirelaiOS/pull/2032) · Issues: [#1989](https://github.com/drawmeanelephant/VirelaiOS/issues/1989) · Labels: none
- [PR #2036: M92f: recover retryable mid-boot kills and pin acceptance](https://github.com/drawmeanelephant/VirelaiOS/pull/2036) · Issues: [#1991](https://github.com/drawmeanelephant/VirelaiOS/issues/1991) · Labels: none
- [PR #2037: M92d: live service settings and fail-closed init reload](https://github.com/drawmeanelephant/VirelaiOS/pull/2037) · Issues: [#1990](https://github.com/drawmeanelephant/VirelaiOS/issues/1990) · Labels: none
- [PR #2043: Monitor: hold service locks through handlers](https://github.com/drawmeanelephant/VirelaiOS/pull/2043) · Issues: [#2042](https://github.com/drawmeanelephant/VirelaiOS/issues/2042) · Labels: none

## M93: the bounded CSS browser

The Dillo-tier browser arc froze its CSS subset, hardened and fuzzed the HTML parser, added cascade, box/block/flex layout, painted boxes and bounded PNG/SVG images, then integrated the browser behind outbound TCP connect and production trust with an exact-golden live-web comparator at 36/36.

- [PR #2017: M93a (R1): freeze the browser CSS subset and pipeline contracts](https://github.com/drawmeanelephant/VirelaiOS/pull/2017) · Issues: [#1993](https://github.com/drawmeanelephant/VirelaiOS/issues/1993) · Labels: none
- [PR #2021: M93c: add bounded CSS subset parser and cascade](https://github.com/drawmeanelephant/VirelaiOS/pull/2021) · Issues: [#1995](https://github.com/drawmeanelephant/VirelaiOS/issues/1995) · Labels: none
- [PR #2024: M93d: add bounded box, block and flex layout](https://github.com/drawmeanelephant/VirelaiOS/pull/2024) · Issues: [#1996](https://github.com/drawmeanelephant/VirelaiOS/issues/1996) · Labels: none
- [PR #2031: M93b: harden the HTML parser and fuzz its invariants](https://github.com/drawmeanelephant/VirelaiOS/pull/2031) · Issues: [#1994](https://github.com/drawmeanelephant/VirelaiOS/issues/1994) · Labels: none
- [PR #2033: M93e: paint CSS boxes and decode bounded PNG/SVG images](https://github.com/drawmeanelephant/VirelaiOS/pull/2033) · Issues: [#1997](https://github.com/drawmeanelephant/VirelaiOS/issues/1997) · Labels: none
- [PR #2041: Netconnect: bounded outbound TCP connect (M93f prerequisite)](https://github.com/drawmeanelephant/VirelaiOS/pull/2041) · Issues: [#2040](https://github.com/drawmeanelephant/VirelaiOS/issues/2040) · Labels: none
- [PR #2045: M93f: integrate bounded CSS browser and production trust](https://github.com/drawmeanelephant/VirelaiOS/pull/2045) · Issues: [#1998](https://github.com/drawmeanelephant/VirelaiOS/issues/1998) · Labels: none
- [PR #2067: M93g: in-guest reference renders, exact-golden comparator, live-web 36/36](https://github.com/drawmeanelephant/VirelaiOS/pull/2067) · Issues: [#1999](https://github.com/drawmeanelephant/VirelaiOS/issues/1999) · Labels: none

## M94: observability

The observability ABI froze, then grew a syscall tracer, a bounded IRQ/Go profiler, a heap analyzer with leak detection, crash-receipt viewing, a streaming per-app log viewer, and a single acceptance gate holding all of it inside the overhead budget.

- [PR #2020: M94f: add streaming per-app log viewer](https://github.com/drawmeanelephant/VirelaiOS/pull/2020) · Issues: [#2002](https://github.com/drawmeanelephant/VirelaiOS/issues/2002) · Labels: none
- [PR #2022: M94a: freeze observability ABI and split ENOSYS stubs (R1)](https://github.com/drawmeanelephant/VirelaiOS/pull/2022) · Issues: [#2001](https://github.com/drawmeanelephant/VirelaiOS/issues/2001) · Labels: none
- [PR #2025: M94b: bounded syscall tracer and STRACE](https://github.com/drawmeanelephant/VirelaiOS/pull/2025) · Issues: [#2003](https://github.com/drawmeanelephant/VirelaiOS/issues/2003) · Labels: none
- [PR #2026: M94c: add bounded IRQ sampling and Go profiler](https://github.com/drawmeanelephant/VirelaiOS/pull/2026) · Issues: [#2004](https://github.com/drawmeanelephant/VirelaiOS/issues/2004) · Labels: none
- [PR #2038: M94e: crash receipt viewer and traced reopening](https://github.com/drawmeanelephant/VirelaiOS/pull/2038) · Issues: [#2006](https://github.com/drawmeanelephant/VirelaiOS/issues/2006) · Labels: none
- [PR #2039: M94d: bounded heap analyzer and leak detection](https://github.com/drawmeanelephant/VirelaiOS/pull/2039) · Issues: [#2005](https://github.com/drawmeanelephant/VirelaiOS/issues/2005) · Labels: none
- [PR #2055: M94g: gate trace + profile + heap on one real app session inside the overhead budget](https://github.com/drawmeanelephant/VirelaiOS/pull/2055) · Issues: [#2007](https://github.com/drawmeanelephant/VirelaiOS/issues/2007) · Labels: none

## M95: Gostalgia as a guest

The pinned unmodified Gostalgia build was audited, adapted through gsport overlays, run as supervised EL0 processes on a bound window tty with confined /host documents, packaged as GOSTALGIA.ELF, and closed by the run-08 acceptance journey.

- [PR #2030: M95a: audit the pinned unmodified Gostalgia build](https://github.com/drawmeanelephant/VirelaiOS/pull/2030) · Issues: [#2009](https://github.com/drawmeanelephant/VirelaiOS/issues/2009) · Labels: none
- [PR #2056: M95b: gsport adapters + overlay mechanism for the pinned Gostalgia guest](https://github.com/drawmeanelephant/VirelaiOS/pull/2056) · Issues: [#2010](https://github.com/drawmeanelephant/VirelaiOS/issues/2010) · Labels: none
- [PR #2057: M95d — VFS on /host: confined documents, recoverable saves, byte-identical on both transports](https://github.com/drawmeanelephant/VirelaiOS/pull/2057) · Issues: [#2012](https://github.com/drawmeanelephant/VirelaiOS/issues/2012) · Labels: none
- [PR #2058: M95e: Gostalgia children as EL0 processes — bounded capture, receipts, M92c supervision](https://github.com/drawmeanelephant/VirelaiOS/pull/2058) · Issues: [#2013](https://github.com/drawmeanelephant/VirelaiOS/issues/2013) · Labels: none
- [PR #2059: M95c — Gostalgia's Bubble Tea v1 on a bound GOTABWM window tty](https://github.com/drawmeanelephant/VirelaiOS/pull/2059) · Issues: [#2011](https://github.com/drawmeanelephant/VirelaiOS/issues/2011) · Labels: none
- [PR #2068: M95f: package GOSTALGIA.ELF — APPS.TXT v2 row, static-image/notices asserts, launcher+exit gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2068) · Issues: [#2014](https://github.com/drawmeanelephant/VirelaiOS/issues/2014) · Labels: none
- [PR #2120: M95g (#2015): go-gostalgia run 08 — the acceptance journey gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2120) · Issues: [#2015](https://github.com/drawmeanelephant/VirelaiOS/issues/2015) · Labels: none

## M96: Zig 0.17 flag day

The toolchain port went from recon and a dual-clean prep sweep to the pinned 0.17.0 flag-day commit and a K=3 acceptance on main.

- [PR #2139: M96a: Zig 0.17 recon — break table + deterministic audit runner](https://github.com/drawmeanelephant/VirelaiOS/pull/2139) · Issues: [#2061](https://github.com/drawmeanelephant/VirelaiOS/issues/2061) · Labels: none
- [PR #2140: M96b zig 0.17 dual-clean prep: ** -> @splat, meta.Int -> @Int, typeInfo fields -> meta.tags](https://github.com/drawmeanelephant/VirelaiOS/pull/2140) · Issues: [#2062](https://github.com/drawmeanelephant/VirelaiOS/issues/2062) · Labels: none
- [PR #2141: M96c zig 0.17.0 flag day: pin 0.17.0 + port the exclusive breaks](https://github.com/drawmeanelephant/VirelaiOS/pull/2141) · Issues: [#2063](https://github.com/drawmeanelephant/VirelaiOS/issues/2063) · Labels: none
- [PR #2142: M96e: Zig 0.17 acceptance — K=3 green on main + contributor docs](https://github.com/drawmeanelephant/VirelaiOS/pull/2142) · Issues: [#2065](https://github.com/drawmeanelephant/VirelaiOS/issues/2065) · Labels: none

## M97: the security-audit fixes

The consolidated audit findings landed as fix lanes: syscall and memory-management repairs, uaccess read-side proofs, ARP/DHCP/DNS and TCP trust bounds, virtio file-channel bounds, ELF/process-lifecycle fixes, and Go userspace hardening across WM_RPC auth, cookies, framing, secrets, TLS, and ttf.

- [PR #2117: M97f/M97g Go userspace security fixes (WM_RPC auth, cookies, framing, secrets, TLS, ttf)](https://github.com/drawmeanelephant/VirelaiOS/pull/2117) · Issues: [#2080](https://github.com/drawmeanelephant/VirelaiOS/issues/2080), [#2091](https://github.com/drawmeanelephant/VirelaiOS/issues/2091), [#2105](https://github.com/drawmeanelephant/VirelaiOS/issues/2105), [#2106](https://github.com/drawmeanelephant/VirelaiOS/issues/2106), [#2107](https://github.com/drawmeanelephant/VirelaiOS/issues/2107), [#2108](https://github.com/drawmeanelephant/VirelaiOS/issues/2108) · Labels: none
- [PR #2118: M97 VF lane: bound host-controlled virtio file-channel inputs (#2087 #2088 #2089 #2090 #2100)](https://github.com/drawmeanelephant/VirelaiOS/pull/2118) · Issues: [#2087](https://github.com/drawmeanelephant/VirelaiOS/issues/2087), [#2088](https://github.com/drawmeanelephant/VirelaiOS/issues/2088), [#2089](https://github.com/drawmeanelephant/VirelaiOS/issues/2089), [#2090](https://github.com/drawmeanelephant/VirelaiOS/issues/2090), [#2100](https://github.com/drawmeanelephant/VirelaiOS/issues/2100) · Labels: none
- [PR #2119: M97mm: memory-management + ELF/process-lifecycle security fixes](https://github.com/drawmeanelephant/VirelaiOS/pull/2119) · Issues: [#2095](https://github.com/drawmeanelephant/VirelaiOS/issues/2095), [#2096](https://github.com/drawmeanelephant/VirelaiOS/issues/2096), [#2097](https://github.com/drawmeanelephant/VirelaiOS/issues/2097), [#2099](https://github.com/drawmeanelephant/VirelaiOS/issues/2099) · Labels: none
- [PR #2121: M97h: update M97 status row — audits done, fixes in flight](https://github.com/drawmeanelephant/VirelaiOS/pull/2121) · Issues: none · Labels: none
- [PR #2122: M97g: eight owner-approved security follow-up fixes](https://github.com/drawmeanelephant/VirelaiOS/pull/2122) · Issues: [#2079](https://github.com/drawmeanelephant/VirelaiOS/issues/2079), [#2081](https://github.com/drawmeanelephant/VirelaiOS/issues/2081), [#2082](https://github.com/drawmeanelephant/VirelaiOS/issues/2082), [#2083](https://github.com/drawmeanelephant/VirelaiOS/issues/2083), [#2084](https://github.com/drawmeanelephant/VirelaiOS/issues/2084), [#2085](https://github.com/drawmeanelephant/VirelaiOS/issues/2085), [#2086](https://github.com/drawmeanelephant/VirelaiOS/issues/2086), [#2093](https://github.com/drawmeanelephant/VirelaiOS/issues/2093) · Labels: none
- [PR #2124: M97b syscall/MM security fixes — exit statuses, mmap hints, shared teardown, rlimits, table roots](https://github.com/drawmeanelephant/VirelaiOS/pull/2124) · Issues: [#2092](https://github.com/drawmeanelephant/VirelaiOS/issues/2092), [#2098](https://github.com/drawmeanelephant/VirelaiOS/issues/2098), [#2112](https://github.com/drawmeanelephant/VirelaiOS/issues/2112), [#2113](https://github.com/drawmeanelephant/VirelaiOS/issues/2113), [#2114](https://github.com/drawmeanelephant/VirelaiOS/issues/2114), [#2115](https://github.com/drawmeanelephant/VirelaiOS/issues/2115), [#2116](https://github.com/drawmeanelephant/VirelaiOS/issues/2116) · Labels: none
- [PR #2125: kernel: uaccess read-side source proof, loud shared refcounts, surface resize bound (M97b F1-F3)](https://github.com/drawmeanelephant/VirelaiOS/pull/2125) · Issues: [#2109](https://github.com/drawmeanelephant/VirelaiOS/issues/2109), [#2110](https://github.com/drawmeanelephant/VirelaiOS/issues/2110), [#2111](https://github.com/drawmeanelephant/VirelaiOS/issues/2111) · Labels: none
- [PR #2137: M97e network security fixes: ARP/DHCP/DNS trust bounds, TCP RX validation, NET-lock-free connect](https://github.com/drawmeanelephant/VirelaiOS/pull/2137) · Issues: [#2094](https://github.com/drawmeanelephant/VirelaiOS/issues/2094), [#2101](https://github.com/drawmeanelephant/VirelaiOS/issues/2101), [#2102](https://github.com/drawmeanelephant/VirelaiOS/issues/2102), [#2103](https://github.com/drawmeanelephant/VirelaiOS/issues/2103), [#2104](https://github.com/drawmeanelephant/VirelaiOS/issues/2104) · Labels: none

## M98: the daily-driver shell

GOSH gained a real PWD and one path resolver, globbing, a directory stack, aliases, history expansion, toolbox round two, job control, shared contextual completion, session polish, and the go-shell-polish VZ acceptance spec; ADR 0044 proposed the agent control channel that M98's second index later shipped.

- [PR #2143: M98j shlib: real PWD — canonical cd + one resolver for every path-taking verb](https://github.com/drawmeanelephant/VirelaiOS/pull/2143) · Issues: [#2136](https://github.com/drawmeanelephant/VirelaiOS/issues/2136) · Labels: none
- [PR #2144: M98a: glob */?/[...] against the PWD listing](https://github.com/drawmeanelephant/VirelaiOS/pull/2144) · Issues: none · Labels: none
- [PR #2146: ADR 0044: agent control channel v1 spec (M98, proposed)](https://github.com/drawmeanelephant/VirelaiOS/pull/2146) · Issues: none · Labels: none
- [PR #2147: M98d: cd -/OLDPWD + bounded pushd/popd/dirs](https://github.com/drawmeanelephant/VirelaiOS/pull/2147) · Issues: none · Labels: none
- [PR #2148: M98b: alias/unalias + head-word expansion in runSegment](https://github.com/drawmeanelephant/VirelaiOS/pull/2148) · Issues: none · Labels: none
- [PR #2149: M98e: tee, xargs, tr, basename, dirname](https://github.com/drawmeanelephant/VirelaiOS/pull/2149) · Issues: none · Labels: none
- [PR #2150: M98f: kill %N/PID, jobs -l, Ctrl-C interruptible waits](https://github.com/drawmeanelephant/VirelaiOS/pull/2150) · Issues: none · Labels: none
- [PR #2151: M98c: interactive history expansion in the Editor submit path](https://github.com/drawmeanelephant/VirelaiOS/pull/2151) · Issues: [#2129](https://github.com/drawmeanelephant/VirelaiOS/issues/2129) · Labels: none
- [PR #2152: M98g: shared contextual Tab completion in shlib](https://github.com/drawmeanelephant/VirelaiOS/pull/2152) · Issues: [#2133](https://github.com/drawmeanelephant/VirelaiOS/issues/2133) · Labels: none
- [PR #2153: M98h: session polish — time CMD, tab-title cwd sync, \\? \\D escapes, Ctrl-X Ctrl-E](https://github.com/drawmeanelephant/VirelaiOS/pull/2153) · Issues: [#2134](https://github.com/drawmeanelephant/VirelaiOS/issues/2134) · Labels: none
- [PR #2154: M98i: go-shell-polish acceptance spec — every M98 leg on VZ](https://github.com/drawmeanelephant/VirelaiOS/pull/2154) · Issues: [#2135](https://github.com/drawmeanelephant/VirelaiOS/issues/2135) · Labels: none

## Kernel, runtime, and tooling fixes

Cross-cutting repairs covered the virtio TX buffer ownership bug, process-exit kills across sibling waits, next-hop ARP for off-subnet sends, console staging serialization, lock order, service receipt ordering, the Go IPC refusal for GOOS=virelai, kernel image headroom, and the repo's own chronicle attachment publisher.

- [PR #1952: Gate (#1948): keep IPC send and echo markers atomic](https://github.com/drawmeanelephant/VirelaiOS/pull/1952) · Issues: [#1948](https://github.com/drawmeanelephant/VirelaiOS/issues/1948) · Labels: none
- [PR #1961: VirtioFS (#1957): establish identity storage at mount setup](https://github.com/drawmeanelephant/VirelaiOS/pull/1961) · Issues: [#1957](https://github.com/drawmeanelephant/VirelaiOS/issues/1957) · Labels: none
- [PR #1962: Go runtime (#1958): avoid clearing fresh sbrk padding](https://github.com/drawmeanelephant/VirelaiOS/pull/1962) · Issues: [#1958](https://github.com/drawmeanelephant/VirelaiOS/issues/1958) · Labels: none
- [PR #1968: Kernel (#1965): keep demo workers behind ready EL0 tasks](https://github.com/drawmeanelephant/VirelaiOS/pull/1968) · Issues: [#1965](https://github.com/drawmeanelephant/VirelaiOS/issues/1965) · Labels: none
- [PR #1974: Kernel (#1972): never rewrite a TX buffer the device still owns](https://github.com/drawmeanelephant/VirelaiOS/pull/1974) · Issues: [#1972](https://github.com/drawmeanelephant/VirelaiOS/issues/1972) · Labels: none
- [PR #1981: Chronicle (#1977): claimed attachment publisher and replacement prompt](https://github.com/drawmeanelephant/VirelaiOS/pull/1981) · Issues: [#1977](https://github.com/drawmeanelephant/VirelaiOS/issues/1977) · Labels: none
- [PR #1982: Kernel (#1978): preserve process-exit kills across sibling waits](https://github.com/drawmeanelephant/VirelaiOS/pull/1982) · Issues: [#1978](https://github.com/drawmeanelephant/VirelaiOS/issues/1978) · Labels: none
- [PR #1984: Agents: document standing authority](https://github.com/drawmeanelephant/VirelaiOS/pull/1984) · Issues: none · Labels: none
- [PR #2035: Recover kernel image headroom without raising the loader ceiling](https://github.com/drawmeanelephant/VirelaiOS/pull/2035) · Issues: [#2034](https://github.com/drawmeanelephant/VirelaiOS/issues/2034) · Labels: none
- [PR #2044: net: compile stdlib net for GOOS=virelai as refusing in-process IPC (#2029)](https://github.com/drawmeanelephant/VirelaiOS/pull/2044) · Issues: [#2029](https://github.com/drawmeanelephant/VirelaiOS/issues/2029) · Labels: none
- [PR #2046: goset: save receipts precede manifest publish (receipt-order race)](https://github.com/drawmeanelephant/VirelaiOS/pull/2046) · Issues: [#2051](https://github.com/drawmeanelephant/VirelaiOS/issues/2051) · Labels: none
- [PR #2047: lockorder: restore canonical service-domain lock order (#2041, #2043)](https://github.com/drawmeanelephant/VirelaiOS/pull/2047) · Issues: [#2041](https://github.com/drawmeanelephant/VirelaiOS/issues/2041), [#2043](https://github.com/drawmeanelephant/VirelaiOS/issues/2043), [#2049](https://github.com/drawmeanelephant/VirelaiOS/issues/2049) · Labels: none
- [PR #2050: netroute: next-hop ARP — off-subnet sends ride the NAT gateway](https://github.com/drawmeanelephant/VirelaiOS/pull/2050) · Issues: none · Labels: none
- [PR #2053: vsys: serialize console staging through the write syscall](https://github.com/drawmeanelephant/VirelaiOS/pull/2053) · Issues: [#2052](https://github.com/drawmeanelephant/VirelaiOS/issues/2052) · Labels: none
