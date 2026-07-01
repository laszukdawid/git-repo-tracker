package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

// Row metrics for the main popover (option 3a). The row uses the design's literal
// spacing (14px sides / 9px top-bottom) rather than theme.Padding so it matches
// the mock; the list's inter-row separator still uses theme.Padding.
const (
	rowHPad      = 14 // row horizontal padding
	rowVPad      = 9  // row vertical padding
	glyphColW    = 18 // status-glyph column width
	glyphGap     = 11 // gap between the glyph column and the name
	branchGap    = 6  // gap between the repo name and its inline branch
	detailIndent = 43 // left indent of the expanded detail panel (aligns under name)
	nameSize     = 13.5
	branchSize   = 11
	countSize    = 12
	rightReserve = 66 // right-edge space kept for the count / two action chips
	actionBox    = 28 // hover action chip size
	actionRadius = 7
)

// iconButton is a minimal tappable icon chip with its own hover highlight. It is
// NOT Hoverable: the row below tracks the pointer itself (via MouseMoved) and
// toggles each button's highlight, so the icons never steal hover from the row (a
// widget.Button would, making the hover buttons vanish as you reach for them).
//
// The chip carries a resting background tint (the design reveals pull/open as
// tinted chips, not bare icons) that brightens on hover, and tints its icon to a
// named theme colour so pull reads accent-blue and open reads muted.
type iconButton struct {
	widget.BaseWidget
	res      fyne.Resource
	iconName fyne.ThemeColorName // icon tint; empty = themed foreground
	tip      string
	rest     color.Color // resting background
	hover    color.Color // background while hovered
	onTap    func()
	hovered  bool
}

func newIconButton(res fyne.Resource, iconName fyne.ThemeColorName, tip string, rest, hover color.Color, onTap func()) *iconButton {
	b := &iconButton{res: res, iconName: iconName, tip: tip, rest: rest, hover: hover, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *iconButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *iconButton) setHovered(h bool) {
	if b.hovered != h {
		b.hovered = h
		b.Refresh()
	}
}

func (b *iconButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(b.rest)
	bg.CornerRadius = actionRadius
	var res fyne.Resource
	if b.iconName != "" {
		res = theme.NewColoredResource(b.res, b.iconName)
	} else {
		res = theme.NewThemedResource(b.res)
	}
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	return &iconButtonRenderer{b: b, bg: bg, img: img, objects: []fyne.CanvasObject{bg, img}}
}

type iconButtonRenderer struct {
	b       *iconButton
	bg      *canvas.Rectangle
	img     *canvas.Image
	objects []fyne.CanvasObject
}

func (r *iconButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	inset := float32(6)
	r.img.Move(fyne.NewPos(inset, inset))
	r.img.Resize(fyne.NewSize(size.Width-inset*2, size.Height-inset*2))
}
func (r *iconButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(actionBox, actionBox) }
func (r *iconButtonRenderer) Refresh() {
	if r.b.hovered {
		r.bg.FillColor = r.b.hover
	} else {
		r.bg.FillColor = r.b.rest
	}
	r.bg.Refresh()
	r.img.Refresh()
}
func (r *iconButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *iconButtonRenderer) Destroy()                     {}

// repoRow is one repo entry in the grouped popover list (option 3a). Collapsed it
// shows a tinted status glyph, the repo name with its branch inline, and (when
// behind) a right-aligned "↓N". Clicking the body expands an inline detail panel
// indented under the name (the row grows via list.SetItemHeight). Hovering reveals
// pull / open action chips in place of the count.
type repoRow struct {
	widget.BaseWidget

	pal      palette
	repo     monitor.RepoState
	expanded bool
	pulling  bool
	dim      bool // synced (up-to-date) rows read calmer

	glyphSlot *fyne.Container // holds the current status glyph (rebuilt per repo)
	fullName  string          // untruncated repo name (name.Text is truncated to fit in Layout)
	name      *canvas.Text
	branch    *canvas.Text
	count     *canvas.Text // "↓N" behind count
	pullBtn   *iconButton
	openBtn   *iconButton
	spinner   *widget.Activity
	rightBox  *fyne.Container
	detailBox *fyne.Container
	marquees  []*marqueeText // scrollable detail lines (path + commit messages)
	tips      *tooltipLayer

	hovered bool

	onExpand func(monitor.RepoState)
	onPull   func(monitor.RepoState)
	onOpen   func(monitor.RepoState)
}

func newRepoRow(tips *tooltipLayer, pal palette) *repoRow {
	r := &repoRow{tips: tips, pal: pal}
	r.name = canvas.NewText("", pal.rowName)
	r.name.TextStyle = fyne.TextStyle{Bold: true}
	r.name.TextSize = nameSize
	r.branch = canvas.NewText("", pal.faint)
	r.branch.TextStyle = fyne.TextStyle{Monospace: true}
	r.branch.TextSize = branchSize
	r.count = canvas.NewText("", pal.statusBehind)
	r.count.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	r.count.TextSize = countSize

	r.pullBtn = newIconButton(theme.DownloadIcon(), theme.ColorNamePrimary, "Pull (fast-forward)",
		pal.pullBtnBg, pal.btnHover, func() {
			if r.onPull != nil {
				r.onPull(r.repo)
			}
		})
	r.openBtn = newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open folder",
		pal.openBtnBg, pal.btnHover, func() {
			if r.onOpen != nil {
				r.onOpen(r.repo)
			}
		})
	r.spinner = widget.NewActivity()
	r.spinner.Hide()
	r.rightBox = container.New(&rowButtonsLayout{}, r.pullBtn, r.openBtn, r.spinner)
	r.glyphSlot = container.NewStack()
	r.detailBox = container.New(&tightVBox{gap: 4})
	r.detailBox.Hide()

	r.ExtendBaseWidget(r)
	return r
}

