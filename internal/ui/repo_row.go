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

// Text sizes used inside a repo row. The geometry constants live in tokens.go.
const (
	nameSize   = textMd
	branchSize = textXs
	countSize  = textXs
)

// rowPhase is where a repository sits in a pull. It exists because a pull used
// to be visible only as a spinner that vanished the instant the work finished,
// leaving the user unsure whether their click had registered at all.
type rowPhase int

const (
	rowPulling rowPhase = iota
	rowPulled
	rowPullFailed
)

// rowStatus is the short-lived message a row shows while, and just after, it is
// pulled. It lives in a map on App keyed by repo path rather than on the widget:
// widget.List recycles row widgets as the list scrolls, so per-widget state
// would follow the wrong repository.
type rowStatus struct {
	phase   rowPhase
	msg     string
	expires time.Time // zero while the pull is still running
}

func (s *rowStatus) pulling() bool { return s != nil && s.phase == rowPulling }

// rowState and rowActions carry everything a row needs to render and to act.
// They replace a Configure signature that had grown to eight positional
// parameters, where `false, false, nil, nil, nil, nil, nil` at a call site told
// the reader nothing.
type rowState struct {
	expanded bool
	status   *rowStatus // nil when the row is idle
	detail   *monitor.Details
	selected bool // keyboard highlight
	branches branchSectionState

	// match is the branch a search matched on, when the repository's own name
	// and path did not. Shown on the branch line so a hit is never unexplained.
	match string

	// The editor chip's appearance, resolved once per paint by the App rather
	// than per row — every row shows the same editor.
	ideIcon    fyne.Resource
	ideTip     string
	ideAltTip  string
	ideMissing bool
}

// branchSectionState is everything the collapsible Branches section renders from.
//
// It must reach the row through rowState and nothing else. The popover measures
// each expanded row by configuring a throwaway copy of it, and the two agree
// only because they are handed identical inputs — a row that read branch state
// from App through a closure would measure one height and render another, and
// the window would end up the wrong size.
type branchSectionState struct {
	open    bool
	loading bool
	// gen changes whenever anything about this repo's branches changes. It is
	// what makes the state comparable: the fields below are a map and a pointer,
	// which Go cannot compare with ==, so the row would either rebuild on every
	// 30-second tick (killing marquee scroll and hover) or never rebuild at all.
	gen  uint64
	list *monitor.BranchList
	busy map[string]bool
	errs map[string]string

	// limit is how many branches are rendered. It grows a page at a time when
	// the user asks for more, so a repository with hundreds of branches neither
	// renders them all nor hides the rest behind a dead label.
	limit int
	// fetching is set while an explicit fetch of this repository is running.
	fetching bool
}

// fingerprint is the comparable part of the section state.
func (s branchSectionState) fingerprint() branchFingerprint {
	return branchFingerprint{open: s.open, loading: s.loading, gen: s.gen,
		limit: s.limit, fetching: s.fetching}
}

type branchFingerprint struct {
	open     bool
	loading  bool
	gen      uint64
	limit    int
	fetching bool
}

type rowActions struct {
	onExpand func(monitor.RepoState)
	onPull   func(monitor.RepoState)
	onFresh  func(monitor.RepoState)
	onOpen   func(monitor.RepoState)

	onOpenIDE func(monitor.RepoState)
	onPickIDE func(monitor.RepoState)

	onToggleBranches func(monitor.RepoState)
	onPullBranch     func(monitor.RepoState, monitor.BranchInfo)
	onTrackBranch    func(monitor.RepoState, monitor.BranchInfo)
	onOpenBranch     func(monitor.RepoState, monitor.BranchInfo)
	onDismissBranch  func(monitor.RepoState, string)
	onFetchRepo      func(monitor.RepoState)
	onMoreBranches   func(monitor.RepoState)
	onCopyBranch     func(string)
}

// branchLabel is the text shown for a repo's branch, using an em dash when unknown.
func branchLabel(branch string) string {
	if branch == "" {
		return "—"
	}
	return branch
}

// Every repo row is two lines — name above, branch below — and therefore the
// same height as every other row.
//
// It used to be adaptive: the branch sat inline beside the name when both fitted,
// and dropped to a second line when they did not. That made a list of real
// repositories visibly ragged, because whether a row was tall or short depended
// on the length of its name. It also cost two fyne.MeasureText calls per visible
// row on *every keystroke* in the search field, since the popover's height had
// to predict each row's wrap decision before laying it out. A fixed two-line row
// removes the raggedness and the measuring together.

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
	res            fyne.Resource
	iconName       fyne.ThemeColorName // icon tint; empty = themed foreground
	tip            string
	rest           color.Color // resting background
	hover          color.Color // background while hovered
	onTap          func()
	onFocusChanged func(bool)
	keyboardGuard  func() bool
	state          interactionState
	disabled       bool

	// dim draws the chip at reduced emphasis without making it inert — used by
	// branch rows, whose chips are always present and brighten under the pointer.
	dim bool

	// size controls the hit target. surfaceSize and iconSize can make the drawn
	// chrome quieter without shrinking the accessible pointer target.
	size        float32
	surfaceSize float32
	iconSize    float32
}

// box is the hit target's edge length. The drawn surface and glyph are measured
// separately so visual density never reduces pointer accessibility.
func (b *iconButton) box() float32 {
	if b.size > 0 {
		return b.size
	}
	return actionBox
}

