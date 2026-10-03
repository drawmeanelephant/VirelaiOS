# ADR 0039: Standalone, bounded QuickJS at native EL0

- Status: **PROPOSED; not implementation permission**
- Date: 2026-10-03 · Design card: #1921 (M88a/R1) · Index: #1915
- Required approver and proposed accountable maintainer: **drawmeanelephant**,
  ADR 0030's Zig Guest Target/Backend Owner.
- Approval: **pending**. The owner must explicitly approve this design,
  its numeric budgets and ADR 0030's A2 amendment before merge. Filing the
  card, claiming it or opening a PR is not that approval.
- Related: [ADR 0028](0028-html-renderer.md) D2/Amendment A,
  [ADR 0030](0030-go-is-el0.md) D3/A1/A2,
  [ADR 0038](0038-zig-guest-target.md), ADRs 0007/0020/0024.

## 1. Decision and evidence boundary

Admit exactly one additional native program, **QJS.BIN**, a host-built
static AArch64 ELF with a Zig adapter and the pinned QuickJS C interpreter.
Go cannot link C: no cgo/FFI, and ADR 0030 D3 remains unchanged.
The closed four-workload A1 exception does not already admit QuickJS;
the proposed A2 amendment is necessary. No implementation starts until
owner approval and the design's landing.

This is not a browser or another shell. **WEB.ELF never executes JS.**
ADR 0028 D2 and the negative JS-in-WASM decision stand; no WASM caps,
WASI, wasm EH, renderer, kernel ABI or boot default change. No general
libc/POSIX layer, DOM, web APIs, fetch, npm, JIT, C-to-Go transpilation,
Go/C embedding or host-command authority.

**Observed R1 diagnostics**, repository baseline
`03d929f25226d9055dfbae4571faa824b8d3ffeb`: source/hash inspection,
hosted object compilation and source-reference extraction, and two
hosted heap probes. Host: Apple M4, 16 GiB, macOS 27.2; Apple Clang
21.0.0 (`clang-2100.3.34.2`), Zig 0.16.0. No VM was launched.
Raw source, commands, ASTs, hashes and results stay in ignored
`artifacts/m88-r1/`, not in this PR.

ADR 0028 measured Bellard QuickJS `04be246`, `quickjs.c` **2,033,048 B**,
failing a freestanding compile at `inttypes.h`'s `include_next`.
It separately measured **MicroQuickJS** `203d5bb` at **666,384 B**
as a native `-Os` object. MicroQuickJS is not this engine or a size
estimate for it. This R1 resolves the same QuickJS short hash to the
full pin below; the C file's byte count matches.

A fresh, unmodified-source diagnostic:

```sh
zig cc -target aarch64-freestanding-none -nostdlib -Os \
  -DCONFIG_VERSION=\"2026-06-04\" -c \
  artifacts/m88-r1/quickjs/quickjs.c \
  -o artifacts/m88-r1/freestanding.o
```

**Observed:** exit 1, `quickjs.c:25:10: fatal error: 'stdlib.h' file not found`.
This is an earlier missing-header boundary than the historical diagnostic,
not a successful port or a contradiction of it. Complete freestanding
compile/link, ELF size delta, native stack/heap measurements, five cold
evals, interruption bounds and guest file/interactive proofs are **blocked
until M88b supplies the approved private headers/bridges and M88c the
product**. None is reported as passed.

## 2. Pin, license and offline inputs

| Identity | Exact input |
|---|---|
| Upstream | `https://github.com/bellard/quickjs.git`, not quickjs-ng or MicroQuickJS |
| Version file | `2026-06-04` |
| Commit | `04be246001599f5995fa2f2d8c91a0f198d3f34c` (2026-06-16) |
| Git tree | `80b9627f20a51861df2723ab6af25c25cb9f654d` |
| License | MIT, Fabrice Bellard / Charlie Gordon, notices retained |
| Full-source hash manifest | SHA-256 `e2d3d3638f9063f19fe4d66320366ec2e6cc0631edeb3149600ccdcc86a63d3a` |

The manifest covers all **77 tracked files**, sorted by bytewise pathname,
one `SHA256` + two spaces + relative path + LF per file, original bytes,
no `.git` or generated files. The commit/tree and this manifest pin the
disabled sources too. R1 fetched only into `artifacts/m88-r1/quickjs/`.
No engine is committed on this design card.

Hashes of the selected build closure and its configuration:

