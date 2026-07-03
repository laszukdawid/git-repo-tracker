package ui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// marqueeSeparator sits between the end of an overflowing line and its repeat: a
// little breathing space, a central dot to mark the wrap, then space again — so a
// looping line reads "…the whole message   ·   the whole message…" rather than
// snapping abruptly back to the start.
const marqueeSeparator = "   ·   "

// marqueeSpeed is the scroll rate in points per second. The line is translated by a
// continuous sub-pixel offset each frame (not stepped per character), so motion is
// smooth and easy to follow.
const marqueeSpeed = 42.0

// marqueeText is a single line of text that scrolls horizontally when it's wider
// than its box. It renders the text (plus a separator, doubled) as one object and
// slides it by a continuous pixel offset, clipped to its own bounds — so the motion
// is smooth rather than character-stepped. Interactions: hovering the text pauses
// the scroll so it can be read, and it can be dragged left/right to scroll it by
// hand.
type marqueeText struct {
	widget.BaseWidget
	full    string // source text
	doubled string // (full + separator) x2, for seamless wrap while scrolling
	style   fyne.TextStyle
	size    float32

	text     *canvas.Text
	width    float32 // widget width
	textW    float32 // rendered width of full
	loopW    float32 // rendered width of full + separator (one loop)
	height   float32 // rendered text height
	scroll   bool    // auto-scroll wanted (the detail panel is shown)
	hovered  bool    // cursor over the text → pause
	dragging bool    // being dragged → pause
	px       float32 // scroll offset in points (into the loop; wraps)
	anim     *fyne.Animation

	// onHover lets the parent row stay "hovered" while the pointer is on a detail
	// line; without it this Hoverable line steals hover and hides the row's chips.
	onHover func(bool)
}

func newMarquee(s string, col color.Color, size float32, bold, mono bool) *marqueeText {
	style := fyne.TextStyle{Bold: bold, Monospace: mono}
	loop := s + marqueeSeparator
	m := &marqueeText{
		full:    s,
		doubled: loop + loop,
		size:    size,
		style:   style,
		textW:   fyne.MeasureText(s, size, style).Width,
		loopW:   fyne.MeasureText(loop, size, style).Width,
		height:  fyne.MeasureText("Ag", size, style).Height,
	}
	m.text = canvas.NewText(s, col)
	m.text.TextSize = size
	m.text.TextStyle = style
	m.ExtendBaseWidget(m)
	return m
}

func (m *marqueeText) overflow() bool { return m.width > 0 && m.textW > m.width+0.5 }

// SetActive marks whether the line should auto-scroll (the detail panel is shown).
// It only actually scrolls when the text overflows and isn't paused.
func (m *marqueeText) SetActive(b bool) {
	if m.scroll == b {
		return
	}
	m.scroll = b
	m.update()
}

func (m *marqueeText) Resize(s fyne.Size) {
	m.BaseWidget.Resize(s)
	if s.Width == m.width {
		return
	}
	m.width = s.Width
	if m.px > m.maxDragPx() {
		m.px = m.maxDragPx()
	}
	m.update()
}

// update reconciles the animation with the current state: scroll only when wanted,
// overflowing, and not paused (hover/drag); otherwise freeze and render statically.
func (m *marqueeText) update() {
	if m.scroll && m.overflow() && !m.hovered && !m.dragging {
		if m.anim == nil {
			m.render() // ensure the doubled text is in place before the first tick
			m.startAnim()
		}
		return
	}
	m.stopAnim()
	m.render()
}

// render sets the text content and position for the current offset: the full text
// (left-aligned) when it fits, else the doubled loop slid left by px.
func (m *marqueeText) render() {
	if !m.overflow() {
		m.setText(m.full)
		m.moveText(0)
		return
	}
	m.setText(m.doubled)
	m.moveText(-m.wrappedPx())
}

