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

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
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
	lineGap      = 2  // gap between the name line and the branch line (two-line mode)
	detailIndent = 43 // left indent of the expanded detail panel (aligns under name)
	nameSize     = 13.5
	branchSize   = 11
	countSize    = 12
	rightReserve = 66 // right-edge space kept for the count / two action chips
	actionBox    = 28 // hover action chip size
	actionRadius = 7
)

// branchLabel is the text shown for a repo's branch, using an em dash when unknown.
func branchLabel(branch string) string {
	if branch == "" {
		return "—"
	}
	return branch
}

// availTitleWidth is the horizontal space a row of total width rowW leaves for the
// name+branch title line: the row minus the left glyph column and the reserved
// right-edge area (behind count / hover action chips).
func availTitleWidth(rowW float32) float32 {
	return rowW - float32(2*rowHPad+glyphColW+glyphGap+rightReserve)
}

// estRowWidth approximates the width a repo row is given inside the popover list.
// A row's height comes from MinSize(), which has no width, yet the height depends on
// whether the title wraps to a second line — so that decision is made in Configure
// from this estimate. The popover isn't resizable, so it matches the width Layout
// later receives; it is deliberately on the narrow side so a title that wraps here
// still fits the real, equal-or-wider row (the name keeps priority and is never
// squeezed down to fit the branch inline).
func estRowWidth() float32 { return popoverWidth - 4*theme.Padding() }

// titleWraps reports whether a repo's name and branch can't share one title line at
// the given available width, so the branch drops onto a second line under the name.
// Shared by the row layout and the popover's height estimate so both agree on height.
func titleWraps(name, branch string, avail float32) bool {
	nameW := fyne.MeasureText(name, nameSize, fyne.TextStyle{Bold: true}).Width
	branchW := fyne.MeasureText(branch, branchSize, fyne.TextStyle{Monospace: true}).Width
	return nameW+branchGap+branchW > avail
}

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
// update / open action chips in place of the count.
type repoRow struct {
	widget.BaseWidget

	pal      palette
	repo     monitor.RepoState
	expanded bool
	pulling  bool
	dim      bool // synced (up-to-date) rows read calmer

	glyphSlot  *fyne.Container // holds the current status glyph (rebuilt per repo)
	fullName   string          // untruncated repo name (name.Text is truncated to fit in Layout)
	fullBranch string          // untruncated branch label (branch.Text is truncated to fit in Layout)
	twoLine    bool            // branch dropped onto a second line under the name (title won't fit on one)
	name       *canvas.Text
	branch     *canvas.Text
	count      *canvas.Text // "↓N" behind count
	pullBtn    *iconButton
	freshBtn   *iconButton
	openBtn    *iconButton
	spinner    *widget.Activity
	rightBox   *fyne.Container
	detailBox  *fyne.Container
	marquees   []*marqueeText // scrollable detail lines (path + commit messages)
	tips       *tooltipLayer
	detailRepo detailRenderState
	detailData monitor.Details
	detailSet  bool
	detailMade bool

	// Two sources because the detail lines are themselves Hoverable: selfHovered is
	// the pointer on the row body, detailHovered on a detail line. Either keeps the
	// row hovered (and its chips visible).
	hovered       bool
	selfHovered   bool
	detailHovered bool

	onExpand func(monitor.RepoState)
	onPull   func(monitor.RepoState)
	onFresh  func(monitor.RepoState)
	onOpen   func(monitor.RepoState)
}

type detailRenderState struct {
	path     string
	branch   string
	behind   int
	err      string
	fetchErr string
}