func (b *iconButton) surfaceBox() float32 {
	if b.surfaceSize > 0 {
		return b.surfaceSize
	}
	return b.box()
}

func (b *iconButton) glyphBox() float32 {
	if b.iconSize > 0 {
		return b.iconSize
	}
	return actionIcon
}

func (b *iconButton) inset() float32 {
	return (b.box() - b.glyphBox()) / 2
}

func (b *iconButton) useCompactRowChrome() {
	b.surfaceSize = rowActionSurface
	b.iconSize = rowActionIcon
}

func newIconButton(res fyne.Resource, iconName fyne.ThemeColorName, tip string, rest, hover color.Color, onTap func()) *iconButton {
	b := &iconButton{res: res, iconName: iconName, tip: tip, rest: rest, hover: hover, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *iconButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTap == nil {
		return
	}
	b.onTap()
}

func (b *iconButton) setPresentation(res fyne.Resource, tip string) {
	if res == nil {
		return
	}
	if b.res != nil && b.res.Name() == res.Name() && b.tip == tip {
		return
	}
	b.res, b.tip = res, tip
	b.Refresh()
}

func (b *iconButton) setHovered(h bool) {
	if !h {
		b.setPressed(false)
	}
	if b.state.setHovered(h) {
		b.Refresh()
	}
}

func (b *iconButton) MouseDown(*desktop.MouseEvent) { b.setPressed(true) }
func (b *iconButton) MouseUp(*desktop.MouseEvent)   { b.setPressed(false) }

func (b *iconButton) setPressed(v bool) {
	if b.disabled {
		v = false
	}
	if b.state.setPressed(v) {
		b.Refresh()
	}
}

func (b *iconButton) FocusGained() {
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

func (b *iconButton) FocusLost() {
	if b.state.setFocused(false) {
		b.Refresh()
	}
	if b.onFocusChanged != nil {
		b.onFocusChanged(false)
	}
}

func (b *iconButton) TypedRune(rune) {}

func (b *iconButton) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name != fyne.KeySpace && ev.Name != fyne.KeyReturn && ev.Name != fyne.KeyEnter {
		return
	}
	if b.disabled || !b.Visible() || (b.keyboardGuard != nil && !b.keyboardGuard()) {
		b.cancelInteraction()
		return
	}
	keyboardActivate(&b.state, b.Refresh, func() { b.Tapped(nil) })
}

func (b *iconButton) cancelInteraction() {
	unfocusCanvasObjects(b)
	if b.state.clear() {
		b.Refresh()
	}
	if b.onFocusChanged != nil {
		b.onFocusChanged(false)
	}
}

func (b *iconButton) release() {
	b.cancelInteraction()
	b.onTap, b.onFocusChanged, b.keyboardGuard = nil, nil, nil
}

// setDisabled greys the chip and makes it inert, so an action already running
// visibly cannot be started again.
func (b *iconButton) setDisabled(d bool) {
	if b.disabled != d {
		b.disabled = d
		if d {
			b.cancelInteraction()
		}
		b.Refresh()
	}
}

func (b *iconButton) imageResource() fyne.Resource {
	if b.iconName != "" {
		return theme.NewColoredResource(b.res, b.iconName)
	}
	return theme.NewThemedResource(b.res)
}

// headerButton is the icon chip used in the popover header. It is the same shape
// as a row's action chip, but unlike iconButton it handles its own hover: there
// is no parent row tracking the pointer on its behalf.
//
// The header used to use widget.Button, which now inherits the theme's 12px
// input radius — nearly a circle on a 34px square. Sharing iconButton keeps the
// header and the rows visibly the same family.
type headerButton struct {
	widget.BaseWidget
	btn  *iconButton
	tips *tooltipLayer
}

func newHeaderButton(tips *tooltipLayer, pal palette, res fyne.Resource, tip string, onTap func()) *headerButton {
	h := &headerButton{
		btn:  newIconButton(res, colorNameMuted, tip, color.Transparent, pal.btnHover, onTap),
		tips: tips,
	}
	h.ExtendBaseWidget(h)
	return h
}

func (h *headerButton) setDisabled(d bool) {
	if d {
		unfocusCanvasObjects(h)
		h.btn.setHovered(false)
		if h.tips != nil {
			h.tips.hide()
		}
	}
	h.btn.setDisabled(d)
}

func (h *headerButton) Tapped(ev *fyne.PointEvent) { h.btn.Tapped(ev) }
func (h *headerButton) MouseDown(ev *desktop.MouseEvent) {
	h.btn.MouseDown(ev)
}
func (h *headerButton) MouseUp(ev *desktop.MouseEvent) { h.btn.MouseUp(ev) }
func (h *headerButton) FocusGained() {
	if h.btn.disabled {
		unfocusCanvasObjects(h)
		h.btn.cancelInteraction()
		return
	}
	h.btn.FocusGained()
}
func (h *headerButton) FocusLost()                 { h.btn.FocusLost() }
func (h *headerButton) TypedRune(r rune)           { h.btn.TypedRune(r) }
func (h *headerButton) TypedKey(ev *fyne.KeyEvent) { h.btn.TypedKey(ev) }

func (h *headerButton) MouseIn(*desktop.MouseEvent) {
	h.btn.setHovered(true)
	if h.tips != nil {
		h.tips.show(h.btn.tip, h)
	}
}
func (h *headerButton) MouseMoved(*desktop.MouseEvent) {}
func (h *headerButton) MouseOut() {
	h.btn.setHovered(false)
	if h.tips != nil {
		h.tips.hide()
	}
}
func (h *headerButton) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(h.btn) }

