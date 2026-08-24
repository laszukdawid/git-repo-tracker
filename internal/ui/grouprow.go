package ui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// Group-header metrics (option 3a). The section header is denser than a repo row:
// a small disclosure chevron, the scan-root path in mono, a faint repo count, and
// a right-aligned amber "N behind" pill.
// The chevron occupies the same gutter as a repo row's status glyph, and the
// root path starts at the same nameX — so headers and repo names share one left
// rail. Previously the header text began 8px to the left of the names beneath
// it, which is a large part of why the list looked misaligned.
const (
	groupHPad    = rowPadX
	groupTopPad  = spaceSm
	groupBotPad  = spaceXs
	groupPathGap = glyphGap
	groupChevron = 13
	groupPathSz  = textSm
	groupCountSz = textXs
	pillTextSz   = textXs
	pillHPad     = spaceSm
	pillVPad     = space3xs
)

// popoverItem is one entry in the grouped popover list: either a scan-root section
// header or a repo row beneath it. widget.List needs a single item template, so
// popoverRow renders whichever kind this describes.
type popoverItem struct {
	header    bool
	root      string // group: the scan-root display path (e.g. ~/projects)
	count     int    // group: repos in the section
	behind    int    // group: repos behind in the section
	collapsed bool   // group: whether the section is folded
	repo      monitor.RepoState
}

// groupHeaderRow is a collapsible scan-root section header.
type groupHeaderRow struct {
	widget.BaseWidget
	pal       palette
	root      string
	count     int
	behind    int
	collapsed bool

	glyphSlot *fyne.Container
	path      *canvas.Text
	repos     *canvas.Text
	pillBg    *canvas.Rectangle
	pillTxt   *canvas.Text

	toggle        func(string)
	onToggle      func(string)
	state         interactionState
	keyboardGuard func() bool
}

func newGroupHeaderRow(pal palette, onToggle func(string)) *groupHeaderRow {
	h := &groupHeaderRow{pal: pal, toggle: onToggle, onToggle: onToggle}
	h.glyphSlot = container.NewStack()
	h.path = canvas.NewText("", pal.detailKey)
	h.path.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	h.path.TextSize = groupPathSz
	h.repos = canvas.NewText("", pal.faint)
	h.repos.TextStyle = fyne.TextStyle{Monospace: true}
	h.repos.TextSize = groupCountSz
	h.pillBg = canvas.NewRectangle(pal.pillBehindBg)
	h.pillTxt = canvas.NewText("", pal.statusBehind)
	h.pillTxt.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	h.pillTxt.TextSize = pillTextSz
	h.ExtendBaseWidget(h)
	return h
}

func (h *groupHeaderRow) Configure(root string, count, behind int, collapsed bool) {
	if h.root != "" && h.root != root {
		h.deactivate()
	}
	h.root = root
	h.onToggle = h.toggle
	h.count = count
	h.behind = behind
	h.collapsed = collapsed

	kind := glyphChevron
	if collapsed {
		kind = glyphChevronRight
	}
	h.glyphSlot.RemoveAll()
	h.glyphSlot.Add(newGlyph(kind, h.pal.faint, groupChevron, 1.7))

	h.path.Text = root
	h.repos.Text = fmt.Sprintf("%d repos", count)
	if behind > 0 {
		h.pillTxt.Text = fmt.Sprintf("%d behind", behind)
		h.pillBg.Show()
		h.pillTxt.Show()
	} else {
		h.pillTxt.Text = ""
		h.pillBg.Hide()
		h.pillTxt.Hide()
	}
	h.Refresh()
}

func (h *groupHeaderRow) Tapped(*fyne.PointEvent) {
	if h.onToggle != nil {
		h.onToggle(h.root)
	}
}

func (h *groupHeaderRow) MouseDown(*desktop.MouseEvent)  { h.setPressed(true) }
func (h *groupHeaderRow) MouseUp(*desktop.MouseEvent)    { h.setPressed(false) }
func (h *groupHeaderRow) MouseIn(*desktop.MouseEvent)    { h.setHovered(true) }
func (h *groupHeaderRow) MouseMoved(*desktop.MouseEvent) {}
func (h *groupHeaderRow) MouseOut() {
	h.setHovered(false)
	h.setPressed(false)
}

