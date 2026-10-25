<!-- docs/registry/gocmdlink.md — row fragment for docs/component-registry.md. The
     aggregate is GENERATED (tools/component-registry/compile.py);
     edit the row cells below + rerun the compiler. Never hand-edit
     docs/component-registry.md. `section`/`order` place the block in
     the emitted table; new blocks pick a free order between
     neighbours. -->
---
key: gocmdlink
section: gate-fleet
order: 150
---

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| Guest linker | tool | GOCMDLINK.ELF | ver 1.0.0 | tools/go/build-gotool.sh, tools/gate/specs/live-gobuild.spec | staged |