| File | SHA-256 |
|---|---|
| `VERSION` | `234057c4079458cba9862d4bbb6f8a059ac2e4a194947673da0ad38ca16a37fd` |
| `LICENSE` | `598fd7fc928e4350abce36e337ba5a1346923c5c692f5be92c3d8e29ddd7c18d` |
| `Makefile` (inspected, not executed for guest builds) | `eb55139e6f7f62709a7953ebee7468c65e62f4af1267d58f2f9f5fa5ce425102` |
| `quickjs.c` | `a68622cecb806f39bf24738c376a0a73032ea8913478cad702e0265c46f7999f` |
| `quickjs.h` | `2165f47772af9faee1798999a599fa9de850d1bf0259502dade1c25d4a588316` |
| `quickjs-atom.h` | `342194359417664e652f54e7d682ee2ef91dd982e91f74decbde992a2c848236` |
| `quickjs-opcode.h` | `a9c60a8c9366820733d9d2acbb805f48b2e9e2778df526e6bcb845fdb6b47834` |
| `dtoa.c` | `af5abd68fa9806d1a19bdd5f2daef00d5fd0990ae56311382dfed4700343f074` |
| `dtoa.h` | `462070f7678a894b9f3612422f280f4dce412eb360a5cc585a513f407fc23114` |
| `libregexp.c` | `fdfcf86167029ab2c39c35730d4ae6875b7bbeb9406a0fcc886de4e9b1f5e2ac` |
| `libregexp.h` | `f5c5ef1899f224dcc718422a22c789ef9d8b5b837b622818fcfaf681ae793446` |
| `libregexp-opcode.h` | `2a98d646089f3a72f25480116297c5fdb5b28fd9f815447273951f4ade4a115e` |
| `libunicode.c` | `26203ae888c0582e7d0e2113f13db0c9b39dc7b0b3836d68fa308c54f7a0898c` |
| `libunicode.h` | `ce310152bc80d7415dcb657e23abd9a40bf83e393c0d05d325dae384bb01d259` |
| `libunicode-table.h` | `cf782bc7a07549e976f606bd3cb8555858482b279574554dcb8d46412986006c` |
| `cutils.c` | `b73a403a59da30726257ddbdf5e399298941c1def997782ee0d4d33f796a80a2` |
| `cutils.h` | `d2da6d06a75b9e6c116c82b7a41df6bcc170c8b1779f374fa953ecf688eda647` |
| `list.h` | `181c6b25d9d0e4853315d36a6e6fab3b2c3554271693d898a439384ba0f67c2b` |

MIT permits inclusion in this proprietary repository while retaining its
copyright/permission notice and warranty disclaimer with source and binary
distribution. The root proprietary license must not relabel the MIT input.
ADR 0028's AGPL Elk refusal remains relevant; no Elk input is adopted.

**M88b layout:** original selected C/header files plus MIT license in
`user/zig/quickjs/upstream/`; lock, complete original-file manifest,
patches, private headers and native bindings in `user/zig/quickjs/`;
isolated acquisition/build/check tooling in `tools/quickjs-runtime/`.
Track only the required source closure, not the upstream shell/tool suite.
Record the full-tree manifest and selected-file hashes in the lock.

Acquisition is an explicit maintenance step, never a build action.
Builds verify original bytes and patch hashes and fail `SourceDrift` or
`MissingPinnedSource` offline; they do not download or regenerate Unicode
tables, `repl.c`, bytecode or test262. Apply checked, uniquely anchored
patches to a private copy under `.build/quickjs/`. Never edit upstream
originals, installed Zig or a shared cache. Local patches may remove
platform features, select allocation/seed hooks, add safety polling/stack
checks and replace hosted diagnostics. They may not silently change
language algorithms or add services.

drawmeanelephant owns security monitoring, pins, patches and upgrades.
An upgrade requires a new full pin/hash review, license review, complete
source/undefined-symbol recount, Math/Unicode/JSON/regexp goldens,
allocator/stack/interrupt negatives, offline reproducibility, ELF checks
and all M88 guest proofs. Hold the old pin or suspend QJS on failure.
New features, symbols above the approved caps or budget growth require
owner-approved design amendments, not automatic upstream adoption.

## 3. Actual-source platform inventory

### Method and counting

Inspect the five core translation units, optional `quickjs-libc.c`,
`qjs.c`, `qjsc.c`, `run-test262.c`, `unicode_gen.c`, and all remaining
C sources (examples, fuzzers, test modules and `compat/test-closefrom.c`).
Use Clang AST references from source function bodies **and global
function-pointer initializers**, plus a comment/string-stripped call
scan of disabled branches, then reconcile hosted `nm -u` against
definitions across the input tree. Headers/macros and Windows-only
branches are cross-checked directly. Header declarations alone and
unrelated inline system-header bodies are not references.

Count each canonical external platform identifier **once across the
whole tree**, including disabled/test/debug sources. An identifier reused
by an excluded tool and the core retains its core disposition; excluding
the tool does not authorize its other operations. Internal `JS_*`,
`js_*`, dtoa/regexp/Unicode/cutils references are resolved within the source
closure, not platform shims. `qjsc_repl` is generated shell data, excluded.

Normalize `malloc_size`, `_msize` and `malloc_usable_size` to one allocation
size entry; `_NSGetEnviron` to `environ`; Darwin `__error` to `errno`;
Darwin stdio globals to `stdin`/`stdout`/`stderr`; assert implementations
to `assert`. Fortified memory/format aliases map to their unfortified
operation. These aliases get no extra budget seat. Windows APIs without
such an equivalent keep their own entries.

Compiler/header primitives (`va_start/va_end/va_arg`, `alloca`,
`__builtin_frame_address`, expect/count-leading/trailing-zero/byte-swap,
`isnan/isfinite/signbit`, infinity/NaN and integer/format constants) are
not imported platform symbols. Supply freestanding definitions/compiler
intrinsics, not hosted headers. `fenv.h` is included but supplies no
referenced fenv operation; remove that include, not a fake rounding API.
Compiler-generated `__udivti3` uses the pinned Zig compiler-rt, inventoried
separately at final link. Darwin stack probes/protectors, rune helpers,
fortify imports and stdio implementation symbols from hosted diagnostics
are not guest imports; the guest checker rejects any remaining ones.

