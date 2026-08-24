package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// A branch inside a repository's expanded detail panel.
//
// Branch rows are one line and, crucially, a FIXED height: the action chips
// brighten on hover, but the row measures the same whether hovered or not. widget.List
// reads each item's height from MinSize, so a height that changed on hover would
// fire SetItemHeight on every pointer move across the section and the list would
// shudder under the cursor.

const (
	// Branch controls share the app-wide action target. The section scrolls, so
	// keeping every control comfortably hittable does not make the popover grow
	// without bound.
	branchActionBox = actionBox
	branchRowH      = 36
	branchGap2      = space2xs
	branchTextSize  = textSm

	// The section scrolls, and its scrollbar is drawn over the right edge — so
	// the chips stop short of it rather than sitting underneath.
	branchRightGutter = 10

	// branchMinLabelW is all the width a branch row ASKS for its name. It is not
	// the name's real width, and that is the whole point — see MinSize below.
	branchMinLabelW = 60
)

var branchSyncResource = fyne.NewStaticResource("branch-sync.svg", []byte(`
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
  <path d="M7 3a1 1 0 0 1 1 1v12.6l2.3-2.3a1 1 0 1 1 1.4 1.4l-4 4a1 1 0 0 1-1.4 0l-4-4a1 1 0 1 1 1.4-1.4L6 16.6V4a1 1 0 0 1 1-1Z"/>
  <path d="M17 21a1 1 0 0 1-1-1V7.4l-2.3 2.3a1 1 0 0 1-1.4-1.4l4-4a1 1 0 0 1 1.4 0l4 4a1 1 0 1 1-1.4 1.4L18 7.4V20a1 1 0 0 1-1 1Z"/>
</svg>`))

// branchRow renders one branch: its name, how far it is from its upstream, and
// how long ago it last moved.
type branchRow struct {
	widget.BaseWidget
	pal   palette
	info  monitor.BranchInfo
	busy  bool
	tips  *tooltipLayer
	label *canvas.Text
	act   *iconButton
	box   *fyne.Container

	openBtn *iconButton
	copyBtn *iconButton

	onHover        func(bool)
	onPull         func(monitor.BranchInfo)
	onTrack        func(monitor.BranchInfo)
	onOpen         func(monitor.BranchInfo)
	onCopy         func(string)
	hovered        bool
	actionFocused  bool
	parentDisabled bool
	rowWidth       float32
}

func newBranchRow(tips *tooltipLayer, pal palette, info monitor.BranchInfo,
	onHover func(bool), onPull, onTrack, onOpen func(monitor.BranchInfo), onCopy func(string),
	busy bool) *branchRow {

	b := &branchRow{pal: pal, info: info, tips: tips, busy: busy,
		onHover: onHover, onPull: onPull, onTrack: onTrack, onOpen: onOpen, onCopy: onCopy}

	b.label = canvas.NewText(branchLineText(info), pal.rowSub)
	b.label.TextStyle = fyne.TextStyle{Monospace: true}
	b.label.TextSize = branchTextSize
	if info.Current {
		b.label.Color = pal.detailKey
		b.label.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	}
	if info.RemoteOnly {
		// Dimmed: it exists on the remote but not here yet.
		b.label.Color = pal.syncedName
	}

	// A remote-only branch offers "create it locally"; every other branch gets
	// one two-way sync control whose operation follows the branch's own state.
	// Tapping the name itself never acts: a branch is too easy to touch by accident.
	switch {
	case info.RemoteOnly:
		b.act = newIconButton(theme.ContentAddIcon(), theme.ColorNamePrimary,
			"Create a local branch tracking "+info.Upstream, pal.openBtnBg, pal.btnHover,
			func() {
				if b.onTrack != nil {
					b.onTrack(b.info)
				}
			})
	case monitor.PlanBranch(info) == monitor.BranchPush:
		b.act = newIconButton(branchSyncResource, theme.ColorNamePrimary,
			syncBranchTip(info), pal.pushBtnBg, pal.btnHover,
			func() {
				if b.onPull != nil {
					b.onPull(b.info)
				}
			})
	default:
		b.act = newIconButton(branchSyncResource, theme.ColorNamePrimary,
			syncBranchTip(info), pal.pullBtnBg, pal.btnHover,
			func() {
				if b.onPull != nil {
					b.onPull(b.info)
				}
			})
	}
	b.act.size = branchActionBox
	b.act.useCompactRowChrome()
	b.openBtn = newIconButton(theme.ComputerIcon(), colorNameMuted,
		"Open "+info.Name+" worktree in the selected editor", pal.openBtnBg, pal.btnHover,
		func() {
			if b.onOpen != nil {
				b.onOpen(b.info)
			}
		})
	b.openBtn.size = branchActionBox
	b.openBtn.useCompactRowChrome()

	// Every branch offers something, whether or not it can be fast-forwarded, and
	// it offers it WITHOUT being hovered.
	//
	// Hover-revealed chips are wrong here twice over. A branch that is level with
	// its upstream showed no control at all, which reads as "branches have no
	// actions" rather than "this one has nothing to do" — and on a root with
	// autoFetch off that is every branch. And a chip that is hidden when the row
	// is first laid out has no size when it is later shown, so it paints as
	// nothing inside the expanded row however correct its geometry looks.
	// Persistent chips are laid out with everything else and simply dim when the
	// pointer is elsewhere.
	// A filled back, like every other chip in the app: a bare icon on a dark row
	// does not read as something you can press.
	b.copyBtn = newIconButton(theme.ContentCopyIcon(), colorNameMuted,
		"Copy "+info.Name, pal.openBtnBg, pal.btnHover, func() {
			if b.onCopy != nil {
				b.onCopy(b.info.Name)
			}
		})
	b.copyBtn.size = branchActionBox
	b.copyBtn.useCompactRowChrome()
	b.act.onFocusChanged = b.setActionFocused
	b.openBtn.onFocusChanged = b.setActionFocused
	b.copyBtn.onFocusChanged = b.setActionFocused
	b.box = container.NewWithoutLayout(b.label, b.copyBtn, b.openBtn, b.act)
	b.ExtendBaseWidget(b)
	return b
}

