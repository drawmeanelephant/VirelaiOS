<!-- docs/registry/boris.md — row fragment for docs/component-registry.md. The
     aggregate is GENERATED (tools/component-registry/compile.py);
     edit the row cells below + rerun the compiler. Never hand-edit
     docs/component-registry.md. `section`/`order` place the block in
     the emitted table; new blocks pick a free order between
     neighbours. -->
---
key: boris
section: host-tools
order: 30
---

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| boris (docs site toolchain) | host-tool | tools/zig/boris | pin .github/boris-pin.json | .github/workflows/docs-gate.yml | host |
