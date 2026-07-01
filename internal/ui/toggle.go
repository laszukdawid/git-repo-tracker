package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Pill-switch metrics (option 1e). A compact iOS-style toggle: a rounded track
// with a knob that slides to the right when on.
const (
	toggleW    = 34
	toggleH    = 20
	toggleKnob = 16
	togglePad  = 2
)

// toggleSwitch is a small pill toggle in the segChip spirit: a rounded track that
// fills accent-blue when on (knob right) and a muted grey when off (knob left). It
// replaces widget.Check for the auto-fetch and launch-at-login controls so those
// read as switches, matching the settings mock.
type toggleSwitch struct {
	widget.BaseWidget
	on        bool
	hovered   bool
	pal       palette
	onChanged func(bool)
}

func newToggleSwitch(on bool, pal palette, onChanged func(bool)) *toggleSwitch {
	t := &toggleSwitch{on: on, pal: pal, onChanged: onChanged}
	t.ExtendBaseWidget(t)
	return t
}

func (t *toggleSwitch) Tapped(*fyne.PointEvent) {
	t.on = !t.on
	t.Refresh()
	if t.onChanged != nil {
		t.onChanged(t.on)
	}
}

func (t *toggleSwitch) MouseIn(*desktop.MouseEvent)    { t.setHovered(true) }
func (t *toggleSwitch) MouseMoved(*desktop.MouseEvent) {}
func (t *toggleSwitch) MouseOut()                      { t.setHovered(false) }

func (t *toggleSwitch) setHovered(h bool) {
	if t.hovered != h {
		t.hovered = h
		t.Refresh()
	}
}

func (t *toggleSwitch) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(t.pal.toggleOffBg)
	track.CornerRadius = toggleH / 2
	knob := canvas.NewCircle(t.pal.toggleKnobOff)
	r := &toggleRenderer{t: t, track: track, knob: knob, objects: []fyne.CanvasObject{track, knob}}
	r.Refresh()
	return r
}

type toggleRenderer struct {
	t       *toggleSwitch
	track   *canvas.Rectangle
	knob    *canvas.Circle
	objects []fyne.CanvasObject
}

func (r *toggleRenderer) MinSize() fyne.Size { return fyne.NewSize(toggleW, toggleH) }

func (r *toggleRenderer) Layout(size fyne.Size) {
	r.track.Resize(size)
	r.track.Move(fyne.NewPos(0, 0))
	knobSz := size.Height - togglePad*2
	x := float32(togglePad)
	if r.t.on {
		x = size.Width - togglePad - knobSz
	}
	r.knob.Resize(fyne.NewSize(knobSz, knobSz))
	r.knob.Move(fyne.NewPos(x, togglePad))
}

func (r *toggleRenderer) Refresh() {
	if r.t.on {
		r.track.FillColor = r.t.pal.rowName // accent fill when on
		r.knob.FillColor = chipSelectedText // white knob
	} else {
		r.track.FillColor = r.t.pal.toggleOffBg
		r.knob.FillColor = r.t.pal.toggleKnobOff
	}
	r.Layout(r.t.Size()) // reposition the knob for the new on/off state
	r.track.Refresh()
	r.knob.Refresh()
}

func (r *toggleRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *toggleRenderer) Destroy()                     {}
