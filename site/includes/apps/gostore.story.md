The store's human path — browse, search, detail, install,
update, remove, rollback and pin as a tab on the default seat.
Every action drives the same signed legs STORE.ELF runs — verify,
fetch, sha256, publish, `.prev` — through `virelai/store` linked
in-process: GOSTORE never execs STORE.ELF, because four concurrent
Go runtimes would exceed the seat's proven three-runtime envelope.
`go-gostore.spec` walks an install through to a launched payload;
the tile is [ICONSTOR](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconstor.qoi).