**Exact inventory: 190 canonical symbols = 51 provide + 5 bounded-error
stub + 134 refusal.** The 12 separately enumerated feature refusals below
make the refused symbol/feature budget **146**, and the combined inventory
**202 entries**. No residual entry is unclassified. Provide is a
requirement on M88b, not an assertion these bridges exist today.

### Provide: 51 symbols

Each name in a row has the disposition **provide** and that row's reason.
All bridges are private to QJS's static artifact, never an SDK libc ABI.

| Count | Symbols | Implementation / reason |
|---:|---|---|
| 4 | `malloc`, `free`, `realloc`, `malloc_usable_size` | Same SDK arena and charged allocation headers; reusable frees, checked sizes, allocate-copy-free charges both blocks. `JS_NewRuntime2` owns the callbacks; no host allocator or second arena |
| 9 | `memchr`, `memcmp`, `memcpy`, `memmove`, `memset`, `strchr`, `strcmp`, `strlen`, `strrchr` | Owned freestanding byte/string routines over valid engine buffers; no locale, descriptors or OS authority |
| 1 | `abs` | Integer helper in dtoa; preserve C's supported operand contract |
| 2 | `snprintf`, `vsnprintf` | Bounded formatter for the pinned core's literal formats, including integer length/width/precision and strings; preserve would-have-written count/NUL rules. Unsupported format fails, never successful partial formatting. JS numeric conversion remains pinned dtoa |
| 2 | `abort`, `assert` | Allocation-free `EngineInvariant` diagnostic and nonzero native exit, never return to corrupted engine state |
| 33 | `acos`, `acosh`, `asin`, `asinh`, `atan`, `atan2`, `atanh`, `cbrt`, `ceil`, `cos`, `cosh`, `exp`, `expm1`, `fabs`, `floor`, `fmax`, `fmin`, `fmod`, `hypot`, `log`, `log10`, `log1p`, `log2`, `lrint`, `modf`, `pow`, `round`, `sin`, `sinh`, `sqrt`, `tan`, `tanh`, `trunc` | Native Zig math/compiler intrinsics with explicit C/JS corner-case tests: NaN, infinity, signed zero, subnormals, overflow, ties. `lrint` requires nearest/ties-even; `round` is ties-away. No fast-math or system libm |

### Bounded-error stub: 5 symbols

| Count | Symbols | Failure contract |
|---:|---|---|
| 5 | `printf`, `fprintf`, `fputc`, `fwrite`, `putchar` | Upstream dumps/debug paths only, not JS output. No FILE objects, output or argument dereference. Return negative/EOF, or zero items for `fwrite`, and latch `UnsupportedHostedDiagnostic`; adapter checks the latch after every engine boundary and refuses the run. Never pretend the diagnostic was written |

Normal JS `print`/`console.log` and exceptions use owned callbacks.
Dumps remain disabled. M88b directly tests all five stubs and their latch;
any unexpected reach becomes nonzero `UnsupportedHostedDiagnostic`.

### Refusal: 134 symbols

Each name in a row has the disposition **refusal**. Do not link an
ENOSYS-emulating function for these names. Exclude the named caller/
feature from the guest build; an unexpected reference is `UnexpectedImport`.