// Configure rebinds the row to a repo and its state/callbacks. widget.List
// recycles row objects during scroll, so this runs on every listUpdate.
func (r *repoRow) Configure(repo monitor.RepoState, expanded, pulling bool, detail *monitor.Details,
	onExpand, onPull, onOpen func(monitor.RepoState)) {

	if r.repo.Path != repo.Path {
		r.setHovered(false)
	}
	r.repo = repo
	r.expanded = expanded
	r.pulling = pulling
	r.onExpand, r.onPull, r.onOpen = onExpand, onPull, onOpen

	kind, gcol, dim := repoGlyph(repo, r.pal)
	r.dim = dim
	r.glyphSlot.RemoveAll()
	r.glyphSlot.Add(newGlyph(kind, gcol, glyphBox(kind), glyphStroke(kind)))

	r.fullName = repo.Name
	r.name.Text = repo.Name // truncated to the available width in Layout
	if dim {
		r.name.Color = r.pal.syncedName
	} else {
		r.name.Color = r.pal.rowName
	}
	branch := repo.Branch
	if branch == "" {
		branch = "—"
	}
	r.branch.Text = branch

	if repo.Behind > 0 {
		r.count.Text = fmt.Sprintf("↓%d", repo.Behind)
	} else {
		r.count.Text = ""
	}

	if expanded {
		r.rebuildDetail(detail)
		r.detailBox.Show()
	} else {
		r.clearMarquees()
		r.detailBox.Hide()
	}

	r.updateActions()
	r.Refresh()
}

// repoGlyph maps a repo to its status glyph, most-urgent first: an error (red "!")
// beats dirty (coral dot), which beats behind (amber down-arrow); an even repo
// shows a green check and reads dimmed.
func repoGlyph(r monitor.RepoState, pal palette) (kind glyphKind, col color.Color, dim bool) {
	switch {
	case r.Err != "" || r.FetchErr != "":
		return glyphError, pal.statusError, false
	case r.Dirty:
		return glyphDirty, pal.statusDirty, false
	case r.Behind > 0:
		return glyphBehind, pal.statusBehind, false
	default:
		return glyphSynced, pal.statusSynced, true
	}
}

