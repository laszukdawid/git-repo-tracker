package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// tooltipLayer is a non-intercepting overlay placed on top of a window's content
// that shows a small label near a target widget on hover. Fyne 2.7 has no
// built-in tooltips, and unlike widget.PopUp this never captures clicks.
type tooltipLayer struct {
	obj       *fyne.Container // without-layout, sits at the top of the content stack
	bg        *canvas.Rectangle
	text      *canvas.Text
	shownText string
	shownFor  fyne.CanvasObject
}

func newTooltipLayer(pal palette) *tooltipLayer {
	t := &tooltipLayer{}
	t.bg = canvas.NewRectangle(pal.tipBg)
	t.bg.StrokeColor = pal.tipBorder
	t.bg.StrokeWidth = 1
	t.bg.CornerRadius = 6
	t.text = canvas.NewText("", pal.tipText)
	t.text.TextSize = 12
	t.obj = container.NewWithoutLayout(t.bg, t.text)
	t.obj.Hide()
	return t
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
	if t.obj.Visible() && t.shownText == text && t.shownFor == target {
		return
	}
	t.shownText = text
	t.shownFor = target
	t.text.Text = text
	ts := fyne.MeasureText(text, t.text.TextSize, t.text.TextStyle)
	const padX, padY, gap = float32(8), float32(4), float32(3)
	w, h := ts.Width+padX*2, ts.Height+padY*2

	drv := fyne.CurrentApp().Driver()
	pos := drv.AbsolutePositionForObject(target)
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
	t.text.Move(fyne.NewPos(x+padX, y+padY))
	t.text.Resize(ts)
	t.bg.Refresh()
	t.text.Refresh()
	t.obj.Show()
}

func (t *tooltipLayer) hide() {
	t.shownText = ""
	t.shownFor = nil
	if t.obj.Visible() {
		t.obj.Hide()
	}
}

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