| Count | Symbols | Excluded caller / reason |
|---:|---|---|
| 15 | `clock_gettime`, `gettimeofday`, `gmtime`, `localtime`, `localtime_r`, `mktime`, `nanosleep`, `usleep`, `pthread_cond_destroy`, `pthread_cond_init`, `pthread_cond_signal`, `pthread_cond_timedwait`, `pthread_cond_wait`, `pthread_mutex_lock`, `pthread_mutex_unlock` | Date, clock-derived random seed, Atomics blocking and optional OS/tool timing. Replace random initialization with the explicit deterministic seed; disable Date/Atomics and hosted timing |
| 45 | `access`, `clearerr`, `close`, `closedir`, `closefrom`, `dup`, `dup2`, `fclose`, `fdopen`, `feof`, `ferror`, `fflush`, `fgetc`, `fgets`, `fileno`, `fopen`, `fread`, `fseek`, `fseeko`, `ftell`, `ftello`, `ftw`, `getcwd`, `ioctl`, `isatty`, `lseek`, `lstat`, `mkdir`, `open`, `opendir`, `read`, `readdir`, `readlink`, `realpath`, `remove`, `rename`, `rmdir`, `stat`, `symlink`, `tcgetattr`, `tcsetattr`, `tmpfile`, `unlink`, `utimes`, `write` | `quickjs-libc`, upstream shell/tools/tests, Linux large-file branches and closefrom probe. Adapter uses native SDK calls, not these APIs. No JS filesystem/terminal access or FILE layer |
| 37 | `_exit`, `atexit`, `chdir`, `clock`, `dlclose`, `dlopen`, `dlsym`, `environ`, `errno`, `execve`, `execvp`, `exit`, `fork`, `getenv`, `getpid`, `kill`, `optind`, `pclose`, `pipe`, `poll`, `popen`, `pthread_attr_destroy`, `pthread_attr_init`, `pthread_attr_setdetachstate`, `pthread_attr_setstacksize`, `pthread_cond_broadcast`, `pthread_create`, `pthread_join`, `pthread_mutex_destroy`, `pthread_mutex_init`, `setenv`, `setgid`, `setuid`, `signal`, `sysconf`, `unsetenv`, `waitpid` | Optional libc/OS library, shell/compiler/test tools and workers. No process, environment, signal, module-loader, polling or thread compatibility layer. Normal product exit is the SDK's native finish |
| 22 | `atoi`, `fputs`, `isdigit`, `isspace`, `perror`, `putc`, `qsort`, `stderr`, `stdin`, `stdout`, `strcat`, `strcpy`, `strcspn`, `strdup`, `strerror`, `strncmp`, `strspn`, `strstr`, `strtod`, `strtol`, `strtoul`, `vfprintf` | Hosted tools/tests and debug-only paths. Core uses owned dtoa and cutils `rqsort`, not hosted conversion/sort/stdio. Exclude dtoa/libregexp TEST mains, cutils TEST sort and engine FILE dump APIs |
| 15 | `CloseHandle`, `CreateEvent`, `GetConsoleScreenBufferInfo`, `GetCurrentProcess`, `GetProcessAffinityMask`, `ResetEvent`, `SetConsoleMode`, `SetEvent`, `Sleep`, `WaitForMultipleObjects`, `_fullpath`, `_get_osfhandle`, `_putenv`, `_setmode`, `_utime` | Disabled Windows OS/tool branches; neither Windows nor a backend impersonating it is selected |

Core source contains refused references even though the optional OS library
is excluded: Date/seed, internally enabled Atomics, FILE dump routines,
and TEST arms. M88b must remove/guard these sources before compiling,
not rely only on final-link dead stripping. `CONFIG_ATOMICS` is defined
inside `quickjs.c`; a command-line `-UCONFIG_ATOMICS` alone is insufficient.

### Translation units and feature refusals

| Unit | v1 disposition |
|---|---|
| `quickjs.c` | Enabled with reviewed native-boundary patches: Atomics/Date/FILE dump removal, deterministic seed, safety polling and stack enforcement |
| `dtoa.c`, `libregexp.c`, `libunicode.c`, `cutils.c` | Enabled; generated Unicode/opcode tables pinned, TEST/debug mains disabled |
| `quickjs-libc.c` / `.h` | Excluded entirely; **not** a required part of the core despite upstream `QJS_LIB_OBJS` including it |
| `qjs.c`, generated `repl.c`/`repl.js` | Excluded; Virelai owns loading, line input and printing |
| `qjsc.c`, `run-test262.c`, `unicode_gen.c`, closefrom probe | Excluded host tooling; never used to generate guest code during a build |
| Examples, fuzzers, test C modules | Excluded guest inputs; source references included in the inventory, no dynamic modules |

The **12 feature refusal entries** are: (1) Date/wall/local time,
(2) Atomics/SharedArrayBuffer, (3) Worker, (4) upstream `std` module,
(5) upstream `os` module, (6) module loading/import/require/native modules,
(7) Promise/async/job execution, (8) WeakRef/FinalizationRegistry,
(9) JS file/terminal authority, (10) sockets/network/fetch,
(11) timers/performance clock, (12) DOM/browser APIs.
Use `JS_NewContextRaw` plus BaseObjects/Eval/StringNormalize/RegExp/JSON/
MapSet, not `JS_NewContext`'s all-intrinsics initialization.
Plain synchronous language features, BigInt, Math and owned-array
operations remain; unsupported async syntax/bytecode is refused before
execution, not left waiting in an undrained job queue.

Uninstalled builtins fail with ordinary ReferenceError/TypeError;
module/async entry points explicitly report `UnsupportedFeature`.
M88c pins the error and absent-global expectations for every entry.
Never accept a host feature through `eval`, Function constructors,
dynamic import, an upstream module loader or a precompiled bytecode blob.
No bytecode ingestion or binary serialization host API is installed.

### Diagnostic reconciliation

Hosted compile configuration: Apple Clang, `-Os -fno-builtin -fwrapv`,
`_GNU_SOURCE`, version above, no TEST/dump defines; ordinary Darwin
headers and upstream's internally enabled Atomics. Those headers still
expand fortify macros, so this is a diagnostic, not the guest recipe.

| Core object | Hosted Mach-O file bytes |
|---|---:|
| `quickjs.c.o` | 817,992 |
| `dtoa.c.o` | 12,992 |
| `libregexp.c.o` | 37,176 |
| `libunicode.c.o` | 68,856 |
| `cutils.c.o` | 7,944 |
| **Sum** | **944,960** |

All ten core/optional/tool diagnostic translation units compiled.
Their undefined references reconcile to internal engine objects,
the canonical inventory, or the separately identified compiler/header
artifacts above. AST/global-initializer inspection captures Math function
pointers that a call-only grep would miss; disabled-branch inspection
captures Windows, Linux large-file and TEST references that hosted
`nm` cannot see. The sum is **not** an ELF, engine-added-byte delta,
stripped release footprint or guest result.

