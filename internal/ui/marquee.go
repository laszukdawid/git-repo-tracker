package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// marqueeText is a single line of text that truncates to fit its width, and —
// when made active (the row is hovered) and the text overflows — scrolls
// horizontally by sliding a substring window. Because it always displays a
// fitting substring, no clipping is required.
type marqueeText struct {
	widget.BaseWidget
	runes []rune
	style fyne.TextStyle
	size  float32

	text    *canvas.Text
	width   float32
	visible int // runes that fit at the current width
	active  bool
	anim    *fyne.Animation
}

func newMarquee(s string, col color.Color, size float32, bold, mono bool) *marqueeText {
	m := &marqueeText{runes: []rune(s), size: size, style: fyne.TextStyle{Bold: bold, Monospace: mono}}
	m.text = canvas.NewText(s, col)
	m.text.TextSize = size
	m.text.TextStyle = m.style
	m.ExtendBaseWidget(m)
	return m
}

func (m *marqueeText) overflow() bool { return m.visible < len(m.runes) }

// SetActive toggles scrolling. It only scrolls if the text actually overflows.
func (m *marqueeText) SetActive(b bool) {
	if m.active == b {
		return
	}
	m.active = b
	m.refreshDisplay()
}

func (m *marqueeText) Resize(s fyne.Size) {
	m.BaseWidget.Resize(s)
	if s.Width == m.width {
		return
	}
	m.width = s.Width
	m.visible = m.fitCount(s.Width)
	m.refreshDisplay()
}

// fitCount returns the largest rune prefix whose rendered width is <= w.
func (m *marqueeText) fitCount(w float32) int {
	if len(m.runes) == 0 || w <= 0 {
		return 0
	}
	lo, hi := 0, len(m.runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fyne.MeasureText(string(m.runes[:mid]), m.size, m.style).Width <= w {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func (m *marqueeText) refreshDisplay() {
	m.stopAnim()
	if !m.overflow() {
		m.setText(string(m.runes))
		return
	}
	if m.active && m.visible > 0 {
		m.startAnim()
		return
	}
	// Idle, or too narrow to fit even one rune: show a truncated, fitting prefix.
	v := m.visible
	if v < 1 {
		v = 1
	}
	m.setText(string(m.runes[:v-1]) + "…")
}

func (m *marqueeText) startAnim() {
	maxOff := len(m.runes) - m.visible
	if maxOff <= 0 {
		return
	}
	// ~160ms per hidden rune, plus a pause at each end so the start/end is readable.
	dur := time.Duration(maxOff)*160*time.Millisecond + 800*time.Millisecond
	m.anim = fyne.NewAnimation(dur, func(p float32) {
		off := int(p*float32(maxOff) + 0.5)
		if off < 0 {
			off = 0
		} else if off > maxOff {
			off = maxOff
		}
		m.setText(string(m.runes[off : off+m.visible]))
	})
	m.anim.RepeatCount = fyne.AnimationRepeatForever
	m.anim.AutoReverse = true
	m.anim.Curve = fyne.AnimationLinear
	m.anim.Start()
}

func (m *marqueeText) stopAnim() {
	if m.anim != nil {
		m.anim.Stop()
		m.anim = nil
	}
}

func (m *marqueeText) setText(s string) {
	if m.text.Text != s {
		m.text.Text = s
		m.text.Refresh()
	}
}

func (m *marqueeText) CreateRenderer() fyne.WidgetRenderer { return &marqueeRenderer{m: m} }

type marqueeRenderer struct{ m *marqueeText }

func (r *marqueeRenderer) Layout(size fyne.Size) {
	r.m.text.Move(fyne.NewPos(0, 0))
	r.m.text.Resize(size)
}
func (r *marqueeRenderer) MinSize() fyne.Size {
	return fyne.NewSize(16, fyne.MeasureText("Ag", r.m.size, r.m.style).Height)
}
func (r *marqueeRenderer) Refresh()                     { r.m.text.Refresh() }
func (r *marqueeRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.m.text} }
func (r *marqueeRenderer) Destroy()                     { r.m.stopAnim() }