func (b *iconButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(b.rest)
	bg.CornerRadius = radiusSm
	img := canvas.NewImageFromResource(b.imageResource())
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
	surface := r.b.surfaceBox()
	surfaceX := (size.Width - surface) / 2
	surfaceY := (size.Height - surface) / 2
	r.bg.Move(fyne.NewPos(surfaceX, surfaceY))
	r.bg.Resize(fyne.NewSize(surface, surface))

	glyph := r.b.glyphBox()
	glyphX := (size.Width - glyph) / 2
	glyphY := (size.Height - glyph) / 2
	r.img.Move(fyne.NewPos(glyphX, glyphY))
	r.img.Resize(fyne.NewSize(glyph, glyph))
}
func (r *iconButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.b.box(), r.b.box())
}
func (r *iconButtonRenderer) Refresh() {
	r.img.Resource = r.b.imageResource()
	switch {
	case r.b.disabled:
		r.bg.FillColor = color.Transparent
		r.img.Translucency = 0.6
	case r.b.state.pressed:
		r.bg.FillColor = r.b.hover
		r.img.Translucency = 0
	case r.b.state.active():
		r.bg.FillColor = r.b.hover
		r.img.Translucency = 0
	case r.b.dim:
		r.bg.FillColor = r.b.rest
		r.img.Translucency = 0.2
	default:
		r.bg.FillColor = r.b.rest
		r.img.Translucency = 0
	}
	if r.b.disabled {
		r.bg.StrokeWidth = 0
	} else if r.b.state.pressed {
		r.bg.StrokeColor = r.b.palAccent()
		r.bg.StrokeWidth = 2
	} else if r.b.state.focused {
		r.bg.StrokeColor = r.b.palAccent()
		r.bg.StrokeWidth = hairlineW
	} else {
		r.bg.StrokeWidth = 0
	}
	r.bg.Refresh()
	r.img.Refresh()
}

func (b *iconButton) palAccent() color.Color {
	return theme.Color(theme.ColorNamePrimary)
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
	status   *rowStatus // transient pull message, nil when idle
	tip      string     // multi-line row tooltip, composed in Configure
	dim      bool       // synced (up-to-date) rows read calmer

	chevSlot  *fyne.Container // disclosure mark: this row opens
	keepBadge *canvas.Image   // permanent marker on keep-fresh repos
	// dirtyDot marks uncommitted changes beside the name. It exists because the
	// status glyph now answers a different question — where this repository
	// stands against its remote — and the two facts are independent: a repo can
	// be twelve commits behind AND have local edits, and one mark cannot say so.
	dirtyDot       *canvas.Circle
	noteColor      color.Color     // non-nil while the branch slot carries a pull message
	glyphSlot      *fyne.Container // holds the current status glyph (rebuilt per repo)
	fullName       string          // untruncated repo name (name.Text is truncated to fit in Layout)
	fullBranch     string          // untruncated branch label (branch.Text is truncated to fit in Layout)
	name           *canvas.Text
	branch         *canvas.Text
	count          *canvas.Text // "↓N" behind count
	pullBtn        *iconButton
	freshBtn       *iconButton
	ideBtn         *ideButton
	openBtn        *iconButton
	spinner        *widget.Activity
	rightBox       *fyne.Container
	detailBox      *fyne.Container
	marquees       []*marqueeText // scrollable detail lines (path + commit messages)
	branchRows     []*branchRow   // branch rows currently in the detail panel
	detailControls []interface {
		deactivate()
		setDisabled(bool)
	}
	tips       *tooltipLayer
	detailRepo detailRenderState
	detailData monitor.Details
	detailSet  bool
	detailMade bool
	branchFP   branchFingerprint // comparable digest of the Branches section

	// Two sources because the detail lines are themselves Hoverable: selfHovered is
	// the pointer on the row body, detailHovered on a detail line. Either keeps the
	// row hovered (and its chips visible).
	hovered       bool
	selfHovered   bool
	detailHovered bool
	selected      bool // keyboard highlight (Up/Down in the search field)
	interaction   interactionState
	actionFocused bool
	keyboardGuard func() bool

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
	// A placeholder until Configure supplies the real editor: the row is built
	// once and rebound many times, and the chosen editor can change underneath it.
	r.ideBtn = newIDEButton(pal, theme.NewColoredResource(theme.ComputerIcon(), colorNameMuted),
		"Open in editor", "Choose an editor", false, nil, nil)
	r.ideBtn.Hide()
	r.openBtn = newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open folder",
		pal.openBtnBg, pal.btnHover, func() {
			if r.onOpen != nil {
				r.onOpen(r.repo)
			}
		})
	for _, button := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
		button.useCompactRowChrome()
		button.onFocusChanged = r.setActionFocused
		button.keyboardGuard = r.acceptsKeyboard
	}
	r.ideBtn.useCompactRowChrome()
	r.ideBtn.onFocusChanged = r.setActionFocused
	r.ideBtn.keyboardGuard = r.acceptsKeyboard
	// Same icon as the keep-fresh toggle, so the badge and the control that sets
	// it read as one concept.
	r.keepBadge = canvas.NewImageFromResource(
		theme.NewColoredResource(theme.ViewRefreshIcon(), theme.ColorNamePrimary))
	r.keepBadge.FillMode = canvas.ImageFillContain
	r.keepBadge.Hide()

	r.dirtyDot = canvas.NewCircle(pal.statusDirty)
	r.dirtyDot.Hide()

	r.spinner = widget.NewActivity()
	r.spinner.Hide()
	r.rightBox = container.New(&actionClusterLayout{}, r.pullBtn, r.freshBtn, r.ideBtn, r.openBtn, r.spinner)
	r.chevSlot = container.NewStack()
	r.glyphSlot = container.NewStack()
	r.detailBox = container.New(&tightVBox{gap: 4})
	r.detailBox.Hide()

	r.ExtendBaseWidget(r)
	return r
}

