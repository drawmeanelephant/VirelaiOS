The recorder — `<seconds> <out.wav>` captures the kernel's one
capture stream (S16 / 48 kHz / mono) into a RIFF/WAVE published
crash-safe; a bare launch opens the same flow as a windowed view
with a Record button. The gate does not take the guest's word for
it: the host reads the share and recomputes the 44-byte header,
the duration and the SHA-256 itself — and a refused record (no
device, no consent) leaves no file at all. `live-sound-record.spec`
owns both outcomes. The tile is
[ICONREC](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconrec.qoi).