// actionable reports whether this branch offers anything to click.
//
// A local branch always does. Pulling it fetches first, so "nothing to pull" is
// an answer the button gives — not a reason to withhold it: ahead/behind counts
// are only as fresh as the last fetch, and on a root with autoFetch off they
// never move at all, so hiding the control meant no branch was ever pullable.
func (b *branchRow) actionable() bool {
	if b.info.Gone {
		return false // its upstream is gone; there is nothing to pull from
	}
	if b.info.RemoteOnly {
		return true
	}
	return b.info.Upstream != ""
}

func (b *branchRow) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	if b.onHover != nil {
		b.onHover(true)
	}
	if b.tips != nil {
		b.tips.showDelayed(branchTooltip(b.info), b, tipDelay)
	}
	b.Refresh()
}

// MouseMoved highlights whichever chip the pointer is over and swaps in that
// chip's tooltip. The chips are not Hoverable themselves — a widget that took
// hover would steal it from the row, and the row is what decides they are shown
// at all, so they would vanish as the pointer reached them.
func (b *branchRow) MouseMoved(e *desktop.MouseEvent) {
	tip := branchTooltip(b.info)
	for _, btn := range []*iconButton{b.act, b.openBtn, b.copyBtn} {
		over := btn.Visible() && e.Position.X >= btn.Position().X &&
			e.Position.X < btn.Position().X+branchActionBox
		btn.setHovered(over)
		if over && btn.tip != "" {
			tip = btn.tip
		}
	}
	if b.tips != nil {
		b.tips.showDelayed(tip, b, tipDelay)
	}
}

func (b *branchRow) MouseOut() {
	b.hovered = false
	b.act.setHovered(false)
	b.openBtn.setHovered(false)
	b.copyBtn.setHovered(false)
	if b.onHover != nil {
		b.onHover(false)
	}
	if b.tips != nil {
		b.tips.hide()
	}
	b.Refresh()
}

func (b *branchRow) setActionFocused(bool) {
	focused := b.act.state.focused || b.openBtn.state.focused || b.copyBtn.state.focused
	if b.actionFocused == focused {
		return
	}
	b.actionFocused = focused
	b.Refresh()
}

func (b *branchRow) setKeyboardGuard(guard func() bool) {
	b.act.keyboardGuard = guard
	b.openBtn.keyboardGuard = guard
	b.copyBtn.keyboardGuard = guard
}

func (b *branchRow) setDisabled(disabled bool) {
	b.parentDisabled = disabled
	b.act.setDisabled(disabled || b.busy)
	b.openBtn.setDisabled(disabled || b.busy)
	b.copyBtn.setDisabled(disabled)
	b.Refresh()
}

func (b *branchRow) deactivate() {
	unfocusCanvasObjects(b.act, b.openBtn, b.copyBtn)
	b.act.release()
	b.openBtn.release()
	b.copyBtn.release()
	b.onHover, b.onPull, b.onTrack, b.onOpen, b.onCopy = nil, nil, nil, nil, nil
	b.hovered, b.actionFocused = false, false
}

func (b *branchRow) CreateRenderer() fyne.WidgetRenderer {
	return &branchRowRenderer{b: b, objects: []fyne.CanvasObject{b.box}}
}

type branchRowRenderer struct {
	b       *branchRow
	objects []fyne.CanvasObject
}

// MinSize is hover-independent on purpose — see the note at the top of the file.
//
// The width is deliberately NOT the label's own width, and that is load-bearing.
// container.Scroll lays its content out at max(content.MinSize(), viewport) in
// BOTH axes, whatever its scroll direction: a row that asked for the full width
// of its branch name would make every row in the section that wide, and a
// section holding one long name would push its right edge — where the chips
// live — outside the viewport, where it is clipped away. The result is a list of
// branches with no controls at all and names cut off without an ellipsis, which
// is exactly what this looked like. The label truncates itself in Layout, so the
// row is happy at any width.
func (r *branchRowRenderer) MinSize() fyne.Size {
	h := float32(branchRowH)
	if ah := float32(branchActionBox); ah > h {
		h = ah
	}
	return fyne.NewSize(branchMinLabelW+3*(branchActionBox+branchGap2)+branchRightGutter, h)
}

