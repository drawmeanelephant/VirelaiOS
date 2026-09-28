package main

import (
	"unsafe"

	"virelai/theme"
	"virelai/vi"
)

// The center is an in-memory history of recently queued kind-12 notices.
// It is intentionally separate from the short-lived toast queue: expiry
// removes a toast, but the user can still review and act on its notice.
const (
	NotifyCenterMax = 8
	notifyCenterW   = 520
	notifyCenterH   = 360
	notifyRowH      = 34
	notifyHeaderH   = 30
	notifyFooterH   = 28
	notifyCenterPad = 12

	MarkerNotifyCenterOpen   = "gotabwm: notify center open"
	MarkerNotifyCenterClose  = "gotabwm: notify center close"
	MarkerNotifyCenterPaint  = "gotabwm: notify center paint"
	MarkerNotifyCenterAction = "gotabwm: notify center "
)

type centerNotification struct {
	tabID      uint32
	id         uint32
	source     string
	text       string
	sourceGone bool
}

var (
	notifyCenter        []centerNotification
	notifyCenterOpen    bool
	notifyCenterNextID  uint32
	notifyCenterPainted bool
	focusNotifySource   = focusHosted
)

func notifyCenterPush(tabID uint32, text string) uint32 {
	source := "App " + vi.Itoa64(int64(tabID))
	if i := tabs.index(tabID); i >= 0 && tabs.At(i).Title != "" {
		source = tabs.At(i).Title
	}
	if len(notifyCenter) == NotifyCenterMax {
		copy(notifyCenter, notifyCenter[1:])
		notifyCenter = notifyCenter[:NotifyCenterMax-1]
	}
	notifyCenterNextID++
	if notifyCenterNextID == 0 {
		notifyCenterNextID++
	}
	notifyCenter = append(notifyCenter, centerNotification{
		tabID: tabID, id: notifyCenterNextID, source: source, text: text,
	})
	return notifyCenterNextID
}

// notifyCenterSourceClosed preserves the notification while making its dead
// source explicit. The closed bit is latched, so a later window-id reuse can
// never turn a historical item into a link to an unrelated app.
func notifyCenterSourceClosed(tabID uint32) {
	for i := range notifyCenter {
		if notifyCenter[i].tabID == tabID {
			notifyCenter[i].sourceGone = true
		}
	}
}

func dismissNotifyCenter(index int) bool {
	if index < 0 || index >= len(notifyCenter) {
		return false
	}
	copy(notifyCenter[index:], notifyCenter[index+1:])
	notifyCenter[len(notifyCenter)-1] = centerNotification{}
	notifyCenter = notifyCenter[:len(notifyCenter)-1]
	return true
}

func dismissNotifyCenterEntry(index int) bool {
	if index < 0 || index >= len(notifyCenter) {
		return false
	}
	id := notifyCenter[index].id
	dismissNotifyCenter(index)
	keep := notifyQueue[:0]
	dismissed := false
	var sender uint32
	for _, toast := range notifyQueue {
		if toast.centerID == id {
			dismissed = true
			sender = toast.tabID
			continue
		}
		keep = append(keep, toast)
	}
	notifyQueueSet(keep)
	if dismissed {
		vi.ConsoleLine(MarkerNotifyDismiss + vi.Itoa64(int64(sender)))
	}
	return true
}

func clearNotifyCenter() int {
	n := len(notifyCenter)
	notifyCenter = nil
	if len(notifyQueue) > 0 {
		queued := notifyQueue
		notifyQueueSet(nil)
		for _, toast := range queued {
			vi.ConsoleLine(MarkerNotifyDismiss + vi.Itoa64(int64(toast.tabID)))
		}
	}
	return n
}

func notifyCenterRect(width, height int) (x, y, w, h int) {
	if width <= 0 || height <= 0 {
		return 0, 0, 0, 0
	}
	w, h = notifyCenterW, notifyCenterH
	if w > width-2*chromeInset {
		w = width - 2*chromeInset
	}
	if h > height-2*chromeInset {
		h = height - 2*chromeInset
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0
	}
	return (width - w) / 2, (height - h) / 2, w, h
}

func notifyCenterClockHit(px, py uint32, width, height int) bool {
	x, y, w, h := chromeRect(width, height)
	return w > 0 && h > 0 && int(px) >= x && int(px) < x+w &&
		int(py) >= y && int(py) < y+h
}

func notifyCenterHit(px, py uint32, width, height int) bool {
	if !notifyCenterOpen {
		return false
	}
	x, y, w, h := notifyCenterRect(width, height)
	return w > 0 && h > 0 && int(px) >= x && int(px) < x+w &&
		int(py) >= y && int(py) < y+h
}

