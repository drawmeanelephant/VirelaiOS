# Bounded fart-app synth

The only upstream input is the unchanged `src/synth.zig` at
`bd453937b30ae6eb28c69f6a497ba202efe4cd05`, content-checked by
`tools/zig/fart-lock.json`. It has no local imports. No upstream host entry,
libc, speech command, audio command, miniaudio or NINJAM module is built.

```sh
python3 tools/zig/sdk.py fetch # explicit compiler fetch, once
python3 tools/zig/fart.py build # offline, pinned SDK and ReleaseSafe
python3 tools/zig/fart.py check
bash tools/gate/vgate.sh tools/gate/specs/live-zig-fart.spec
```

Native monitor usage:

```text
exec FARTSYN.BIN --phrase "kujamba karibu" --wav /host/FART.WAV
exec FARTSYN.BIN --seed 0 --stdout
exec FARTSYN.BIN --phrase m --play
```

One phrase (≤255 bytes) or decimal u64 seed; default upstream voice only.
One destination: WAV file, binary stdout, or finite playback. Failures exit
70 with a named stderr diagnostic. Rendering and output checks precede file
creation. I/O failure can leave a partial file; this is not an atomic publisher.
Paths use A3's ≤64-byte full-path/≤31-byte component subset.

The real phrase planner preflights ≤220,500 samples (5 s); the seed path is
bounded by upstream's three ≤340 ms segments and two ≤70 ms gaps. WAV is
mono S16 little-endian 44.1 kHz, ≤441,044 bytes. One 4 MiB arena includes all
plans, concurrent PCM/WAV buffers, allocator metadata and SDK state. Only
one output file is open, besides three native streams. No engine recursion
or input-dependent call depth is used. Compiler-frame sums conservatively
bound the fixed call depth; the native entry also measures stack high-water.

Playback queries slot 42, excludes the 44-byte WAV header, resamples with
integer nearest-neighbor selection, duplicates mono for stereo, and encodes
S16, S32 or Float32 little-endian at the six rates the native driver supports.
It submits once via slot 43, ≤min(device max, 65,536) converted bytes. Longer
audio refuses `PlaybackLimit`; use WAV export instead. No chunked START/STOP
loop, capture, continuous playback, UI parity or host commands.
The live playback proof uses one vCPU. The current native multi-period call
can return `AudioDeviceRefused` even with a negotiated device on one or two vCPUs;
the adapter propagates that failure, never retries/chunks or claims it played.
The gate distinguishes confirmed playback from explicit native refusal while
independently checking the converted bytes and single submission in both cases.

The A3 std stream tokens still refuse I/O on this baseline. This CLI uses
B1's native fd 1/2 calls with confirmed ≤256-byte writes. SDK startup/panic
diagnostics retain A2's console fallback, not stderr-isolation claims.
WAV goldens are compared byte-for-byte under the pinned ReleaseSafe recipe,
not upstream's cross-optimization acoustic fingerprints. The independent host
oracle calls the upstream renderers directly and explicitly links the pinned
compiler's math runtime. Darwin's libSystem math differs by a few rounding
units; it is not the supported freestanding synth path. Neither the guest
synth nor its float settings are changed to match the host.

If macOS creates `.DS_Store` inside the compiler materialization, A2 correctly
refuses the additional files. Keep the archive/materialization in an isolated
cache outside the watched workspace and pass `--cache PATH` to these commands
(and `FART_SDK_CACHE=PATH` to the live gate). This never loosens A2's hash checks
or silently fetches during a build.