func (r *branchRowRenderer) Layout(size fyne.Size) {
	r.b.rowWidth = size.Width
	r.b.box.Resize(size)

	// The chips sit at the right edge and stay present at lower emphasis until
	// hover or focus. They are placed right to
	// left, so the primary action is always in the same place whether or not the
	// editor and copy chips are beside it.
	x := size.Width - branchRightGutter
	y := (size.Height - branchActionBox) / 2
	for _, btn := range []*iconButton{r.b.act, r.b.openBtn, r.b.copyBtn} {
		if !btn.Visible() {
			continue
		}
		x -= branchActionBox
		btn.Move(fyne.NewPos(x, y))
		btn.Resize(fyne.NewSize(branchActionBox, branchActionBox))
		x -= branchGap2
	}
	// x has walked left past whatever chips are showing, so it is exactly the
	// width left for the label.
	r.b.label.Text = truncateToWidth(branchLineText(r.b.info), x, branchTextSize, r.b.label.TextStyle)
	ls := r.b.label.MinSize()
	r.b.label.Move(fyne.NewPos(0, (size.Height-ls.Height)/2))
	r.b.label.Resize(ls)
}

func (r *branchRowRenderer) Refresh() {
	// Only the primary chip comes and goes, and only with the branch's own state
	// — never with the pointer. Everything else is a change of emphasis.
	if r.b.actionable() {
		r.b.act.Show()
	} else {
		r.b.act.Hide()
	}
	// While a branch operation runs the chips stay in place, greyed: a control that
	// disappeared mid-click would read as the click having gone nowhere.
	r.b.act.setDisabled(r.b.busy || r.b.parentDisabled)
	r.b.openBtn.setDisabled(r.b.busy || r.b.parentDisabled)
	r.b.copyBtn.setDisabled(r.b.parentDisabled)
	r.b.act.dim = !r.b.hovered && !r.b.actionFocused && !r.b.busy
	r.b.openBtn.dim = !r.b.hovered && !r.b.actionFocused && !r.b.busy
	r.b.copyBtn.dim = !r.b.hovered && !r.b.actionFocused
	r.Layout(fyne.NewSize(r.b.rowWidth, r.MinSize().Height))
	r.b.copyBtn.Refresh()
	r.b.openBtn.Refresh()
	r.b.act.Refresh()
	r.b.label.Refresh()
}

func (r *branchRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *branchRowRenderer) Destroy()                     {}

// branchLineText is the whole of a branch row: name, distance from upstream, age.
// It stays terse because the panel is narrow — the commit subject is one hover
// away in the tooltip.
func branchLineText(b monitor.BranchInfo) string {
	var parts []string
	parts = append(parts, b.Name)
	switch {
	case b.RemoteOnly:
		// Ahead/behind are meaningless for a branch with no local counterpart.
	case b.Gone:
		parts = append(parts, "⨯ gone")
	case b.Ahead > 0 && b.Behind > 0:
		parts = append(parts, fmt.Sprintf("↑%d ↓%d", b.Ahead, b.Behind))
	case b.Behind > 0:
		parts = append(parts, fmt.Sprintf("↓%d", b.Behind))
	case b.Ahead > 0:
		parts = append(parts, fmt.Sprintf("↑%d", b.Ahead))
	}
	line := strings.Join(parts, "  ")
	if age := shortAge(b.Time); age != "" {
		line += " · " + age
	}
	return line
}

// shortAge is a very compact relative time — "2h", not "2 hours ago" — because a
// branch row has room for a few characters at most. The verbose forms live in
// humanizeTime (the detail panel) and humanizeLong (tooltips).
func shortAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}

const worktreeRowH = 46

type worktreeRow struct {
	widget.BaseWidget
	pal      palette
	info     monitor.WorktreeInfo
	tips     *tooltipLayer
	bg       *canvas.Rectangle
	title    *canvas.Text
	path     *canvas.Text
	ide      *iconButton
	folder   *iconButton
	box      *fyne.Container
	onHover  func(bool)
	hovered  bool
	disabled bool
	width    float32
}

