# live-entropy-refusal.spec -- M97g (issue #2084): slot 72 fails CLOSED
# when the host entropy device is absent. The runner's --no-entropy flag
# detaches VZVirtioEntropyDeviceConfiguration, so the boot seed read fails
# (`entropy: seed failed n=0`) and the CSPRNG holds only the deterministic
# fallback — which EL0 must never be served. ENTPROBE.BIN (a minimal naked
# svc72 caller) reports `entprobe: refused` and exits with status 11
# (-EAGAIN): the honest transient refusal.
#
# Complements live-entropy.spec, which proves the seeded path on the same
# fleet (`entropy: seeded n=64`).

vgate_name live-entropy-refusal "slot 72 fails closed with no entropy device on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
exec ENTPROBE.BIN
echo rx-refusal-ok
EOF

vgate_run NR -- --no-entropy --script '$RUN_DIR/script.txt' --script-after 'tasks user-el0 exited status=7' --script-expect 'tasks user-exec reaped' --timeout 60

vgate_assert NR serial-exact 'VirelaiOS kernel has seized control.' 1
vgate_assert NR serial-count 'entropy: seed failed n=0 (deterministic fallback)' 1
vgate_assert NR serial-absent 'entropy: seeded n=64'
vgate_assert NR serial-count 'exec: loaded ENTPROBE.BIN size=' 1
vgate_assert NR serial-exact 'entprobe: refused' 1
vgate_assert NR serial-absent 'entprobe: ok'
vgate_assert NR serial-count 'exited status=11' 1
vgate_assert NR serial-exact 'rx-refusal-ok' 1
vgate_assert NR serial-absent '[EXC] parking:'
