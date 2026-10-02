---
title: 2026-W36 merged PR chronicle
status: published
tags: [chronicle, merged-prs]
---

# 2026-W36: September 1–6, 17 selected PRs

## M34–M35: host files and WASM

M34 moved user data onto the host folder and added file-channel cloning; M35 completed WASM core work and the `wc` capstone.

- [PR #792: M34 HF5: user-data migration to the host folder (#739)](https://github.com/drawmeanelephant/VirelaiOS/pull/792) · Issues: [#739](https://github.com/drawmeanelephant/VirelaiOS/issues/739) · Labels: `m34-host-file-channel`
- [PR #816: M34 HF7 CLONE (issue #741) + #810 self-decoding instrumentation (claims 1312/9094)](https://github.com/drawmeanelephant/VirelaiOS/pull/816) · Issues: [#741](https://github.com/drawmeanelephant/VirelaiOS/issues/741), [#810](https://github.com/drawmeanelephant/VirelaiOS/issues/810) · Labels: none
- [PR #813: M35 W4+W5: wasm-core completeness + the wc capstone (closes #765, #766)](https://github.com/drawmeanelephant/VirelaiOS/pull/813) · Issues: [#765](https://github.com/drawmeanelephant/VirelaiOS/issues/765), [#766](https://github.com/drawmeanelephant/VirelaiOS/issues/766) · Labels: none
- [PR #815: M35 post: cross-language author proof — virelai.zig shim + a rustc-authored nl app (claim 9746)](https://github.com/drawmeanelephant/VirelaiOS/pull/815) · Issues: none · Labels: none

## M37–M38: desktop polish and typography

The desktop work added design tokens and snap guides, followed by vector typography using Inter and Fira Code.

- [PR #886: M37 DQ4: design tokens & cohesion](https://github.com/drawmeanelephant/VirelaiOS/pull/886) · Issues: [#838](https://github.com/drawmeanelephant/VirelaiOS/issues/838) · Labels: none
- [PR #894: M37 DQ5: window snap guides (issue #837)](https://github.com/drawmeanelephant/VirelaiOS/pull/894) · Issues: [#837](https://github.com/drawmeanelephant/VirelaiOS/issues/837), [#888](https://github.com/drawmeanelephant/VirelaiOS/issues/888) · Labels: none
- [PR #913: M38: High-Fidelity Desktop & Vector Typography (Inter + Fira Code)](https://github.com/drawmeanelephant/VirelaiOS/pull/913) · Issues: [#821](https://github.com/drawmeanelephant/VirelaiOS/issues/821), [#905](https://github.com/drawmeanelephant/VirelaiOS/issues/905) · Labels: none

## M40–M41: declarative verification fleet

M40 introduced discovered gate inventory and frozen vgate specs, then rewired `just` and CI sharding and closed the rollout; M41 completed the harness cutover.

- [PR #942: M40 GF1: generated gate fleet inventory + freeze rule](https://github.com/drawmeanelephant/VirelaiOS/pull/942) · Issues: [#934](https://github.com/drawmeanelephant/VirelaiOS/issues/934), [#941](https://github.com/drawmeanelephant/VirelaiOS/issues/941) · Labels: none
- [PR #946: M40 GF2: vgate harness + frozen spec format + 4 pilots](https://github.com/drawmeanelephant/VirelaiOS/pull/946) · Issues: [#937](https://github.com/drawmeanelephant/VirelaiOS/issues/937), [#944](https://github.com/drawmeanelephant/VirelaiOS/issues/944) · Labels: none
- [PR #948: M40 GF3 wave 1a: core + net-core gates as specs, delete originals](https://github.com/drawmeanelephant/VirelaiOS/pull/948) · Issues: [#938](https://github.com/drawmeanelephant/VirelaiOS/issues/938) · Labels: none
- [PR #960: M40 GF3: wave-1 heavy-net core - 6 net gates as specs, delete originals](https://github.com/drawmeanelephant/VirelaiOS/pull/960) · Issues: none · Labels: none
- [PR #967: M40 GF3: wave-1 shell rest - 12 proc/shell/store gates as specs, delete originals](https://github.com/drawmeanelephant/VirelaiOS/pull/967) · Issues: none · Labels: none
- [PR #1018: M40 GF5: rewire entry points — just, CI sharding, generated inventory](https://github.com/drawmeanelephant/VirelaiOS/pull/1018) · Issues: [#940](https://github.com/drawmeanelephant/VirelaiOS/issues/940), [#1017](https://github.com/drawmeanelephant/VirelaiOS/issues/1017) · Labels: none
- [PR #1021: M40 GF6: closeout — full-fleet evidence, scoreboard, permanent spec rule](https://github.com/drawmeanelephant/VirelaiOS/pull/1021) · Issues: [#931](https://github.com/drawmeanelephant/VirelaiOS/issues/931), [#1019](https://github.com/drawmeanelephant/VirelaiOS/issues/1019) · Labels: none
- [PR #970: M41 TS6: Gate Harness Cutover, Verification & Milestone Closeout (#957)](https://github.com/drawmeanelephant/VirelaiOS/pull/970) · Issues: [#951](https://github.com/drawmeanelephant/VirelaiOS/issues/951), [#957](https://github.com/drawmeanelephant/VirelaiOS/issues/957), [#969](https://github.com/drawmeanelephant/VirelaiOS/issues/969) · Labels: none

## M42: Sexiburger desktop

M42 made TABWM the primary desktop manager and rolled dock apps onto its full-viewport app seam.

- [PR #1000: M42: The Sexiburger Desktop — proper 🐙🍔 emblem, TABWM as the primary manager, the full-screen app seam + library (SX1–SX3, SX5, CALC exemplar)](https://github.com/drawmeanelephant/VirelaiOS/pull/1000) · Issues: [#981](https://github.com/drawmeanelephant/VirelaiOS/issues/981) · Labels: none
- [PR #1002: M42 SX4: fleet rollout — dock apps tab-aware, full-viewport in TABWM](https://github.com/drawmeanelephant/VirelaiOS/pull/1002) · Issues: [#985](https://github.com/drawmeanelephant/VirelaiOS/issues/985) · Labels: none
