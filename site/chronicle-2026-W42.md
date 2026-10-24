---
title: 2026-W42 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W42: October 12–18, 187 selected PRs

199 PRs merged in the range; the unlisted ones are status-row bookkeeping. This was the heaviest single arc week in the chronicle so far: fourteen milestone families closed or advanced.

## Second passes and platform depth

The Go app fleet took a deliberate second pass — git fetch/pull over smart HTTP, editor undo and replace, a real SSH session layer, a decimal calculator engine, and a persistent HTTP/1.1 server — while the launcher grew the Quicksilver strip and the kernel exposed versioned process stats and an unforgeable self-pid.

- [PR #2155: #2054: Quicksilver launcher on the Go seat — left-edge strip + sexiburger](https://github.com/drawmeanelephant/VirelaiOS/pull/2155) · Issues: [#2054](https://github.com/drawmeanelephant/VirelaiOS/issues/2054) · Labels: none
- [PR #2209: GOTGIT second pass: fetch/pull over smart HTTP](https://github.com/drawmeanelephant/VirelaiOS/pull/2209) · Issues: [#2203](https://github.com/drawmeanelephant/VirelaiOS/issues/2203) · Labels: none
- [PR #2210: GOEDIT second pass: undo/redo, selection+clipboard, replace, status line (#2199)](https://github.com/drawmeanelephant/VirelaiOS/pull/2210) · Issues: [#2199](https://github.com/drawmeanelephant/VirelaiOS/issues/2199) · Labels: none
- [PR #2214: sshlib + GOSSH second pass: class-A test floor](https://github.com/drawmeanelephant/VirelaiOS/pull/2214) · Issues: [#2202](https://github.com/drawmeanelephant/VirelaiOS/issues/2202) · Labels: none
- [PR #2215: GOTOP second pass: widgets.TextInput + restored / filter](https://github.com/drawmeanelephant/VirelaiOS/pull/2215) · Issues: [#2197](https://github.com/drawmeanelephant/VirelaiOS/issues/2197) · Labels: none
- [PR #2216: GOCALC second pass: fixed-point decimal engine, programmer mode, keyboard parity (#2201)](https://github.com/drawmeanelephant/VirelaiOS/pull/2216) · Issues: [#2201](https://github.com/drawmeanelephant/VirelaiOS/issues/2201) · Labels: none
- [PR #2232: httpd: GOHTTPD second pass — persistent HTTP/1.1 server](https://github.com/drawmeanelephant/VirelaiOS/pull/2232) · Issues: [#2200](https://github.com/drawmeanelephant/VirelaiOS/issues/2200) · Labels: none
- [PR #2246: vi: refuse nonzero WM_RPC geometry under the Go seat — no silent drop (#2157)](https://github.com/drawmeanelephant/VirelaiOS/pull/2246) · Issues: [#2157](https://github.com/drawmeanelephant/VirelaiOS/issues/2157) · Labels: none
- [PR #2248: #2198: versioned sys_procs v2 row — real CPU/mem in GOTOP + PULSE](https://github.com/drawmeanelephant/VirelaiOS/pull/2248) · Issues: [#2198](https://github.com/drawmeanelephant/VirelaiOS/issues/2198) · Labels: none
- [PR #2249: #2158: sys_getpid (slot 87) — unforgeable self-pid for WmPeers](https://github.com/drawmeanelephant/VirelaiOS/pull/2249) · Issues: [#2158](https://github.com/drawmeanelephant/VirelaiOS/issues/2158) · Labels: none
- [PR #2266: appkit: re-pin dialog composite golden after M99o linear-light blending](https://github.com/drawmeanelephant/VirelaiOS/pull/2266) · Issues: none · Labels: none
- [PR #2332: btmath: salvage Bubble Tea math app onto the M72c overlay mechanism](https://github.com/drawmeanelephant/VirelaiOS/pull/2332) · Issues: none · Labels: none

## M99: sound, fonts, and network depth

The desktop-depth milestone landed a kernel audio mixer and GOSOUND playback with a seat chime, froze the Zig font engine in favor of a Go registry with coverage faces and linear-light glyph blending, resolved web typography honestly end to end, and rebuilt the TCP socket table with per-handle connections, wire-depth options, and DNS AAAA/TTL work — though its acceptance run named an intermittent wav-toast finding under sustained PCM load.

- [PR #2212: M99f: one bounded TCP socket table — socket_core promoted, legacy slots become a shim](https://github.com/drawmeanelephant/VirelaiOS/pull/2212) · Issues: [#2184](https://github.com/drawmeanelephant/VirelaiOS/issues/2184) · Labels: none
- [PR #2230: M99g: per-handle Go connections — slot-80 connect+table, multi-session sshd, netstat socket rows](https://github.com/drawmeanelephant/VirelaiOS/pull/2230) · Issues: [#2185](https://github.com/drawmeanelephant/VirelaiOS/issues/2185) · Labels: none
- [PR #2231: M99h: TCP wire depth — options, RX queue, adaptive RTO](https://github.com/drawmeanelephant/VirelaiOS/pull/2231) · Issues: [#2186](https://github.com/drawmeanelephant/VirelaiOS/issues/2186) · Labels: none
- [PR #2234: M99i: DNS depth — AAAA, multi-answer, TTL cache](https://github.com/drawmeanelephant/VirelaiOS/pull/2234) · Issues: [#2187](https://github.com/drawmeanelephant/VirelaiOS/issues/2187) · Labels: none
- [PR #2238: M99a: audio architecture ADR — kernel mixer over B7](https://github.com/drawmeanelephant/VirelaiOS/pull/2238) · Issues: [#2179](https://github.com/drawmeanelephant/VirelaiOS/issues/2179) · Labels: none
- [PR #2239: M99n: freeze the Zig font engine — ADR 0034 amendment A + cross-engine parity pin](https://github.com/drawmeanelephant/VirelaiOS/pull/2239) · Issues: [#2191](https://github.com/drawmeanelephant/VirelaiOS/issues/2191) · Labels: none
- [PR #2240: M99l: font registry + fallback chain](https://github.com/drawmeanelephant/VirelaiOS/pull/2240) · Issues: [#2189](https://github.com/drawmeanelephant/VirelaiOS/issues/2189) · Labels: none
- [PR #2241: M99b: kernel mixer over the B7 stream + vi.AudioStream](https://github.com/drawmeanelephant/VirelaiOS/pull/2241) · Issues: [#2180](https://github.com/drawmeanelephant/VirelaiOS/issues/2180) · Labels: none
- [PR #2243: M99m (#2190): coverage faces — Noto Serif + subsetted Noto Sans JP](https://github.com/drawmeanelephant/VirelaiOS/pull/2243) · Issues: [#2190](https://github.com/drawmeanelephant/VirelaiOS/issues/2190) · Labels: none
- [PR #2244: M99c: GOSOUND.ELF — bounded WAV player over vi.AudioStream (#2181)](https://github.com/drawmeanelephant/VirelaiOS/pull/2244) · Issues: [#2181](https://github.com/drawmeanelephant/VirelaiOS/issues/2181) · Labels: none
- [PR #2245: M99d: seat notification chime + GOSET volume row](https://github.com/drawmeanelephant/VirelaiOS/pull/2245) · Issues: [#2182](https://github.com/drawmeanelephant/VirelaiOS/issues/2182) · Labels: none
- [PR #2247: M99o (#2192): linear-light glyph blending + a real Inter Bold Italic](https://github.com/drawmeanelephant/VirelaiOS/pull/2247) · Issues: [#2192](https://github.com/drawmeanelephant/VirelaiOS/issues/2192) · Labels: none
- [PR #2250: M99p (#2193): web typography honesty — family, size, weight resolve end to end](https://github.com/drawmeanelephant/VirelaiOS/pull/2250) · Issues: [#2193](https://github.com/drawmeanelephant/VirelaiOS/issues/2193) · Labels: none
- [PR #2256: M99z (#2194): acceptance — three lane end-to-ends + K=3 evidence](https://github.com/drawmeanelephant/VirelaiOS/pull/2256) · Issues: [#2194](https://github.com/drawmeanelephant/VirelaiOS/issues/2194) · Labels: none
- [PR #2265: M99z fixup (#2260): event-driven seat input drain + 64-deep queue + WM RPC re-challenge heal](https://github.com/drawmeanelephant/VirelaiOS/pull/2265) · Issues: [#2260](https://github.com/drawmeanelephant/VirelaiOS/issues/2260) · Labels: none

## M102: custom-Virtio GPU 3D

An opt-in Metal render device rode beside the native 2D scanout: a VirGL-flavored render channel, bounded encoder and fixed-subset decoder on the host, a reviewed WM-only kernel bridge, a pure-Go guest API, seat compositing of completed frames, and the triangle demo with independent pixel proofs — plus a week of live-path defect fixes.

- [PR #2173: M102b2: custom-Virtio 3D host envelope + context lifecycle](https://github.com/drawmeanelephant/VirelaiOS/pull/2173) · Issues: [#2163](https://github.com/drawmeanelephant/VirelaiOS/issues/2163) · Labels: none
- [PR #2174: M102b1: GPU3D render channel — VIR1 wire, transport + bounded context lifecycle](https://github.com/drawmeanelephant/VirelaiOS/pull/2174) · Issues: [#2162](https://github.com/drawmeanelephant/VirelaiOS/issues/2162) · Labels: none
- [PR #2176: M102c1: GPU3D bounded VirGL encoder — stream, submit/readback, retirement (#2164)](https://github.com/drawmeanelephant/VirelaiOS/pull/2176) · Issues: [#2164](https://github.com/drawmeanelephant/VirelaiOS/issues/2164) · Labels: none
- [PR #2177: M102c2: fixed-subset VirGL decoder + real Metal renderer](https://github.com/drawmeanelephant/VirelaiOS/pull/2177) · Issues: [#2165](https://github.com/drawmeanelephant/VirelaiOS/issues/2165) · Labels: none
- [PR #2195: M102d3: opt-in --gpu3d-metal runner attachment](https://github.com/drawmeanelephant/VirelaiOS/pull/2195) · Issues: [#2168](https://github.com/drawmeanelephant/VirelaiOS/issues/2168) · Labels: none
- [PR #2196: M102d1: reviewed WM-only GPU3D bridge — slot-65 cmd 22 (#2166)](https://github.com/drawmeanelephant/VirelaiOS/pull/2196) · Issues: [#2166](https://github.com/drawmeanelephant/VirelaiOS/issues/2166) · Labels: none
- [PR #2206: M102d2: bounded pure-Go gpu3d API over authenticated WM_RPC kind 17](https://github.com/drawmeanelephant/VirelaiOS/pull/2206) · Issues: [#2167](https://github.com/drawmeanelephant/VirelaiOS/issues/2167) · Labels: none
- [PR #2208: M102f1: triangle demo + independent pixel-proof fixtures (#2170)](https://github.com/drawmeanelephant/VirelaiOS/pull/2208) · Issues: [#2170](https://github.com/drawmeanelephant/VirelaiOS/issues/2170) · Labels: none
- [PR #2211: M102e: seat composes completed GPU3D frames into ordinary surfaces (#2169)](https://github.com/drawmeanelephant/VirelaiOS/pull/2211) · Issues: [#2169](https://github.com/drawmeanelephant/VirelaiOS/issues/2169) · Labels: none
- [PR #2227: GPUTRIANGLE: three M102 live-path fixes (bind API, ordering, export mkdir)](https://github.com/drawmeanelephant/VirelaiOS/pull/2227) · Issues: [#2224](https://github.com/drawmeanelephant/VirelaiOS/issues/2224), [#2225](https://github.com/drawmeanelephant/VirelaiOS/issues/2225), [#2226](https://github.com/drawmeanelephant/VirelaiOS/issues/2226) · Labels: none
- [PR #2228: gpu3d: VIR1 wire conformance to host device (#2217-#2221)](https://github.com/drawmeanelephant/VirelaiOS/pull/2228) · Issues: [#2217](https://github.com/drawmeanelephant/VirelaiOS/issues/2217), [#2218](https://github.com/drawmeanelephant/VirelaiOS/issues/2218), [#2219](https://github.com/drawmeanelephant/VirelaiOS/issues/2219), [#2220](https://github.com/drawmeanelephant/VirelaiOS/issues/2220), [#2221](https://github.com/drawmeanelephant/VirelaiOS/issues/2221) · Labels: none
- [PR #2229: VMGPU3D: decode frozen 28B res-ops + host completion pump (#2222, #2223)](https://github.com/drawmeanelephant/VirelaiOS/pull/2229) · Issues: [#2222](https://github.com/drawmeanelephant/VirelaiOS/issues/2222), [#2223](https://github.com/drawmeanelephant/VirelaiOS/issues/2223) · Labels: none
- [PR #2235: live-gpu3d: M102f2 acceptance gate spec + docs (#2171)](https://github.com/drawmeanelephant/VirelaiOS/pull/2235) · Issues: [#2171](https://github.com/drawmeanelephant/VirelaiOS/issues/2171) · Labels: none
- [PR #2242: M102 live-path defects #11/#12/#13: sceneVerts sign, rpc rebind on window reuse, seat composes migrated surfaces](https://github.com/drawmeanelephant/VirelaiOS/pull/2242) · Issues: [#11](https://github.com/drawmeanelephant/VirelaiOS/issues/11), [#12](https://github.com/drawmeanelephant/VirelaiOS/issues/12), [#13](https://github.com/drawmeanelephant/VirelaiOS/issues/13), [#2236](https://github.com/drawmeanelephant/VirelaiOS/issues/2236) · Labels: none
- [PR #2264: M102 follow-up (#2261): custom-Virtio x --vz-restore — documented refusal naming the blocking device](https://github.com/drawmeanelephant/VirelaiOS/pull/2264) · Issues: [#2261](https://github.com/drawmeanelephant/VirelaiOS/issues/2261) · Labels: none

## M103: FLINT, the agent-native SCM

FLINT M0 landed parsers, a flat-file store with canonical hashing, signed commits over Ed25519 Changes, and semantic merge on Ops — ten green go-flint runs — and two standalone-client specs were drafted for the Zig sync surface.

- [PR #2255: M103 (FLINT M0): ADR 0049 — grounded design + card split](https://github.com/drawmeanelephant/VirelaiOS/pull/2255) · Issues: none · Labels: none
- [PR #2257: M103a (#2253): FLINT Go + Zig parsers, canonical hashing, corpus, and in-guest go-flint run 01](https://github.com/drawmeanelephant/VirelaiOS/pull/2257) · Issues: [#2253](https://github.com/drawmeanelephant/VirelaiOS/issues/2253) · Labels: none
- [PR #2259: M103b (#2251): FLINT flat-file store + in-memory index, UUIDv7, init/add/status, and go-flint run 02](https://github.com/drawmeanelephant/VirelaiOS/pull/2259) · Issues: [#2251](https://github.com/drawmeanelephant/VirelaiOS/issues/2251) · Labels: none
- [PR #2262: M103c (#2252): FLINT canonical bytes, signed commit, log, verify, and go-flint runs 03-04](https://github.com/drawmeanelephant/VirelaiOS/pull/2262) · Issues: [#2252](https://github.com/drawmeanelephant/VirelaiOS/issues/2252) · Labels: none
- [PR #2263: M103d (#2254): FLINT fork, init --stream replay, semantic merge on Ops, and go-flint runs 05-10](https://github.com/drawmeanelephant/VirelaiOS/pull/2263) · Issues: [#2254](https://github.com/drawmeanelephant/VirelaiOS/issues/2254) · Labels: none
- [PR #2282: docs: spec for a standalone cross-platform FLINT client (Zig)](https://github.com/drawmeanelephant/VirelaiOS/pull/2282) · Issues: none · Labels: none
- [PR #2290: docs: spec for a standalone FLINT sync server (Zig)](https://github.com/drawmeanelephant/VirelaiOS/pull/2290) · Issues: none · Labels: none

## M104: UI primitives v2

Contract v2 brought depth primitives, widget faces on Canvas with TTF labels, kit controls, seat chrome repainted on the draw engine with new PUA glyphs, a fleet migration to tabapp canvas binding, and composite-golden acceptance.

- [PR #2276: M104a — UI primitives contract v2: depth primitives, widget faces, controls + glyph inventories, adoption audit](https://github.com/drawmeanelephant/VirelaiOS/pull/2276) · Issues: [#2268](https://github.com/drawmeanelephant/VirelaiOS/issues/2268) · Labels: none
- [PR #2277: M104g — glyph batch 2: new PUA shapes via Virelai Sans pipeline](https://github.com/drawmeanelephant/VirelaiOS/pull/2277) · Issues: [#2274](https://github.com/drawmeanelephant/VirelaiOS/issues/2274) · Labels: none
- [PR #2278: M104b (#2269): contract v2 depth primitives in user/go/draw + goldens](https://github.com/drawmeanelephant/VirelaiOS/pull/2278) · Issues: [#2269](https://github.com/drawmeanelephant/VirelaiOS/issues/2269) · Labels: none
- [PR #2279: M104c — widget faces: widgets on Canvas + TTF labels, appkit alignment](https://github.com/drawmeanelephant/VirelaiOS/pull/2279) · Issues: [#2270](https://github.com/drawmeanelephant/VirelaiOS/issues/2270) · Labels: none
- [PR #2280: M104d (#2271): kit controls — scrollbar, toggle, slider, dropdown](https://github.com/drawmeanelephant/VirelaiOS/pull/2280) · Issues: [#2271](https://github.com/drawmeanelephant/VirelaiOS/issues/2271) · Labels: none
- [PR #2281: M104e: seat chrome — gotabwm painters on draw.BytesCanvas + fonts + PUA glyphs](https://github.com/drawmeanelephant/VirelaiOS/pull/2281) · Issues: [#2272](https://github.com/drawmeanelephant/VirelaiOS/issues/2272) · Labels: none
- [PR #2299: M104f (#2273): tabapp canvas binding + fleet migration to the draw engine](https://github.com/drawmeanelephant/VirelaiOS/pull/2299) · Issues: [#2273](https://github.com/drawmeanelephant/VirelaiOS/issues/2273) · Labels: none
- [PR #2300: M104h (#2275): acceptance — composite goldens + go-calc chrome pixels + status row](https://github.com/drawmeanelephant/VirelaiOS/pull/2300) · Issues: [#2275](https://github.com/drawmeanelephant/VirelaiOS/issues/2275) · Labels: none

## M100: a disk of our own

VLFS, the journaled guest filesystem, went from ADR 0050 through a virtio-blk driver, mkfs and read path, journaled writes with a real fsync barrier, fsck and replay evidence, an opt-in /disk data root, and a standing crash-resilience gate that hard-kills the VM mid-write — plus an audit lane tightening bitmap, itable, and rename checks.

- [PR #2330: M100a (#2306): ADR 0050 — VLFS on-disk format, journal ordering, mount model, backing-device pick](https://github.com/drawmeanelephant/VirelaiOS/pull/2330) · Issues: [#2306](https://github.com/drawmeanelephant/VirelaiOS/issues/2306) · Labels: none
- [PR #2338: M100b (#2307): virtio-blk RW driver + runner --data-disk attach per ADR 0050](https://github.com/drawmeanelephant/VirelaiOS/pull/2338) · Issues: [#2307](https://github.com/drawmeanelephant/VirelaiOS/issues/2307) · Labels: none
- [PR #2340: M100c (#2308): mkfs.vlfs + kernel VLFS read path at /disk](https://github.com/drawmeanelephant/VirelaiOS/pull/2340) · Issues: [#2308](https://github.com/drawmeanelephant/VirelaiOS/issues/2308) · Labels: none
- [PR #2351: M100d (#2309): VLFS journaled write path — commits, allocation, fsync barrier](https://github.com/drawmeanelephant/VirelaiOS/pull/2351) · Issues: [#2309](https://github.com/drawmeanelephant/VirelaiOS/issues/2309) · Labels: none
- [PR #2358: M100e (#2310): fsck coverage + replay evidence + kill-mid-write gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2358) · Issues: [#2310](https://github.com/drawmeanelephant/VirelaiOS/issues/2310) · Labels: none
- [PR #2363: M100f (#2311): opt-in /disk data root + GOFILES dual-volume + GOSELF disk receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/2363) · Issues: [#2311](https://github.com/drawmeanelephant/VirelaiOS/issues/2311) · Labels: none
- [PR #2390: VLFS audit lane: multi-block bbitmap check, itable ceil geometry, renameAt cycle guard](https://github.com/drawmeanelephant/VirelaiOS/pull/2390) · Issues: [#2365](https://github.com/drawmeanelephant/VirelaiOS/issues/2365), [#2370](https://github.com/drawmeanelephant/VirelaiOS/issues/2370), [#2376](https://github.com/drawmeanelephant/VirelaiOS/issues/2376) · Labels: none
- [PR #2394: M100g (#2312): standing crash-resilience gate + status row](https://github.com/drawmeanelephant/VirelaiOS/pull/2394) · Issues: [#2312](https://github.com/drawmeanelephant/VirelaiOS/issues/2312) · Labels: none

## M101 + M110: signed software, store, and release

FLINT learned to speak FLINT-SYNC/1 for clone/pull/push; the publisher keyring gates materialization; STORE.ELF installs, updates, and rolls back signed releases; and ADR 0055's release manifest grew signed catalog fields behind the GOSTORE GUI — proven by a third-party publisher drill on a fresh share and a hardening pass over keyring, rollback, and relay correctness.

- [PR #2336: M101a (#2313): FLINT-SYNC/1 client — clone/pull/push/ls-remote on FLINT.ELF](https://github.com/drawmeanelephant/VirelaiOS/pull/2336) · Issues: [#2313](https://github.com/drawmeanelephant/VirelaiOS/issues/2313) · Labels: none
- [PR #2337: M101b (#2314): publisher keyring + verify-before-materialize](https://github.com/drawmeanelephant/VirelaiOS/pull/2337) · Issues: [#2314](https://github.com/drawmeanelephant/VirelaiOS/issues/2314) · Labels: none
- [PR #2355: M101c (#2315): install seam — STORE.ELF ls/install/remove + release-stream convention](https://github.com/drawmeanelephant/VirelaiOS/pull/2355) · Issues: [#2315](https://github.com/drawmeanelephant/VirelaiOS/issues/2315) · Labels: none
- [PR #2356: M101d (#2316): store update/rollback — staged pull, WriteFileSafe publish, prev-head state](https://github.com/drawmeanelephant/VirelaiOS/pull/2356) · Issues: [#2316](https://github.com/drawmeanelephant/VirelaiOS/issues/2316) · Labels: none
- [PR #2364: M101e: standing install gate — update/rollback legs + negatives in go-flint](https://github.com/drawmeanelephant/VirelaiOS/pull/2364) · Issues: [#2317](https://github.com/drawmeanelephant/VirelaiOS/issues/2317) · Labels: none
- [PR #2395: #2367/#2372/#2379: STORE lane — keyring gate at materialization, killed-publish rollback target, correctness minors](https://github.com/drawmeanelephant/VirelaiOS/pull/2395) · Issues: [#2367](https://github.com/drawmeanelephant/VirelaiOS/issues/2367), [#2372](https://github.com/drawmeanelephant/VirelaiOS/issues/2372), [#2379](https://github.com/drawmeanelephant/VirelaiOS/issues/2379) · Labels: none
- [PR #2423: docs: ADR 0055 — the release manifest (M110a)](https://github.com/drawmeanelephant/VirelaiOS/pull/2423) · Issues: [#2403](https://github.com/drawmeanelephant/VirelaiOS/issues/2403) · Labels: none
- [PR #2431: store: ADR 0055 signed catalog fields + store ls/info (M110b)](https://github.com/drawmeanelephant/VirelaiOS/pull/2431) · Issues: [#2404](https://github.com/drawmeanelephant/VirelaiOS/issues/2404) · Labels: none
- [PR #2433: gate: M110d third-party publisher drill — acme app on a fresh share (#2406)](https://github.com/drawmeanelephant/VirelaiOS/pull/2433) · Issues: [#2406](https://github.com/drawmeanelephant/VirelaiOS/issues/2406) · Labels: none
- [PR #2438: M110c (#2405): GOSTORE GUI software catalog over STORE's signed legs](https://github.com/drawmeanelephant/VirelaiOS/pull/2438) · Issues: [#2405](https://github.com/drawmeanelephant/VirelaiOS/issues/2405) · Labels: none

## M108: the host-integration lanes

ADR 0051's signed VHI1 share files carried a bidirectional clipboard lane, host file drops into /host/INBOX with seat toasts and mime dispatch, and notify-out to macOS banners — closed by a standing acceptance gate covering every lane and its negatives, with consent hardening underneath.

- [PR #2333: M108a (#2324): ADR 0051 — host-integration wire + consent design](https://github.com/drawmeanelephant/VirelaiOS/pull/2333) · Issues: [#2324](https://github.com/drawmeanelephant/VirelaiOS/issues/2324) · Labels: none
- [PR #2341: M108b: bidirectional clipboard lane over signed VHI1 share files (ADR 0051)](https://github.com/drawmeanelephant/VirelaiOS/pull/2341) · Issues: [#2325](https://github.com/drawmeanelephant/VirelaiOS/issues/2325) · Labels: none
- [PR #2347: M108c (#2326): host file drop → /host/INBOX + seat toast + mime dispatch](https://github.com/drawmeanelephant/VirelaiOS/pull/2347) · Issues: [#2326](https://github.com/drawmeanelephant/VirelaiOS/issues/2326) · Labels: none
- [PR #2350: M108d (#2328): notify-out — seat toasts to macOS banners (ADR 0051 D5)](https://github.com/drawmeanelephant/VirelaiOS/pull/2350) · Issues: [#2328](https://github.com/drawmeanelephant/VirelaiOS/issues/2328) · Labels: none
- [PR #2362: M108e (#2329): standing host-integration acceptance gate — all lanes + negatives](https://github.com/drawmeanelephant/VirelaiOS/pull/2362) · Issues: [#2329](https://github.com/drawmeanelephant/VirelaiOS/issues/2329) · Labels: none
- [PR #2387: hostclip: commit CLIP_OUT seen-state only after a successful emit](https://github.com/drawmeanelephant/VirelaiOS/pull/2387) · Issues: [#2371](https://github.com/drawmeanelephant/VirelaiOS/issues/2371) · Labels: none
- [PR #2389: #2378: host lane minors — notify cap+clamp, drop O_NOFOLLOW fd copy, constant-time MAC, DataDisk create cleanup](https://github.com/drawmeanelephant/VirelaiOS/pull/2389) · Issues: [#2378](https://github.com/drawmeanelephant/VirelaiOS/issues/2378) · Labels: none

## M107 + M112: build in the guest, ship the image

GOBUILD.ELF drives import-graph builds with a bounded dependency policy, GOEDIT navigates diagnostics, and GODIFF closed the capstone — guest-authored, guest-built, guest-pushed — while the M112 lane built the pinned 19-ELF fleet in-guest, ran package tests there, and wrote a bootable GPT/FAT32 release image through GORELEASE, all under a standing acceptance gate.

- [PR #2335: M107a: in-guest build driver — import graph, importcfg, GOBUILD.ELF](https://github.com/drawmeanelephant/VirelaiOS/pull/2335) · Issues: [#2318](https://github.com/drawmeanelephant/VirelaiOS/issues/2318) · Labels: none
- [PR #2339: M107b: bounded dep closure — std staging policy, allowlist/denylist, named refusals](https://github.com/drawmeanelephant/VirelaiOS/pull/2339) · Issues: [#2319](https://github.com/drawmeanelephant/VirelaiOS/issues/2319) · Labels: none
- [PR #2344: M107c: GOEDIT file:line:col diagnostics navigation](https://github.com/drawmeanelephant/VirelaiOS/pull/2344) · Issues: [#2320](https://github.com/drawmeanelephant/VirelaiOS/issues/2320) · Labels: none
- [PR #2348: M107d (#2321): versioned dev loop — flint diff + push batching + live-flint-loop gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2348) · Issues: [#2321](https://github.com/drawmeanelephant/VirelaiOS/issues/2321) · Labels: none
- [PR #2353: M107e (#2322): GODIFF capstone — guest-authored, guest-built, guest-pushed](https://github.com/drawmeanelephant/VirelaiOS/pull/2353) · Issues: [#2322](https://github.com/drawmeanelephant/VirelaiOS/issues/2322) · Labels: none
- [PR #2361: M107f (#2323): live-dev-loop standing acceptance gate + M107 status row](https://github.com/drawmeanelephant/VirelaiOS/pull/2361) · Issues: [#2323](https://github.com/drawmeanelephant/VirelaiOS/issues/2323) · Labels: none
- [PR #2440: M112a (#2413): gobuild fleet verb — pinned 19-ELF fleet built in-guest + source→ELF manifest](https://github.com/drawmeanelephant/VirelaiOS/pull/2440) · Issues: [#2413](https://github.com/drawmeanelephant/VirelaiOS/issues/2413) · Labels: none
- [PR #2443: M112c (#2415): GORELEASE — userspace GPT/FAT32 image writer + release assembler](https://github.com/drawmeanelephant/VirelaiOS/pull/2443) · Issues: [#2415](https://github.com/drawmeanelephant/VirelaiOS/issues/2415) · Labels: none
- [PR #2444: M112b (#2414): GOBUILD test verb — pinned package tests run in-guest](https://github.com/drawmeanelephant/VirelaiOS/pull/2444) · Issues: [#2414](https://github.com/drawmeanelephant/VirelaiOS/issues/2414) · Labels: none
- [PR #2493: M112d (#2416): acceptance — live-selfhost-go + live-release K=5 green; status row](https://github.com/drawmeanelephant/VirelaiOS/pull/2493) · Issues: [#2416](https://github.com/drawmeanelephant/VirelaiOS/issues/2416) · Labels: none

## The agent channel: M98's v1 into M109

ADR 0044's control channel shipped as AGENTD.ELF with HMAC-challenged JSON lines, then M109 rebuilt identity on Ed25519 with declared caps and bounded verbs, added a MAC'd receipt per verb, ran an end-to-end live agent loop that shipped an app, and put a default-off agent indicator on the seat. ADR 0045 separately specified authenticated remote-desktop transport for the RFB codec.

- [PR #2172: M98: agent control channel v1 — AGENTD.ELF + go-agentd gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2172) · Issues: [#2145](https://github.com/drawmeanelephant/VirelaiOS/issues/2145) · Labels: none
- [PR #2207: agentd: bind kind-16 ui.windows through M97g-F2; pin GOSH redirect as observed](https://github.com/drawmeanelephant/VirelaiOS/pull/2207) · Issues: none · Labels: none
- [PR #2213: ADR 0045: authenticated remote-desktop transport for the RFB codec](https://github.com/drawmeanelephant/VirelaiOS/pull/2213) · Issues: [#2204](https://github.com/drawmeanelephant/VirelaiOS/issues/2204) · Labels: none
- [PR #2424: M109a (#2397): ADR 0052 — agent identity + ADR 0044 concurrency amendment](https://github.com/drawmeanelephant/VirelaiOS/pull/2424) · Issues: [#2397](https://github.com/drawmeanelephant/VirelaiOS/issues/2397) · Labels: none
- [PR #2432: agentd: virelai-agent/2 — Ed25519 identity, declared caps, bounded verbs (M109b)](https://github.com/drawmeanelephant/VirelaiOS/pull/2432) · Issues: [#2398](https://github.com/drawmeanelephant/VirelaiOS/issues/2398) · Labels: none
- [PR #2434: agentd: M109c audit lane — a MAC'd receipt per verb + GOTOP agent view](https://github.com/drawmeanelephant/VirelaiOS/pull/2434) · Issues: [#2399](https://github.com/drawmeanelephant/VirelaiOS/issues/2399) · Labels: none
- [PR #2441: agentd: re-read before declaring peer gone — fix the recv/probe race (#2436)](https://github.com/drawmeanelephant/VirelaiOS/pull/2441) · Issues: [#2436](https://github.com/drawmeanelephant/VirelaiOS/issues/2436) · Labels: none
- [PR #2445: M109d: live-agent-loop — the agent ships an app end to end](https://github.com/drawmeanelephant/VirelaiOS/pull/2445) · Issues: [#2400](https://github.com/drawmeanelephant/VirelaiOS/issues/2400) · Labels: none
- [PR #2446: M109e: default-off agent seat indicator + milestone notify lanes (#2401)](https://github.com/drawmeanelephant/VirelaiOS/pull/2446) · Issues: [#2401](https://github.com/drawmeanelephant/VirelaiOS/issues/2401) · Labels: none

## M111: GPU compute and inference

ADR 0053 scoped the compute surface, slot 90 sys_gpucomp went end to end, GOTOP shows real counters, and GOINFER ran guest LLM inference on the path — capped by a K=5 acceptance.

- [PR #2425: M111a: ADR 0053 — the GPU compute surface](https://github.com/drawmeanelephant/VirelaiOS/pull/2425) · Issues: [#2408](https://github.com/drawmeanelephant/VirelaiOS/issues/2408) · Labels: none
- [PR #2428: ADR 0053: renumber sys_gpucomp to slot 90](https://github.com/drawmeanelephant/VirelaiOS/pull/2428) · Issues: none · Labels: none
- [PR #2435: M111b (#2409): GPU compute end to end — slot 90 sys_gpucomp](https://github.com/drawmeanelephant/VirelaiOS/pull/2435) · Issues: [#2409](https://github.com/drawmeanelephant/VirelaiOS/issues/2409) · Labels: none
- [PR #2439: M111d: GOTOP gpu tab — real slot-90 counters (#2411)](https://github.com/drawmeanelephant/VirelaiOS/pull/2439) · Issues: [#2411](https://github.com/drawmeanelephant/VirelaiOS/issues/2411) · Labels: none
- [PR #2492: M111c (#2410): GOINFER — guest LLM inference on the GPU compute path](https://github.com/drawmeanelephant/VirelaiOS/pull/2492) · Issues: [#2410](https://github.com/drawmeanelephant/VirelaiOS/issues/2410) · Labels: none
- [PR #2510: gpu3d.ComputeSession: unmap the fixed-VA window on Close](https://github.com/drawmeanelephant/VirelaiOS/pull/2510) · Issues: [#2494](https://github.com/drawmeanelephant/VirelaiOS/issues/2494) · Labels: none
- [PR #2526: M111e: status row + live-gpu3d leg-01 screenshot race fix — K=5 acceptance](https://github.com/drawmeanelephant/VirelaiOS/pull/2526) · Issues: [#2412](https://github.com/drawmeanelephant/VirelaiOS/issues/2412) · Labels: none

## M113: measured boot and capability grants

ADR 0054 made measurement the trust key: a Secure Boot flag with a pinned KERNEL.BIN and chain receipt, measured exec with a kernel grant table, signed ReleaseCaps with GOSET consent, per-app grant posture in GOTOP, and a K=5 confined-app acceptance.

- [PR #2427: M113a: ADR 0054 — measurement is the trust key](https://github.com/drawmeanelephant/VirelaiOS/pull/2427) · Issues: [#2417](https://github.com/drawmeanelephant/VirelaiOS/issues/2417) · Labels: none
- [PR #2429: M113b (#2418): verified + measured boot — Secure Boot flag, KERNEL.BIN pin, chain receipt](https://github.com/drawmeanelephant/VirelaiOS/pull/2429) · Issues: [#2418](https://github.com/drawmeanelephant/VirelaiOS/issues/2418) · Labels: none
- [PR #2442: M113c (#2419): measured exec + kernel grant table + audit-mode word gates](https://github.com/drawmeanelephant/VirelaiOS/pull/2442) · Issues: [#2419](https://github.com/drawmeanelephant/VirelaiOS/issues/2419) · Labels: none
- [PR #2453: M113d (#2420): signed ReleaseCaps + GOSET grants consent over slot 89](https://github.com/drawmeanelephant/VirelaiOS/pull/2453) · Issues: [#2420](https://github.com/drawmeanelephant/VirelaiOS/issues/2420) · Labels: none
- [PR #2500: M113e (#2421): GOTOP trust tab — per-app grant posture + receipts](https://github.com/drawmeanelephant/VirelaiOS/pull/2500) · Issues: [#2421](https://github.com/drawmeanelephant/VirelaiOS/issues/2421) · Labels: none
- [PR #2511: M113f (#2422): acceptance — verified boot + grants K=5, end-to-end confined app, audit data table for M116](https://github.com/drawmeanelephant/VirelaiOS/pull/2511) · Issues: [#2422](https://github.com/drawmeanelephant/VirelaiOS/issues/2422) · Labels: none

## M114: the host window grows up

A HostCommand action bus drives a full menu bar; the viewport resizes with aspect lock; the M108 lanes default on with runtime suspend/resume; an inspector drawer tails serial, injects input, and runs postmortem; one chord captures a bounded MP4 — all closed by a K=3 full-window journey.

- [PR #2497: M114a (#2454): HostCommand action bus + full menu bar + --host-cmd-after mirror](https://github.com/drawmeanelephant/VirelaiOS/pull/2497) · Issues: [#2454](https://github.com/drawmeanelephant/VirelaiOS/issues/2454) · Labels: none
- [PR #2501: M114b (#2455): resizable/fullscreen aspect-locked viewport + capturesSystemKeys toggle](https://github.com/drawmeanelephant/VirelaiOS/pull/2501) · Issues: [#2455](https://github.com/drawmeanelephant/VirelaiOS/issues/2455) · Labels: none
- [PR #2505: M114c (#2456): M108 lanes on by default in just session + runtime lane suspend/resume + send-file](https://github.com/drawmeanelephant/VirelaiOS/pull/2505) · Issues: [#2456](https://github.com/drawmeanelephant/VirelaiOS/issues/2456) · Labels: none
- [PR #2508: M114d (#2457): inspector drawer — serial tail/search, send-serial injector, HostInt counters, in-window postmortem](https://github.com/drawmeanelephant/VirelaiOS/pull/2508) · Issues: [#2457](https://github.com/drawmeanelephant/VirelaiOS/issues/2457) · Labels: none
- [PR #2512: M114e (#2458): one-chord capture — bounded ≤60 s MP4 into share CAPTURES/](https://github.com/drawmeanelephant/VirelaiOS/pull/2512) · Issues: [#2458](https://github.com/drawmeanelephant/VirelaiOS/issues/2458) · Labels: none
- [PR #2515: M114f (#2459): acceptance — live-host-ui full-window journey K=3](https://github.com/drawmeanelephant/VirelaiOS/pull/2515) · Issues: [#2459](https://github.com/drawmeanelephant/VirelaiOS/issues/2459) · Labels: none

## M115: persistence and self-update

The .virelaivm profile bundle stabilized the TCC identity; vsock carried virelai-ctl/1 control and guest-initiated suspend; GOSNAP manages named snapshots; GOUPD staged signed OS self-update with .prev rollback — proven K=3 on one profile across suspend, resume, update, apply, and rollback.

- [PR #2524: M115a (#2460): VZVirtioSocketDevice --vsock probe across save/restore](https://github.com/drawmeanelephant/VirelaiOS/pull/2524) · Issues: [#2460](https://github.com/drawmeanelephant/VirelaiOS/issues/2460) · Labels: none
- [PR #2531: M115b (#2461, ADR 0056): virelai-ctl/1 — guest→host vsock control channel](https://github.com/drawmeanelephant/VirelaiOS/pull/2531) · Issues: [#2461](https://github.com/drawmeanelephant/VirelaiOS/issues/2461) · Labels: none
- [PR #2536: M115c: .virelaivm profile bundle + --profile; stable TCC identity](https://github.com/drawmeanelephant/VirelaiOS/pull/2536) · Issues: none · Labels: none
- [PR #2541: M115d (#2463): guest-initiated suspend via sys_vsock_ctl + --resume restore](https://github.com/drawmeanelephant/VirelaiOS/pull/2541) · Issues: [#2463](https://github.com/drawmeanelephant/VirelaiOS/issues/2463) · Labels: none
- [PR #2542: M115e: GOSNAP.ELF — list/restore/delete named snapshots, consent-gated](https://github.com/drawmeanelephant/VirelaiOS/pull/2542) · Issues: [#2464](https://github.com/drawmeanelephant/VirelaiOS/issues/2464) · Labels: none
- [PR #2543: M115f (#2465): guest-staged signed OS self-update — GOUPD + --apply-update/.prev rollback](https://github.com/drawmeanelephant/VirelaiOS/pull/2543) · Issues: [#2465](https://github.com/drawmeanelephant/VirelaiOS/issues/2465) · Labels: none
- [PR #2546: M115g (#2466): live-persistence — suspend/resume/update/apply/rollback journey K=3](https://github.com/drawmeanelephant/VirelaiOS/pull/2546) · Issues: [#2466](https://github.com/drawmeanelephant/VirelaiOS/issues/2466) · Labels: none

## M116: grants move from audit to enforce

Compiled base rows for every shipped app narrowed the ambient floor to named EACCES refusals; install-time consent lands grant rows through GOSTORE; network and filesystem scope is allowlisted per app; a negative corpus names every refused escape; and a confined third-party journey closed the arc.

- [PR #2513: M116a (#2467): fleet grant policy — compiled base rows for every shipped app](https://github.com/drawmeanelephant/VirelaiOS/pull/2513) · Issues: [#2467](https://github.com/drawmeanelephant/VirelaiOS/issues/2467) · Labels: none
- [PR #2527: M116b (#2468): audit→enforce — narrow the ambient floor, named EACCES refusals](https://github.com/drawmeanelephant/VirelaiOS/pull/2527) · Issues: [#2468](https://github.com/drawmeanelephant/VirelaiOS/issues/2468) · Labels: none
- [PR #2532: M116c (#2469): install-time consent — GOSTORE prompt + STORE consent verb land the grant row](https://github.com/drawmeanelephant/VirelaiOS/pull/2532) · Issues: [#2469](https://github.com/drawmeanelephant/VirelaiOS/issues/2469) · Labels: none
- [PR #2535: M116d (#2470): per-app net host:port allowlist + fs path scope in grant rows](https://github.com/drawmeanelephant/VirelaiOS/pull/2535) · Issues: [#2470](https://github.com/drawmeanelephant/VirelaiOS/issues/2470) · Labels: none
- [PR #2537: M116e (#2471): sandbox negative corpus — every escape refused by name](https://github.com/drawmeanelephant/VirelaiOS/pull/2537) · Issues: [#2471](https://github.com/drawmeanelephant/VirelaiOS/issues/2471) · Labels: none
- [PR #2540: M116f: confined third-party app journey — acceptance gate](https://github.com/drawmeanelephant/VirelaiOS/pull/2540) · Issues: [#2472](https://github.com/drawmeanelephant/VirelaiOS/issues/2472) · Labels: none

## M117: IPv6

ADR 0057 scoped SLAAC/NDP/ICMPv6 with bounded state; the datapath, UDP/TCP over v6, ping6, and literal dialing landed; opt-in bridged networking put the guest on a real LAN; grant scope learned v6 prefixes; and live legs proved web-v6 and bridged httpd, K=3.

- [PR #2499: M117a (#2473): ADR 0057 — IPv6 scope (SLAAC/NDP/ICMPv6, bounded state)](https://github.com/drawmeanelephant/VirelaiOS/pull/2499) · Issues: [#2473](https://github.com/drawmeanelephant/VirelaiOS/issues/2473) · Labels: none
- [PR #2509: M117b (#2474): IPv6 datapath — ipv6.zig + ndp.zig + hermetic responders + live-net-v6](https://github.com/drawmeanelephant/VirelaiOS/pull/2509) · Issues: [#2474](https://github.com/drawmeanelephant/VirelaiOS/issues/2474) · Labels: none
- [PR #2517: M117d (#2476): --net-bridged / --net-vmnet opt-in runner flags for a real LAN](https://github.com/drawmeanelephant/VirelaiOS/pull/2517) · Issues: [#2476](https://github.com/drawmeanelephant/VirelaiOS/issues/2476) · Labels: none
- [PR #2521: M117c: UDP/TCP over IPv6, ping6, vi.Dial v6 literal](https://github.com/drawmeanelephant/VirelaiOS/pull/2521) · Issues: [#2475](https://github.com/drawmeanelephant/VirelaiOS/issues/2475) · Labels: none
- [PR #2523: M117e (#2477): grant net scope — v6 prefixes + bridged path class, named refusals](https://github.com/drawmeanelephant/VirelaiOS/pull/2523) · Issues: [#2477](https://github.com/drawmeanelephant/VirelaiOS/issues/2477) · Labels: none
- [PR #2530: M117f (#2478): live-web v6 leg + live-net-bridged LAN-httpd leg, K=3](https://github.com/drawmeanelephant/VirelaiOS/pull/2530) · Issues: [#2478](https://github.com/drawmeanelephant/VirelaiOS/issues/2478) · Labels: none

## M118: audio capture

The --sound-in probe read the INPUT PCM topology and mic/TCC verdict, virtio-snd gained a bounded capture ring, sys_audio capture slots surfaced through vi, and GOREC records to WAV with GOSOUND's record mode — K=3 on the capture chain and its negatives.

- [PR #2516: M118a (#2479): --sound-in probe — INPUT PCM_INFO topology + mic/TCC verdict](https://github.com/drawmeanelephant/VirelaiOS/pull/2516) · Issues: [#2479](https://github.com/drawmeanelephant/VirelaiOS/issues/2479) · Labels: none
- [PR #2520: M118b (#2480): virtio-snd capture path — RX queue, bounded ring, S16/48k/mono cap](https://github.com/drawmeanelephant/VirelaiOS/pull/2520) · Issues: [#2480](https://github.com/drawmeanelephant/VirelaiOS/issues/2480) · Labels: none
- [PR #2525: M118c (#2481): sys_audio capture slots + vi capture API + go-selftest leg](https://github.com/drawmeanelephant/VirelaiOS/pull/2525) · Issues: [#2481](https://github.com/drawmeanelephant/VirelaiOS/issues/2481) · Labels: none
- [PR #2529: M118d (#2482): GOREC.ELF records N s to WAV via WriteFileSafe](https://github.com/drawmeanelephant/VirelaiOS/pull/2529) · Issues: [#2482](https://github.com/drawmeanelephant/VirelaiOS/issues/2482) · Labels: none
- [PR #2533: M118e (#2483): GOSOUND adopts record — mic meter + record mode](https://github.com/drawmeanelephant/VirelaiOS/pull/2533) · Issues: [#2483](https://github.com/drawmeanelephant/VirelaiOS/issues/2483) · Labels: none
- [PR #2539: M118f (#2484): acceptance — capture chain + negatives K=3; M118 status row](https://github.com/drawmeanelephant/VirelaiOS/pull/2539) · Issues: [#2484](https://github.com/drawmeanelephant/VirelaiOS/issues/2484) · Labels: none

## M119: measured performance

Every budget in the bench corpus is now pinned from a measured baseline under a class-B spec; the IPC mailbox ring grew 8 to 32; the spawn-shape matrix priced a third-child death; and the M70g USB-MSD restore refusal was characterized rather than silently swallowed.

- [PR #2498: M119a: benchmark corpus + budget table — every budget pinned from a measured baseline](https://github.com/drawmeanelephant/VirelaiOS/pull/2498) · Issues: [#2485](https://github.com/drawmeanelephant/VirelaiOS/issues/2485) · Labels: none
- [PR #2502: M119d: spawn-shape matrix + ADR 0035 amendment 11 (third-child death = task-pool bound)](https://github.com/drawmeanelephant/VirelaiOS/pull/2502) · Issues: none · Labels: none
- [PR #2504: M119b (#2486): live-bench class-B spec — run the corpus on VZ, fail on breach](https://github.com/drawmeanelephant/VirelaiOS/pull/2504) · Issues: [#2486](https://github.com/drawmeanelephant/VirelaiOS/issues/2486) · Labels: none
- [PR #2506: M119e (#2489): characterize the M70g G3 USB-MSD 8/9 restore refusal](https://github.com/drawmeanelephant/VirelaiOS/pull/2506) · Issues: [#2489](https://github.com/drawmeanelephant/VirelaiOS/issues/2489) · Labels: none
- [PR #2514: M119c: raise the IPC mailbox ring 8 → 32 + a bench leg that pins it](https://github.com/drawmeanelephant/VirelaiOS/pull/2514) · Issues: [#2487](https://github.com/drawmeanelephant/VirelaiOS/issues/2487) · Labels: none

## The usability wave opens

Late in the week the M120–M129 arcs filed their indexes and began landing: session persistence wiring, the seat command palette, incremental builds, a clickable GOSET, the ghost-app purge, machine settings, WEB history/bookmarks/find, the OUTBOX export lane, the AGENTD companion, a host-integration v2 amendment, and the measured capacity table.

- [PR #2614: M127a: ADR 0051 v2 amendment — sender_id, TagOutbox, TagNotifyFocus, drop-toast actions](https://github.com/drawmeanelephant/VirelaiOS/pull/2614) · Issues: [#2599](https://github.com/drawmeanelephant/VirelaiOS/issues/2599) · Labels: none
- [PR #2615: M120a (#2558): just-session persistence wiring — VLFS mint, ctl/update/agent credentials, --vsock --vsock-ctl, save-capable share posture](https://github.com/drawmeanelephant/VirelaiOS/pull/2615) · Issues: [#2558](https://github.com/drawmeanelephant/VirelaiOS/issues/2558) · Labels: none
- [PR #2625: M127e (#2603): AGENTD companion — status pill, HMAC-verified receipts, lane-agent choke](https://github.com/drawmeanelephant/VirelaiOS/pull/2625) · Issues: [#2603](https://github.com/drawmeanelephant/VirelaiOS/issues/2603) · Labels: none
- [PR #2626: M126a (#2593): WEB history dropdown + go/stop chip + live-web nav leg](https://github.com/drawmeanelephant/VirelaiOS/pull/2626) · Issues: [#2593](https://github.com/drawmeanelephant/VirelaiOS/issues/2593) · Labels: none
- [PR #2627: M127d (#2602): OUTBOX export lane — GOSH export + GOFILES Export to Host → signed TagOutbox → host Downloads](https://github.com/drawmeanelephant/VirelaiOS/pull/2627) · Issues: [#2602](https://github.com/drawmeanelephant/VirelaiOS/issues/2602) · Labels: none
- [PR #2629: M129c: capacity table in hardware-contract.md — measured ceilings with named symptoms](https://github.com/drawmeanelephant/VirelaiOS/pull/2629) · Issues: [#2612](https://github.com/drawmeanelephant/VirelaiOS/issues/2612) · Labels: none
- [PR #2631: M121a: seat-owned command palette on ctrl+shift+p (#2564)](https://github.com/drawmeanelephant/VirelaiOS/pull/2631) · Issues: [#2564](https://github.com/drawmeanelephant/VirelaiOS/issues/2564) · Labels: none
- [PR #2632: M122a: incremental just-session builds + toolchain doctor](https://github.com/drawmeanelephant/VirelaiOS/pull/2632) · Issues: [#2570](https://github.com/drawmeanelephant/VirelaiOS/issues/2570) · Labels: none
- [PR #2633: M122e: clickable GOSET — tab headers, click-to-toggle rows, Esc back-out](https://github.com/drawmeanelephant/VirelaiOS/pull/2633) · Issues: [#2574](https://github.com/drawmeanelephant/VirelaiOS/issues/2574) · Labels: none
- [PR #2635: M128a: ⌘, machine settings panel — HostSettingsModel/View on the bus](https://github.com/drawmeanelephant/VirelaiOS/pull/2635) · Issues: [#2605](https://github.com/drawmeanelephant/VirelaiOS/issues/2605) · Labels: none
- [PR #2642: M126b (#2594): WEB bookmarks — FLINT-store atoms, Ctrl+D, Ctrl+Shift+B sidebar](https://github.com/drawmeanelephant/VirelaiOS/pull/2642) · Issues: [#2594](https://github.com/drawmeanelephant/VirelaiOS/issues/2594) · Labels: none
- [PR #2644: M122c: ghost-app purge — every launcher row opens a window (#2572)](https://github.com/drawmeanelephant/VirelaiOS/pull/2644) · Issues: [#2572](https://github.com/drawmeanelephant/VirelaiOS/issues/2572) · Labels: none
- [PR #2656: M126c (#2595): WEB Ctrl+F find-in-page bar + shared find matcher](https://github.com/drawmeanelephant/VirelaiOS/pull/2656) · Issues: [#2595](https://github.com/drawmeanelephant/VirelaiOS/issues/2595) · Labels: none

## Kernel, relay, and gate fixes

Cross-cutting repairs: a bounded slot-31 send drain, relay dedup and MSS pacing, a uaccess range fix, .disk handle staleness, host test gating, exec-churn starvation, the share-channel transient, poll-gated relay deadlines, MP4 encoder back-pressure, staged per-call vsys.Open, serial line ownership, conflict-marker resolution in the net gates, argv-block residuals, and gostalgia/fork overlay exclusions — plus a site refresh and the M119f fleet-hardening K-pass.

- [PR #2237: syscall: bounded drain+poll+retry on slot-31 send — no EAGAIN leak (#2233)](https://github.com/drawmeanelephant/VirelaiOS/pull/2237) · Issues: [#2233](https://github.com/drawmeanelephant/VirelaiOS/issues/2233) · Labels: none
- [PR #2342: just: fix arg passthrough — {{ARGS}} splat silently re-splits quoted flag values](https://github.com/drawmeanelephant/VirelaiOS/pull/2342) · Issues: none · Labels: none
- [PR #2343: shlib: fix stale open-test fixture — audio has a handler since M99c](https://github.com/drawmeanelephant/VirelaiOS/pull/2343) · Issues: none · Labels: none
- [PR #2346: docs: refresh public site + README for the M100/M101/M107/M108 landings](https://github.com/drawmeanelephant/VirelaiOS/pull/2346) · Issues: none · Labels: none
- [PR #2352: runner relay: dedup retransmits + pace by guest MSS/window (#2349)](https://github.com/drawmeanelephant/VirelaiOS/pull/2352) · Issues: [#2349](https://github.com/drawmeanelephant/VirelaiOS/issues/2349) · Labels: none
- [PR #2359: site: extend milestone coverage through M104+ and refresh the open arcs](https://github.com/drawmeanelephant/VirelaiOS/pull/2359) · Issues: [#2357](https://github.com/drawmeanelephant/VirelaiOS/issues/2357) · Labels: none
- [PR #2360: #2354: uaccess range_ok covers ranges straddling adjacent armed regions](https://github.com/drawmeanelephant/VirelaiOS/pull/2360) · Issues: [#2354](https://github.com/drawmeanelephant/VirelaiOS/issues/2354) · Labels: none
- [PR #2388: #2381: blk_boot_probe targets a scratch block; status.md M105 row points at flint-client](https://github.com/drawmeanelephant/VirelaiOS/pull/2388) · Issues: [#2381](https://github.com/drawmeanelephant/VirelaiOS/issues/2381) · Labels: none
- [PR #2391: #2375+#2374: .disk handle mode and staleness fixes (KFT lane)](https://github.com/drawmeanelephant/VirelaiOS/pull/2391) · Issues: [#2374](https://github.com/drawmeanelephant/VirelaiOS/issues/2374), [#2375](https://github.com/drawmeanelephant/VirelaiOS/issues/2375) · Labels: none
- [PR #2392: #2373: gate Go/Swift host tests; fix slot-80-stale tests and artifact skips](https://github.com/drawmeanelephant/VirelaiOS/pull/2392) · Issues: [#2373](https://github.com/drawmeanelephant/VirelaiOS/issues/2373) · Labels: none
- [PR #2393: #2380: Go minors — hostdrop sanitize, diag~ sweep, hostint dirlist lane + reserved flags, edit diag strictness](https://github.com/drawmeanelephant/VirelaiOS/pull/2393) · Issues: [#2380](https://github.com/drawmeanelephant/VirelaiOS/issues/2380) · Labels: none
- [PR #2396: NetTcpRelay hardening: Int32.min trap, bounded tx, death-path teardown, ACK clamp + async connect](https://github.com/drawmeanelephant/VirelaiOS/pull/2396) · Issues: [#2366](https://github.com/drawmeanelephant/VirelaiOS/issues/2366), [#2368](https://github.com/drawmeanelephant/VirelaiOS/issues/2368), [#2369](https://github.com/drawmeanelephant/VirelaiOS/issues/2369), [#2377](https://github.com/drawmeanelephant/VirelaiOS/issues/2377) · Labels: none
- [PR #2437: kernel+toolchain: fix exec-churn task/resource starvation (#2430)](https://github.com/drawmeanelephant/VirelaiOS/pull/2437) · Issues: [#2430](https://github.com/drawmeanelephant/VirelaiOS/issues/2430) · Labels: none
- [PR #2518: #2495: share-channel transient — spliced path staging + parked-chain hazards](https://github.com/drawmeanelephant/VirelaiOS/pull/2518) · Issues: [#2495](https://github.com/drawmeanelephant/VirelaiOS/issues/2495) · Labels: none
- [PR #2538: #2507: VMTcp relay tests — poll-gated read/accept so deadlines are real](https://github.com/drawmeanelephant/VirelaiOS/pull/2538) · Issues: [#2507](https://github.com/drawmeanelephant/VirelaiOS/issues/2507) · Labels: none
- [PR #2544: vector: re-measure on nonzero AllocsPerRun (#2534)](https://github.com/drawmeanelephant/VirelaiOS/pull/2544) · Issues: [#2534](https://github.com/drawmeanelephant/VirelaiOS/issues/2534) · Labels: none
- [PR #2628: #2616: queue produced frames through encoder back-pressure — fix testToggleWritesARealMp4 flake](https://github.com/drawmeanelephant/VirelaiOS/pull/2628) · Issues: [#2616](https://github.com/drawmeanelephant/VirelaiOS/issues/2616) · Labels: none
- [PR #2630: #2519: vsys.Open stages paths per-call — drop the shared virPathStaging](https://github.com/drawmeanelephant/VirelaiOS/pull/2630) · Issues: [#2519](https://github.com/drawmeanelephant/VirelaiOS/issues/2519) · Labels: none
- [PR #2636: fleet hardening: 54 fixes from the M119f K-pass (part of #2490)](https://github.com/drawmeanelephant/VirelaiOS/pull/2636) · Issues: [#2490](https://github.com/drawmeanelephant/VirelaiOS/issues/2490) · Labels: none
- [PR #2643: go: keep gostalgia stage overlays out of the fork src tree (#2634)](https://github.com/drawmeanelephant/VirelaiOS/pull/2643) · Issues: [#2634](https://github.com/drawmeanelephant/VirelaiOS/issues/2634) · Labels: none
- [PR #2646: env-check: exclude gostalgia stage overlays from fork drift cmp](https://github.com/drawmeanelephant/VirelaiOS/pull/2646) · Issues: [#2645](https://github.com/drawmeanelephant/VirelaiOS/issues/2645) · Labels: none
- [PR #2655: argv-block residuals: dead pads/asserts, stale bounds, census re-pin (#2651)](https://github.com/drawmeanelephant/VirelaiOS/pull/2655) · Issues: [#2651](https://github.com/drawmeanelephant/VirelaiOS/issues/2651) · Labels: none
- [PR #2657: net gates (#2647): resolve committed conflict markers, fix ARP-async gates, de-collide clis relay port](https://github.com/drawmeanelephant/VirelaiOS/pull/2657) · Issues: [#2647](https://github.com/drawmeanelephant/VirelaiOS/issues/2647) · Labels: none
- [PR #2658: #2649: serial open-line ownership + one-write marker lines](https://github.com/drawmeanelephant/VirelaiOS/pull/2658) · Issues: [#2649](https://github.com/drawmeanelephant/VirelaiOS/issues/2649) · Labels: none
