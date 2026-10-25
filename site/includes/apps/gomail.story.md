The smallest honest mail client — IMAP4rev1 LIST/SELECT/FETCH
for boxes, bounded headers and one readable body, plus SMTP submit
for a single message, over `tls.Dial` with the plaintext-consent
rule at the dial boundary and named refusals for everything out of
scope, attachments included. The wire work caches into a bounded
`MAIL/` subtree on the data root, so `search` answers offline and
a failed dial falls back to what the cache can prove — never
fabricated mail. `go-mail.spec` walks fetch INBOX end to end
against a scripted server. The tile is
[ICONMAIL](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconmail.qoi).