func newWorktreeRow(tips *tooltipLayer, pal palette, info monitor.WorktreeInfo,
	onHover func(bool), onIDE, onFolder func(monitor.WorktreeInfo)) *worktreeRow {
	w := &worktreeRow{pal: pal, info: info, tips: tips, onHover: onHover}
	w.bg = canvas.NewRectangle(color.Transparent)
	w.title = canvas.NewText(worktreeLineText(info), pal.rowSub)
	w.title.TextSize = branchTextSize
	w.title.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	w.path = canvas.NewText(collapseHome(info.Path), pal.faint)
	w.path.TextSize = textXs
	w.path.TextStyle = fyne.TextStyle{Monospace: true}
	w.ide = newIconButton(theme.ComputerIcon(), colorNameMuted,
		"Open this worktree in the selected editor", pal.openBtnBg, pal.btnHover,
		func() {
			if onIDE != nil {
				onIDE(w.info)
			}
		})
	w.folder = newIconButton(theme.FolderOpenIcon(), colorNameMuted,
		"Open this worktree folder", pal.openBtnBg, pal.btnHover,
		func() {
			if onFolder != nil {
				onFolder(w.info)
			}
		})
	for _, button := range []*iconButton{w.ide, w.folder} {
		button.size = branchActionBox
		button.useCompactRowChrome()
	}
	w.box = container.NewWithoutLayout(w.bg, w.title, w.path, w.ide, w.folder)
	w.ExtendBaseWidget(w)
	return w
}

func (w *worktreeRow) MouseIn(*desktop.MouseEvent) {
	w.hovered = true
	w.bg.FillColor = w.pal.rowHover
	if w.onHover != nil {
		w.onHover(true)
	}
	if w.tips != nil {
		w.tips.showDelayed(worktreeTooltip(w.info), w, tipDelay)
	}
	w.Refresh()
}

func (w *worktreeRow) MouseMoved(e *desktop.MouseEvent) {
	tip := worktreeTooltip(w.info)
	for _, button := range []*iconButton{w.ide, w.folder} {
		over := withinBox(e.Position, button.Position(), button.Size())
		button.setHovered(over)
		if over {
			tip = button.tip
		}
	}
	if w.tips != nil {
		w.tips.showDelayed(tip, w, tipDelay)
	}
}

func (w *worktreeRow) MouseOut() {
	w.hovered = false
	w.bg.FillColor = color.Transparent
	w.ide.setHovered(false)
	w.folder.setHovered(false)
	if w.onHover != nil {
		w.onHover(false)
	}
	if w.tips != nil {
		w.tips.hide()
	}
	w.Refresh()
}

func (w *worktreeRow) setKeyboardGuard(guard func() bool) {
	w.ide.keyboardGuard = guard
	w.folder.keyboardGuard = guard
}

func (w *worktreeRow) setDisabled(disabled bool) {
	w.disabled = disabled
	w.ide.setDisabled(disabled)
	w.folder.setDisabled(disabled)
}

func (w *worktreeRow) deactivate() {
	w.ide.release()
	w.folder.release()
	w.onHover = nil
}

func (w *worktreeRow) CreateRenderer() fyne.WidgetRenderer {
	return &worktreeRowRenderer{w: w, objects: []fyne.CanvasObject{w.box}}
}

type worktreeRowRenderer struct {
	w       *worktreeRow
	objects []fyne.CanvasObject
}

func (r *worktreeRowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(branchMinLabelW+2*(branchActionBox+branchGap2)+branchRightGutter, worktreeRowH)
}

func (r *worktreeRowRenderer) Layout(size fyne.Size) {
	r.w.width = size.Width
	r.w.box.Resize(size)
	r.w.bg.Resize(size)
	x := size.Width - branchRightGutter
	y := (size.Height - branchActionBox) / 2
	for _, button := range []*iconButton{r.w.folder, r.w.ide} {
		x -= branchActionBox
		button.Move(fyne.NewPos(x, y))
		button.Resize(fyne.NewSize(branchActionBox, branchActionBox))
		x -= branchGap2
	}
	textWidth := x - spaceXs
	r.w.title.Text = truncateToWidth(worktreeLineText(r.w.info), textWidth, branchTextSize, r.w.title.TextStyle)
	r.w.path.Text = truncateToWidth(collapseHome(r.w.info.Path), textWidth, textXs, r.w.path.TextStyle)
	r.w.title.Move(fyne.NewPos(spaceXs, 5))
	r.w.title.Resize(r.w.title.MinSize())
	r.w.path.Move(fyne.NewPos(spaceXs, 24))
	r.w.path.Resize(r.w.path.MinSize())
}

func (r *worktreeRowRenderer) Refresh() {
	if r.w.hovered {
		r.w.bg.FillColor = r.w.pal.rowHover
	} else {
		r.w.bg.FillColor = color.Transparent
	}
	r.w.ide.dim = !r.w.hovered
	r.w.folder.dim = !r.w.hovered
	r.w.ide.setDisabled(r.w.disabled)
	r.w.folder.setDisabled(r.w.disabled)
	r.Layout(fyne.NewSize(r.w.width, r.MinSize().Height))
	r.w.bg.Refresh()
	r.w.title.Refresh()
	r.w.path.Refresh()
	r.w.ide.Refresh()
	r.w.folder.Refresh()
}

func (r *worktreeRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *worktreeRowRenderer) Destroy()                     {}

