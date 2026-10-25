The sound player — the `audio` mime's registered handler.
RIFF/WAVE parses as a stream (PCM, float and the two ADPCM codecs,
one block at a time), resamples by a 16-tap windowed sinc into the
mixer's negotiated tuple, and plays through the audio stream seam
— memory constant in the file's size. The same window carries
record and live mic-meter modes over the kernel's one capture
stream. `live-sound-playback.spec` owns the playback path; the
tile is [ICONSND](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconsnd.qoi).
