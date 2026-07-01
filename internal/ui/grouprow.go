package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

// Group-header metrics (option 3a). The section header is denser than a repo row:
// a small disclosure chevron, the scan-root path in mono, a faint repo count, and
// a right-aligned amber "N behind" pill.
const (
	groupHPad    = 14
	groupTopPad  = 10
	groupBotPad  = 8
	groupPathGap = 8
	groupChevron = 13
	groupPathSz  = 11
	groupCountSz = 11
	pillTextSz   = 10.5
	pillHPad     = 8
	pillVPad     = 2
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

	onToggle func(string)
}

func newGroupHeaderRow(pal palette, onToggle func(string)) *groupHeaderRow {
	h := &groupHeaderRow{pal: pal, onToggle: onToggle}
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
	h.root = root
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

func (r *groupHeaderRenderer) MinSize() fyne.Size {
	h := groupTopPad + r.contentHeight() + groupBotPad
	w := float32(groupHPad + groupChevron + groupPathGap + groupHPad)
	w += r.h.path.MinSize().Width + groupPathGap + r.h.repos.MinSize().Width
	return fyne.NewSize(w, h)
}

func (r *groupHeaderRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	ch := r.contentHeight()
	top := float32(groupTopPad)

	r.h.glyphSlot.Move(fyne.NewPos(groupHPad, top+(ch-groupChevron)/2))
	r.h.glyphSlot.Resize(fyne.NewSize(groupChevron, groupChevron))

	x := float32(groupHPad + groupChevron + groupPathGap)
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
		r.h.pillBg.CornerRadius = pillH / 2
		r.h.pillBg.Move(fyne.NewPos(pillX, pillY))
		r.h.pillBg.Resize(fyne.NewSize(pillW, pillH))
		r.h.pillTxt.Move(fyne.NewPos(pillX+pillHPad, pillY+pillVPad))
		r.h.pillTxt.Resize(ts)
	}
}

func (r *groupHeaderRenderer) Refresh() {
	r.bg.FillColor = r.h.pal.groupHeaderBg
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
	repo          *repoRow
	header        *groupHeaderRow
	showingHeader bool
}

func newPopoverRow(tips *tooltipLayer, pal palette, onToggleGroup func(string)) *popoverRow {
	p := &popoverRow{
		repo:   newRepoRow(tips, pal),
		header: newGroupHeaderRow(pal, onToggleGroup),
	}
	p.repo.Hide()
	p.ExtendBaseWidget(p)
	return p
}

func (p *popoverRow) Configure(it popoverItem, expanded, pulling bool, detail *monitor.Details,
	onExpand, onPull, onOpen func(monitor.RepoState)) {
	if it.header {
		p.repo.deactivate() // stop a recycled repo's marquees/spinner before hiding it
		p.repo.Hide()
		p.header.Show()
		p.header.Configure(it.root, it.count, it.behind, it.collapsed)
		p.showingHeader = true
	} else {
		p.header.Hide()
		p.repo.Show()
		p.repo.Configure(it.repo, expanded, pulling, detail, onExpand, onPull, onOpen)
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
	return &popoverRowRenderer{p: p, objects: []fyne.CanvasObject{p.header, p.repo}}
}

type popoverRowRenderer struct {
	p       *popoverRow
	objects []fyne.CanvasObject
}

func (r *popoverRowRenderer) Layout(size fyne.Size) {
	a := r.p.active()
	a.Move(fyne.NewPos(0, 0))
	a.Resize(size)
}
func (r *popoverRowRenderer) MinSize() fyne.Size           { return r.p.active().MinSize() }
func (r *popoverRowRenderer) Refresh()                     { r.p.active().Refresh() }
func (r *popoverRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *popoverRowRenderer) Destroy()                     {}
