package appkit

import (
	"virelai/draw"
	"virelai/icons"
	"virelai/theme"
	"virelai/ttf"
	"virelai/vi"
	"virelai/widgets"
)

// DialogKind identifies the small set of app-owned modal flows appkit owns.
// These are deliberately not replacements for the kernel's WIN_UNSAVED dialog:
// an app may ask for a path or confirm an overwrite, while the kernel remains
// the authority for the unsaved-document contract.
type DialogKind uint8

const (
	MessageDialog DialogKind = iota
	ConfirmDialog
	PromptDialog
)

// DialogButton is both a drawable control and its hit rectangle. Keeping the
// rectangle beside the label makes the pure state machine and the guest draw
// path agree without a second geometry source.
type DialogButton struct {
	Label string
	R     widgets.Rect
}

// Dialog is a modal state machine. It has no syscall or drawing dependency in
// its state transitions, so host tests can cover focus, cancellation, and text
// input without a window.
type Dialog struct {
	Kind    DialogKind
	Title   string
	Message string
	Input   string
	Buttons []DialogButton
	Focus   int
	Result  string
	Open    bool
	R       widgets.Rect
	// Faces are optional for a legacy filler canvas. When supplied, title,
	// body, controls and the icon use the same tinted TrueType mask path.
	UIFace   *ttf.Face
	IconFace *ttf.Face
}

// NewMessage creates a one-button informational dialog.
func NewMessage(title, message string) Dialog {
	return newDialog(MessageDialog, title, message, false)
}

// NewConfirm creates a Yes/No dialog. No is the safe default focus.
func NewConfirm(title, message string) Dialog {
	d := newDialog(ConfirmDialog, title, message, false)
	d.Focus = 1
	return d
}

// NewPrompt creates a bounded single-line text prompt.
func NewPrompt(title, message string) Dialog {
	return newDialog(PromptDialog, title, message, true)
}

func newDialog(kind DialogKind, title, message string, prompt bool) Dialog {
	d := Dialog{
		Kind:    kind,
		Title:   title,
		Message: message,
		Open:    true,
		R:       widgets.Rect{X: 96, Y: 72, W: 320, H: 156},
	}
	if prompt {
		d.Buttons = []DialogButton{
			{Label: "OK", R: widgets.Rect{X: 246, Y: 184, W: 72, H: 28}},
			{Label: "Cancel", R: widgets.Rect{X: 326, Y: 184, W: 72, H: 28}},
		}
	} else if kind == ConfirmDialog {
		d.Buttons = []DialogButton{
			{Label: "Yes", R: widgets.Rect{X: 206, Y: 184, W: 72, H: 28}},
			{Label: "No", R: widgets.Rect{X: 286, Y: 184, W: 72, H: 28}},
		}
	} else {
		d.Buttons = []DialogButton{{Label: "OK", R: widgets.Rect{X: 286, Y: 184, W: 72, H: 28}}}
	}
	d.layoutButtons()
	return d
}

// layoutButtons derives paint and hit rects from the current plate, including
// when an app moves the dialog after construction. The last button is right
// aligned, with a PadSM gap; the hit path calls this too.
func (d *Dialog) layoutButtons() {
	tok := theme.Current
	x := d.R.Right() - (tok.PadMD + tok.PadSM)
	y := d.R.Bottom() - (tok.PadMD + tok.PadSM) - 28
	for i := len(d.Buttons) - 1; i >= 0; i-- {
		x -= 72
		d.Buttons[i].R = widgets.Rect{X: x, Y: y, W: 72, H: 28}
		x -= tok.PadSM
	}
}

// HandleKey consumes one event while the dialog is open. changed means the
// caller should repaint; done means the dialog has produced its result and
// has closed itself.
func (d *Dialog) HandleKey(ev vi.Event) (changed, done bool) {
	if !d.Open || ev.Kind != vi.EvKeyDown {
		return false, false
	}
	// The event wire exposes the HID usage in Arg0 and the decoded symbol in
	// Arg1. Accept both spellings, matching existing app handlers and tests.
	usage, ch := ev.Arg0, ev.Arg1
	switch {
	case usage == 0x28 || ch == '\r' || ch == '\n':
		d.choose()
		return true, true
	case usage == 0x29 || ch == 0x1b:
		d.cancel()
		return true, true
	case usage == 0x2b || ch == '\t':
		if len(d.Buttons) > 0 {
			d.Focus = (d.Focus + 1) % len(d.Buttons)
		}
		return true, false
	case (usage == 0x2a || ch == 0x08 || ch == 0x7f) && d.Kind == PromptDialog:
		if d.Input != "" {
			d.Input = d.Input[:len(d.Input)-1]
		}
		return true, false
	case d.Kind == PromptDialog && ch >= 0x20 && ch < 0x7f:
		if len(d.Input) < 128 {
			d.Input += string(byte(ch))
		}
		return true, false
	}
	return false, false
}