func (h *groupHeaderRow) setHovered(v bool) {
	if h.state.setHovered(v) {
		h.Refresh()
	}
}

func (h *groupHeaderRow) setPressed(v bool) {
	if h.state.setPressed(v) {
		h.Refresh()
	}
}

func (h *groupHeaderRow) FocusGained() {
	if h.state.setFocused(true) {
		h.Refresh()
	}
}

func (h *groupHeaderRow) FocusLost() {
	if h.state.setFocused(false) {
		h.Refresh()
	}
}

func (h *groupHeaderRow) TypedRune(rune) {}

func (h *groupHeaderRow) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name != fyne.KeySpace && ev.Name != fyne.KeyReturn && ev.Name != fyne.KeyEnter {
		return
	}
	if !h.acceptsKeyboard() {
		return
	}
	keyboardActivate(&h.state, h.Refresh, func() { h.Tapped(nil) })
}

func (h *groupHeaderRow) setKeyboardGuard(guard func() bool) { h.keyboardGuard = guard }

func (h *groupHeaderRow) acceptsKeyboard() bool {
	if !h.Visible() || (h.keyboardGuard != nil && !h.keyboardGuard()) {
		h.deactivate()
		return false
	}
	return true
}

func (h *groupHeaderRow) deactivate() {
	unfocusCanvasObjects(h)
	h.onToggle = nil
	if h.state.clear() {
		h.Refresh()
	}
}

func (h *groupHeaderRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(h.pal.groupHeaderBg)
	return &groupHeaderRenderer{
		h:       h,
		bg:      bg,
		objects: []fyne.CanvasObject{bg, h.glyphSlot, h.path, h.repos, h.pillBg, h.pillTxt},
	}
}

type groupHeaderRenderer struct {
	h       *groupHeaderRow
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *groupHeaderRenderer) contentHeight() float32 {
	ph := r.h.path.MinSize().Height
	if ph < groupChevron {
		ph = groupChevron
	}
	return ph
}

// glyphGutter is the chevron's slot: the same width as a repo row's status-glyph
// column, so header text and repo names line up on one rail.
const glyphGutter = glyphColW

func (r *groupHeaderRenderer) MinSize() fyne.Size {
	h := groupTopPad + r.contentHeight() + groupBotPad
	w := float32(nameX + groupHPad)
	w += r.h.path.MinSize().Width + groupPathGap + r.h.repos.MinSize().Width
	return fyne.NewSize(w, h)
}

func (r *groupHeaderRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	ch := r.contentHeight()
	top := float32(groupTopPad)

	// The chevron sits in the same disclosure column as a repo row's, so the two
	// marks line up down the whole list.
	r.h.glyphSlot.Move(fyne.NewPos(groupHPad+(chevColW-groupChevron)/2, top+(ch-groupChevron)/2))
	r.h.glyphSlot.Resize(fyne.NewSize(groupChevron, groupChevron))

	x := float32(nameX)
	ps := r.h.path.MinSize()
	r.h.path.Move(fyne.NewPos(x, top+(ch-ps.Height)/2))
	r.h.path.Resize(ps)
	x += ps.Width + groupPathGap

	rs := r.h.repos.MinSize()
	r.h.repos.Move(fyne.NewPos(x, top+(ch-rs.Height)/2))
	r.h.repos.Resize(rs)

	if r.h.pillTxt.Visible() {
		ts := r.h.pillTxt.MinSize()
		pillW := ts.Width + pillHPad*2
		pillH := ts.Height + pillVPad*2
		pillX := size.Width - groupHPad - pillW
		pillY := top + (ch-pillH)/2
		r.h.pillBg.CornerRadius = canvas.RadiusMaximum
		r.h.pillBg.Move(fyne.NewPos(pillX, pillY))
		r.h.pillBg.Resize(fyne.NewSize(pillW, pillH))
		r.h.pillTxt.Move(fyne.NewPos(pillX+pillHPad, pillY+pillVPad))
		r.h.pillTxt.Resize(ts)
	}
}