// Configure rebinds the row to a repo and its state/callbacks. widget.List
// recycles row objects during scroll, so this runs on every listUpdate.
func (r *repoRow) Configure(repo monitor.RepoState, st rowState, act rowActions) {
	detail := st.detail
	if r.repo.Path != repo.Path {
		r.releaseBinding()
	}
	r.repo = repo
	r.expanded = st.expanded
	r.status = st.status
	r.pulling = st.status.pulling()
	r.selected = st.selected
	r.onExpand, r.onPull, r.onFresh, r.onOpen = act.onExpand, act.onPull, act.onFresh, act.onOpen
	r.bindIDE(repo, st, act)
	r.tip = repoTooltip(repo, time.Now())
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

	// The dot only appears when the glyph is busy saying something else. When
	// the glyph IS the dirty mark, a second one beside the name would be the
	// same fact twice.
	if repo.Dirty && kind != glyphDirty && kind != glyphRing {
		r.dirtyDot.FillColor = r.pal.statusDirty
		r.dirtyDot.Show()
	} else {
		r.dirtyDot.Hide()
	}

	// The disclosure mark points right when the row is closed and down when it
	// is open — the same vocabulary as a scan-root section header, in the same
	// column, so one gesture is learned rather than two.
	chevKind := glyphChevronRight
	if st.expanded {
		chevKind = glyphChevron
	}
	r.chevSlot.RemoveAll()
	r.chevSlot.Add(newGlyph(chevKind, r.pal.faint, chevColW-2, 1.9))

	r.fullName = repo.Name
	r.name.Text = repo.Name // truncated to the available width in Layout
	if dim {
		r.name.Color = r.pal.syncedName
	} else {
		r.name.Color = r.pal.rowName
	}
	// The branch slot doubles as the pull-status slot: while a pull runs, and for
	// a few seconds after, it carries the message instead of the branch name.
	// Crucially the *wrap decision below* still uses the branch, never the
	// message — otherwise a long message would change the row's height mid-pull
	// and shove every row beneath it, fighting the popover's height animation.
	r.fullBranch = branchLabel(repo.Branch)
	if st.match != "" {
		r.fullBranch += "  · matched " + st.match
	}
	r.noteColor = nil
	if st.status != nil {
		r.fullBranch = st.status.msg
		r.noteColor = r.noteColour(st.status.phase)
	}
	r.branch.Text = r.fullBranch

	// The keep-fresh badge is permanent, not hover-revealed: toggling keep-fresh
	// used to change only the hovered button's tint, so from the user's side the
	// click did nothing at all.
	if repo.KeepFresh {
		r.keepBadge.Show()
	} else {
		r.keepBadge.Hide()
	}

	r.count.Text = syncCount(repo)

	if st.expanded {
		state := detailState(repo)
		fp := st.branches.fingerprint()
		detailChanged := r.detailMade && (r.detailRepo != state || r.detailSet != (detail != nil) || r.branchFP != fp)
		if !detailChanged && r.detailMade && detail != nil {
			detailChanged = r.detailData != *detail
		}
		if !r.detailMade || detailChanged {
			r.rebuildDetail(detail, repo, st.branches, act)
			r.detailRepo = state
			r.detailSet = detail != nil
			r.branchFP = fp
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

// noteColour maps a pull phase to the colour its message is drawn in.
func (r *repoRow) noteColour(p rowPhase) color.Color {
	switch p {
	case rowPulled:
		return r.pal.statusSynced
	case rowPullFailed:
		return r.pal.statusError
	default:
		return r.pal.accent // accent while the work is running
	}
}

// repoGlyph maps a repository to its status glyph.
//
// The glyph answers ONE question — what is this repository's relationship to its
// remote — and it answers it specifically: pull, push, both, or settled. It used
// to conflate that with "has uncommitted changes", and because dirty outranked
// behind, a repository that was both showed a dot and never mentioned the twelve
// commits waiting for it. On a machine where most working trees are dirty, that
// made the whole column a row of identical dots that meant nothing.
//
// Uncommitted changes have not gone away: they are their own mark beside the
// name (see dirtyBadge), so the two facts are shown as two facts.
//
// Order, most urgent first:
//
//	error      — the last fetch or status failed; nothing below is trustworthy
//	conflict   — mid-merge/rebase or unresolved conflicts; finish or abort first
//	diverged   — commits on both sides; a pull will not fast-forward
//	behind     — commits to pull
//	ahead      — commits to push
//	dirty      — only when the branch itself is settled
//	synced     — nothing to do
func repoGlyph(r monitor.RepoState, pal palette) (kind glyphKind, col color.Color, dim bool) {
	switch {
	case r.Err != "" || r.FetchErr != "":
		return glyphError, pal.statusError, false
	case r.Unsettled():
		return glyphConflict, pal.statusError, false
	case r.Behind > 0 && r.NeedsPush():
		return glyphDiverged, pal.statusBehind, false
	case r.Behind > 0:
		return glyphBehind, pal.statusBehind, false
	case r.NeedsPush():
		return glyphAhead, pal.statusAhead, false
	case r.Dirty:
		// Some palettes draw dirty as an outline rather than a filled dot, so the
		// mark reads as "something, but not urgent".
		if pal.dirtyOutlined {
			return glyphRing, pal.statusDirty, false
		}
		return glyphDirty, pal.statusDirty, false
	default:
		// A palette may say "nothing to do here" by drawing nothing at all.
		if pal.syncedGlyphHidden {
			return glyphNone, pal.statusSynced, true
		}
		return glyphSynced, pal.statusSynced, true
	}
}

// syncCount is the right-hand counter: what is waiting to come in, what is
// waiting to go out, or both. It used to report only the behind count, so a
// repository with unpushed work looked identical to one with nothing to do.
func syncCount(r monitor.RepoState) string {
	switch {
	case r.Behind > 0 && r.NeedsPush():
		return fmt.Sprintf("↓%d ↑%d", r.Behind, r.Ahead)
	case r.Behind > 0:
		return fmt.Sprintf("↓%d", r.Behind)
	case r.NeedsPush():
		return fmt.Sprintf("↑%d", r.Ahead)
	default:
		return ""
	}
}

func glyphBox(k glyphKind) float32 {
	switch k {
	case glyphSynced:
		return 14
	case glyphDirty:
		return 9
	case glyphRing:
		return 10
	case glyphNone:
		return 0
	case glyphDiverged, glyphConflict:
		// Two arrows, or a triangle, need the extra pixel to stay legible.
		return 16
	default: // behind/ahead arrow, error badge
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
	for _, control := range r.detailControls {
		control.deactivate()
	}
	r.detailControls = nil
	for _, m := range r.marquees {
		m.stopAnim()
	}
	r.marquees = nil
	r.branchRows = nil
	// Detail lines are gone; clear the flag so a rebuild mid-hover can't wedge the
	// row "hovered".
	r.detailHovered = false
}

func (r *repoRow) rebuildDetail(d *monitor.Details, repo monitor.RepoState,
	sec branchSectionState, act rowActions) {
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
		r.addBranchSection(repo, sec, act)
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

	r.addBranchSection(repo, sec, act)
	r.activateMarquees()
	r.detailBox.Refresh()
}

// The branch section scrolls inside itself rather than making the row taller
// without limit.
//
// It used to just cap the number of rows, which was not enough: fifteen branches
// plus the commit detail already made one list item 536px tall inside a popover
// that stops at 560, so the section's own header — and the chevron that closes
// it — scrolled out of reach and the section looked like it had refused to open.
// Bounding the section's height and scrolling within it keeps the row a sane
// size however many branches a repository has.
const (
	branchScrollMax = 172 // about six branch rows
	branchPageSize  = 40  // how many more branches one "show more" reveals
)

// addBranchSection appends the collapsible Branches section to the detail panel.
// Closed it is one tappable line; open it lists the repository's branches, each
// of which can be fast-forwarded on its own without touching the working tree.
func (r *repoRow) addBranchSection(repo monitor.RepoState, sec branchSectionState, act rowActions) {
	header := newSectionHeader(r.pal, branchSectionText(sec), sec.open, func() {
		if act.onToggleBranches != nil {
			act.onToggleBranches(repo)
		}
	})
	header.onHover = r.setDetailHovered
	header.setKeyboardGuard(r.acceptsKeyboard)
	r.detailControls = append(r.detailControls, header)
	// Fetching is offered on the header rather than per branch: it is one
	// network call for the whole repository, and it is what makes every branch's
	// ahead/behind true. Without it, a root with autoFetch off shows every
	// branch as level with its upstream and therefore offers nothing to pull.
	if act.onFetchRepo != nil {
		tip := "Fetch " + repo.Name + " from its remote"
		if sec.fetching {
			tip = "Fetching…"
		}
		fetch := newIconButton(theme.ViewRefreshIcon(), colorNameMuted, tip,
			color.Transparent, r.pal.btnHover, func() { act.onFetchRepo(repo) })
		fetch.disabled = sec.fetching
		header.setAction(fetch)
	}
	r.detailBox.Add(header)
	if !sec.open {
		return
	}

	switch {
	case sec.loading:
		// A text line rather than widget.Activity: the renderer's Destroy only
		// stops the row's own spinner, so an animation started in here would keep
		// running after widget.List recycled the row.
		r.detailBox.Add(r.line("Loading branches…", r.pal.faint, branchSize, false, false, false))
		return
	case sec.list == nil:
		return
	case sec.list.Err != "":
		r.detailBox.Add(r.line(sec.list.Err, r.pal.statusError, branchSize, false, false, true))
		return
	case len(sec.list.Branches) == 0:
		r.detailBox.Add(r.line("no branches", r.pal.faint, branchSize, false, false, false))
		return
	}

	limit := sec.limit
	if limit <= 0 {
		limit = branchPageSize
	}
	shown := sec.list.Branches
	if len(shown) > limit {
		shown = shown[:limit]
	}

	list := container.New(&tightVBox{gap: space3xs})
	for _, b := range shown {
		row := newBranchRow(r.tips, r.pal, b, r.setDetailHovered,
			func(bi monitor.BranchInfo) {
				if act.onPullBranch != nil {
					act.onPullBranch(repo, bi)
				}
			},
			func(bi monitor.BranchInfo) {
				if act.onTrackBranch != nil {
					act.onTrackBranch(repo, bi)
				}
			},
			func(bi monitor.BranchInfo) {
				if act.onOpenBranch != nil {
					act.onOpenBranch(repo, bi)
				}
			},
			act.onCopyBranch,
			sec.busy[b.Name])
		row.setKeyboardGuard(r.acceptsKeyboard)
		r.branchRows = append(r.branchRows, row)
		r.detailControls = append(r.detailControls, row)
		list.Add(row)

		// A refused pull explains itself directly under the branch it belongs to
		// and stays until dismissed — a modal would interrupt, and a fading
		// message would be gone before it was read.
		if msg := sec.errs[b.Name]; msg != "" {
			name := b.Name
			dismiss := newDismissRow(r.pal, msg, func() {
				if act.onDismissBranch != nil {
					act.onDismissBranch(repo, name)
				}
			})
			dismiss.setKeyboardGuard(r.acceptsKeyboard)
			r.detailControls = append(r.detailControls, dismiss)
			list.Add(dismiss)
		}
	}

	// Bound the section and let it scroll: this is what keeps the expanded row
	// from outgrowing the window when a repository has many branches.
	scroll := container.NewVScroll(list)
	h := list.MinSize().Height
	if h > branchScrollMax {
		h = branchScrollMax
	}
	scroll.SetMinSize(fyne.NewSize(0, h))
	r.detailBox.Add(scroll)

	// The "show more" control sits OUTSIDE the scroll, so it is visible without
	// scrolling to the bottom of a list of two hundred branches.
	if extra := len(sec.list.Branches) - len(shown); extra > 0 {
		next := extra
		if next > branchPageSize {
			next = branchPageSize
		}
		label := fmt.Sprintf("Show %d more  (%d hidden)", next, extra)
		more := newMoreRow(r.pal, label, func() {
			if act.onMoreBranches != nil {
				act.onMoreBranches(repo)
			}
		})
		more.setKeyboardGuard(r.acceptsKeyboard)
		r.detailControls = append(r.detailControls, more)
		r.detailBox.Add(more)
	}
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
	r.releaseBinding()
	r.detailBox.Hide()
}

func (r *repoRow) releaseBinding() {
	r.cancelKeyboardInteraction()
	r.clearMarquees()
	r.spinner.Stop()
	r.spinner.Hide()
	r.onExpand, r.onPull, r.onFresh, r.onOpen = nil, nil, nil, nil
	r.ideBtn.onOpen, r.ideBtn.onPick = nil, nil
	r.detailMade = false
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
	for _, control := range r.detailControls {
		control.setDisabled(r.pulling)
	}
	showCount := false
	if r.pulling {
		for _, button := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
			button.setDisabled(true)
		}
		r.ideBtn.setDisabled(true)
		r.pullBtn.Hide()
		r.freshBtn.Hide()
		r.openBtn.Hide()
		r.spinner.Show()
		r.spinner.Start()
	} else {
		for _, button := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
			button.setDisabled(false)
		}
		r.ideBtn.setDisabled(false)
		r.spinner.Stop()
		r.spinner.Hide()
		if r.hovered || r.interaction.focused || r.actionFocused {
			if r.repo.Behind > 0 {
				r.pullBtn.Show()
				r.freshBtn.Hide()
			} else {
				r.pullBtn.Hide()
				r.freshBtn.Show()
			}
			r.ideBtn.Show()
			r.openBtn.Show()
		} else {
			r.pullBtn.Hide()
			r.freshBtn.Hide()
			r.ideBtn.Hide()
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

func (r *repoRow) MouseDown(*desktop.MouseEvent) { r.setPressed(true) }
func (r *repoRow) MouseUp(*desktop.MouseEvent)   { r.setPressed(false) }

func (r *repoRow) setPressed(v bool) {
	if r.pulling {
		v = false
	}
	if r.interaction.setPressed(v) {
		r.Refresh()
	}
}

func (r *repoRow) FocusGained() {
	if r.interaction.setFocused(true) {
		r.updateActions()
		r.Refresh()
	}
}

func (r *repoRow) FocusLost() {
	if r.interaction.setFocused(false) {
		r.updateActions()
		r.Refresh()
	}
}

func (r *repoRow) TypedRune(rune) {}

func (r *repoRow) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name != fyne.KeySpace && ev.Name != fyne.KeyReturn && ev.Name != fyne.KeyEnter {
		return
	}
	if !r.acceptsKeyboard() {
		return
	}
	keyboardActivate(&r.interaction, r.Refresh, func() { r.Tapped(nil) })
}

func (r *repoRow) setKeyboardGuard(guard func() bool) { r.keyboardGuard = guard }

func (r *repoRow) acceptsKeyboard() bool {
	if !r.Visible() || (r.keyboardGuard != nil && !r.keyboardGuard()) {
		r.cancelKeyboardInteraction()
		return false
	}
	return true
}

func (r *repoRow) cancelKeyboardInteraction() {
	unfocusCanvasObjects(r, r.pullBtn, r.freshBtn, r.ideBtn, r.openBtn)
	r.interaction.clear()
	for _, button := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
		button.state.clear()
	}
	r.ideBtn.state.clear()
	r.actionFocused = false
	r.resetHover()
	r.updateActions()
	r.Refresh()
}

func (r *repoRow) MouseIn(ev *desktop.MouseEvent) {
	r.selfHovered = true
	r.interaction.setHovered(true)
	// The row body is hovered, so the pointer is not on a detail line; clearing this
	// here self-heals a detailHovered flag stranded by a detail rebuild under a
	// stationary cursor (a destroyed marquee never fires MouseOut).
	r.detailHovered = false
	r.recomputeHover()
	r.MouseMoved(ev)
}

// MouseMoved resolves what the pointer is over, in priority order: the
// keep-fresh badge, then an action chip, then the row itself. The row tip is
// handled before the pulling guard so the explanation stays available while a
// pull runs — that is exactly when a user wants to know what is going on.
func (r *repoRow) MouseMoved(ev *desktop.MouseEvent) {
	if !r.hovered {
		return
	}
	if r.tips != nil && r.keepBadge.Visible() && within(ev.Position, r.keepBadge) {
		r.tips.showDelayed(keepFreshTooltip(r.repo, time.Now()), r, tipDelay)
		return
	}
	if r.pulling {
		if r.tips != nil {
			r.tips.showDelayed(r.tip, r, tipDelay)
		}
		return
	}

	var hovered *iconButton
	for _, b := range []*iconButton{r.pullBtn, r.freshBtn, r.openBtn} {
		in := false
		if b.Visible() {
			in = withinBox(ev.Position, r.rightBox.Position().Add(b.Position()), b.Size())
		}
		b.setHovered(in)
		if in {
			hovered = b
		}
	}

	// The editor chip carries two zones: its corner changes the editor, the rest
	// of it opens the repository. Hit-testing happens here because the chips are
	// deliberately not Hoverable themselves — see iconButton.
	ideIn := false
	if r.ideBtn.Visible() {
		origin := r.rightBox.Position().Add(r.ideBtn.Position())
		size := r.ideBtn.Size()
		ideIn = withinBox(ev.Position, origin, size)
		alt := ideIn && ev.Position.X >= origin.X+size.Width-ideAltZone &&
			ev.Position.Y >= origin.Y+size.Height-ideAltZone
		r.ideBtn.setAltHovered(alt)
	}
	r.ideBtn.setHovered(ideIn)

	if r.tips != nil {
		switch {
		case ideIn:
			r.tips.show(r.ideBtn.currentTip(), r.ideBtn)
		case hovered != nil:
			r.tips.show(hovered.tip, hovered) // chips keep the immediate tip
		default:
			r.tips.showDelayed(r.tip, r, tipDelay)
		}
	}
}

// withinBox reports whether p falls inside a box at origin of the given size.
func withinBox(p, origin fyne.Position, size fyne.Size) bool {
	return p.X >= origin.X && p.X <= origin.X+size.Width &&
		p.Y >= origin.Y && p.Y <= origin.Y+size.Height
}

// bindIDE points the editor chip at the currently chosen editor. The choice is
// re-resolved on every bind rather than cached, so an editor deleted since the
// last paint is shown as missing instead of failing on click.
func (r *repoRow) bindIDE(repo monitor.RepoState, st rowState, act rowActions) {
	r.ideBtn.res = st.ideIcon
	r.ideBtn.missing = st.ideMissing
	r.ideBtn.tip, r.ideBtn.altTip = st.ideTip, st.ideAltTip
	r.ideBtn.onOpen = func() {
		if act.onOpenIDE != nil {
			act.onOpenIDE(repo)
		}
	}
	r.ideBtn.onPick = func() {
		if act.onPickIDE != nil {
			act.onPickIDE(repo)
		}
	}
	r.ideBtn.Refresh()
}

// within reports whether p falls inside obj's own bounds.
func within(p fyne.Position, obj fyne.CanvasObject) bool {
	o, s := obj.Position(), obj.Size()
	return p.X >= o.X && p.X <= o.X+s.Width && p.Y >= o.Y && p.Y <= o.Y+s.Height
}

// MouseOut also fires when the pointer crosses onto a detail line (a Hoverable
// child), so it clears only the body's hover — detailHovered may keep the row hovered.
func (r *repoRow) MouseOut() {
	r.pullBtn.setHovered(false)
	r.freshBtn.setHovered(false)
	r.openBtn.setHovered(false)
	r.ideBtn.setHovered(false)
	r.setPressed(false)
	if r.tips != nil {
		r.tips.hide()
	}
	r.selfHovered = false
	r.interaction.setHovered(false)
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
	r.ideBtn.setHovered(false)
	r.setPressed(false)
	r.selfHovered = false
	r.interaction.setHovered(false)
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

func (r *repoRow) setActionFocused(bool) {
	focused := r.pullBtn.state.focused || r.freshBtn.state.focused || r.ideBtn.state.focused || r.openBtn.state.focused
	if r.actionFocused == focused {
		return
	}
	r.actionFocused = focused
	r.updateActions()
	r.Refresh()
}

func (r *repoRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	return &repoRowRenderer{
		row: r,
		bg:  bg,
		objects: []fyne.CanvasObject{bg, r.chevSlot, r.glyphSlot, r.name, r.dirtyDot, r.keepBadge,
			r.branch, r.count, r.rightBox, r.detailBox},
	}
}

type repoRowRenderer struct {
	row     *repoRow
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

// line1Height is the height of the name line (glyph, name, badge, count/chips).
// It has a floor so the row keeps its proportions whatever the font metrics say.
func (rr *repoRowRenderer) line1Height() float32 {
	h := rr.row.name.MinSize().Height
	if c := rr.row.count.MinSize().Height; c > h {
		h = c
	}
	if h < lineMinH {
		h = lineMinH
	}
	return h
}

// line2Height is the height of the branch line. It is measured from a constant
// sample rather than the actual text, so a row with an empty branch is exactly
// as tall as one with a long branch name.
func (rr *repoRowRenderer) line2Height() float32 {
	return fyne.MeasureText("Ag", branchSize, rr.row.branch.TextStyle).Height
}

// titleHeight is identical for every repo row in the list.
func (rr *repoRowRenderer) titleHeight() float32 {
	return rowPadY*2 + rr.line1Height() + lineGap + rr.line2Height()
}

func (rr *repoRowRenderer) Layout(size fyne.Size) {
	rr.bg.Resize(size)
	line1 := rr.line1Height()
	top := float32(rowPadY)

	// Disclosure mark, then the status-glyph gutter, both aligned with the name.
	rr.row.chevSlot.Move(fyne.NewPos(rowPadX, top))
	rr.row.chevSlot.Resize(fyne.NewSize(chevColW, line1))
	rr.row.glyphSlot.Move(fyne.NewPos(rowPadX+chevColW+chevGap, top))
	rr.row.glyphSlot.Resize(fyne.NewSize(glyphColW, line1))

	// Line 1: name, then the keep-fresh badge, truncated so neither bleeds into
	// the reserved right edge (the behind count, or the hover chips).
	titleRight := size.Width - rowPadX - rightReserve
	badgeW := float32(0)
	if rr.row.dirtyDot.Visible() {
		badgeW += dirtyDotSize + keepBadgeGap
	}
	if rr.row.keepBadge.Visible() {
		badgeW += keepBadgeSize + keepBadgeGap
	}
	rr.row.name.Text = truncateToWidth(rr.row.fullName, titleRight-nameX-badgeW, nameSize, rr.row.name.TextStyle)
	nameSz := rr.row.name.MinSize()
	rr.row.name.Move(fyne.NewPos(nameX, top+(line1-nameSz.Height)/2))
	rr.row.name.Resize(nameSz)
	badgeX := nameX + nameSz.Width + keepBadgeGap
	if rr.row.dirtyDot.Visible() {
		rr.row.dirtyDot.Move(fyne.NewPos(badgeX, top+(line1-dirtyDotSize)/2))
		rr.row.dirtyDot.Resize(fyne.NewSize(dirtyDotSize, dirtyDotSize))
		badgeX += dirtyDotSize + keepBadgeGap
	}
	if rr.row.keepBadge.Visible() {
		rr.row.keepBadge.Move(fyne.NewPos(badgeX, top+(line1-keepBadgeSize)/2))
		rr.row.keepBadge.Resize(fyne.NewSize(keepBadgeSize, keepBadgeSize))
	}

	// Line 2: the branch (or the transient pull message), spanning the full width
	// — the count and chips sit on line 1, so nothing competes for this row.
	rr.row.branch.Text = truncateToWidth(rr.row.fullBranch, size.Width-rowPadX-nameX, branchSize, rr.row.branch.TextStyle)
	branchSz := rr.row.branch.MinSize()
	rr.row.branch.Move(fyne.NewPos(nameX, top+line1+lineGap))
	rr.row.branch.Resize(branchSz)

	// Right edge: hover action chips, else the behind count.
	rb := rr.row.rightBox.MinSize()
	rr.row.rightBox.Resize(rb)
	rr.row.rightBox.Move(fyne.NewPos(size.Width-rowPadX-rb.Width, top+(line1-rb.Height)/2))

	if rr.row.count.Visible() {
		cs := rr.row.count.MinSize()
		rr.row.count.Move(fyne.NewPos(size.Width-rowPadX-cs.Width, top+(line1-cs.Height)/2))
		rr.row.count.Resize(cs)
	}

	if rr.row.expanded {
		ty := rr.titleHeight()
		rr.row.detailBox.Move(fyne.NewPos(detailIndent, ty))
		rr.row.detailBox.Resize(fyne.NewSize(size.Width-detailIndent-rowPadX, size.Height-ty-rowPadY))
	}
}

func (rr *repoRowRenderer) MinSize() fyne.Size {
	h := rr.titleHeight()
	if rr.row.expanded {
		h += rr.row.detailBox.MinSize().Height + rowPadY
	}
	w := float32(nameX) + rr.row.name.MinSize().Width + rightReserve + rowPadX
	return fyne.NewSize(w, h)
}

func (rr *repoRowRenderer) Refresh() {
	switch {
	case rr.row.interaction.pressed:
		rr.bg.FillColor = rr.row.pal.btnHover
	case rr.row.selected:
		// Keyboard highlight wins: the accent selection tint is distinct from the
		// neutral hover/expanded tints so the user can tell where Return will land.
		rr.bg.FillColor = theme.Color(theme.ColorNameSelection)
	case rr.row.expanded:
		rr.bg.FillColor = rr.row.pal.rowExpandedBg
	case rr.row.hovered || rr.row.interaction.focused || rr.row.actionFocused:
		rr.bg.FillColor = rr.row.pal.rowHover
	default:
		rr.bg.FillColor = color.Transparent
	}
	if rr.row.noteColor != nil {
		rr.row.branch.Color = rr.row.noteColor
	} else {
		rr.row.branch.Color = rr.row.pal.faint
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
