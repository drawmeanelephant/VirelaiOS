The process monitor — a live process list with click-to-kill
plus the network counters view, the GPU tab's compute counters
and the Trust tab's grant-decision tail. CPU% and MEM are real:
the versioned `sys_procs` row reports scheduler tick share and
resident footprint (ADR 0047), and on a pre-v2 kernel the columns
disappear rather than approximate. `go-top.spec`'s proof is a
kill, not a chart — it reads the pid out of TOP's own row line
and watches the kernel reap it. The tile is
[ICONTOP](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/icontop.qoi).
