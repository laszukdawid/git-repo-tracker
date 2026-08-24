package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/ide"
)

// The row's "open in IDE" control.
//
// It is one chip with two zones rather than two chips: a row already reserves
// space for a pull/keep-fresh chip and an open-folder chip, and a fourth would
// start eating repository names. Tapping the chip opens the remembered editor;
// tapping the small chevron in its corner opens the picker to change it.
//
// The chip carries the editor's own icon, extracted from its application
// bundle, so it is recognisable without reading anything — the wording lives in
// the tooltip.

const (
	// The change zone is a corner of the chip, big enough to hit but small
	// enough that the chip still reads as one control.
	ideAltZone = 12
	ideChev    = 7
)

type ideButton struct {
	widget.BaseWidget
	pal palette

	res     fyne.Resource // the editor's icon, or a generic fallback
	tip     string
	altTip  string
	missing bool // the remembered editor is no longer installed
	onOpen  func()
	onPick  func()

	state      interactionState
	altHovered bool
	disabled   bool
	compact    bool

	onFocusChanged func(bool)
	keyboardGuard  func() bool
}

func (b *ideButton) useCompactRowChrome() { b.compact = true }

func newIDEButton(pal palette, res fyne.Resource, tip, altTip string, missing bool, onOpen, onPick func()) *ideButton {
	b := &ideButton{pal: pal, res: res, tip: tip, altTip: altTip, missing: missing,
		onOpen: onOpen, onPick: onPick}
	b.ExtendBaseWidget(b)
	return b
}

// Tapped routes by where the pointer is: the corner changes the editor, the
// rest of the chip opens it. A missing editor opens the picker wherever it is
// tapped — there is nothing to launch.
func (b *ideButton) Tapped(ev *fyne.PointEvent) {
	if b.disabled {
		return
	}
	if b.altHovered || b.missing {
		if b.onPick != nil {
			b.onPick()
		}
		return
	}
	if b.onOpen != nil {
		b.onOpen()
	}
}

func (b *ideButton) setHovered(h bool) {
	if !h {
		b.setPressed(false)
	}
	if b.state.setHovered(h) {
		if !h {
			b.altHovered = false
		}
		b.Refresh()
	}
}

func (b *ideButton) MouseDown(*desktop.MouseEvent) { b.setPressed(true) }
func (b *ideButton) MouseUp(*desktop.MouseEvent)   { b.setPressed(false) }

func (b *ideButton) setPressed(v bool) {
	if b.disabled {
		v = false
	}
	if b.state.setPressed(v) {
		b.Refresh()
	}
}

func (b *ideButton) setDisabled(v bool) {
	if b.disabled == v {
		return
	}
	b.disabled = v
	if v {
		b.cancelInteraction()
	}
	b.Refresh()
}

func (b *ideButton) FocusGained() {
	if b.disabled {
		b.cancelInteraction()
		return
	}
	if b.state.setFocused(true) {
		b.Refresh()
	}
	if b.onFocusChanged != nil {
		b.onFocusChanged(true)
	}
}

func (b *ideButton) FocusLost() {
	if b.state.setFocused(false) {
		b.Refresh()
	}
	if b.onFocusChanged != nil {
		b.onFocusChanged(false)
	}
}

func (b *ideButton) TypedRune(rune) {}

func (b *ideButton) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name != fyne.KeySpace && ev.Name != fyne.KeyReturn && ev.Name != fyne.KeyEnter {
		return
	}
	if b.disabled || !b.Visible() || (b.keyboardGuard != nil && !b.keyboardGuard()) {
		b.cancelInteraction()
		return
	}
	keyboardActivate(&b.state, b.Refresh, func() { b.Tapped(nil) })
}

func (b *ideButton) cancelInteraction() {
	unfocusCanvasObjects(b)
	b.altHovered = false
	if b.state.clear() {
		b.Refresh()
	}
	if b.onFocusChanged != nil {
		b.onFocusChanged(false)
	}
}

// setAltHovered records whether the pointer is over the change corner. The
// parent row hit-tests it, the same way it does the other chips.
func (b *ideButton) setAltHovered(h bool) {
	if b.altHovered != h {
		b.altHovered = h
		b.Refresh()
	}
}

// currentTip is what the row should show for wherever the pointer is.
func (b *ideButton) currentTip() string {
	if b.altHovered {
		return b.altTip
	}
	return b.tip
}