## 4. Native C build, without changing the shared target

Reuse ADR 0038's pinned Zig 0.16.0 distribution, checked private std overlay,
SDK entry/startup/arena and linker/ELF checks read-only. The isolated M88b
builder adds explicit C objects to that recipe; it does not edit
`tools/zig/lock.json`, its dependency list, the shared SDK or root build.
The QuickJS-specific lock records the compiler archive/overlay hashes
and every source/header/patch/build input.

Compile C with that distribution's `zig cc`, target
`aarch64-freestanding-none`, baseline CPU, C11, `-Os -ffreestanding
-fno-builtin -fwrapv -funsigned-char -fno-stack-protector -fno-pic
-fno-pie -fno-unwind-tables -ffixed-x18`, section splitting and no LTO.
Use `-nostdinc` with only the private audited headers and pinned compiler
resource headers for freestanding types/varargs/intrinsics. No macOS/Linux
sysroot, `_GNU_SOURCE`, host `include_next`, fortify, pthread or libc headers.
Set the exact version; explicitly disable TEST, all DUMP modes,
profiling/sanitizers and shared-library paths. Keep stack checks enabled;
do not fake `__EMSCRIPTEN__` to disable Atomics and accidentally disable
the stack guard too.

Private declarations cover only §3's provided/stubbed surface. Native
Zig exports have the C calling convention; the Zig adapter imports them
through an explicit SDK/runtime module, not Go or `@cImport` of hosted
headers. Use ADR 0038's ReleaseSafe Zig root hooks, single-threaded
selection and x18 reservation. Provide libm operations from Zig math/
compiler intrinsics, and verify compiler-rt closure without linking libc.
No POSIX fd/errno translation or fabricated metadata is necessary.

Link a static gap ELF using the existing native entry and two-segment
contract: RX at `0x0040_0000`, RW at aligned text end plus 4,096.
No PT_INTERP/PT_DYNAMIC/PT_TLS, runtime relocation, unresolved symbol,
W+X mapping or compiler TLS. Check the current kernel parser **and** the
narrow SDK profile; the historic contiguous `zc` checker alone is not
enough. Reject new platform imports even if the linker could find them
on the host. Artifact receipts include flags, hashes, section/file sizes,
mapped bytes, C and Zig frames and exact remaining external names.

These are selected build requirements, **not an observed native C build**.
A missing Zig math operation, header, polling propagation or SDK primitive
blocks M88b at that exact dependency; it does not authorize a libc,
shared-target change or budget increase.

## 5. Virelai-owned v1 contract

The complete runtime boundary is `create`, `eval(source, filename, sink)`,
`cancel`, `reset` and `destroy`, with a single live runtime/context.
Second creation fails `ContextLimit`. Files, argv, env, tty and receipt
handles remain exclusively in the Zig adapter, never JS values.

- **File mode:** `QJS.BIN /host/FILE.js [--receipt=/host/OUT]`.
  Validate the supplied path and use the SDK's no-follow contained open,
  then sequentially read to EOF with an extra-byte source-limit probe;
  close input before evaluating.
  Evaluate UTF-8 source as a global script. Do not auto-detect modules,
  resolve imports, search directories or inspect npm metadata.
- **Interactive mode:** `QJS.BIN --repl [--receipt=/host/OUT]`.
  Open the process's `/dev/tty` with native calls, attach the existing
  serial front-end through slot 67, and yield when its read returns no
  pending bytes. Zero pending tty bytes are **not EOF**. No shell/WM edit
  or tty ABI change. Read one UTF-8 line, evaluate, emit the result, repeat.
  This is a line-eval loop, not upstream's JS repl or a multiline editor.
  `:reset`, `:quit`, Ctrl-D on an empty line and Ctrl-C are adapter commands.
  Ctrl-C cancels an active run at a poll point; partial-line Ctrl-C discards
  the line. Detach/close on exit. Refuse `TerminalUnavailable` rather than
  borrow another process's input.
- **Only host APIs visible to JS:** `print(...values)` and
  `console.log(...values)`, identical string conversion, one ASCII space
  between arguments, LF termination, embedded NUL treated as a byte.
  All conversion allocation and execution charge the same budgets.
  Returned interactive values use the same bounded conversion, not
  JSON serialization that silently drops values.
- **Output:** confirmed native console/stream writes, at most 256 bytes
  per slot-1 call; short writes loop, zero progress is `WriteFailed`.
  Optional adapter-owned receipt mirrors the evaluated output and
  records bounded result/error/resource/timing fields, never JS file
  access. Console and receipt copies share one logical output count.
  Create receipts exclusively through the existing contained directory-pin
  API; refuse `ReceiptExists` rather than truncate any existing file.
  Close the transient directory pin before opening the interactive tty.
  Flush/sync/close failures make file-mode exit nonzero. A new failed receipt
  may be partial, but no previous published artifact is replaced/destroyed.
  No racy stat-then-create, atomic-publication or rollback claim.
