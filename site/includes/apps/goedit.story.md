The editor — the app the registry calls the owner of the editor
arms race. Find and goto-line bars, an undo ring, syntax-aware
editing with themes, a minimap, bracket matching, format-on-save,
and the build-diagnostics navigation that makes it the face of the
in-guest `GOBUILD` error list. It also owns the seat's
unsaved-changes dialog client: a dirty buffer is published by
Ctrl-S or an explicit Save, never silently by a close — that is
the whole point of the dialog. `go-edit.spec` pins the
load-edit-save-close chain end to end; the tile is
[ICONEDIT](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconedit.qoi).