func glyphBox(k glyphKind) float32 {
	switch k {
	case glyphSynced:
		return 14
	case glyphDirty:
		return 9
	default: // behind arrow, error badge
		return 15
	}
}

func glyphStroke(k glyphKind) float32 {
	switch k {
	case glyphSynced:
		return 2.4
	case glyphError:
		return 2.2 // exclamation stem
	default:
		return 2
	}
}

// repoErrorMsg returns a human-readable description of a repo's last error, or "".
// A fetch error (network/auth) is reported ahead of a local status error.
func repoErrorMsg(r monitor.RepoState) string {
	if r.FetchErr != "" {
		return "Fetch error: " + r.FetchErr
	}
	if r.Err != "" {
		return "Status error: " + r.Err
	}
	return ""
}

// truncateToWidth shortens s with a trailing ellipsis until it renders within maxW.
// canvas.Text doesn't clip or truncate itself, so long repo names would otherwise
// draw over the count/action area — this keeps the title within its column.
func truncateToWidth(s string, maxW, size float32, style fyne.TextStyle) string {
	if maxW <= 0 {
		return ""
	}
	if fyne.MeasureText(s, size, style).Width <= maxW {
		return s
	}
	r := []rune(s)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fyne.MeasureText(string(r[:mid])+"…", size, style).Width <= maxW {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return "…"
	}
	return string(r[:lo]) + "…"
}

func (r *repoRow) clearMarquees() {
	for _, m := range r.marquees {
		m.stopAnim()
	}
	r.marquees = nil
}

func (r *repoRow) rebuildDetail(d *monitor.Details) {
	r.clearMarquees()
	r.detailBox.RemoveAll()
	// Surface any fetch/status error first, in red — this is what the red "!" glyph
	// and the tray error badge are pointing at. Shown straight from repo state so
	// it's visible even before (or when) commit details fail to load.
	if msg := repoErrorMsg(r.repo); msg != "" {
		r.detailBox.Add(r.line(msg, r.pal.statusError, branchSize, false, false, true))
	}
	if d == nil {
		if repoErrorMsg(r.repo) == "" {
			r.detailBox.Add(r.line("Loading…", r.pal.faint, branchSize, false, false, false))
		}
		r.activateMarquees() // let a long error message scroll even before details load
		r.detailBox.Refresh()
		return
	}
	branch := r.repo.Branch
	if branch == "" {
		branch = "—"
	}
	// Line 1: the repo path, with the behind summary appended when it's behind —
	// mirroring "~/projects/dash · behind 20 commits" from the mock.
	head := d.Path
	if r.repo.Behind > 0 {
		head = fmt.Sprintf("%s · behind %d commits", d.Path, r.repo.Behind)
	}
	r.detailBox.Add(r.line(head, r.pal.faint, branchSize, false, true, true))
	r.detailBox.Add(r.line("Local "+branch, r.pal.detailKey, branchSize, true, false, false))
	r.addCommit(d.LocalHash, d.LocalTime, d.LocalMsg)
	r.detailBox.Add(r.line("Origin "+d.OriginRef, r.pal.detailKey, branchSize, true, false, false))
	r.addCommit(d.OriginHash, d.OriginTime, d.OriginMsg)

	r.activateMarquees()
	r.detailBox.Refresh()
}

// activateMarquees starts auto-scroll on the detail's overflowing lines; the detail
// is shown, so each line scrolls on its own (pausing on hover, draggable by hand).
func (r *repoRow) activateMarquees() {
	for _, m := range r.marquees {
		m.SetActive(true)
	}
}

// deactivate stops the row's animations without reconfiguring it — used when a
// recycled list item is repurposed as a group header, so a previously expanded or
// pulling repo doesn't keep its marquees/spinner running off-screen.
func (r *repoRow) deactivate() {
	r.clearMarquees()
	r.spinner.Stop()
	r.spinner.Hide()
}