func worktreeLineText(w monitor.WorktreeInfo) string {
	name := w.Branch
	if w.Detached || name == "" {
		name = "detached"
		if w.Head != "" {
			name += " @ " + w.Head
		}
	}
	var status []string
	changes := w.Staged + w.Modified + w.Deleted + w.Untracked
	switch {
	case w.Err != "":
		status = append(status, "error")
	case w.Prunable != "":
		status = append(status, "prunable")
	case w.Conflicts > 0:
		status = append(status, fmt.Sprintf("%d conflicts", w.Conflicts))
	case w.Operation != "":
		status = append(status, w.Operation)
	case changes > 0:
		status = append(status, fmt.Sprintf("%d changes", changes))
	}
	if w.Ahead > 0 {
		status = append(status, fmt.Sprintf("↑%d", w.Ahead))
	}
	if w.Behind > 0 {
		status = append(status, fmt.Sprintf("↓%d", w.Behind))
	}
	if len(status) == 0 {
		return name
	}
	return name + "  · " + strings.Join(status, "  ")
}

func worktreeTooltip(w monitor.WorktreeInfo) string {
	lines := []string{worktreeLineText(w), collapseHome(w.Path)}
	if w.Locked != "" {
		lines = append(lines, "Locked: "+w.Locked)
	}
	if w.Prunable != "" {
		lines = append(lines, "Prunable: "+w.Prunable)
	}
	if w.Err != "" {
		lines = append(lines, w.Err)
	}
	return strings.Join(lines, "\n")
}

func worktreeSectionText(sec worktreeSectionState) string {
	if sec.list == nil || sec.list.Err != "" {
		return "Worktrees"
	}
	dirty := 0
	for _, worktree := range sec.list.Worktrees {
		if worktree.Dirty || worktree.Conflicts > 0 || worktree.Operation != "" {
			dirty++
		}
	}
	if dirty > 0 {
		return fmt.Sprintf("Worktrees (%d · %d dirty)", len(sec.list.Worktrees), dirty)
	}
	return fmt.Sprintf("Worktrees (%d)", len(sec.list.Worktrees))
}

// branchTooltip carries what the one-line row could not: the commit subject, the
// upstream, and why a branch cannot be pulled.
func branchTooltip(b monitor.BranchInfo) string {
	var lines []string
	if b.Subject != "" {
		lines = append(lines, b.Subject)
	} else {
		lines = append(lines, b.Name)
	}
	meta := b.Hash
	if !b.Time.IsZero() {
		if meta != "" {
			meta += " · "
		}
		meta += humanizeLong(b.Time, time.Now())
	}
	if b.Upstream != "" {
		if meta != "" {
			meta += " · "
		}
		meta += b.Upstream
	}
	if meta != "" {
		lines = append(lines, meta)
	}
	switch {
	case b.RemoteOnly:
		lines = append(lines, "Only on the remote — create a local branch")
	case b.Gone:
		lines = append(lines, "Its upstream branch no longer exists")
	case b.Worktree != "":
		lines = append(lines, "Checked out at "+collapseHome(b.Worktree)+" — pulling advances it there")
	case b.Upstream == "":
		lines = append(lines, "No upstream branch set")
	}
	return strings.Join(lines, "\n")
}

// branchSectionText is the collapsible header's label. The count only appears
// once the branches have actually been read — before that, saying a number would
// mean running git for every repository in the list, which is exactly what the
// lazy section avoids.
func branchSectionText(sec branchSectionState) string {
	if sec.list == nil || sec.list.Err != "" {
		return "Branches"
	}
	// The behind count is the part worth reading: in a repository with two
	// hundred branches, "7 behind" is the difference between a list to scroll
	// and a list to act on. Those seven are sorted to the top.
	behind := 0
	for _, b := range sec.list.Branches {
		if b.Behind > 0 && !b.RemoteOnly {
			behind++
		}
	}
	if behind > 0 {
		return fmt.Sprintf("Branches (%d · %d behind)", len(sec.list.Branches), behind)
	}
	return fmt.Sprintf("Branches (%d)", len(sec.list.Branches))
}

// dismissRow is an inline error under a branch, with a control to clear it. It
// stays until dismissed rather than fading, because a refused pull is something
// the user has to decide about.
type dismissRow struct {
	widget.BaseWidget
	pal     palette
	msg     string
	onClear func()
	text    *canvas.Text
	close   *iconButton
	box     *fyne.Container
	width   float32
}

func (d *dismissRow) deactivate() {
	d.close.release()
	d.onClear = nil
}

func (d *dismissRow) setKeyboardGuard(guard func() bool) { d.close.keyboardGuard = guard }
func (d *dismissRow) setDisabled(disabled bool)          { d.close.setDisabled(disabled) }

func (d *dismissRow) MouseIn(e *desktop.MouseEvent) { d.MouseMoved(e) }
func (d *dismissRow) MouseMoved(e *desktop.MouseEvent) {
	d.close.setHovered(withinBox(e.Position, d.close.Position(), d.close.Size()))
}
func (d *dismissRow) MouseOut() { d.close.setHovered(false) }

