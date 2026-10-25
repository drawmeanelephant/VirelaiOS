The IRC client — an RFC 1459/2812 core with a channel list,
scrollback, mention highlights and bounded per-channel logs. TLS
is the default dial (`tls.Dial`, 6697-style); plaintext refuses
unless `--plain` sets the session's consent bit, and SASL
credentials resolve out of the kernel secret store — an argv
password is refused outright. `go-irc.spec` registers, joins,
hard-drops, backs off and re-joins against a fixture IRCd. The
tile is [ICONIRC](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconirc.qoi).
