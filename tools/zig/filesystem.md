# Shared Zig filesystem bridge

Use the pinned SDK recipe in ADR 0038. This bridge exposes merged ADR 0007
B2/B3/B4/B5; it does not amend the frozen workload budgets or add a syscall.

```zig
const fs = sdk.filesystem(); // after sdk.initialize; same table as sdk.io
const meta = try fs.metadata("/host/content", "nested/page.md");
const identity = try fs.identity("/host/content", "nested/page.md");
const file = try fs.openContained("/host/content", "nested/page.md", .read);
defer file.close(sdk.io);
// Read/write/sync/truncate use sdk.io on this pinned file, never its old path.

const cursor = try fs.openSnapshot("/host/content", "");
defer fs.closeSnapshot(cursor) catch sdk.fail("CloseFailed", 70);
var page: sdk.fs.Page = undefined; // 5,776 bytes, or allocate in the SDK arena
var offset: u64 = 0;
while (true) {
    try fs.snapshotPage(cursor, offset, 16, &page);
    for (page.rows()) |*row| {
        _ = row.nameBytes(); // lossless borrowed bytes, not NUL scanning
        _ = row.metadata; // full kind, u64 size, identity, timestamps, host facts
    }
    offset = page.header.next;
    if (page.done()) break;
}

// B5: name operations act on a pinned directory object, not a re-walked path.
const out = try fs.openDirectory("/host/site", "");
defer fs.closeDirectory(out) catch sdk.fail("CloseFailed", 70);
const stage = try fs.makeDirectory(out, "stage"); // returned already pinned
const html = try fs.createExclusive(stage, "index.html"); // write-only, new only
// ... write, sync; html.length(sdk.io) is the fd's own object (op 10).
html.close(sdk.io);
try fs.rename(out, "stage", out, "live", .preserve_existing); // stales `stage`
try fs.closeDirectory(stage); // still required for a stale pin
```

## Supported boundary

| API | Meaning |
|---|---|
| `metadata(root, relative)`, `identity(root, relative)` | Fresh no-follow path queries, not metadata for an earlier open file. Identity is the pair `(filesystem, inode)`, valid only within the mount lifetime. Host mode/uid/gid are facts, never guest permissions. |
| `openContained(root, relative, .read/.write)` | One slot-79 existing regular-file open, pinned through lookup/use. Write-open does **not** create, truncate or append. No stat-then-open. |
| `openSnapshot`, `snapshotPage`, `closeSnapshot` | Immutable rich membership/metadata snapshot. Indexed, repeatable pages, explicit end, lossless 255-byte names. Page errors leave caller output and offset unchanged. A new snapshot is needed for fresh membership. |
| `openDirectory(root, relative)`, `openChildDirectory(dir, name)`, `closeDirectory` | B5 pins of an existing directory: B3's no-follow walk, or one no-follow lookup from a live pin. Each use reauthorizes the pin's recorded name. A guest rename or removal at or under that name stales the pin (`InvalidHandle` until closed); host-side moves are not observed. |
| `directoryMetadata(dir)`, `fileMetadata(file)`, `File.length` | Live attributes of the pin's or contained fd's own object (op 10), never of whatever its old name resolves to now. A changed identity is `InvalidHandle`. |
| `createExclusive(dir, name)`, `makeDirectory(dir, name)` | New regular file (write-only fd, requested host mode 0644) or new directory (returned pinned, 0755) below a pin. An occupied name is `PathAlreadyExists`; never opens, truncates or replaces. |
| `removeFile(dir, name)`, `removeDirectory(dir, name)` | One entry of exactly that kind; a directory must be empty (`PathAlreadyExists` otherwise). A symlink at the name is `AccessDenied`, the wrong kind `InvalidArgument`. Never recursive. |
| `rename(from_dir, from, to_dir, to, .replace/.preserve_existing)` | One backend rename between two of the caller's pins. Names are single components (1–255 bytes, no `/`, NUL or dot names). Replace over a non-empty directory is `PathAlreadyExists`. |
| `publish(from, to, .replace/.preserve_existing)` | One **path-based** B4 rename on the same share. Both-end guest policy applies. No containment guarantee for mutation, no fallback, no delete/copy emulation. Backend rejection preserves source/destination; transport loss after submission can leave an unknown commit outcome. |
| Existing `sdk.io` file I/O, create/mkdir, sync, truncate | Existing semantics retained, with B2 full path/name bounds. Legacy create is path-based and truncating, not contained/exclusive staging. |
| `Dir.openFile(sdk.io, name, .{ .follow_symlinks = false })` or `resolve_beneath = true` | Routes directly to B3 at `/host`, including explicit write-only existing-file opens. Read/write mode, locks and path-only mode refuse. |
| `Dir.rename` / `Dir.renamePreserve` | B4 replace / preserve-existing respectively, only from the borrowed `/host` cwd token. |

B3 and B5 operations explicitly return `Unsupported` on the legacy `/host`
file channel. The SDK rejects roots outside `/host`, including USB, with
`AccessDenied` before issuing a native call. Neither case falls back to B2's
weaker name/size listing.
B4 supports the legacy channel and VirtioFS as documented in ADR 0007.
Root/intermediate/leaf/dangling/enumerated symlinks refuse; guest policy is
checked by the kernel at open and later I/O/page access, not cached by the SDK.