func newDismissRow(pal palette, msg string, onClear func()) *dismissRow {
	d := &dismissRow{pal: pal, msg: msg, onClear: onClear}
	d.text = canvas.NewText(msg, pal.statusError)
	d.text.TextSize = branchSize
	d.close = newIconButton(theme.CancelIcon(), colorNameMuted, "Dismiss",
		color.Transparent, pal.btnHover, onClear)
	d.box = container.NewWithoutLayout(d.text, d.close)
	d.ExtendBaseWidget(d)
	return d
}

func (d *dismissRow) CreateRenderer() fyne.WidgetRenderer {
	return &dismissRowRenderer{d: d, objects: []fyne.CanvasObject{d.box}}
}

type dismissRowRenderer struct {
	d       *dismissRow
	objects []fyne.CanvasObject
}

// MinSize asks for a floor, not for the message's real width — the message
// truncates in Layout, and demanding its full width would widen the whole
// scrolled section. See branchRowRenderer.MinSize.
func (r *dismissRowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(branchMinLabelW+branchActionBox+branchGap2, branchActionBox)
}

func (r *dismissRowRenderer) Layout(size fyne.Size) {
	r.d.width = size.Width
	r.d.box.Resize(size)
	avail := size.Width - branchActionBox - branchGap2
	r.d.text.Text = truncateToWidth(r.d.msg, avail, branchSize, r.d.text.TextStyle)
	ts := r.d.text.MinSize()
	r.d.text.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
	r.d.text.Resize(ts)
	r.d.close.Move(fyne.NewPos(size.Width-branchActionBox, (size.Height-branchActionBox)/2))
	r.d.close.Resize(fyne.NewSize(branchActionBox, branchActionBox))
}

func (r *dismissRowRenderer) Refresh() {
	r.d.text.Color = r.d.pal.statusError
	r.d.text.Refresh()
	r.d.close.Refresh()
}

func (r *dismissRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *dismissRowRenderer) Destroy()                     {}

// sectionHeader is the tappable "Branches (12)" line that folds the section.
// It reuses the same chevrons as the scan-root group headers so the gesture
// reads as the same kind of thing.
type sectionHeader struct {
	widget.BaseWidget
	pal           palette
	text          string
	open          bool
	onTap         func()
	onHover       func(bool)
	label         *canvas.Text
	chev          *fyne.Container
	action        *iconButton // optional trailing control, e.g. "fetch this repo"
	box           *fyne.Container
	state         interactionState
	actionFocused bool
	keyboardGuard func() bool
	disabled      bool
	rowWidth      float32
}

func newSectionHeader(pal palette, text string, open bool, onTap func()) *sectionHeader {
	h := &sectionHeader{pal: pal, text: text, open: open, onTap: onTap}
	h.label = canvas.NewText(text, pal.detailKey)
	h.label.TextSize = branchSize
	h.label.TextStyle = fyne.TextStyle{Bold: true}
	kind := glyphChevronRight
	if open {
		kind = glyphChevron
	}
	h.chev = container.NewStack(newGlyph(kind, pal.faint, 11, 1.9))
	h.box = container.NewWithoutLayout(h.chev, h.label)
	h.ExtendBaseWidget(h)
	return h
}

// setAction hangs a control off the right edge of the header. Unlike the row
// chips it is always visible: it is how a repository's branch distances are made
// current, and a control you have to hover to discover is one the user never
// finds.
func (h *sectionHeader) setAction(btn *iconButton) {
	h.action = btn
	btn.size = branchActionBox
	btn.onFocusChanged = h.setActionFocused
	btn.keyboardGuard = h.acceptsKeyboard
	h.box.Add(btn)
}

func (h *sectionHeader) Tapped(*fyne.PointEvent) {
	if h.disabled {
		return
	}
	if h.onTap != nil {
		h.onTap()
	}
}

func (h *sectionHeader) MouseIn(*desktop.MouseEvent) {
	if h.disabled {
		return
	}
	h.state.setHovered(true)
	if h.onHover != nil {
		h.onHover(true)
	}
	h.Refresh()
}
func (h *sectionHeader) MouseMoved(e *desktop.MouseEvent) {
	if h.action == nil {
		return
	}
	h.action.setHovered(e.Position.X >= h.rowWidth-branchActionBox)
}
func (h *sectionHeader) MouseOut() {
	h.state.setHovered(false)
	h.state.setPressed(false)
	if h.action != nil {
		h.action.setHovered(false)
	}
	if h.onHover != nil {
		h.onHover(false)
	}
	h.Refresh()
}

func (h *sectionHeader) MouseDown(*desktop.MouseEvent) {
	if h.disabled {
		return
	}
	if h.state.setPressed(true) {
		h.Refresh()
	}
}

func (h *sectionHeader) MouseUp(*desktop.MouseEvent) {
	if h.state.setPressed(false) {
		h.Refresh()
	}
}