// wrappedPx is the current offset folded into a single loop, so the doubled text
// always has a copy covering the visible window.
func (m *marqueeText) wrappedPx() float32 {
	if m.loopW <= 0 {
		return 0
	}
	x := float32(math.Mod(float64(m.px), float64(m.loopW)))
	if x < 0 {
		x += m.loopW
	}
	return x
}

func (m *marqueeText) startAnim() {
	m.stopAnim()
	if m.loopW <= 0 {
		return
	}
	base := m.px
	dur := time.Duration(float64(m.loopW) / marqueeSpeed * float64(time.Second))
	if dur < 800*time.Millisecond {
		dur = 800 * time.Millisecond
	}
	m.anim = fyne.NewAnimation(dur, func(p float32) {
		m.px = base + p*m.loopW
		m.moveText(-m.wrappedPx())
	})
	m.anim.RepeatCount = fyne.AnimationRepeatForever
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

func (m *marqueeText) moveText(x float32) {
	if m.text.Position().X != x {
		m.text.Move(fyne.NewPos(x, 0))
		canvas.Refresh(m.text)
	}
}

// maxDragPx is the furthest a manual drag may scroll: enough to bring the end of the
// text to the right edge, but not into the separator/repeat (a drag reads the real
// content, not the looping filler).
func (m *marqueeText) maxDragPx() float32 {
	if d := m.textW - m.width; d > 0 {
		return d
	}
	return 0
}

// --- pause on hover, and a resize-cursor hint that the line is draggable ---

func (m *marqueeText) MouseIn(*desktop.MouseEvent) {
	m.hovered = true
	if m.onHover != nil {
		m.onHover(true)
	}
	m.update()
}
func (m *marqueeText) MouseMoved(*desktop.MouseEvent) {}
func (m *marqueeText) MouseOut() {
	m.hovered = false
	if m.onHover != nil {
		m.onHover(false)
	}
	m.update()
}

func (m *marqueeText) Cursor() desktop.Cursor {
	if m.overflow() {
		return desktop.HResizeCursor
	}
	return desktop.DefaultCursor
}

// --- drag to scroll manually ---

// Dragged scrolls the line by the horizontal drag: dragging right reveals earlier
// text, dragging left reveals later text. Dragging (like hovering) pauses the auto
// scroll; the offset is clamped to the text itself so a drag reads the real content.
func (m *marqueeText) Dragged(e *fyne.DragEvent) {
	if !m.overflow() {
		return
	}
	m.dragging = true
	m.stopAnim()
	m.setText(m.doubled)
	m.px -= e.Dragged.DX
	if m.px < 0 {
		m.px = 0
	}
	if max := m.maxDragPx(); m.px > max {
		m.px = max
	}
	m.moveText(-m.px)
}

func (m *marqueeText) DragEnd() {
	m.dragging = false
	m.update() // resume scrolling unless still hovered
}

func (m *marqueeText) CreateRenderer() fyne.WidgetRenderer { return &marqueeRenderer{m: m} }

type marqueeRenderer struct{ m *marqueeText }

// IsClip marks this renderer so Fyne clips its content to the widget's bounds — the
// scrolling text overflows the box on both sides and must not bleed into the row.
func (r *marqueeRenderer) IsClip() {}

func (r *marqueeRenderer) Layout(size fyne.Size) {
	// Give the text room for its full (doubled) content; it's positioned by the
	// scroll offset, not this layout, and clipped to the widget by IsClip.
	w := r.m.loopW * 2
	if w < size.Width {
		w = size.Width
	}
	r.m.text.Resize(fyne.NewSize(w, size.Height))
	r.m.render()
}
func (r *marqueeRenderer) MinSize() fyne.Size           { return fyne.NewSize(16, r.m.height) }
func (r *marqueeRenderer) Refresh()                     { r.m.text.Refresh() }
func (r *marqueeRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.m.text} }
func (r *marqueeRenderer) Destroy()                     { r.m.stopAnim() }
