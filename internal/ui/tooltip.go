package ui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// tooltipLayer is a non-intercepting overlay placed on top of a window's content
// that shows a small label near a target widget on hover. Fyne 2.7 has no
// built-in tooltips, and unlike widget.PopUp this never captures clicks.
//
// It holds a pool of text lines rather than a single label: canvas.Text performs
// no text parsing, so a "\n" in one object renders as a broken single line. The
// pool is what lets a row explain its whole state — what the status glyph means,
// when it was last fetched and pulled, where the branch stands — in one tip.
type tooltipLayer struct {
	obj   *fyne.Container // without-layout, sits at the top of the content stack
	bg    *canvas.Rectangle
	lines []*canvas.Text // grown on demand; surplus lines are hidden
	pal   palette

	shownText string
	shownFor  fyne.CanvasObject

	// Hover delay, used for row tooltips only. A four-line panel appearing
	// instantly would flash over the next row every time the pointer crossed the
	// list; buttons keep the immediate tip.
	delay       *time.Timer
	pendingText string
	pendingFor  fyne.CanvasObject
}

// tipLineGap is the vertical space between tooltip lines.
const tipLineGap = float32(space3xs)

// tipDelay is how long the pointer must rest on a row before its tip appears.
const tipDelay = 350 * time.Millisecond

func newTooltipLayer(pal palette) *tooltipLayer {
	t := &tooltipLayer{pal: pal}
	t.bg = canvas.NewRectangle(pal.tipBg)
	t.bg.StrokeColor = pal.tipBorder
	t.bg.StrokeWidth = hairlineW
	t.bg.CornerRadius = radiusSm
	t.obj = container.NewWithoutLayout(t.bg)
	t.obj.Hide()
	return t
}

// lineAt returns the nth text line, creating it if the pool is short.
func (t *tooltipLayer) lineAt(i int) *canvas.Text {
	for len(t.lines) <= i {
		txt := canvas.NewText("", t.pal.tipText)
		txt.TextSize = textSm
		t.lines = append(t.lines, txt)
		t.obj.Add(txt)
	}
	return t.lines[i]
}

// wrap layers the tooltip on top of content so it renders above everything.
func (t *tooltipLayer) wrap(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(content, t.obj)
}

// show positions the tooltip just below target (or above, if it would overflow)
// and reveals it.
func (t *tooltipLayer) show(text string, target fyne.CanvasObject) {
	if text == "" {
		return
	}
	t.stopDelay()
	if t.obj.Visible() && t.shownText == text && t.shownFor == target {
		return
	}
	t.shownText = text
	t.shownFor = target

	const padX, padY, gap = float32(spaceSm), float32(space2xs), float32(space3xs)
	drv := fyne.CurrentApp().Driver()
	pos := drv.AbsolutePositionForObject(target)

	// Cap each line to the canvas so a long fetch error cannot run off the edge.
	maxLine := float32(1 << 20)
	if c := drv.CanvasForObject(target); c != nil {
		maxLine = c.Size().Width - 8 - padX*2
	}

	parts := strings.Split(text, "\n")
	var w, h float32
	lineH := float32(0)
	for i, part := range parts {
		line := t.lineAt(i)
		// The first line is the headline — the error, or the working-tree summary.
		line.TextStyle = fyne.TextStyle{Bold: i == 0 && len(parts) > 1}
		line.Text = truncateToWidth(part, maxLine, line.TextSize, line.TextStyle)
		ls := fyne.MeasureText(line.Text, line.TextSize, line.TextStyle)
		if ls.Width > w {
			w = ls.Width
		}
		if ls.Height > lineH {
			lineH = ls.Height
		}
		line.Show()
	}
	for i := len(parts); i < len(t.lines); i++ {
		t.lines[i].Hide()
	}
	n := float32(len(parts))
	h = n*lineH + (n-1)*tipLineGap + padY*2
	w += padX * 2

	sz := target.Size()
	x := pos.X
	y := pos.Y + sz.Height + gap
	if c := drv.CanvasForObject(target); c != nil {
		cs := c.Size()
		if x+w > cs.Width-4 {
			x = cs.Width - 4 - w
		}
		if y+h > cs.Height-4 { // not enough room below — show above
			y = pos.Y - h - gap
		}
	}
	if x < 4 {
		x = 4
	}
	if y < 4 {
		y = 4
	}

	t.bg.Move(fyne.NewPos(x, y))
	t.bg.Resize(fyne.NewSize(w, h))
	for i := range parts {
		line := t.lines[i]
		line.Move(fyne.NewPos(x+padX, y+padY+float32(i)*(lineH+tipLineGap)))
		line.Resize(fyne.NewSize(w-padX*2, lineH))
		line.Refresh()
	}
	t.bg.Refresh()
	t.obj.Show()
}