func (h *sectionHeader) FocusGained() {
	if h.disabled {
		h.state.clear()
		unfocusCanvasObjects(h)
		return
	}
	if h.state.setFocused(true) {
		h.Refresh()
	}
}

func (h *sectionHeader) FocusLost() {
	if h.state.setFocused(false) {
		h.Refresh()
	}
}

func (h *sectionHeader) TypedRune(rune) {}

func (h *sectionHeader) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn || ev.Name == fyne.KeyEnter {
		if !h.acceptsKeyboard() {
			return
		}
		keyboardActivate(&h.state, h.Refresh, func() { h.Tapped(nil) })
	}
}

func (h *sectionHeader) setKeyboardGuard(guard func() bool) { h.keyboardGuard = guard }

func (h *sectionHeader) acceptsKeyboard() bool {
	if h.disabled || !h.Visible() || (h.keyboardGuard != nil && !h.keyboardGuard()) {
		h.state.clear()
		unfocusCanvasObjects(h)
		return false
	}
	return true
}

func (h *sectionHeader) setDisabled(disabled bool) {
	h.disabled = disabled
	if disabled {
		h.state.clear()
		unfocusCanvasObjects(h)
	}
	if h.action != nil {
		h.action.setDisabled(disabled)
	}
	h.Refresh()
}

func (h *sectionHeader) setActionFocused(bool) {
	focused := h.action != nil && h.action.state.focused
	if h.actionFocused == focused {
		return
	}
	h.actionFocused = focused
	h.Refresh()
}

func (h *sectionHeader) deactivate() {
	unfocusCanvasObjects(h)
	h.state.clear()
	if h.action != nil {
		h.action.release()
	}
	h.onTap, h.onHover = nil, nil
	h.keyboardGuard = nil
	h.actionFocused = false
}

func (h *sectionHeader) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = radiusSm
	return &sectionHeaderRenderer{h: h, bg: bg, objects: []fyne.CanvasObject{bg, h.box}}
}

type sectionHeaderRenderer struct {
	h       *sectionHeader
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *sectionHeaderRenderer) MinSize() fyne.Size {
	w := r.h.label.MinSize().Width + 16
	if r.h.action != nil {
		w += branchActionBox + branchGap2
	}
	return fyne.NewSize(w, branchRowH)
}

func (r *sectionHeaderRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.h.rowWidth = size.Width
	r.h.box.Resize(size)
	r.h.chev.Move(fyne.NewPos(0, (size.Height-11)/2))
	r.h.chev.Resize(fyne.NewSize(11, 11))
	ls := r.h.label.MinSize()
	r.h.label.Move(fyne.NewPos(16, (size.Height-ls.Height)/2))
	r.h.label.Resize(ls)
	if r.h.action != nil {
		r.h.action.Move(fyne.NewPos(size.Width-branchActionBox, (size.Height-branchActionBox)/2))
		r.h.action.Resize(fyne.NewSize(branchActionBox, branchActionBox))
	}
}

func (r *sectionHeaderRenderer) Refresh() {
	switch {
	case r.h.disabled:
		r.bg.FillColor = color.Transparent
	case r.h.state.pressed:
		r.bg.FillColor = r.h.pal.btnHover
	case r.h.state.active() || r.h.actionFocused:
		r.bg.FillColor = r.h.pal.rowHover
	default:
		r.bg.FillColor = color.Transparent
	}
	if r.h.state.focused {
		r.bg.StrokeColor = r.h.pal.accent
		r.bg.StrokeWidth = hairlineW
	} else {
		r.bg.StrokeWidth = 0
	}
	if r.h.disabled {
		r.h.label.Color = r.h.pal.faint
	} else if r.h.state.active() || r.h.actionFocused {
		r.h.label.Color = r.h.pal.accent
	} else {
		r.h.label.Color = r.h.pal.detailKey
	}
	r.bg.Refresh()
	r.h.label.Refresh()
}

func (r *sectionHeaderRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *sectionHeaderRenderer) Destroy()                     {}

// moreRow is the tappable line at the foot of a truncated branch list.
//
// It used to be plain text reading "+130 more", drawn as the last item INSIDE
// the section's scroll area — so it neither did anything when clicked nor could
// be reached without scrolling past every branch above it. It is now a control,
// and it lives outside the scroll where it is always in view.
type moreRow struct {
	widget.BaseWidget
	pal           palette
	text          string
	onTap         func()
	label         *canvas.Text
	box           *fyne.Container
	state         interactionState
	keyboardGuard func() bool
	disabled      bool
	rowWidth      float32
}

func newMoreRow(pal palette, text string, onTap func()) *moreRow {
	m := &moreRow{pal: pal, text: text, onTap: onTap}
	m.label = canvas.NewText(text, pal.accent)
	m.label.TextSize = branchSize
	m.box = container.NewWithoutLayout(m.label)
	m.ExtendBaseWidget(m)
	return m
}

func (m *moreRow) Tapped(*fyne.PointEvent) {
	if m.disabled {
		return
	}
	if m.onTap != nil {
		m.onTap()
	}
}