- **Randomness:** Math.random is explicitly non-cryptographic,
  reproducible xorshift state seeded with **1** at create/reset;
  replace `js_random_init`'s clock seed. No gettimeofday, secure-random
  promise or hidden entropy service. Pure Math is not a timer API.

`create` reserves one SDK arena and constructs `JS_NewRuntime2`, then
the selected raw context/intrinsics. SDK state, C headers, engine objects,
source plus NUL, conversion/output buffers and allocator overhead all
charge that arena. No secondary allocator. Free values/strings/input,
run teardown, free context/runtime and return blocks to the arena on
every path; retain the one mapping until process exit. Verify both
allocation reuse and kernel reclamation after reap.
Update QuickJS's stack top only at the outer engine entry, never inside
recursive eval/host callbacks to conceal stack consumption.

Ordinary syntax/runtime exceptions report `JSException` with a bounded
message and leave the interactive context alive; globals modified before
the exception are not rolled back. **Heap, stack, output, cancellation,
deadline, unsupported-bytecode or stub-latch failures discard and recreate
the context**, announcing `ContextReset`; the next eval gets a fresh
context within the same arena. Failure to recreate exits nonzero.
No claim that a poisoned or OOM context can safely continue unchanged.
No user finalizer/async job runs during reset.

Resource/cancel flags are sticky outside JS and checked before and after
each boundary: catching an exception in JS cannot turn an OutputLimit,
OutOfMemory or interruption into success. File mode returns 0 only after
successful eval and writes; usage/startup refusal 64, application/resource/
unsupported/I/O failure 70, fatal invariant/panic 71. Interactive eval
failure is visible and non-success in its receipt; quitting successfully
can exit 0 after recovery. Engine assertions never return.

## 6. Final v1 budgets and enforcement

Numbers below are **chosen ceilings requiring approval**, not measured
guest footprints. None is open. Failure to meet them holds the workload,
not the bound.

| Required budget | Final ceiling | Enforcement / refusal |
|---|---:|---|
| Engine binary bytes added | **1,572,864 B** | Same host-built guest ELF adapter, compiler/flags/linker/strip settings, full engine+bridges versus empty runtime baseline; both file-byte and initialized-load-byte deltas must fit. Preserve baselines and hashes; `EngineSizeLimit` |
| Aggregate per-context/process arena | **4,194,304 B** | One populated SDK arena, inclusive accounting above, callbacks and `JS_SetMemoryLimit`; `OutOfMemory` |
| Cold first successful eval | **5,000 ms** | Five fresh guest processes, no prewarmed context, maximum sample; `ColdEvalLimit` acceptance failure |
| Simultaneous runtimes/contexts | **1** | Single runtime lease, no workers/threads; `ContextLimit` |
| Provided canonical shim entries | **51** | Exact inventory 51; cap replaces draft 48, Math alone needs 33. Recount/allowlist at build; `UnexpectedImport` |
| Bounded-error stub entries | **5** | Exact inventory 5; cap replaces draft 16, no spare implicit APIs |
| Refused symbol/feature entries | **146** | Exact 134 symbol + 12 feature entries; cap replaces draft 32 because excluded OS/tools/backends are counted rather than hidden |
| Source per eval/file | **1,048,576 B** | UTF-8 byte count before eval, extra-byte probe; NUL overhead charged separately; `InputLimit` |
| Interruption completion deadline | **5,000 ms** | Deadline/cancel polling and slow-path bounds below; `EvaluationDeadline` / `Cancelled` |
| Native stack use | **131,072 B** | Existing 196,608 B mapping; QuickJS C-stack limit **98,304 B**, 32,768 B adapter/C-call margin; `StackLimit` |
| Logical output per eval | **65,536 B** | Count conversion/result/print bytes before writes, sticky failure; `OutputLimit` |
| Open native file handles | **2** | File input or tty plus optional receipt, never a third; `HandleLimit` |

Additional A1.2-shaped limits:

- Total artifact file and page-rounded mapped segments plus argv/env tail:
  **4,194,304 B each**; RW data/BSS **1,048,576 B** maximum. `ImageBudget`.
  Heap plus conservatively charged mapped image is at most 8 MiB
  (2,048 pages), below 4,096 recorded pages even with the 192 KiB user
  stack. Reserve only one mmap region; no per-allocation mappings,
  threads or file-backed mmap. Check actual page/region accounting.
- Argv: **7 user arguments**, **255 bytes each**, program name consumes
  the eighth slot. Env: **16 entries**, **127 bytes each**, no JS exposure
  or runtime dependence. Validate owned inputs before packing; retain
  ADR 0038's warning that the monitor clips names/values to 32/64 and
  slot 28 has no env parameter. No general lossless-inheritance claim.
- `/host` paths: **64 bytes full path**, **31 bytes/component**,
  inherited conservative subset despite wider newer native facilities.
  No NUL, traversal, unsupported volume or silent truncation. The only
  device exception is the literal adapter-owned **`/dev/tty`**, not JS.
  Existing principal/capability/no-follow containment checks remain.
- SDK resource records: **7** including the three reserved stream
  identities, borrowed cwd, at most two files and one transient directory
  pin. The pin is not a third native file handle; release it before tty
  attach. Resource-table exhaustion is `HandleLimit`, never eviction.