// showDelayed reveals the tip only once the pointer has rested on target. The
// callback re-checks what is pending, so a pointer that moved on in the meantime
// never gets a stale tooltip.
func (t *tooltipLayer) showDelayed(text string, target fyne.CanvasObject, d time.Duration) {
	if text == "" {
		return
	}
	if t.obj.Visible() && t.shownText == text && t.shownFor == target {
		return // already up: don't restart the delay on every pointer move
	}
	if t.pendingText == text && t.pendingFor == target && t.delay != nil {
		return
	}
	t.stopDelay()
	t.pendingText, t.pendingFor = text, target
	t.delay = time.AfterFunc(d, func() {
		fyne.Do(func() {
			if t.pendingText == text && t.pendingFor == target {
				t.show(text, target)
			}
		})
	})
}

func (t *tooltipLayer) stopDelay() {
	if t.delay != nil {
		t.delay.Stop()
		t.delay = nil
	}
	t.pendingText, t.pendingFor = "", nil
}

func (t *tooltipLayer) hide() {
	t.stopDelay()
	t.shownText = ""
	t.shownFor = nil
	if t.obj.Visible() {
		t.obj.Hide()
	}
}

// tipHover wraps a child object and shows a tooltip while the pointer is over it.
// It's for widgets that don't surface hover themselves (e.g. a plain widget.Entry):
// as the nearest Hoverable ancestor it receives the pointer events the child
// ignores, without disturbing the child's own tap/focus/cursor handling.
type tipHover struct {
	widget.BaseWidget
	child fyne.CanvasObject
	tip   string
	tips  *tooltipLayer
}

func newTipHover(tips *tooltipLayer, tip string, child fyne.CanvasObject) *tipHover {
	h := &tipHover{child: child, tip: tip, tips: tips}
	h.ExtendBaseWidget(h)
	return h
}

func (h *tipHover) MouseIn(*desktop.MouseEvent) {
	if h.tips != nil {
		h.tips.show(h.tip, h)
	}
}
func (h *tipHover) MouseMoved(*desktop.MouseEvent) {}
func (h *tipHover) MouseOut() {
	if h.tips != nil {
		h.tips.hide()
	}
}
func (h *tipHover) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(h.child) }

// tipButton is a widget.Button that shows a tooltip while hovered.
type tipButton struct {
	widget.Button
	tip  string
	tips *tooltipLayer
}

func newTipButton(tips *tooltipLayer, icon fyne.Resource, tip string, tapped func()) *tipButton {
	b := &tipButton{tip: tip, tips: tips}
	b.Icon = icon
	b.OnTapped = tapped
	b.Importance = widget.LowImportance
	b.ExtendBaseWidget(b)
	return b
}

func (b *tipButton) MouseIn(e *desktop.MouseEvent) {
	b.Button.MouseIn(e)
	if b.tips != nil {
		b.tips.show(b.tip, b)
	}
}

func (b *tipButton) MouseOut() {
	b.Button.MouseOut()
	if b.tips != nil {
		b.tips.hide()
	}
}