func (m *moreRow) MouseIn(*desktop.MouseEvent) {
	if m.disabled {
		return
	}
	m.state.setHovered(true)
	m.Refresh()
}
func (m *moreRow) MouseMoved(*desktop.MouseEvent) {}
func (m *moreRow) MouseOut() {
	m.state.setHovered(false)
	m.state.setPressed(false)
	m.Refresh()
}
func (m *moreRow) MouseDown(*desktop.MouseEvent) {
	if m.disabled {
		return
	}
	m.state.setPressed(true)
	m.Refresh()
}
func (m *moreRow) MouseUp(*desktop.MouseEvent) { m.state.setPressed(false); m.Refresh() }
func (m *moreRow) FocusGained() {
	if m.disabled {
		m.state.clear()
		unfocusCanvasObjects(m)
		return
	}
	m.state.setFocused(true)
	m.Refresh()
}
func (m *moreRow) FocusLost()     { m.state.setFocused(false); m.Refresh() }
func (m *moreRow) TypedRune(rune) {}
func (m *moreRow) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn || ev.Name == fyne.KeyEnter {
		if !m.acceptsKeyboard() {
			return
		}
		keyboardActivate(&m.state, m.Refresh, func() { m.Tapped(nil) })
	}
}

func (m *moreRow) setKeyboardGuard(guard func() bool) { m.keyboardGuard = guard }

func (m *moreRow) acceptsKeyboard() bool {
	if m.disabled || !m.Visible() || (m.keyboardGuard != nil && !m.keyboardGuard()) {
		m.state.clear()
		unfocusCanvasObjects(m)
		return false
	}
	return true
}

func (m *moreRow) setDisabled(disabled bool) {
	m.disabled = disabled
	if disabled {
		m.state.clear()
		unfocusCanvasObjects(m)
	}
	m.Refresh()
}

func (m *moreRow) deactivate() {
	unfocusCanvasObjects(m)
	m.state.clear()
	m.onTap = nil
	m.keyboardGuard = nil
}

func (m *moreRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = radiusSm
	return &moreRowRenderer{m: m, bg: bg, objects: []fyne.CanvasObject{bg, m.box}}
}

type moreRowRenderer struct {
	m       *moreRow
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *moreRowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.m.label.MinSize().Width, branchRowH)
}

func (r *moreRowRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.m.rowWidth = size.Width
	r.m.box.Resize(size)
	ls := r.m.label.MinSize()
	r.m.label.Move(fyne.NewPos(16, (size.Height-ls.Height)/2))
	r.m.label.Resize(ls)
}

func (r *moreRowRenderer) Refresh() {
	r.m.label.Text = r.m.text
	if r.m.disabled {
		r.m.label.Color = r.m.pal.syncedName
	} else if r.m.state.active() {
		r.m.label.Color = r.m.pal.accent
	} else {
		r.m.label.Color = r.m.pal.faint
	}
	switch {
	case r.m.disabled:
		r.bg.FillColor = color.Transparent
		r.bg.StrokeWidth = 0
	case r.m.state.pressed:
		r.bg.FillColor = r.m.pal.btnHover
		r.bg.StrokeColor = r.m.pal.accent
		r.bg.StrokeWidth = 2
	case r.m.state.active():
		r.bg.FillColor = r.m.pal.rowHover
		if r.m.state.focused {
			r.bg.StrokeColor = r.m.pal.accent
			r.bg.StrokeWidth = hairlineW
		} else {
			r.bg.StrokeWidth = 0
		}
	default:
		r.bg.FillColor = color.Transparent
		r.bg.StrokeWidth = 0
	}
	r.bg.Refresh()
	r.m.label.Refresh()
}

func (r *moreRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *moreRowRenderer) Destroy()                     {}

// syncBranchTip words the chip for what it will actually do — the decision is
// PlanBranch's, so the sentence and the action can never drift apart.
func syncBranchTip(b monitor.BranchInfo) string {
	switch monitor.PlanBranch(b) {
	case monitor.BranchPush:
		return fmt.Sprintf("Push %s — %d ahead of %s", b.Name, b.Ahead, b.Upstream)
	case monitor.BranchMerge:
		return fmt.Sprintf("Merge %s into %s — diverged (↑%d ↓%d), runs in %s",
			b.Upstream, b.Name, b.Ahead, b.Behind, collapseHome(b.Worktree))
	case monitor.BranchBlocked:
		return fmt.Sprintf("%s has diverged (↑%d ↓%d) and is checked out nowhere — "+
			"check it out to merge or rebase", b.Name, b.Ahead, b.Behind)
	case monitor.BranchPull:
		if b.Worktree != "" {
			return fmt.Sprintf("Pull %s — %d behind, advances the worktree at %s",
				b.Name, b.Behind, collapseHome(b.Worktree))
		}
		return fmt.Sprintf("Pull %s — %d behind, no checkout", b.Name, b.Behind)
	default:
		return "Sync " + b.Name + " — fetch, then pull or push, whichever it needs"
	}
}
