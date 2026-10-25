Minesweeper — three seeded board sizes, first click always
safe (mines place lazily on the first reveal, the clicked
neighbourhood excluded), explicit-stack flood-fill reveal,
right-click flag, chording on a satisfied number, and a notify on
the win. The save_state blob keeps the board, the elapsed seconds
and the seed through the seat's snapshot. `go-games.spec`
hit-tests the seeded board by real pointer input; the tile is
[ICONMINE](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconmine.qoi).
