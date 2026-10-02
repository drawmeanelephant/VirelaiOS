---
title: Evidence & testing
status: published
tags: [evidence, testing]
---

# Evidence & testing

VirelaiOS has unusually serious verification machinery for a hobby OS. The
core rule: **a feature is only "observed" when a matching gate passes.** No
evidence, no claim.

## The two classes

| Class | What it is | Where it runs |
|-------|------------|---------------|
| **A** — deterministic | formatting, unit tests, a byte-identical console transcript, the build/image pipeline | GitHub CI, every push/PR |
| **B** — live-gated | boots a real Virtualization.framework VM on Apple silicon and asserts on what the kernel reports | developer host, not CI |

The distinction matters because class B is the only thing that observes the
kernel running under real firmware — and it cannot run in CI.

- [[class-a|Deterministic checks]] — what runs without a VM and why it still counts.
- [[live-gates|Live VZ gates]] — the hardware-gated evidence.
- [[claims|Claims & evidence philosophy]] — how a claim is filed and flipped to observed.

## The honest vocabulary

| Term | Meaning |
|------|---------|
| **observed** | a saved log or capture exists under `artifacts/` matching the claim |
| **inferred** | reasoned about or documented, but not yet observed on hardware |
| **claim** | a filed unit of work with an id, scope, and gate |
| **gate** | a declarative spec (`tools/gate/specs/*.spec`) run by `just gate <id>` that exits 0 only on passing evidence |

Hardware assumptions are marked `[observed]` vs `[inferred]` in
[`docs/hardware-contract.md`](https://github.com/drawmeanelephant/VirelaiOS/blob/main/docs/hardware-contract.md)
and only flip when a matching probe or serial log lands.

## Reproducing the checks

```bash
just verify-portable   # class A, mirrors CI
just verify-vz         # class B, Apple silicon, boots real VMs
```

The aggregate `verify-vz` sweep re-checks the shared seam across every
subsystem in one class B run — the project's standing regression proof.
(The fleet is discovered by `tools/gate/fleet.sh`; `just gate-list` names the specs.)

<Aside kind="warning">

**LIMITATION.** `zig build` and the transcript test prove code paths, not
hardware behavior. Only the class B gates observe the kernel under real
firmware; the documentation never blurs the two.

</Aside>