func (r *repoRow) addCommit(hash string, t time.Time, msg string) {
	if hash == "" {
		r.detailBox.Add(r.line("—", r.pal.rowSub, branchSize, false, true, false))
		return
	}
	r.detailBox.Add(r.line(commitMeta(hash, t), r.pal.rowSub, branchSize, false, true, false))
	if msg != "" {
		r.detailBox.Add(r.line(msg, r.pal.faint, branchSize, false, true, true))
	}
}

// line builds a detail line; scrollable ones are tracked so hover can animate them.
func (r *repoRow) line(s string, col color.Color, size float32, bold, mono, scrollable bool) *marqueeText {
	m := newMarquee(s, col, size, bold, mono)
	if scrollable {
		r.marquees = append(r.marquees, m)
	}
	return m
}

// updateActions sets which right-side widgets are visible: a spinner while
// pulling, the action chips on hover (pull only when there's something to pull),
// otherwise the behind count and error marker.
func (r *repoRow) updateActions() {
	showCount := false
	if r.pulling {
		r.pullBtn.Hide()
		r.openBtn.Hide()
		r.spinner.Show()
		r.spinner.Start()
	} else {
		r.spinner.Stop()
		r.spinner.Hide()
		if r.hovered {
			if r.repo.Behind > 0 {
				r.pullBtn.Show()
			} else {
				r.pullBtn.Hide()
			}
			r.openBtn.Show()
		} else {
			r.pullBtn.Hide()
			r.openBtn.Hide()
			showCount = true
		}
	}
	if showCount && r.count.Text != "" {
		r.count.Show()
	} else {
		r.count.Hide()
	}
	r.rightBox.Refresh()
}

func (r *repoRow) Tapped(*fyne.PointEvent) {
	if r.onExpand != nil {
		r.onExpand(r.repo)
	}
}

func (r *repoRow) MouseIn(ev *desktop.MouseEvent) {
	r.setHovered(true)
	r.MouseMoved(ev)
}

func (r *repoRow) MouseMoved(ev *desktop.MouseEvent) {
	if !r.hovered || r.pulling {
		return
	}
	var hovered *iconButton
	for _, b := range []*iconButton{r.pullBtn, r.openBtn} {
		in := false
		if b.Visible() {
			origin := r.rightBox.Position().Add(b.Position())
			in = ev.Position.X >= origin.X && ev.Position.X <= origin.X+b.Size().Width &&
				ev.Position.Y >= origin.Y && ev.Position.Y <= origin.Y+b.Size().Height
		}
		b.setHovered(in)
		if in {
			hovered = b
		}
	}
	if r.tips != nil {
		if hovered != nil {
			r.tips.show(hovered.tip, hovered)
		} else {
			r.tips.hide()
		}
	}
}

func (r *repoRow) MouseOut() {
	r.pullBtn.setHovered(false)
	r.openBtn.setHovered(false)
	if r.tips != nil {
		r.tips.hide()
	}
	r.setHovered(false)
}

func (r *repoRow) setHovered(h bool) {
	if r.hovered == h {
		return
	}
	r.hovered = h
	r.updateActions()
	r.Refresh()
}

func (r *repoRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	return &repoRowRenderer{
		row:     r,
		bg:      bg,
		objects: []fyne.CanvasObject{bg, r.glyphSlot, r.name, r.branch, r.count, r.rightBox, r.detailBox},
	}
}