func (r *groupHeaderRenderer) Refresh() {
	switch {
	case r.h.state.pressed:
		r.bg.FillColor = r.h.pal.btnHover
	case r.h.state.active():
		r.bg.FillColor = r.h.pal.rowHover
	default:
		r.bg.FillColor = r.h.pal.groupHeaderBg
	}
	if r.h.state.focused {
		r.bg.StrokeColor = r.h.pal.accent
		r.bg.StrokeWidth = hairlineW
	} else {
		r.bg.StrokeWidth = 0
	}
	r.bg.Refresh()
	r.h.path.Refresh()
	r.h.repos.Refresh()
	r.h.pillBg.FillColor = r.h.pal.pillBehindBg
	r.h.pillBg.Refresh()
	r.h.pillTxt.Refresh()
}

func (r *groupHeaderRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *groupHeaderRenderer) Destroy()                     {}

// popoverRow is the single widget.List item template: it holds both a group header
// and a repo row and shows whichever the bound popoverItem describes. Both children
// are real widgets, so each still receives its own tap/hover events directly.
type popoverRow struct {
	widget.BaseWidget
	repo            *repoRow
	header          *groupHeaderRow
	attachmentProbe *canvas.Rectangle
	showingHeader   bool
	keyboardGuard   func(*popoverRow) bool
}

func newPopoverRow(tips *tooltipLayer, pal palette, onToggleGroup func(string)) *popoverRow {
	p := &popoverRow{
		repo:            newRepoRow(tips, pal),
		header:          newGroupHeaderRow(pal, onToggleGroup),
		attachmentProbe: canvas.NewRectangle(color.Transparent),
	}
	p.repo.Hide()
	p.repo.setKeyboardGuard(p.acceptsKeyboard)
	p.header.setKeyboardGuard(p.acceptsKeyboard)
	p.ExtendBaseWidget(p)
	return p
}

func (p *popoverRow) setKeyboardGuard(guard func(*popoverRow) bool) { p.keyboardGuard = guard }

func (p *popoverRow) acceptsKeyboard() bool {
	return p.keyboardGuard == nil || p.keyboardGuard(p)
}

func (p *popoverRow) Configure(it popoverItem, st rowState, act rowActions) {
	if it.header {
		p.repo.deactivate() // stop a recycled repo's marquees/spinner before hiding it
		p.repo.Hide()
		p.header.Show()
		p.header.Configure(it.root, it.count, it.behind, it.collapsed)
		p.showingHeader = true
	} else {
		p.header.deactivate()
		p.header.Hide()
		p.repo.Show()
		p.repo.Configure(it.repo, st, act)
		p.showingHeader = false
	}
	p.Refresh()
}

func (p *popoverRow) active() fyne.CanvasObject {
	if p.showingHeader {
		return p.header
	}
	return p.repo
}

func (p *popoverRow) CreateRenderer() fyne.WidgetRenderer {
	return &popoverRowRenderer{p: p, objects: []fyne.CanvasObject{p.header, p.repo, p.attachmentProbe}}
}

type popoverRowRenderer struct {
	p       *popoverRow
	objects []fyne.CanvasObject
}

func (r *popoverRowRenderer) Layout(size fyne.Size) {
	a := r.p.active()
	bridge := theme.Padding() / 2
	a.Move(fyne.NewPos(0, -bridge))
	a.Resize(fyne.NewSize(size.Width, size.Height+bridge*2))
	r.p.attachmentProbe.Move(fyne.NewPos(1, 1))
	r.p.attachmentProbe.Resize(fyne.NewSize(1, 1))
}
func (r *popoverRowRenderer) MinSize() fyne.Size           { return r.p.active().MinSize() }
func (r *popoverRowRenderer) Refresh()                     { r.p.active().Refresh() }
func (r *popoverRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *popoverRowRenderer) Destroy() {
	r.p.repo.deactivate()
	r.p.header.deactivate()
}
