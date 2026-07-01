package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Compact metrics for the filtering-header toggles — deliberately smaller than a
// widget.Button so the panel stays short.
const (
	chipTextSize = 11
	chipHPad     = 6
	chipVPad     = 3
	chipIconSize = 12
	chipGap      = 4
)

// chipSelectedText is the label colour on a selected (accent-filled) chip; white
// reads well on the accent blue in both the light and dark palettes.
var chipSelectedText = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

// segChip is a compact toggle for the filtering header: a small bold label with an
// optional leading icon, drawn with a rounded accent fill when selected and a faint
// tint on hover. It's much denser than a widget.Button, keeping the strip short.
type segChip struct {
	widget.BaseWidget
	text     string
	icon     fyne.Resource
	selected bool
	hovered  bool
	pal      palette
	onTap    func()
}

func newSegChip(text string, icon fyne.Resource, pal palette, onTap func()) *segChip {
	c := &segChip{text: text, icon: icon, pal: pal, onTap: onTap}
	c.ExtendBaseWidget(c)
	return c
}

func (c *segChip) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *segChip) MouseIn(*desktop.MouseEvent)    { c.setHovered(true) }
func (c *segChip) MouseMoved(*desktop.MouseEvent) {}
func (c *segChip) MouseOut()                      { c.setHovered(false) }

func (c *segChip) setHovered(h bool) {
	if c.hovered != h {
		c.hovered = h
		c.Refresh()
	}
}

func (c *segChip) setSelected(s bool) {
	if c.selected != s {
		c.selected = s
		c.Refresh()
	}
}

func (c *segChip) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 5
	txt := canvas.NewText(c.text, c.pal.rowSub)
	txt.TextSize = chipTextSize
	txt.TextStyle = fyne.TextStyle{Bold: true}
	objs := []fyne.CanvasObject{bg, txt}
	var img *canvas.Image
	if c.icon != nil {
		img = canvas.NewImageFromResource(theme.NewColoredResource(c.icon, colorNameChipIcon))
		img.FillMode = canvas.ImageFillContain
		objs = append(objs, img)
	}
	r := &segChipRenderer{c: c, bg: bg, txt: txt, img: img, objects: objs}
	r.Refresh()
	return r
}

type segChipRenderer struct {
	c       *segChip
	bg      *canvas.Rectangle
	txt     *canvas.Text
	img     *canvas.Image
	objects []fyne.CanvasObject
}

func (r *segChipRenderer) MinSize() fyne.Size {
	ts := fyne.MeasureText(r.c.text, chipTextSize, fyne.TextStyle{Bold: true})
	w := ts.Width + chipHPad*2
	h := ts.Height + chipVPad*2
	if r.img != nil {
		w += chipIconSize + chipGap
		if ih := float32(chipIconSize + chipVPad*2); ih > h {
			h = ih
		}
	}
	return fyne.NewSize(w, h)
}

func (r *segChipRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	x := float32(chipHPad)
	if r.img != nil {
		r.img.Move(fyne.NewPos(x, (size.Height-chipIconSize)/2))
		r.img.Resize(fyne.NewSize(chipIconSize, chipIconSize))
		x += chipIconSize + chipGap
	}
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(x, (size.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *segChipRenderer) Refresh() {
	switch {
	case r.c.selected:
		r.bg.FillColor = r.c.pal.rowName // accent fill
		r.txt.Color = chipSelectedText
	case r.c.hovered:
		r.bg.FillColor = r.c.pal.btnHover
		r.txt.Color = r.c.pal.rowSub
	default:
		r.bg.FillColor = color.Transparent
		r.txt.Color = r.c.pal.rowSub
	}
	if r.img != nil {
		// Tint the icon to match the label colour beside it: the muted colour normally
		// (including on hover) and the selected-label colour when active.
		name := colorNameChipIcon
		if r.c.selected {
			name = colorNameChipIconOn
		}
		r.img.Resource = theme.NewColoredResource(r.c.icon, name)
		r.img.Refresh()
	}
	r.bg.Refresh()
	r.txt.Refresh()
}

func (r *segChipRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *segChipRenderer) Destroy()                     {}
