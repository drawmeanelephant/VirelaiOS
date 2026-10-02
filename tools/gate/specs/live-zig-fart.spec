# C4: actual pinned fart-app pure synth, not the existing Go FART app.
# Independent host-engine WAV bytes, exact limits and finite native PCM.
# exec-order: assert-proven -- every run waits for the application's reap.
# PCM digest proves the submitted buffer, not audible output/capture.

vgate_name live-zig-fart "C4 bounded deterministic native fart-app synthesis"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 BOOTS
vgate_note "Finite PCM confirmation and native device refusal are reported separately; no audible/capture claim."
vgate_fmt user/zig/fart.zig user/zig/fart_probe.zig user/zig/fart/port.zig user/zig/fart/pcm.zig user/zig/fart/tests.zig user/zig/fart/oracle.zig

vgate_setup_python <<'PY'
import json, os, pathlib, shutil, subprocess, sys
sys.path.insert(0, str(pathlib.Path("tools/zig").resolve()))
import fart
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ.get("VG_SHARE") or rd / "share")
cache = os.environ.get("FART_SDK_CACHE", str(fart.sdk.DEFAULT_CACHE))
subprocess.run([sys.executable, "tools/zig/fart.py", "build", "--cache", cache], check=True)
subprocess.run([sys.executable, "tools/zig/fart.py", "probe", "--cache", cache], check=True)
subprocess.run([sys.executable, "tools/zig/fart.py", "goldens", "--cache", cache,
                "--work", str(rd / "goldens")], check=True)
shutil.copyfile(".build/zig-fart/FARTSYN.BIN", share / "FARTSYN.BIN")
shutil.copyfile(".build/zig-fart/FARTPRO.BIN", share / "FARTPRO.BIN")
cases = [
    ("a", "--phrase a", "a.wav"),
    ("phrase", '--phrase "kujamba karibu"', "phrase.wav"),
    ("repeat", '--phrase "kujamba karibu"', "phrase.wav"),
    ("breath", "--phrase m", "breath.wav"),
    ("cluster", '--phrase "Ng\'OMA"', "cluster.wav"),
    ("seed0", "--seed 0", "seed0.wav"),
    ("seed42", "--seed 42", "seed42.wav"),
    ("seedmax", "--seed 18446744073709551615", "seedmax.wav"),
    ("limit", "--phrase bbbbb" + "a" * 16, "limit.wav"),
    ("input", '--phrase "' + "a" + " " * 254 + '"', "input.wav"),
]
for tag, inputs, golden in cases:
    (rd / (tag + ".txt")).write_text("pages\nexec FARTSYN.BIN " + inputs + " --wav /host/" + tag + ".wav\n")
(rd / "input.txt").write_text("pages\nexec FARTPRO.BIN\n")
(rd / "over.txt").write_text("pages\nexec FARTSYN.BIN --phrase " + "a" * 19 + " --wav /host/PRESERVE.WAV\n")
(share / "PRESERVE.WAV").write_bytes(b"prior-good-output")
(rd / "play.txt").write_text("pages\nexec FARTSYN.BIN --phrase m --play\n")
(rd / "play-over.txt").write_text("pages\nexec FARTSYN.BIN --phrase a --play\n")
(rd / "invalid.txt").write_text("pages\nexec FARTSYN.BIN --seed 18446744073709551616 --wav /host/PRESERVE.WAV\n")
(rd / "unsupported.txt").write_text("pages\nexec FARTSYN.BIN --phrase m --speech\n")
(rd / "cases.json").write_text(json.dumps(cases))
PY

vgate_file after.txt <<'EOF'
pages
procs
syscalls
echo done-zig-fart
EOF

vgate_run a -- --script '$RUN_DIR/a.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run phrase -- --script '$RUN_DIR/phrase.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run repeat -- --script '$RUN_DIR/repeat.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run breath -- --script '$RUN_DIR/breath.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run cluster -- --script '$RUN_DIR/cluster.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run seed0 -- --script '$RUN_DIR/seed0.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run seed42 -- --script '$RUN_DIR/seed42.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run seedmax -- --script '$RUN_DIR/seedmax.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run limit -- --script '$RUN_DIR/limit.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run input -- --script '$RUN_DIR/input.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'fart-probe: phrase 255 accepted, 256 refused; done' --script2-delay 1 --script-expect 'done-zig-fart' --timeout 90
vgate_run over -- --script '$RUN_DIR/over.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run missing -- --script '$RUN_DIR/play.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run play -- --sound --cpus 1 --script '$RUN_DIR/play.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run play-over -- --sound --script '$RUN_DIR/play-over.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run invalid -- --script '$RUN_DIR/invalid.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90
vgate_run unsupported -- --script '$RUN_DIR/unsupported.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-zig-fart' --timeout 90