func (b *ideButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(b.pal.openBtnBg)
	bg.CornerRadius = radiusSm
	img := canvas.NewImageFromResource(b.res)
	img.FillMode = canvas.ImageFillContain

	chevBg := canvas.NewCircle(b.pal.gradTop)
	chev := newGlyph(glyphChevron, b.pal.muted, ideChev, 2.6)

	r := &ideButtonRenderer{b: b, bg: bg, img: img, chevBg: chevBg, chev: chev,
		objects: []fyne.CanvasObject{bg, img, chevBg, chev}}
	r.Refresh()
	return r
}

type ideButtonRenderer struct {
	b       *ideButton
	bg      *canvas.Rectangle
	img     *canvas.Image
	chevBg  *canvas.Circle
	chev    *glyph
	objects []fyne.CanvasObject
}

func (r *ideButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(actionBox, actionBox) }

func (r *ideButtonRenderer) Layout(size fyne.Size) {
	surface := float32(actionBox)
	icon := float32(actionIcon + 2) // the app icon reads better slightly larger
	if r.b.compact {
		surface = rowActionSurface
		icon = rowActionIcon
	}
	surfaceX := (size.Width - surface) / 2
	surfaceY := (size.Height - surface) / 2
	r.bg.Move(fyne.NewPos(surfaceX, surfaceY))
	r.bg.Resize(fyne.NewSize(surface, surface))
	iconX := (size.Width - icon) / 2
	iconY := (size.Height - icon) / 2
	r.img.Move(fyne.NewPos(iconX, iconY))
	r.img.Resize(fyne.NewSize(icon, icon))

	// The change affordance sits in the bottom-right corner, on a disc of the
	// popover's own background so it stays legible over any app icon.
	d := float32(ideAltZone)
	r.chevBg.Move(fyne.NewPos(size.Width-d, size.Height-d))
	r.chevBg.Resize(fyne.NewSize(d, d))
	r.chev.Move(fyne.NewPos(size.Width-d+(d-ideChev)/2, size.Height-d+(d-ideChev)/2))
	r.chev.Resize(fyne.NewSize(ideChev, ideChev))
}

func (r *ideButtonRenderer) Refresh() {
	switch {
	case r.b.disabled:
		r.bg.FillColor = color.Transparent
	case r.b.state.pressed:
		r.bg.FillColor = r.b.pal.btnHover
	case r.b.altHovered:
		r.bg.FillColor = r.b.pal.openBtnBg
	case r.b.state.active():
		r.bg.FillColor = r.b.pal.btnHover
	default:
		r.bg.FillColor = r.b.pal.openBtnBg
	}
	// A missing editor is shown faded rather than hidden, so the reason the chip
	// no longer opens anything is visible instead of mysterious.
	r.img.Resource = r.b.res
	if r.b.disabled {
		r.img.Translucency = 0.6
	} else if r.b.missing {
		r.img.Translucency = 0.55
	} else {
		r.img.Translucency = 0
	}
	// The chevron only appears while the chip is hovered — at rest the chip is
	// just the editor's icon.
	if r.b.state.hovered && !r.b.disabled {
		r.chevBg.Show()
		r.chev.Show()
		r.chevBg.FillColor = r.b.pal.gradTop
		if r.b.altHovered {
			r.chevBg.FillColor = r.b.pal.accent
		}
	} else {
		r.chevBg.Hide()
		r.chev.Hide()
	}
	if r.b.disabled {
		r.bg.StrokeWidth = 0
	} else if r.b.state.pressed {
		r.bg.StrokeColor = r.b.pal.accent
		r.bg.StrokeWidth = 2
	} else if r.b.state.focused {
		r.bg.StrokeColor = r.b.pal.accent
		r.bg.StrokeWidth = hairlineW
	} else {
		r.bg.StrokeWidth = 0
	}
	r.bg.Refresh()
	r.img.Refresh()
	r.chevBg.Refresh()
	r.chev.Refresh()
}

func (r *ideButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *ideButtonRenderer) Destroy()                     {}

// ideIconResource turns an editor's own icon into something Fyne can draw,
// falling back to a themed glyph when the editor has no readable icon — a bare
// shell command, or an application with an old-format icon.
func ideIconResource(i ide.IDE) fyne.Resource {
	if png, err := i.IconPNG(); err == nil && len(png) > 0 {
		return fyne.NewStaticResource("ide-"+i.Name+".png", png)
	}
	return theme.NewColoredResource(theme.ComputerIcon(), colorNameMuted)
}