func detailState(repo monitor.RepoState) detailRenderState {
	return detailRenderState{
		path: repo.Path, branch: repo.Branch, behind: repo.Behind,
		err: repo.Err, fetchErr: repo.FetchErr,
	}
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
	r.freshBtn = newIconButton(theme.ViewRefreshIcon(), theme.ColorNamePrimary, "Keep fresh",
		pal.openBtnBg, pal.btnHover, func() {
			if r.onFresh != nil {
				r.onFresh(r.repo)
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
	r.rightBox = container.New(&rowButtonsLayout{}, r.pullBtn, r.freshBtn, r.openBtn, r.spinner)
	r.glyphSlot = container.NewStack()
	r.detailBox = container.New(&tightVBox{gap: 4})
	r.detailBox.Hide()

	r.ExtendBaseWidget(r)
	return r
}

// Configure rebinds the row to a repo and its state/callbacks. widget.List
// recycles row objects during scroll, so this runs on every listUpdate.
func (r *repoRow) Configure(repo monitor.RepoState, expanded, pulling bool, detail *monitor.Details,
	onExpand, onPull, onFresh, onOpen func(monitor.RepoState)) {

	if r.repo.Path != repo.Path {
		r.resetHover()
	}
	r.repo = repo
	r.expanded = expanded
	r.pulling = pulling
	r.onExpand, r.onPull, r.onFresh, r.onOpen = onExpand, onPull, onFresh, onOpen
	if repo.KeepFresh {
		r.freshBtn.tip = "Stop keeping fresh"
		r.freshBtn.rest = r.pal.pullBtnBg
	} else {
		r.freshBtn.tip = "Keep fresh"
		r.freshBtn.rest = r.pal.openBtnBg
	}
	r.freshBtn.Refresh()

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
	r.fullBranch = branchLabel(repo.Branch)
	r.branch.Text = r.fullBranch
	// Decide up front whether the name+branch fit on one line; if not, the branch
	// drops below the name (which keeps priority). This drives the row height, which
	// the list reads from MinSize() before Layout runs — hence the fixed-width estimate.
	r.twoLine = titleWraps(r.fullName, r.fullBranch, availTitleWidth(estRowWidth()))

	if repo.Behind > 0 {
		r.count.Text = fmt.Sprintf("↓%d", repo.Behind)
	} else {
		r.count.Text = ""
	}

	if expanded {
		state := detailState(repo)
		detailChanged := r.detailMade && (r.detailRepo != state || r.detailSet != (detail != nil))
		if !detailChanged && r.detailMade && detail != nil {
			detailChanged = r.detailData != *detail
		}
		if !r.detailMade || detailChanged {
			r.rebuildDetail(detail)
			r.detailRepo = state
			r.detailSet = detail != nil
			if detail != nil {
				r.detailData = *detail
			} else {
				r.detailData = monitor.Details{}
			}
			r.detailMade = true
		}
		r.detailBox.Show()
	} else {
		r.clearMarquees()
		r.detailBox.Hide()
		r.detailMade = false
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
	// Detail lines are gone; clear the flag so a rebuild mid-hover can't wedge the
	// row "hovered".
	r.detailHovered = false
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
	r.resetHover() // drop any lingering hover so a repurposed row shows no chips
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
	m.onHover = r.setDetailHovered
	if scrollable {
		r.marquees = append(r.marquees, m)
	}
	return m
}

// updateActions sets which right-side widgets are visible: a spinner while
// pulling, the action chips on hover (pull when behind, keep-fresh when synced),
// otherwise the behind count and error marker.
func (r *repoRow) updateActions() {
	showCount := false
	if r.pulling {
		r.pullBtn.Hide()
		r.freshBtn.Hide()
		r.openBtn.Hide()
		r.spinner.Show()
		r.spinner.Start()
	} else {
		r.spinner.Stop()
		r.spinner.Hide()
		if r.hovered {
			if r.repo.Behind > 0 {
				r.pullBtn.Show()
				r.freshBtn.Hide()
			} else {
				r.pullBtn.Hide()
				r.freshBtn.Show()
			}
			r.openBtn.Show()
		} else {
			r.pullBtn.Hide()
			r.freshBtn.Hide()
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
	r.selfHovered = true
	// The row body is hovered, so the pointer is not on a detail line; clearing this
	// here self-heals a detailHovered flag stranded by a detail rebuild under a
	// stationary cursor (a destroyed marquee never fires MouseOut).
	r.detailHovered = false
	r.recomputeHover()
	r.MouseMoved(ev)
}

func (r *repoRow) MouseMoved(ev *desktop.MouseEvent) {
	if !r.hovered || r.pulling {
		return
	}
	var hovered *iconButton
	for _, b := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
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

// MouseOut also fires when the pointer crosses onto a detail line (a Hoverable
// child), so it clears only the body's hover — detailHovered may keep the row hovered.
func (r *repoRow) MouseOut() {
	r.pullBtn.setHovered(false)
	r.freshBtn.setHovered(false)
	r.openBtn.setHovered(false)
	if r.tips != nil {
		r.tips.hide()
	}
	r.selfHovered = false
	r.recomputeHover()
}

// setDetailHovered is called by the detail lines as the pointer enters/leaves them.
func (r *repoRow) setDetailHovered(h bool) {
	if r.detailHovered == h {
		return
	}
	r.detailHovered = h
	r.recomputeHover()
}

// resetHover clears all hover sources; used when a recycled row is rebound.
func (r *repoRow) resetHover() {
	r.pullBtn.setHovered(false)
	r.freshBtn.setHovered(false)
	r.openBtn.setHovered(false)
	r.selfHovered = false
	r.detailHovered = false
	r.recomputeHover()
}

// recomputeHover folds the two hover sources into the effective state, refreshing
// only on a real change.
func (r *repoRow) recomputeHover() {
	h := r.selfHovered || r.detailHovered
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
	h := rowVPad*2 + rr.lineHeight()
	if rr.row.twoLine {
		h += rr.row.branch.MinSize().Height + lineGap
	}
	return h
}

func (rr *repoRowRenderer) Layout(size fyne.Size) {
	rr.bg.Resize(size)
	lineH := rr.lineHeight()
	top := float32(rowVPad)

	// Status-glyph column (aligned with the name line).
	rr.row.glyphSlot.Move(fyne.NewPos(rowHPad, top))
	rr.row.glyphSlot.Resize(fyne.NewSize(glyphColW, lineH))

	// Name + branch. The name has priority: it takes the full title width, and only
	// when name + branch can't share one line does the branch drop below it. Both are
	// truncated to their own available width so neither bleeds into the reserved
	// right-edge area (the behind count, or the hover action chips).
	nameX := float32(rowHPad + glyphColW + glyphGap)
	titleRight := size.Width - rowHPad - rightReserve
	if rr.row.twoLine {
		rr.row.name.Text = truncateToWidth(rr.row.fullName, titleRight-nameX, nameSize, rr.row.name.TextStyle)
		nameSz := rr.row.name.MinSize()
		rr.row.name.Move(fyne.NewPos(nameX, top+(lineH-nameSz.Height)/2))
		rr.row.name.Resize(nameSz)

		// Branch on its own line, spanning to the right padding (the count/chips sit on
		// the name line, so the branch line is free to use the full width).
		rr.row.branch.Text = truncateToWidth(rr.row.fullBranch, size.Width-rowHPad-nameX, branchSize, rr.row.branch.TextStyle)
		branchSz := rr.row.branch.MinSize()
		rr.row.branch.Move(fyne.NewPos(nameX, top+lineH+lineGap))
		rr.row.branch.Resize(branchSz)
	} else {
		rr.row.branch.Text = rr.row.fullBranch
		branchSz := rr.row.branch.MinSize()
		availName := titleRight - nameX - branchGap - branchSz.Width
		rr.row.name.Text = truncateToWidth(rr.row.fullName, availName, nameSize, rr.row.name.TextStyle)
		nameSz := rr.row.name.MinSize()
		rr.row.name.Move(fyne.NewPos(nameX, top+(lineH-nameSz.Height)/2))
		rr.row.name.Resize(nameSz)
		rr.row.branch.Move(fyne.NewPos(nameX+nameSz.Width+branchGap, top+(lineH-branchSz.Height)/2))
		rr.row.branch.Resize(branchSz)
	}

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