type repoRowRenderer struct {
	row     *repoRow
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

// lineHeight is the height of the collapsed title line (name + glyph + count).
func (rr *repoRowRenderer) lineHeight() float32 {
	h := rr.row.name.MinSize().Height
	if c := rr.row.count.MinSize().Height; c > h {
		h = c
	}
	if h < 16 {
		h = 16
	}
	return h
}

func (rr *repoRowRenderer) titleHeight() float32 {
	return rowVPad*2 + rr.lineHeight()
}

func (rr *repoRowRenderer) Layout(size fyne.Size) {
	rr.bg.Resize(size)
	lineH := rr.lineHeight()
	top := float32(rowVPad)

	// Status-glyph column.
	rr.row.glyphSlot.Move(fyne.NewPos(rowHPad, top))
	rr.row.glyphSlot.Resize(fyne.NewSize(glyphColW, lineH))

	// Name + inline branch. The name is truncated so name + branch never bleed into
	// the reserved right-edge area (the behind count, or the hover action chips).
	nameX := float32(rowHPad + glyphColW + glyphGap)
	branchSz := rr.row.branch.MinSize()
	titleRight := size.Width - rowHPad - rightReserve
	availName := titleRight - nameX - branchGap - branchSz.Width
	rr.row.name.Text = truncateToWidth(rr.row.fullName, availName, nameSize, rr.row.name.TextStyle)
	nameSz := rr.row.name.MinSize()
	rr.row.name.Move(fyne.NewPos(nameX, top+(lineH-nameSz.Height)/2))
	rr.row.name.Resize(nameSz)
	rr.row.branch.Move(fyne.NewPos(nameX+nameSz.Width+branchGap, top+(lineH-branchSz.Height)/2))
	rr.row.branch.Resize(branchSz)

	// Right edge: hover action chips, else the behind count (+ error marker).
	rb := rr.row.rightBox.MinSize()
	rr.row.rightBox.Resize(rb)
	rr.row.rightBox.Move(fyne.NewPos(size.Width-rowHPad-rb.Width, top+(lineH-rb.Height)/2))

	if rr.row.count.Visible() {
		cs := rr.row.count.MinSize()
		rr.row.count.Move(fyne.NewPos(size.Width-rowHPad-cs.Width, top+(lineH-cs.Height)/2))
		rr.row.count.Resize(cs)
	}

	if rr.row.expanded {
		ty := rr.titleHeight()
		rr.row.detailBox.Move(fyne.NewPos(detailIndent, ty))
		rr.row.detailBox.Resize(fyne.NewSize(size.Width-detailIndent-rowHPad, size.Height-ty-rowVPad))
	}
}

func (rr *repoRowRenderer) MinSize() fyne.Size {
	h := rr.titleHeight()
	if rr.row.expanded {
		h += rr.row.detailBox.MinSize().Height + rowVPad
	}
	w := float32(rowHPad+glyphColW+glyphGap) + rr.row.name.MinSize().Width + rightReserve + rowHPad
	return fyne.NewSize(w, h)
}

func (rr *repoRowRenderer) Refresh() {
	switch {
	case rr.row.expanded:
		rr.bg.FillColor = rr.row.pal.rowExpandedBg
	case rr.row.hovered:
		rr.bg.FillColor = rr.row.pal.rowHover
	default:
		rr.bg.FillColor = color.Transparent
	}
	rr.bg.Refresh()
	rr.row.name.Refresh()
	rr.row.branch.Refresh()
	rr.row.count.Refresh()
}

func (rr *repoRowRenderer) Objects() []fyne.CanvasObject { return rr.objects }

// Destroy stops any running animations so a row torn down mid-pull (or with the
// detail expanded) doesn't leak the spinner/marquee animations.
func (rr *repoRowRenderer) Destroy() {
	rr.row.spinner.Stop()
	rr.row.clearMarquees()
}

// tightVBox stacks objects vertically with a small fixed gap and no per-item
// padding (unlike container.NewVBox / widget.Label), keeping the detail dense.
type tightVBox struct{ gap float32 }

func (l *tightVBox) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + l.gap
	}
}

func (l *tightVBox) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		w = max(w, s.Width)
		h += s.Height
		n++
	}
	if n > 1 {
		h += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

// rowButtonsLayout packs the visible right-side widgets left-to-right.
type rowButtonsLayout struct{}

func (rowButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gap := float32(4)
	x := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		o.Move(fyne.NewPos(x, (size.Height-s.Height)/2))
		o.Resize(s)
		x += s.Width + gap
	}
}

func (rowButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	gap := float32(4)
	var w, h float32
	n := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		w += s.Width
		h = max(h, s.Height)
		n++
	}
	if n > 1 {
		w += gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}