- Interactive line: **4,096 UTF-8 bytes**; reject and drain an oversized
  line without evaluating its prefix. **256 evals/session**, **60,000 ms
  idle wait**, **1,048,576 B aggregate session output**, **4,194,304 B
  aggregate session source**; `LineLimit`/`SessionLimit`/`IdleLimit`.
  A line-limit refusal preserves the context; run-time budget refusals
  follow the reset rule. One active eval, no recursive host eval.
- Diagnostics: **16,384 B/session** (file mode is one session), separate
  from normal output; reserve **256 B** for the terminal refusal.
  Receipt bytes, including mirrored output and metadata: **1,114,112 B/
  session**. `DiagnosticLimit`/`ReceiptLimit`; no unbounded stack traces.

### Source/heap observation

A hosted-only harness used `JS_NewRuntime2` with charged 8-byte allocation
headers, allocate-copy-free realloc, a 4 MiB aggregate cap including the
1 MiB source+NUL, and the raw-context intrinsics above, with QuickJS's
C-stack limit set to 98,304 B:

| 1,048,576-byte source shape | Observed result | Charged peak | Retained after destroy |
|---|---|---:|---:|
| `1+2*3;` padded with spaces | Successful eval | 1,185,081 B | 0 B |
| Dense `let a=[0,0,…];` | `InternalError: out of memory`, 2 allocation refusals | 4,039,556 B | 0 B |

This answers the card's 1 MiB question: size alone does **not** guarantee
compilation inside 4 MiB. Product maps the second result to `OutOfMemory`.
The harness uses host malloc backing, not the SDK free-list or actual
guest alignment/metadata, so the figures are source-design evidence,
not native peaks, stack proof or universal input acceptance.

### Timing, stack and cancellation

Reference acceptance hardware: **Apple M4, 16 GiB host RAM, macOS 27.2,
Apple VZ, 2 vCPUs, 256 MiB guest RAM**. Use §4's ReleaseSafe Zig/`-Os` C,
baseline CPU, no LTO/fast-math/profiling. Record host load and competing VM
activity with samples; do not remove a slow sample or average it away.

Cold timing starts immediately before an existing native launcher fixture's
accepted slot-28 launch (reject failed launches separately) and ends when
the child successfully emits the first computed result, not at file read,
parse start or a self-reported banner. Share a counter/frequency identity
through the receipt so the host can independently compute the difference.
Take the maximum of **five** fresh-process `1+2*3` file evals.

Use existing **CNTVCT_EL0 / CNTFRQ_EL0** reads, already used by the native
SDK's network clock, for monotonic deadlines. Counter precision is
`1/frequency`, not the scheduler's one-second sleep tick or slot-66 wall
seconds. Refuse `ClockUnavailable` on zero/invalid frequency; never
simulate elapsed time with an instruction count.

Set a **4,000 ms** internal eval deadline, reserving **1,000 ms** for
poll latency, unwinding/reset and diagnostic completion within the
5,000 ms acceptance ceiling. QuickJS's default counter is **10,000**;
M88b reduces it to **1,000** and installs `JS_SetInterruptHandler`.
That counter alone is **not a time bound**: parse, regexp, BigInt,
normalization, JSON/sort/string operations, GC and host conversions may
do substantial C work between interpreter polls.

Audit every input-dependent native loop/recursive path; add propagating
polls/stack checks in the narrow patches and bridges. Within each such
path check at most every **1,024 processed elements/bytes** and bound
recursive entry before descending. Require worst supported unchecked
region **≤100 ms**, deadline observation plus teardown **≤1,000 ms**
on the reference host, and a **10,000,000 charged `js_poll_interrupts`
sites** work ceiling (`WorkLimit`) independent of elapsed time. Count
each site, not only every thousandth handler invocation; this is not a
claim that QuickJS polls every bytecode. Cancellation is sticky
and uncatchable at the outer boundary. A prequeued Ctrl-C is checked at
entry; otherwise active eval samples tty input at polls, not a new signal
or thread. Preserve non-Ctrl-C bytes in the charged next-line buffer;
overflow is `LineLimit`, never silently dropped input. No bytecode path
may clear an active deadline. Native I/O completion also charges elapsed
time: an unbounded blocking write or teardown path blocks acceptance,
not permission to exclude it from the timing result.

C/Zig frame analysis, QuickJS's own guard **and native stack high-water
probes** must establish the 131,072 B ceiling, including parser/regexp/
BigInt recursion, formatter and error unwinding. Keep 64 KiB outside that
budget unused in the existing mapping; a kernel guard fault is a failed
proof, not `StackLimit`. If an upstream C loop cannot propagate a bounded
refusal, disable its feature by owner-approved revision or block release.

These timing/stack values are release requirements, not hard real-time
guarantees under arbitrary host descheduling. M88b must supply enforceable
algorithmic polling/recursion coverage; M88c must test maximum-work cases,
not infer safety from five small evals. R1 cannot measure those regions
without the port. No external harness kill/timeout counts as a successful
guest interruption.

## 7. Final implementation cards and ownership