// notifyCenterClick handles a pointer press in the center or its clock-panel
// affordance. A source click focuses the sender but leaves the center open;
// row X dismisses just that item, while the footer clears the full history.
func notifyCenterClick(px, py uint32) bool {
	if !notifyCenterOpen {
		if !notifyCenterClockHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight) {
			return false
		}
		notifyCenterOpen = true
		notifyCenterPainted = false
		vi.ConsoleLine(MarkerNotifyCenterOpen)
		return true
	}

	x, y, w, h := notifyCenterRect(vi.ScanoutWidth, vi.ScanoutHeight)
	if w <= 0 || h <= 0 {
		notifyCenterOpen = false
		notifyCenterPainted = false
		vi.ConsoleLine(MarkerNotifyCenterClose)
		return true
	}
	pxi, pyi := int(px), int(py)
	if !notifyCenterHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight) {
		notifyCenterOpen = false
		notifyCenterPainted = false
		vi.ConsoleLine(MarkerNotifyCenterClose)
		return true
	}
	if pyi < y+notifyHeaderH && pxi >= x+w-42 {
		notifyCenterOpen = false
		notifyCenterPainted = false
		vi.ConsoleLine(MarkerNotifyCenterClose)
		return true
	}
	visible := notifyCenterVisibleRows(h)
	rowsY := y + notifyHeaderH
	if pyi >= rowsY && pyi < rowsY+visible*notifyRowH {
		index := (pyi - rowsY) / notifyRowH
		if index < len(notifyCenter) {
			if pxi >= x+w-34 {
				if dismissNotifyCenterEntry(index) {
					vi.ConsoleLine(MarkerNotifyCenterAction + "dismiss")
				}
				return true
			}
			entry := notifyCenter[index]
			if !entry.sourceGone && tabs.index(entry.tabID) >= 0 {
				if focusNotifySource(entry.tabID) {
					vi.ConsoleLine(MarkerNotifyCenterAction + "focus id=" + vi.Itoa64(int64(entry.tabID)))
					vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(entry.tabID)))
					vi.ConsoleLine(MarkerHostFocus + vi.Itoa64(int64(entry.tabID)))
				} else {
					// The kernel reaps a client's windows when its process
					// exits. A refused focus latches a closed source instead
					// of retrying a dead window id on every later click.
					notifyCenter[index].sourceGone = true
					vi.ConsoleLine(MarkerNotifyCenterAction + "source closed id=" + vi.Itoa64(int64(entry.tabID)))
				}
			}
			return true
		}
	}
	if pyi >= y+h-notifyFooterH && pxi < x+w-42 {
		if clearNotifyCenter() > 0 {
			vi.ConsoleLine(MarkerNotifyCenterAction + "clear all")
		}
		return true
	}
	return true
}

func notifyCenterVisibleRows(height int) int {
	rows := (height - notifyHeaderH - notifyFooterH) / notifyRowH
	if rows < 0 {
		return 0
	}
	if rows > NotifyCenterMax {
		return NotifyCenterMax
	}
	return rows
}

func paintNotifyCenter(scan []byte, width, height int) int {
	if !notifyCenterOpen || width <= 0 || height <= 0 || len(scan) < 4 {
		return 0
	}
	x, y, w, h := notifyCenterRect(width, height)
	if w <= 0 || h <= 0 {
		return 0
	}
	pixels := len(scan) / 4
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(&scan[0])), pixels)
	maxH := pixels / width
	if maxH <= 0 {
		return 0
	}
	tok := theme.Current
	written := fillRect(pix, width, maxH, x, y, w, h, tok.Surface)
	written += fillRect(pix, width, maxH, x, y, w, tok.BorderW, tok.Rule)
	written += fillRect(pix, width, maxH, x, y, 2, h, tok.Accent)
	written += drawText8(pix, width, maxH, x+notifyCenterPad, y+10, "Notifications", tok.Ink)
	written += drawText8(pix, width, maxH, x+w-28, y+10, "X", tok.Muted)
	written += fillRect(pix, width, maxH, x+notifyCenterPad, y+notifyHeaderH-1,
		w-2*notifyCenterPad, 1, tok.Border)

	rows := notifyCenterVisibleRows(h)
	for i := 0; i < rows && i < len(notifyCenter); i++ {
		rowY := y + notifyHeaderH + i*notifyRowH
		entry := notifyCenter[i]
		source := entry.source
		fg := tok.Ink
		if entry.sourceGone || tabs.index(entry.tabID) < 0 {
			source += " [closed]"
			fg = tok.Muted
		}
		written += drawText8(pix, width, maxH, x+notifyCenterPad, rowY+2,
			centerText(source, 48), fg)
		written += drawText8(pix, width, maxH, x+notifyCenterPad, rowY+16,
			centerText(entry.text, 48), tok.InkMuted)
		written += drawText8(pix, width, maxH, x+w-28, rowY+10, "X", tok.Muted)
		if i+1 < rows && i+1 < len(notifyCenter) {
			written += fillRect(pix, width, maxH, x+notifyCenterPad, rowY+notifyRowH-1,
				w-2*notifyCenterPad, 1, tok.Border)
		}
	}
	footerY := y + h - notifyFooterH
	written += fillRect(pix, width, maxH, x+notifyCenterPad, footerY,
		w-2*notifyCenterPad, tok.BorderW, tok.Border)
	written += drawText8(pix, width, maxH, x+notifyCenterPad, footerY+10,
		"Clear all", tok.Ink)
	return written
}

func notifyCenterPaintMarker(painted, presented bool) (string, bool) {
	if !notifyCenterOpen || !painted || !presented || notifyCenterPainted {
		return "", false
	}
	notifyCenterPainted = true
	return MarkerNotifyCenterPaint, true
}

func centerText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