// HandleMouse handles a left-button press at a canvas point. A click focuses
// and chooses the matching button; it never returns to the app handler.
func (d *Dialog) HandleMouse(x, y int, down bool) (changed, done bool) {
	if !d.Open || !down {
		return false, false
	}
	d.layoutButtons()
	for i := range d.Buttons {
		if d.Buttons[i].R.Contains(x, y) {
			d.Focus = i
			d.choose()
			return true, true
		}
	}
	return false, false
}

func (d *Dialog) choose() {
	if d.Focus < 0 || d.Focus >= len(d.Buttons) {
		d.cancel()
		return
	}
	d.Result = d.Buttons[d.Focus].Label
	if d.Kind == PromptDialog && d.Focus == 0 {
		d.Result = d.Input
	}
	d.Open = false
}

func (d *Dialog) cancel() {
	d.Result = "cancel"
	d.Open = false
}

// Draw paints the M86 dialog chrome: an AA window plate and inset border,
// title row, text, input (for a prompt), and right-aligned controls with an
// inside focus ring. This is one surface on the caller's own window buffer.
func (d *Dialog) Draw(c widgets.Canvas) {
	if !d.Open {
		return
	}
	d.layoutButtons()
	tok := theme.Current
	inset := tok.PadMD + tok.PadSM
	c.FillRoundedRect(d.R, 8, draw.Opaque(tok.Surface))
	c.StrokeRoundedRect(d.R, 8, tok.BorderW, draw.Opaque(tok.Border))
	title := widgets.Rect{X: d.R.X + inset + 20, Y: d.R.Y + inset, W: d.R.W - 2*inset - 20, H: 24}
	icon := widgets.Rect{X: d.R.X + inset, Y: d.R.Y + inset, W: 16, H: 24}
	cp := icons.Inventory[12].Codepoint // search for prompts
	switch d.Kind {
	case MessageDialog:
		cp = icons.Inventory[18].Codepoint // info-circle
	case ConfirmDialog:
		cp = icons.Inventory[16].Codepoint // warning-triangle
	}
	draw.Glyph(c, d.IconFace, icon, icon.X, icon.Y+20, icons.NominalPx, cp, draw.Opaque(tok.Accent))
	dialogText(c, d.UIFace, title, d.Title, tok.Text, false)
	body := widgets.Rect{X: d.R.X + inset, Y: d.R.Y + 44, W: d.R.W - 2*inset, H: 28}
	dialogText(c, d.UIFace, body, d.Message, tok.Muted, false)
	if d.Kind == PromptDialog {
		line := widgets.Rect{X: d.R.X + inset, Y: d.R.Y + 82, W: d.R.W - 2*inset, H: 24}
		c.FillRoundedRect(line, 4, draw.Opaque(tok.ChromeBg))
		c.StrokeRoundedRect(line, 4, tok.BorderW, draw.Opaque(tok.Border))
		dialogText(c, d.UIFace, line.Inset(tok.PadSM), d.Input, tok.Ink, false)
		// The active text entry has a visible caret even when it is empty.
		caretX := line.X + tok.PadSM + 1
		if d.UIFace != nil {
			for _, ch := range d.Input {
				caretX += d.UIFace.AdvancePx(ch, 13)
			}
		} else {
			caretX += len(d.Input) * 8
		}
		caretX = min(caretX, line.Right()-tok.PadSM-1)
		c.FillRect(widgets.Rect{X: caretX, Y: line.Y + 5, W: 1, H: 14}, draw.Opaque(tok.Caret))
	}
	for i := range d.Buttons {
		b := d.Buttons[i]
		face, border, label := tok.BtnIdle, tok.Border, tok.Text
		if i == d.Focus {
			face, border = tok.BtnHover, tok.Accent
		}
		if i == 0 && d.Kind != ConfirmDialog || i == 1 && d.Kind == ConfirmDialog {
			border, label = tok.Accent, tok.OnAccent
		}
		c.FillRoundedRect(b.R, 4, draw.Opaque(face))
		c.StrokeRoundedRect(b.R, 4, tok.BorderW, draw.Opaque(border))
		if i == d.Focus {
			c.StrokeRoundedRect(b.R, 4, tok.FocusW, draw.Opaque(tok.Accent))
		}
		dialogText(c, d.UIFace, b.R.Inset(tok.PadSM), b.Label, label, true)
	}
}

func dialogText(c widgets.Canvas, face *ttf.Face, r widgets.Rect, text string, rgb uint32, centered bool) {
	if face == nil {
		widgets.DrawText(c, r, text, draw.Opaque(rgb))
		return
	}
	const size = 13
	asc, desc, _ := face.LineMetrics(size)
	x := r.X + theme.Current.PadXS
	if centered {
		w := 0
		for _, ch := range text {
			w += face.AdvancePx(ch, size)
		}
		if w < r.W {
			x = r.X + (r.W-w)/2
		}
	}
	draw.Text(c, face, r, x, r.Y+(r.H-asc-desc)/2+asc, size, text, draw.Opaque(rgb))
}
