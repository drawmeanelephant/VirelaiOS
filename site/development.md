---
title: Development
status: published
tags: [development, contributing]
---

# Development

How the repository is laid out, how to change it safely, and where the real
engineering documents live.

- [[layout|Repository layout]] — where the code, docs, and gates live.
- [[contributing|Contributing & toolchain]] — toolchain pins, the claims ceremony, and what a PR must pass.

## The short version

```bash
git clone https://github.com/drawmeanelephant/VirelaiOS.git
cd VirelaiOS
zig build            # build
just verify-portable # class A (mirrors CI)
just verify-vz       # class B (Apple silicon, real VMs)
```

Read `AGENTS.md` before changing anything — it is the project's rulebook.

## The two docs surfaces

There are deliberately two documentation surfaces:

1. **This site** (`site/`) — the public front door, compiled by Boris.
2. **The engineering warehouse** (`docs/`) — decisions (ADRs), gate
   inventory, hardware contract, status, roadmap, march trackers, and
   archived prompts.

The warehouse is source material, not automatically-published pages. The
public site promotes and summarizes it, and links to the canonical files on
GitHub rather than duplicating them.

## Where to look for ugly detail

- `docs/decisions/` — the ADR set: binding decisions from kernel proper
  (0004), runtime function tables, the MMU debt boundary, and the syscall
  ABI (0007) onward.
- GitHub issues labeled `claim` — per-claim scope and evidence (claims
  live on the tracker, not in repo files).
- `docs/hardware-contract.md` — observed vs inferred hardware facts.
- `docs/gate-inventory.md` — the lean gate table; full historical detail in
  `docs/archive/gate-inventory-detail.md`.
- `docs/status.md` — the canonical living status.

<Aside kind="note">

**PLANNED.** The project accepts contributions that follow `AGENTS.md` — the
claims ceremony and the two-class evidence discipline are not optional for
merged work.

</Aside>
