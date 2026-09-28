package appkit

import (
	"virelai/shlib"
	"virelai/widgets"
)

// ListController adds keyboard selection and scroll-into-view to the pure
// widgets.List. It deliberately keeps the widget's row geometry authoritative.
type ListController struct {
	List *widgets.List
}

func NewListController(list *widgets.List) *ListController {
	return &ListController{List: list}
}

func (c *ListController) valid() bool {
	return c != nil && c.List != nil && len(c.List.Items) > 0
}

func (c *ListController) Move(delta int) bool {
	if !c.valid() {
		return false
	}
	if c.List.Sel < 0 {
		c.List.Sel = 0
	}
	next := c.List.Sel + delta
	if next < 0 {
		next = 0
	}
	if next >= len(c.List.Items) {
		next = len(c.List.Items) - 1
	}
	changed := next != c.List.Sel
	c.List.Sel = next
	c.ensureVisible()
	return changed
}

func (c *ListController) Home() bool {
	if !c.valid() {
		return false
	}
	changed := c.List.Sel != 0
	c.List.Sel = 0
	c.ensureVisible()
	return changed
}

func (c *ListController) End() bool {
	if !c.valid() {
		return false
	}
	last := len(c.List.Items) - 1
	changed := c.List.Sel != last
	c.List.Sel = last
	c.ensureVisible()
	return changed
}

func (c *ListController) Bounds() widgets.Rect       { return c.List.Bounds() }
func (c *ListController) HitTest(x, y int) bool      { return c.List.HitTest(x, y) }
func (c *ListController) Draw(cv widgets.RectCanvas) { c.List.Draw(cv) }
func (c *ListController) OnClick(x, y int) bool {
	if !c.valid() {
		return false
	}
	i := c.List.ItemAt(x, y)
	if i < 0 {
		return false
	}
	c.List.Sel = i
	c.ensureVisible()
	return true
}

func (c *ListController) HandleKey(k Key) bool {
	switch k.Named() {
	case NamedUp:
		return c.Move(-1)
	case NamedDown:
		return c.Move(1)
	case NamedHome:
		return c.Home()
	case NamedEnd:
		return c.End()
	}
	return false
}

func (c *ListController) OnKey(k Key) bool  { return c.HandleKey(k) }
func (c *ListController) SetFocused(v bool) { c.List.Focused = v }

func (c *ListController) ensureVisible() {
	if c.List.RowH <= 0 || c.List.R.H <= 0 {
		return
	}
	visible := c.List.VisibleRows()
	if visible < 1 {
		return
	}
	maxTop := len(c.List.Items) - visible
	if maxTop < 0 {
		maxTop = 0
	}
	if c.List.ScrollTop > maxTop {
		c.List.ScrollTop = maxTop
	}
	if c.List.Sel < c.List.ScrollTop {
		c.List.ScrollTop = c.List.Sel
	}
	if c.List.Sel >= c.List.ScrollTop+visible {
		c.List.ScrollTop = c.List.Sel - visible + 1
	}
	if c.List.ScrollTop < 0 {
		c.List.ScrollTop = 0
	}
}

// TextField is a bounded GUI single-line editor. It uses shlib.LineBuffer for
// byte/caret operations while appkit owns normalized key handling and paint.
type TextField struct {
	R           widgets.Rect
	Buffer      shlib.LineBuffer
	Fg          uint32
	Bg          uint32
	Caret       uint32
	Focused     bool
	Max         int
	Prefix      string
	Placeholder string
}

func NewTextField(r widgets.Rect, max int) TextField {
	if max < 1 {
		max = 1
	}
	return TextField{R: r, Buffer: shlib.NewLineBuffer(max), Max: max}
}

func (f *TextField) Bounds() widgets.Rect  { return f.R }
func (f *TextField) HitTest(x, y int) bool { return f.R.Contains(x, y) }

func (f *TextField) Value() string      { return f.Buffer.Value() }
func (f *TextField) CaretPosition() int { return f.Buffer.Caret() }

func (f *TextField) SetValue(s string) { f.Buffer.SetValue(s) }
func (f *TextField) Clear()            { f.Buffer.Clear() }
func (f *TextField) SetFocused(v bool) { f.Focused = v }

func (f *TextField) OnKey(k Key) bool {
	switch k.Named() {
	case NamedBackspace:
		return f.Buffer.Backspace()
	case NamedDelete:
		return f.Buffer.Delete()
	case NamedLeft:
		return f.Buffer.Left()
	case NamedRight:
		return f.Buffer.Right()
	case NamedHome:
		return f.Buffer.Home()
	case NamedEnd:
		return f.Buffer.End()
	}
	if k.Rune != 0 && !k.Ctrl && !k.Alt {
		return f.Buffer.InsertByte(byte(k.Rune))
	}
	return false
}

func (f *TextField) OnClick(x, y int) bool { return f.HitTest(x, y) }

func (f *TextField) Draw(c widgets.RectCanvas) {
	if f.R.Empty() {
		return
	}
	fg, bg := f.Fg, f.Bg
	if fg == 0 {
		fg = 0xffffff
	}
	if bg == 0 {
		bg = 0x101418
	}
	if f.Caret == 0 {
		f.Caret = 0x3b82f6
	}
	c.FillRect(f.R, bg)
	// The existing widget text painter is intentionally reused. A caret is a
	// one-cell accent line at the current byte position, clamped to the plate.
	text := f.Prefix + f.Value()
	if text == "" && f.Placeholder != "" {
		text = f.Placeholder
	}
	widgets.DrawText(c, f.R, text, fg)
	if f.Focused {
		x := f.R.X + 2 + (len([]rune(f.Prefix+f.Value())))*8
		if x >= f.R.Right()-2 {
			x = f.R.Right() - 3
		}
		if x < f.R.X+2 {
			x = f.R.X + 2
		}
		c.FillRect(widgets.Rect{X: x, Y: f.R.Y + 2, W: 2, H: f.R.H - 4}, f.Caret)
	}
}
