// Serial markers for the M74c (issue #1646) help browser. Every line is
// printed only AFTER the event it describes has happened (frame markers only
// after the frame is painted), so the go-help class-B gate's serial asserts
// can only pass when the app really ran. These shapes are test surface:
// model_test.go pins them on the host.
package main

const (
	markerOpen    = "gohelp: open id="
	markerAttach  = "gohelp: attached"
	markerPainted = "gohelp: painted"
	markerReady   = "gohelp: ready"
	markerPresent = "gohelp: present"
	markerRepaint = "gohelp: repainted"
	markerKey     = "gohelp: key "
	markerMouse   = "gohelp: mouse b="
	markerResized = "gohelp: resized "
	markerClose   = "gohelp: close"
	markerOK      = "gohelp OK"

	// The catalog face: rows straight from shlib.HelpRows (single-sourced
	// from GOSH's helpCatalog — no forked help text in this app).
	markerCatalog = "gohelp: catalog n="

	// The docs face: /host/docs entries the browser can open.
	markerDocs     = "gohelp: docs n="
	markerDocsOpen = "gohelp: docs open"
	markerDoc      = "gohelp: doc "
	markerDocErr   = "gohelp: doc error "
	markerDocFocus = "gohelp: docfocus "

	// Navigation and detail.
	markerFocus  = "gohelp: focus "
	markerDetail = "gohelp: detail "
	markerBrowse = "gohelp: browse"

	// `/`-to-filter: one marker per edit (n = visible matches), then the
	// clear. The gate pins `filter ec n=3` and the 45-row catalog clear.
	markerFilter        = "gohelp: filter "
	markerFilterOn      = "gohelp: filter on"
	markerFilterCleared = "gohelp: filter cleared n="

	// markerSettled is printed after the detail frame has been painted and
	// its `gohelp: detail …` marker flushed: the gate's close script waits on
	// it so the screenshot at the detail marker is never raced by teardown.
	markerSettled = "gohelp: settled after detail"
)