vgate_assert a share-equals a.wav goldens/a.wav
vgate_assert phrase share-equals phrase.wav goldens/phrase.wav
vgate_assert repeat share-equals repeat.wav goldens/phrase.wav
vgate_assert breath share-equals breath.wav goldens/breath.wav
vgate_assert cluster share-equals cluster.wav goldens/cluster.wav
vgate_assert seed0 share-equals seed0.wav goldens/seed0.wav
vgate_assert seed42 share-equals seed42.wav goldens/seed42.wav
vgate_assert seedmax share-equals seedmax.wav goldens/seedmax.wav
vgate_assert limit share-equals limit.wav goldens/limit.wav
vgate_assert input share-equals input.wav goldens/input.wav
vgate_assert input share-equals STDOUT.WAV goldens/seed0.wav
vgate_assert input share-contains STDERR.TXT 'fart-synth: wav=20330'
vgate_assert input serial-contains 'fart-probe: OutOfMemory at arena=4194304, reused'
vgate_assert input serial-contains 'fart-probe: phrase 255 accepted, 256 refused; done'
vgate_assert over share-equals PRESERVE.WAV prior-good-output
vgate_assert over serial-contains 'fart-synth: DurationLimit'
vgate_assert missing serial-contains 'fart-synth: NoSoundDevice'
vgate_assert missing serial-contains '43 sys_audio_play calls=0'
vgate_assert play serial-contains '43 sys_audio_play calls=1'
vgate_assert play-over serial-contains 'fart-synth: PlaybackLimit'
vgate_assert play-over serial-contains '43 sys_audio_play calls=0'
vgate_assert invalid serial-contains 'fart-synth: InvalidSeed'
vgate_assert invalid share-equals PRESERVE.WAV prior-good-output
vgate_assert unsupported serial-contains 'fart-synth: UnsupportedOperation'

vgate_assert unsupported python <<'PY'
import hashlib, json, os, pathlib, re, sys
sys.path.insert(0, str(pathlib.Path("tools/zig").resolve()))
from test_fart import converted, wav_samples
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ["VG_SHARE"])
cases = json.loads((rd / "cases.json").read_text())
expected_samples = {"a":10584, "phrase":79380, "repeat":79380, "breath":3969,
                    "cluster":24696, "seed0":10143, "seed42":34839,
                    "seedmax":None, "limit":220500, "input":10584}
peaks, stacks = [], []
for tag, _, golden in cases:
    actual = (share / (tag + ".wav")).read_bytes()
    expected = (rd / "goldens" / golden).read_bytes()
    assert actual == expected, ("golden drift", tag)
    samples = wav_samples(actual)
    if expected_samples[tag] is not None:
        assert len(samples) == expected_samples[tag], (tag, len(samples))
    assert any(samples) and len(actual) <= 441044
assert (share / "STDOUT.WAV").read_bytes() == (rd / "goldens/seed0.wav").read_bytes()
stderr = (share / "STDERR.TXT").read_bytes()
assert stderr.startswith(b"fart-synth: wav=20330 ") and stderr.endswith(b" files=0\n")
assert b"RIFF" not in stderr
for tag in [c[0] for c in cases] + ["over", "missing", "play", "play-over", "invalid", "unsupported"]:
    serial = (rd / ("vm-serial-" + tag + ".log")).read_text()
    good = tag not in {"over", "missing", "play-over", "invalid", "unsupported"}
    if tag == "play" and "fart-synth: AudioDeviceRefused\n" in serial:
        # Native ENXIO also covers attached-device refusal. It must not be
        # mislabeled successful playback, discarded, retried, or chunked.
        good = False
    assert "procs FARTSYN.BIN exited status=" + ("0" if good else "70") in serial, tag
    assert "[EXC]" not in serial, tag
    pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial, re.M)
    assert len(pages) >= 2 and pages[0] == pages[-1], ("page leak", tag, pages)
    if good:
        match = re.search(r"wav=(\d+) arena_peak=(\d+) stack_high_water=(\d+) files=(\d+)", serial)
        assert match, tag
        n, peak, stack, files = map(int, match.groups())
        assert n <= 441044 and peak <= 4194304 and stack <= 131072 and files <= 1
        peaks.append(peak)
        stacks.append(stack)
serial = (rd / "vm-serial-play.log").read_text()
match = re.search(r"pcm fmt=(\d+) rate=(\d+) ch=(\d+) bytes=(\d+) sha256=([a-f0-9]{64})", serial)
assert match
fmt, rate, channels, count = map(int, match.groups()[:4])
hz = {0:8000, 2:16000, 3:22050, 5:32000, 6:44100, 7:48000}[rate]
raw = converted((rd / "goldens/breath.wav").read_bytes(), hz, channels, fmt)
assert len(raw) == count <= 65536
assert hashlib.sha256(raw).hexdigest() == match[5]
assert not raw.startswith(b"RIFF")
confirmed = "procs FARTSYN.BIN exited status=0" in serial
if not confirmed:
    assert "fart-synth: AudioDeviceRefused\n" in serial and "exited status=70" in serial
print(f"C4: 11 byte-exact exports, 16 boots with clean reaps; PCM {count} B, one submission, "
      f"confirmed={int(confirmed)} native_refusal={int(not confirmed)}; "
      f"arena peak={max(peaks)}, measured stack={max(stacks)}")
PY