Owner fields are exactly the backend's reported uid/gid, not a promise of
macOS identity equivalence. The VirtioFS server can map those identities;
the SDK preserves its wire values and never substitutes host ownership or
guest permissions. The live gate records both namespaces separately.

The custom API preserves native file-domain errors as `InvalidArgument`,
`InvalidHandle`, `BadAddress`, `Unsupported`, `ResourceLimit`, `FileNotFound`,
`AccessDenied`, `NameTooLong`, `PathAlreadyExists`, `OutOfMemory`.
`InvalidArgument` also covers native malformed metadata/transport errors.
Malformed wire successes become `ProtocolViolation`, never EOF or made-up
metadata. Narrow std error sets use their honest equivalents or `Unexpected`
with a named diagnostic. No-error std close remains fatal on failure;
`closeChecked` and `closeSnapshot` permit fallible cleanup and retry.

## Refusals and native gaps

- **Path fds have no pinned object.** For files from `Dir.openFile`/`createFile`
  on the legacy path, `fileMetadata` returns `Unsupported` and `File.length`
  returns `Unexpected` with `Unsupported:FdMetadataUnavailable`. No pathname is
  retained to emulate them.
- **No full std `File.Stat` or std directory iterator.** `File.stat` stays
  refused (`FdMetadataUnavailable`). Use rich SDK metadata and snapshots. The
  std permissions hook represents creation defaults, not factual host mode or
  guest ACLs; inventing a full std stat would be dishonest.
- **B5 is the custom API only.** `std.Io.Dir` exclusive create, `openDir`,
  `deleteFile`/`deleteDir`/`deleteTree` and `createFileAtomic` are not wired to
  pins and keep refusing. Do not combine a metadata check with legacy mutation
  and call that containment.
- Append, read/write opens, positioning, permission changes, links, recursive
  removal, directory fsync, watch and multi-file transactions remain
  unsupported. File sync alone is not power-loss durability.

Any further C3 dependency on a missing native guarantee needs a bounded
ADR 0007 amendment and coordinated kernel ownership **before** shared-kernel
edits. B5 (#1895) is that amendment for pinned publication.

The one native correction is memory use, not a new guarantee:
`fs_metadata.Snapshot.sort` uses in-place heap sort instead of block sort's
184,320-byte automatic cache. The latter inflated the metadata syscall frame
beyond what its 192 KiB exception stack could hold with callers. Full-capacity
row tests, compiler-frame checks and the unchanged full-page/EOF acceptance
cases cover this regression; no stack or ABI budget is raised.

## Bounds and C3 handoff

- Three stream identities + borrowed cwd + **four combined files/cursors/pins**
  = eight SDK resources. The cwd counts against Boris's five non-stream
  resources. There is no additional cursor or pin table. Reserve before native
  open; never evict. Stale tokens cannot alias reused native fds/cursors/pins.
  Normal finish closes all three kinds; kernel death cleanup remains the final
  backstop. A created file also holds one of eight shared VirtioFS handles.
- A pin's share-relative path may have 16 components (child pins extend it),
  ≤512 bytes. Path-based B3 opens below a root keep their eight-component limit.
- Full guest path ≤512 bytes, component ≤255 bytes, no clipping or escape
  normalization. B3 permits eight relative components below a supplied root
  with at most eight share-relative components. Legacy/B4 permit eight
  components below `/host`; that native distinction is not a budget increase.
  B3 treats names as opaque bytes except `/`, NUL and dot components; legacy
  SDK path calls additionally retain their backslash/colon refusal.
- The pre-existing `Backend.path(..., *[64]u8)` preflight retains its
  64-byte path/31-byte component contract for existing workload adapters.
  Custom `Backend(Driver)` instances retain those Io bounds too, unless
  their driver explicitly sets `filesystem_b2 = true`. The shared
  `sdk.io` runtime opts in; new custom path callers use `fs.fullPath`.
- Snapshot ≤256 entries, page 1–16 rows. Native eight-global-cursor and
  512-object identity-pin limits remain. A snapshot may fail below the input
  limit because these are shared native resources.
- C3 must still enforce aggregate visited entries (including ignored entries
  and directories), relative-root traversal depth, emitted files, input/output,
  arena and worst-case stack budgets. Materialize bounded traversal state and
  close cursors instead of keeping one per depth. This bridge is not a scanner.
- Secure entropy remains the existing slot-72 adapter. C3 must prove its
  actual temporary naming/commit path, short-fill/error handling and refusal
  before publication. B5 gives exclusive staging and single renames, not batch
  rollback: a failure after submission can leave an unknown commit outcome
  (a created file may exist), and C3 owns its own recovery.

`zig build zig-guest-check` includes shared SDK tests, exported-body probes,
two fresh offline filesystem builds, ELF validation and compiler-frame checks.
`live-user-fs` adds real SDK EL0 boots on both backends, independent name/byte/
host-attribute checks, no-follow and pinned replacement, a pinned Boris-style
stage/swap/park publication, mixed-resource exhaustion, exact/over bounds and
cleanup. Existing raw B1/B2/B3/B4 boots remain; the raw B3 boot also runs B5.
`tools/zig/fs.py` builds only the acceptance fixture; it downloads nothing.
Fixture stack high-water is evidence for this bounded fixture, not C3's
worst-case compiler stack proof. Logs and receipts stay under `artifacts/`.