Replace the held M88b/c shapes with these two cards **after this ADR and
the approved A2 amendment merge**. Do not file them in R1. Sol implements;
drawmeanelephant reviews their complete evidence. Paths below are the
entire exclusive ownership sets, in coordination-gate syntax; `dir/*`
is a prefix including descendants. No double-star tokens, shared editor,
status/ADR/root-build/manifest/shell/SDK-root edits.

| Card | Touches | Complete, independently closable deliverable |
|---|---|---|
| **M88b — complete offline native runtime** | `user/zig/quickjs/*, tools/quickjs-runtime/*` | Pinned source/license/manifest/patch lock, private headers and all §3 bridges, raw-context runtime API and complete resource/reset/interrupt/stack behavior, isolated native build plus host/native-linked contract fixtures and inspection. No partially completed shim PR |
| **M88c — QJS file and interactive product acceptance** | `user/src/quickjs.zig, tools/js-client/*, tests/fixtures/js/*, tools/gate/specs/live-quickjs.spec` | SDK-based QJS.BIN adapter, file/tty line loop and receipts, offline product builder, small independently checked fixtures, complete on-guest positive/refusal/budget/cleanup evidence |

M88c depends on merged M88b and consumes its API read-only. Its builder
imports the existing `user/zig/` SDK/root hooks and M88b runtime as modules;
M88b's builder exposes the checked C objects/lock contract. Neither needs
a root `build.zig` step or installed command/menu entry. Stage QJS.BIN
explicitly in the gate and run by path. A shared-file requirement is a
blocker requiring reviewed scope/Touches revision, not an unclaimed edit.

**M88b verification:** two clean offline native builds with identical
artifacts/input receipts; ELF parser/SDK checks; complete provided/stub/
refusal inventory match and no unexpected import. Unit/host-engine goldens
for all 51 provided entries, all five stub latches, math corners and
UTF-8/Unicode/regexp/BigInt/JSON; direct boundary/context/allocator/OOM/
teardown/reset/poll/stack checks, plus build-negative references for all
134 refused symbols. Inspect every loop/recursive path identified above;
missing coverage blocks the runtime card. The native-linked fixture proves
the complete adapter-free contract compiles; hosted tests are not guest
execution claims. Existing SDK checks run read-only.

**M88c's one new declarative spec:** `live-quickjs` is a genuinely new
language workload. The existing `live-el0-exec` covers generic loader/SDK
startup, not file evaluation, persistent JS state or interpreter refusal
semantics; do not overload it with a second runtime acceptance suite.
Keep it unchanged and run it as a regression. No new verify shell script.

Named **on-guest proofs**, all owned by M88c:

| Proof | Required independent evidence |
|---|---|
| `QJS-FILE-EVAL` | Real UTF-8 file computes arithmetic, BigInt, Math and JSON/regexp output; byte-exact stdout/receipt versus pinned host-engine goldens, read beyond 2,048 B, output beyond 256 B, missing/denied file nonzero |
| `QJS-INTERACTIVE-EVAL` | Real serial tty input: `let n=40`, then `n+2` → 42; ordinary exception then `n` → 40; `:reset` removes n; budget refusal announces reset then `6*7` → 42; quit detaches/closes. Host input is delivered only after guest ready markers, no host JS eval substitute |
| `QJS-REFUSALS` | All 12 refused features exercised, including static/dynamic imports, async and indirect eval attempts; native fixture directly exercises every stub's error/latch, and guest scripts prove their denied host paths do not gain authority. Unexpected refused symbol cannot link |
| `QJS-BOUNDS` | Exact/over source, line, output/session/receipt/diagnostic, argv/env/path/handle/context limits; hostile allocation, recursive stack, parser/regexp/BigInt/normalization/JSON/sort work, `for(;;){}` deadline, caught-error loop and Ctrl-C. Named guest refusal plus next usable eval, never harness timeout |
| `QJS-COLD-AND-CLEANUP` | Five cold timings and maximum, actual image/delta/heap/stack/handle peaks, repeated create/reset/destroy, process reap and independent page/handle recovery, I/O failure propagation |

M88c runs `just gate live-quickjs`, `just gate live-el0-exec`, the
affected host tests/offline builders, `bash tools/inventory-gates.sh --check`,
`git diff --check` and `just verify-coordination`. Commit only small pinned
fixtures and input/provenance metadata, not logs, engine-build output or
the generated fleet snapshot. Local guest evidence is standard gate
artifacts plus `artifacts/m88-acceptance/`.

## 8. R1 acceptance and owner review

The R1 PR changes only this file and ADR 0030's header/A2 amendment.
Its checks are source/hash and diagnostic reconciliation, all seven
index budget entries plus the card's additional limits, complete card/
Touches review, `git diff --check` and `just verify-coordination`.
No engine, implementation, gate-spec, SDK, build or status change.

**Blocking owner decision:** drawmeanelephant must approve the additional
program, C-under-native-target recipe, accountable maintenance role,
inventory dispositions, every numeric budget and final card split.
Record the actual approval link/date before merge and acceptance; until
then both this ADR and A2 remain proposed, and #1921 is not done.
M88 implementation cards are filed only afterward. The R1 leaves #1915
open; the owner accepts that index only after file eval, interactive eval
and every budget/refusal proof land with evidence.
