package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
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
	state     interactionState
	pal       palette
	onChanged func(bool)
	tips      *tooltipLayer // optional; set with tip to show a hover tooltip
	tip       string
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

func (t *toggleSwitch) MouseIn(*desktop.MouseEvent) {
	t.setHovered(true)
	if t.tips != nil && t.tip != "" {
		t.tips.show(t.tip, t)
	}
}
func (t *toggleSwitch) MouseMoved(*desktop.MouseEvent) {}
func (t *toggleSwitch) MouseOut() {
	t.setHovered(false)
	t.setPressed(false)
	if t.tips != nil {
		t.tips.hide()
	}
}

func (t *toggleSwitch) setHovered(h bool) {
	if t.state.setHovered(h) {
		t.Refresh()
	}
}

func (t *toggleSwitch) MouseDown(*desktop.MouseEvent) { t.setPressed(true) }
func (t *toggleSwitch) MouseUp(*desktop.MouseEvent)   { t.setPressed(false) }

func (t *toggleSwitch) setPressed(v bool) {
	if t.state.setPressed(v) {
		t.Refresh()
	}
}

func (t *toggleSwitch) FocusGained() {
	if t.state.setFocused(true) {
		t.Refresh()
	}
}

func (t *toggleSwitch) FocusLost() {
	if t.state.setFocused(false) {
		t.Refresh()
	}
}

func (t *toggleSwitch) TypedRune(rune) {}

func (t *toggleSwitch) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn || ev.Name == fyne.KeyEnter {
		keyboardActivate(&t.state, t.Refresh, func() { t.Tapped(nil) })
	}
}

func (t *toggleSwitch) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(t.pal.toggleOffBg)
	track.CornerRadius = canvas.RadiusMaximum
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

func (r *toggleRenderer) MinSize() fyne.Size { return fyne.NewSize(toggleW, actionBox) }

func (r *toggleRenderer) Layout(size fyne.Size) {
	trackY := (size.Height - toggleH) / 2
	r.track.Resize(fyne.NewSize(size.Width, toggleH))
	r.track.Move(fyne.NewPos(0, trackY))
	knobSz := float32(toggleKnob)
	x := float32(togglePad)
	if r.t.on {
		x = size.Width - togglePad - knobSz
	}
	if r.t.state.pressed {
		r.track.StrokeColor = theme.Color(theme.ColorNamePrimary)
		r.track.StrokeWidth = 2
	} else if r.t.state.focused {
		r.track.StrokeColor = theme.Color(theme.ColorNamePrimary)
		r.track.StrokeWidth = hairlineW
	} else if r.t.state.hovered {
		r.track.StrokeColor = r.t.pal.rowName
		r.track.StrokeWidth = hairlineW
	} else {
		r.track.StrokeWidth = 0
	}
	r.knob.Resize(fyne.NewSize(knobSz, knobSz))
	r.knob.Move(fyne.NewPos(x, trackY+togglePad))
}

func (r *toggleRenderer) Refresh() {
	if r.t.on {
		r.track.FillColor = r.t.pal.accent  // accent fill when on
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
