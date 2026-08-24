package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

// searchEntry is the popover's search field. It intercepts Escape so the popover
// can close even while the entry has keyboard focus — Fyne only routes the
// canvas-level key handler when nothing is focused, so a plain Entry would
// otherwise swallow Escape.
type searchEntry struct {
	widget.Entry
	onEscape func()
	// onNav gets first look at every key so the list can be driven from the
	// keyboard while the entry keeps focus (Up/Down/Return/Tab). It reports
	// whether it consumed the key; unconsumed keys fall through to the Entry.
	onNav func(*fyne.KeyEvent) bool
}

func newSearchEntry(onChanged func(string), onEscape func()) *searchEntry {
	e := &searchEntry{onEscape: onEscape}
	e.ExtendBaseWidget(e)
	e.SetPlaceHolder("Search repositories…")
	e.OnChanged = onChanged
	return e
}

func (e *searchEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if e.onEscape != nil {
			e.onEscape()
		}
		return
	}
	if e.onNav != nil && e.onNav(ev) {
		return
	}
	e.Entry.TypedKey(ev)
}

// AcceptsTab keeps Tab inside the entry so it reaches onNav (expand details)
// instead of triggering focus traversal — the search field is the popover's
// only focusable control anyway.
func (e *searchEntry) AcceptsTab() bool { return true }

func newPopoverSearchField(entry *searchEntry) fyne.CanvasObject {
	return container.NewThemeOverride(entry, searchTheme(entry.Theme()))
}

// buildPopover constructs the borderless search popover that the tray icon
// toggles: a search field and "more" menu across the top, the repo list in the
// middle, and a summary label along the bottom. A splash (undecorated) window is
// used so a tray tap toggles it like a real popover; on platforms without splash
// support we fall back to a regular window. The window is created once; its
// content is (re)built by buildPopoverContent so a theme change can repaint it.
func (a *App) buildPopover() {
	var w fyne.Window
	if useSplashPopover() {
		drv := a.fyneApp.Driver().(desktop.Driver)
		w = drv.CreateSplashWindow()
	} else {
		w = a.fyneApp.NewWindow("Git Repositories")
	}
	a.win = w
	w.SetTitle(popoverTitle) // how the macOS native helper finds this NSWindow
	w.Resize(fyne.NewSize(popoverWidth, popoverMaxHeight))
	w.SetCloseIntercept(a.hidePopover) // hide instead of quitting the app
	a.buildPopoverContent()
}

func useSplashPopover() bool {
	// The splash-window popover depends on macOS native helpers for placement,
	// focus-loss dismissal and top-anchored resizing. On Linux, use a normal window
	// and let the window manager place it predictably.
	return runtime.GOOS == "darwin"
}

// buildPopoverContent builds (or rebuilds) the popover's widgets and sets them as
// the window content, using the current palette. It's safe to call again after a
// theme change: the custom colours below are baked into canvas objects, so they
// only pick up a new variant when the content is recreated here.
func (a *App) buildPopoverContent() {
	// Build the entry without its OnChanged first: restoring the preserved query via
	// SetText below must not fire applyFilter against the half-built content (stale
	// list/footer, premature resize). Wire OnChanged once the text is in place.
	a.search = newSearchEntry(nil, a.hidePopover)
	if a.query != "" {
		a.search.SetText(a.query) // preserve the active filter across a content rebuild
	}
	a.search.OnChanged = func(s string) {
		a.query = s
		if strings.TrimSpace(s) != "" {
			a.ensureBranchIndex()
		}
		a.applyFilter()
	}
	a.search.onNav = a.handleNavKey

	a.tips = newTooltipLayer(a.pal)
	a.updateAllBtn = newHeaderButton(a.tips, a.pal, theme.DownloadIcon(),
		"Update all (pull every repo that's behind)", a.updateAll)
	settingsBtn := newHeaderButton(a.tips, a.pal, theme.SettingsIcon(), "Settings", a.showSettings)
	a.moreBtn = newHeaderButton(a.tips, a.pal, theme.MenuIcon(), "Show/hide filtering header", a.toggleOptions)
	right := container.New(&actionClusterLayout{}, a.updateAllBtn, settingsBtn, a.moreBtn)
	searchRow := container.New(&searchToolbarLayout{}, newPopoverSearchField(a.search), right)

	// The filter/sort toggles live in a panel below the search row that the ☰ button
	// expands. It's part of the header, so the popover's height math (which measures
	// a.header) grows to include it automatically when shown.
	a.optionsPanel = a.buildOptionsPanel()
	a.optionsPanel.Hide()
	a.header = container.NewVBox(searchRow, a.optionsPanel)

	// Footer: repo/behind totals on the left, scan-root count on the right (3a).
	a.footerLeft = canvas.NewText("", a.pal.faint)
	a.footerLeft.TextStyle = fyne.TextStyle{Monospace: true}
	a.footerLeft.TextSize = textXs
	a.footerRight = canvas.NewText("", a.pal.faint)
	a.footerRight.TextStyle = fyne.TextStyle{Monospace: true}
	a.footerRight.TextSize = textXs
	// The status bar needs a surface of its own. Without one it is just text over
	// the backdrop, and a list row scrolled underneath shows straight through it.
	footerBg := canvas.NewRectangle(a.pal.footerBg)
	a.footer = container.NewStack(
		footerBg,
		container.NewPadded(container.NewHBox(a.footerLeft, layout.NewSpacer(), a.footerRight)),
	)

	a.list = widget.NewList(a.listLen, a.listCreate, a.listUpdate)
	a.list.HideSeparators = true
	// No list.OnSelected: rows handle their own tap (expand) + hover buttons.

	content := container.NewBorder(a.header, a.footer, nil, nil, a.list)

	// Glassy backdrop: a gradient under the content. On macOS the window itself is
	// rounded + shadowed natively (see placePopover); the gradient is clipped to
	// those rounded corners. True OS translucency isn't reachable via stock Fyne,
	// so this evokes the look instead (see glassTheme).
	bg := canvas.NewLinearGradient(a.pal.gradTop, a.pal.gradBottom, 135)
	a.win.SetContent(a.tips.wrap(container.NewStack(bg, container.NewPadded(content))))

	// Escape hides the window without quitting.
	a.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			a.hidePopover()
		}
	})

	// Focus the search up front so keystrokes land there when the popover is
	// toggled open via the tray icon (Fyne routes typed runes to the canvas's
	// focused object).
	a.win.Canvas().Focus(a.search)
	a.applyFilter() // populate rows + status for the freshly built content
}

// hidePopover hides the window and clears the visibility flag the tray toggle
// reads. The options panel is collapsed too, so the popover reopens lean rather
// than remembering an expanded panel from last time.
func (a *App) hidePopover() {
	if a.resizeAnim != nil {
		a.resizeAnim.Stop() // don't keep resizing a hidden window
		a.resizeAnim = nil
	}
	if a.optionsPanel != nil {
		a.optionsPanel.Hide()
	}
	a.clearSelection()
	a.win.Hide()
	a.popVisible = false
}

// handleNavKey drives the repo list from the keyboard while the search field
// keeps focus: Up/Down move the highlight across repo rows (section headers are
// skipped), Return opens the highlighted repo with the configured click action
// and dismisses the popover, Tab expands/collapses its detail panel. Left/Right
// are deliberately left to the entry so the caret still works.
func (a *App) handleNavKey(ev *fyne.KeyEvent) bool {
	switch ev.Name {
	case fyne.KeyDown:
		a.moveSelection(1)
	case fyne.KeyUp:
		a.moveSelection(-1)
	case fyne.KeyReturn, fyne.KeyEnter:
		if r, ok := a.selectedRepo(); ok {
			a.activate(r)
			a.hidePopover()
		}
	case fyne.KeyTab:
		if r, ok := a.selectedRepo(); ok {
			a.toggleExpand(r)
		}
	default:
		return false
	}
	return true
}

// selectedRepo returns the keyboard-highlighted repo, if any.
func (a *App) selectedRepo() (monitor.RepoState, bool) {
	if a.selIdx < 0 || a.selIdx >= len(a.visible) || a.visible[a.selIdx].header {
		return monitor.RepoState{}, false
	}
	return a.visible[a.selIdx].repo, true
}

// moveSelection steps the highlight by delta over repo rows, skipping group
// headers and clamping at either end. With no current selection, Down lands on
// the first repo.
func (a *App) moveSelection(delta int) {
	n := len(a.visible)
	if n == 0 {
		return
	}
	i := a.selIdx
	for {
		i += delta
		if i < 0 || i >= n {
			return
		}
		if !a.visible[i].header {
			break
		}
	}
	a.setSelection(i)
}

// setSelection highlights visible[i] (or nothing for -1), repainting only the rows
// that changed and scrolling the new one into view.
func (a *App) setSelection(i int) {
	prev := a.selIdx
	a.selIdx = i
	a.selPath = ""
	if i >= 0 && i < len(a.visible) && !a.visible[i].header {
		a.selPath = a.visible[i].repo.Path
	}
	if a.list == nil {
		return
	}
	if prev >= 0 && prev < len(a.visible) && prev != i {
		a.list.RefreshItem(prev)
	}
	if i >= 0 && i < len(a.visible) {
		a.list.RefreshItem(i)
		a.list.ScrollTo(i)
	}
}

func (a *App) clearSelection() { a.setSelection(-1) }

// reconcileSelection re-derives selIdx after the visible rows changed: the same
// repo stays highlighted if it is still listed; otherwise, while a query is
// active, the first match is highlighted so Return opens it immediately.
func (a *App) reconcileSelection() {
	a.selIdx = -1
	first := -1
	for i, it := range a.visible {
		if it.header {
			continue
		}
		if first < 0 {
			first = i
		}
		if a.selPath != "" && it.repo.Path == a.selPath {
			a.selIdx = i
			return
		}
	}
	if strings.TrimSpace(a.query) != "" && first >= 0 {
		a.selIdx = first
		a.selPath = a.visible[first].repo.Path
		return
	}
	a.selPath = ""
}

// showWindow brings the popover to the front, positioned next to the tray icon,
// with fresh data and the search field focused so the user can type immediately.
// The window is sized to its content (capped at popoverMaxHeight) so a short list
// stays lean instead of leaving dead space below the last row.
func (a *App) showWindow() {
	a.maybeFollowSystemTheme() // pick up an OS light/dark flip before showing
	if a.resizeAnim != nil {
		a.resizeAnim.Stop() // don't carry a stale animation across a hide/show
		a.resizeAnim = nil
	}
	a.refresh()
	// Warm the branch-name index so a search can find a branch in a repository
	// that has never been expanded. It is a background pass and costs nothing
	// when everything is already indexed.
	a.ensureBranchIndex()
	h := a.desiredPopoverHeight()
	a.popoverH = h
	a.win.Resize(fyne.NewSize(popoverWidth, h))
	a.win.Show()
	a.popVisible = true
	placePopover(popoverTitle, popoverWidth, h)
	a.win.RequestFocus()
	if a.search != nil {
		a.win.Canvas().Focus(a.search)
	}
}

// maybeFollowSystemTheme rebuilds the popover for the current OS appearance when
// the theme is set to "system" and the OS has since flipped light/dark. Fyne
// repaints its own widgets on an OS change, but the popover's custom colours are
// baked into canvas objects, so they only update when the content is rebuilt.
func (a *App) maybeFollowSystemTheme() {
	if a.cfg.ThemeMode() != config.ThemeSystem {
		return
	}
	if sys := a.fyneApp.Settings().ThemeVariant(); sys != a.variant {
		a.applyTheme()
		a.buildPopoverContent()
	}
}

func (a *App) listLen() int { return len(a.visible) }

func (a *App) listCreate() fyne.CanvasObject {
	row := newPopoverRow(a.tips, a.pal, a.toggleGroup)
	row.setKeyboardGuard(a.listRowKeyboardActive)
	return row
}

func (a *App) listRowKeyboardActive(row *popoverRow) bool {
	if a.list == nil || !a.list.Visible() || !row.Visible() || !row.active().Visible() {
		return false
	}
	app := fyne.CurrentApp()
	if app == nil {
		return false
	}
	driver := app.Driver()
	listCanvas := driver.CanvasForObject(a.list)
	rowCanvas := driver.CanvasForObject(row)
	probeCanvas := driver.CanvasForObject(row.attachmentProbe)
	if listCanvas == nil || rowCanvas == nil || probeCanvas == nil || listCanvas != rowCanvas || listCanvas != probeCanvas {
		return false
	}
	listPos := driver.AbsolutePositionForObject(a.list)
	rowPos := driver.AbsolutePositionForObject(row)
	probePos := driver.AbsolutePositionForObject(row.attachmentProbe)
	listSize, rowSize := a.list.Size(), row.Size()
	if listSize.Width <= 0 || listSize.Height <= 0 || rowSize.Width <= 0 || rowSize.Height <= 0 {
		return false
	}
	if rowPos == fyne.NewPos(0, 0) && probePos == fyne.NewPos(0, 0) {
		return false
	}
	if rowPos == probePos && row.attachmentProbe.Position() != row.Position() {
		return false
	}
	return rowPos.X+rowSize.Width > listPos.X && rowPos.X < listPos.X+listSize.Width &&
		rowPos.Y+rowSize.Height > listPos.Y && rowPos.Y < listPos.Y+listSize.Height
}

func (a *App) listUpdate(id widget.ListItemID, o fyne.CanvasObject) {
	if id < 0 || id >= len(a.visible) {
		return
	}
	it := a.visible[id]
	row := o.(*popoverRow)
	if it.header {
		row.Configure(it, rowState{}, rowActions{})
	} else {
		row.Configure(it, a.rowStateFor(it.repo.Path, id), a.rowActions())
		row.repo.openBtn.setPresentation(a.repoOpenPresentation())
	}
	a.list.SetItemHeight(id, row.MinSize().Height)
}

func (a *App) repoOpenPresentation() (fyne.Resource, string) {
	action, _ := a.cfg.Click()
	switch action {
	case config.ActionTerminal:
		if runtime.GOOS == "windows" {
			return theme.ComputerIcon(), "Open in Command Prompt"
		}
		return theme.ComputerIcon(), "Open in Terminal"
	case config.ActionEditor:
		args, fallback := actions.ResolveEditorCommand("")
		if fallback {
			return theme.FileApplicationIcon(), "Open with " + args[0] + " (fallback)"
		}
		choice := a.currentIDE()
		if choice.set {
			return theme.FileApplicationIcon(), "Open with " + choice.editor.Name
		}
		return theme.FileApplicationIcon(), "Open with " + args[0]
	case config.ActionCustom:
		return theme.FileApplicationIcon(), "Run custom action"
	default:
		switch runtime.GOOS {
		case "darwin":
			return theme.FolderOpenIcon(), "Open in Finder"
		case "windows":
			return theme.FolderOpenIcon(), "Open in File Explorer"
		default:
			return theme.FolderOpenIcon(), "Open folder"
		}
	}
}

// rowStateFor assembles everything a repo row renders from. It is the single
// place that reads App state into a row, so the throwaway probe used to measure
// row heights and the real row can never disagree about what they are showing.
// visIdx is the row's index in the visible slice, or -1 when measuring.
func (a *App) rowStateFor(path string, visIdx int) rowState {
	st := rowState{
		expanded: a.expandedPath == path,
		status:   a.rowStatus[path],
		detail:   a.details[path],
		selected: visIdx >= 0 && visIdx == a.selIdx,
		match:    a.matchHint[path],
	}
	if st.expanded {
		st.worktrees = a.worktreeSectionFor(path)
		st.branches = a.branchSectionFor(path)
	}
	// Every row shows the same editor, so it is resolved once per paint rather
	// than per row — which also means one existence check, not thirty-six.
	if a.ideCacheGen != a.ideGen {
		c := a.currentIDE()
		open, change := ideTooltips(c)
		a.ideCached = rowState{ideIcon: a.ideIcon(c), ideTip: open, ideAltTip: change, ideMissing: c.missing}
		a.ideCacheGen = a.ideGen
	}
	st.ideIcon, st.ideTip = a.ideCached.ideIcon, a.ideCached.ideTip
	st.ideAltTip, st.ideMissing = a.ideCached.ideAltTip, a.ideCached.ideMissing
	return st
}

func (a *App) rowActions() rowActions {
	return rowActions{
		onExpand: a.toggleExpand,
		onPull:   a.pullRepo,
		onFresh:  a.toggleKeepFresh,
		onOpen:   a.activate,

		onOpenIDE:            a.openInIDE,
		onPickIDE:            a.pickIDE,
		onOpenConfig:         a.openRepoConfigInIDE,
		onToggleWorktrees:    a.toggleWorktrees,
		onRefreshWorktrees:   a.refreshWorktrees,
		onOpenWorktreeIDE:    a.openWorktreeIDE,
		onOpenWorktreeFolder: a.openWorktreeFolder,

		onToggleBranches: a.toggleBranches,
		onPullBranch:     a.startBranchSync,
		onTrackBranch:    a.startTrackBranch,
		onOpenBranch:     a.startBranchWorktree,
		onDismissBranch:  a.dismissBranchError,
		onFetchRepo:      a.fetchRepoNow,
		onMoreBranches:   a.showMoreBranches,
		onCopyBranch:     a.copyBranchName,
	}
}

func (a *App) toggleKeepFresh(r monitor.RepoState) {
	if err := a.mgr.SetKeepFresh(r.Path, !r.KeepFresh); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	a.refresh()
}

// toggleGroup folds or unfolds a scan-root section. The list content updates
// immediately, but the popover height glides to its new value so the section reads
// as collapsing/expanding rather than snapping (a big jump would otherwise flash a
// scaled transition frame). suppressAutoResize keeps applyFilter's instant resize
// out of the way so the animation owns the height.
func (a *App) toggleGroup(root string) {
	a.collapsedGrp[root] = !a.collapsedGrp[root]
	a.suppressAutoResize = true
	a.applyFilter()
	a.suppressAutoResize = false
	a.animatePopoverTo(a.desiredPopoverHeight())
}

// toggleExpand expands a repo's inline detail panel (collapsing any other), or
// collapses it if already open. Commit details are fetched lazily.
func (a *App) toggleExpand(r monitor.RepoState) {
	if a.expandedPath == r.Path {
		a.expandedPath = ""
	} else {
		a.expandedPath = r.Path
		if _, ok := a.details[r.Path]; !ok {
			a.fetchDetails(r)
		}
	}
	a.applyFilter() // re-render rows + recompute item heights
}

func (a *App) fetchDetails(r monitor.RepoState) {
	go func() {
		d := a.mgr.Details(r.Path)
		fyne.Do(func() {
			a.details[r.Path] = &d
			if a.expandedPath == r.Path {
				a.applyFilter()
			}
		})
	}()
}

// pullRepo fast-forwards a single repo. A second click while the first pull is
// still running is ignored — the row already says "Pulling…", and repeating the
// request would only queue redundant git processes.
func (a *App) pullRepo(r monitor.RepoState) {
	if a.rowStatus[r.Path].pulling() {
		return
	}
	a.setRowStatus(r.Path, &rowStatus{phase: rowPulling, msg: "Pulling…"})
	a.startPull(r)
}

// updateAll fast-forwards every repo that is behind its origin. Errors stay on
// their own row rather than raising a dialog per failure.
func (a *App) updateAll() {
	var targets []monitor.RepoState
	for _, r := range a.all {
		if r.Behind > 0 && !a.rowStatus[r.Path].pulling() {
			a.rowStatus[r.Path] = &rowStatus{phase: rowPulling, msg: "Pulling…"}
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		return
	}
	a.rearmExpiry()
	a.applyFilter() // mark every target at once
	for _, r := range targets {
		a.startPull(r)
	}
}

// startPull runs the pull in the background (the repo is already marked pulling)
// and leaves the outcome on the row for a few seconds, so a pull that finishes
// in 200ms still leaves visible evidence that it happened.
func (a *App) startPull(r monitor.RepoState) {
	go func() {
		err := a.mgr.Pull(r.Path)
		fyne.Do(func() {
			st := &rowStatus{phase: rowPulled, msg: "Pulled · just now", expires: time.Now().Add(transientHold)}
			if err != nil {
				st = &rowStatus{phase: rowPullFailed, msg: pullFailureNote(err), expires: time.Now().Add(transientHold)}
			}
			a.rowStatus[r.Path] = st
			a.rearmExpiry()
			delete(a.details, r.Path) // stale after a successful pull
			if a.expandedPath == r.Path {
				a.fetchDetails(r)
			}
			a.refresh()
		})
	}()
}

// pullFailureNote condenses a git error into something that fits the branch
// slot. The full text stays available in the row's tooltip and its error glyph.
func pullFailureNote(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "diverging"):
		return "Pull failed · diverged"
	case strings.Contains(msg, "would be overwritten"):
		return "Pull failed · local changes"
	case strings.Contains(msg, "Could not resolve host") || strings.Contains(msg, "Could not read from remote"):
		return "Pull failed · no connection"
	default:
		return "Pull failed"
	}
}

// commitMeta renders the short hash and a relative committed time, e.g.
// "908e320 · 2h ago" (option 3a). The commit message is shown on its own line.
func commitMeta(hash string, t time.Time) string {
	return hash + " · " + humanizeTime(t)
}

// humanizeTime renders a compact relative time ("just now", "5m ago", "2h ago",
// "3d ago") and falls back to an absolute date beyond a week.
func humanizeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}

// applyFilter recomputes the visible rows from the latest snapshot using the
// current query and filter, groups them by scan root, then refreshes the list and
// footer. Grouping (option 3a) turns the flat list into collapsible sections.
func (a *App) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(a.query))
	if a.matchHint == nil {
		a.matchHint = map[string]string{}
	}
	clear(a.matchHint)
	out := make([]monitor.RepoState, 0, len(a.all))
	for _, r := range a.all {
		switch a.filter {
		case filterUpdatable:
			if r.Behind == 0 {
				continue
			}
		case filterDirty:
			if !r.Dirty {
				continue
			}
		}
		if q != "" {
			hit, ok := matchQuery(r, q, a.bindex.get(r.Path))
			if !ok {
				continue
			}
			// Record the branch that brought this repository in, if that is what
			// did. Searching "v3" and being shown a row reading "v2" looks like a
			// broken search until the row says which branch matched.
			if hit != "" {
				a.matchHint[r.Path] = hit
			}
		}
		out = append(out, r)
	}

	a.visible, a.groupCount = a.groupItems(out)
	a.reconcileSelection()
	if a.list != nil {
		a.list.Refresh()
	}
	a.updateFooter()
	a.resizePopoverToContent()
}

// matchQuery reports whether a repo matches a lower-cased search term, and if a
// branch is what matched, which one.
//
// Every space-separated word must match the repository's name, its current
// branch, its (home-abbreviated) path, or ANY of its branches — so "www sw67"
// narrows to repos under a folder containing both, a parent-folder name finds
// repos the bare repo name would not, and a branch name finds the repository
// holding it without having opened anything.
//
// The returned name is the first branch that matched a word the repository's own
// text did not. It is empty when the repository matched on its own name, path or
// current branch, which is the case that needs no explaining.
//
// branches is the indexed set of that repository's branch names; it may be nil
// while the index is still being built, in which case only the current branch is
// searchable for that repository.
func matchQuery(r monitor.RepoState, q string, branches []string) (string, bool) {
	hay := strings.ToLower(r.Name + "\x00" + r.Branch + "\x00" + collapseHome(r.Path))
	hit := ""
	for _, word := range strings.Fields(q) {
		if strings.Contains(hay, word) {
			continue
		}
		name := findBranch(branches, word)
		if name == "" {
			return "", false
		}
		if hit == "" {
			hit = name
		}
	}
	return hit, true
}

// matchesQuery is the plain yes/no form, kept for callers that do not care why.
func matchesQuery(r monitor.RepoState, q string, branches []string) bool {
	_, ok := matchQuery(r, q, branches)
	return ok
}

// findBranch returns the first indexed branch name containing term, or "".
func findBranch(branches []string, term string) string {
	for _, name := range branches {
		if strings.Contains(name, term) {
			return name
		}
	}
	return ""
}

// groupItems buckets repos by their scan root and flattens them into the list's
// header+repo item sequence. Groups appear in configured-root order (extra roots
// after), each preceded by a section header; a folded section contributes only its
// header. Within a section repos are sorted by the active sort (behind-first by
// default). It also returns the number of distinct sections shown.
func (a *App) groupItems(repos []monitor.RepoState) ([]popoverItem, int) {
	type expRoot struct{ display, prefix string }
	var er []expRoot
	for _, rt := range a.cfg.RootList() {
		if p := config.ExpandPath(rt.Path); p != "" {
			er = append(er, expRoot{display: rt.Path, prefix: p})
		}
	}
	groupOf := func(repoPath string) string {
		best := -1
		for i := range er {
			pfx := er[i].prefix
			if repoPath == pfx || strings.HasPrefix(repoPath, pfx+string(filepath.Separator)) {
				if best == -1 || len(er[i].prefix) > len(er[best].prefix) {
					best = i
				}
			}
		}
		if best >= 0 {
			return er[best].display
		}
		return collapseHome(filepath.Dir(repoPath))
	}

	groups := map[string][]monitor.RepoState{}
	var order []string
	seen := map[string]bool{}
	for _, e := range er { // configured roots keep their order, even if empty
		if !seen[e.display] {
			order = append(order, e.display)
			seen[e.display] = true
		}
	}
	for _, r := range repos {
		g := groupOf(r.Path)
		if !seen[g] {
			order = append(order, g)
			seen[g] = true
		}
		groups[g] = append(groups[g], r)
	}

	var items []popoverItem
	shown := 0
	for _, g := range order {
		rs := groups[g]
		if len(rs) == 0 {
			continue
		}
		shown++
		sortRepos(rs, a.sort)
		behind := 0
		for _, r := range rs {
			if r.Behind > 0 {
				behind++
			}
		}
		collapsed := a.collapsedGrp[g]
		items = append(items, popoverItem{
			header: true, root: g, count: len(rs), behind: behind, collapsed: collapsed,
		})
		if !collapsed {
			for _, r := range rs {
				items = append(items, popoverItem{repo: r})
			}
		}
	}
	return items, shown
}

// sortRepos orders repos within a section. Errored repos always float to the top
// (a broken repo is the most important thing to surface — otherwise it's buried and
// the red tray badge stays a mystery). After that: by name, or (default) most-behind
// first then by name, so the repos needing a pull rise to the top of each root.
func sortRepos(rs []monitor.RepoState, mode sortMode) {
	errored := func(r monitor.RepoState) bool { return r.Err != "" || r.FetchErr != "" }
	sort.SliceStable(rs, func(i, j int) bool {
		if ei, ej := errored(rs[i]), errored(rs[j]); ei != ej {
			return ei
		}
		if mode != sortName && rs[i].Behind != rs[j].Behind {
			return rs[i].Behind > rs[j].Behind
		}
		return strings.ToLower(rs[i].Name) < strings.ToLower(rs[j].Name)
	})
}

// collapseHome renders an absolute path with a leading ~ for the user's home, so
// an ungrouped repo's parent reads like the configured roots (e.g. ~/code).
func collapseHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + p[len(home):]
		}
	}
	return p
}

// resizePopoverToContent shrinks or grows the open popover so it fits its rows
// (up to popoverMaxHeight). It keeps the window's top-left corner fixed rather
// than re-anchoring to the cursor, so resizing while the user types in the search
// field doesn't make the window hop to the pointer. No-op while hidden or while a
// collapse/expand animation owns the height (suppressAutoResize).
func (a *App) resizePopoverToContent() {
	if !a.popVisible || a.win == nil || a.suppressAutoResize {
		return
	}
	if a.resizeAnim != nil {
		a.resizeAnim.Stop() // a fresh instant resize supersedes an in-flight animation
		a.resizeAnim = nil
	}
	a.applyPopoverHeight(a.desiredPopoverHeight())
}

// applyPopoverHeight resizes the popover window to the given height, keeping the
// top edge anchored (see resizePopover), and records it as the current height.
func (a *App) applyPopoverHeight(h float32) {
	a.popoverH = h
	a.win.Resize(fyne.NewSize(popoverWidth, h))
	resizePopover(popoverTitle, popoverWidth, h)
}

// animatePopoverTo glides the popover height from its current value to target over
// a short ease, so a group collapse/expand reads as motion. It falls back to an
// instant resize when the popover is hidden or the start height is unknown.
func (a *App) animatePopoverTo(target float32) {
	if !a.popVisible || a.win == nil {
		return
	}
	from := a.popoverH
	if from <= 0 || from == target {
		a.applyPopoverHeight(target)
		return
	}
	if a.resizeAnim != nil {
		a.resizeAnim.Stop()
	}
	var anim *fyne.Animation
	anim = fyne.NewAnimation(130*time.Millisecond, func(p float32) {
		a.applyPopoverHeight(from + (target-from)*p)
		if p >= 1 && a.resizeAnim == anim {
			a.resizeAnim = nil // done — don't leave a finished animation around
		}
	})
	anim.Curve = fyne.AnimationEaseInOut
	a.resizeAnim = anim
	anim.Start()
}

// desiredPopoverHeight is the height needed to show every visible row without
// scrolling, clamped to popoverMaxHeight. It mirrors the popover's layout: the
// padded content (top + bottom) plus the two border gaps around the list make up
// a fixed chrome (4 paddings), to which the header, status label and the summed
// row heights are added.
func (a *App) desiredPopoverHeight() float32 {
	pad := theme.Padding()
	chrome := pad * 4
	var headerH, footerH float32
	if a.header != nil {
		headerH = a.header.MinSize().Height
	}
	if a.footer != nil {
		footerH = a.footer.MinSize().Height
	}
	body := a.listContentHeight()
	if min := a.collapsedRowHeight(); body < min {
		body = min // keep room for at least one row when empty/over-filtered
	}
	h := chrome + headerH + footerH + body
	if h > popoverMaxHeight {
		h = popoverMaxHeight
	}
	return h
}

// listContentHeight is the height the list needs to show every visible item without
// scrolling, matching widget.List.contentMinSize: summed item heights plus one
// inter-row separator (theme.Padding()) between each pair. Group headers and
// collapsed repo rows have constant, memoised heights; only the single expanded row
// — whose detail panel makes it taller — is probed, keeping applyFilter (one call
// per keystroke) at one widget probe at most instead of one per visible item.
func (a *App) listContentHeight() float32 {
	n := len(a.visible)
	if n == 0 {
		return 0
	}
	var total float32
	for _, it := range a.visible {
		switch {
		case it.header:
			total += a.groupHeaderHeight()
		case a.expandedPath != "" && it.repo.Path == a.expandedPath:
			total += a.expandedRowHeight(it.repo)
		default:
			// Every collapsed repo row is the same height, so one memoised probe
			// answers for all of them — no per-row measuring per keystroke.
			total += a.collapsedRowHeight()
		}
	}
	return total + theme.Padding()*float32(n-1)
}

// probeRow builds a throwaway list item purely to measure a height. Its group
// toggle is nil (it is never displayed or tapped).
func (a *App) probeRow() *popoverRow { return newPopoverRow(a.tips, a.pal, nil) }

// collapsedRowHeight is the height of a single collapsed repo row. It's constant for
// the app's theme sizes, so it's measured once via a throwaway probe and memoised;
// it also serves as the minimum body height so an empty/over-filtered list still
// shows a row's worth of space.
func (a *App) collapsedRowHeight() float32 {
	if a.collapsedRow == 0 {
		probe := a.probeRow()
		probe.Configure(popoverItem{repo: monitor.RepoState{Name: "Ag"}}, rowState{}, rowActions{})
		a.collapsedRow = probe.MinSize().Height
		probe.repo.clearMarquees()
	}
	return a.collapsedRow
}

// groupHeaderHeight is the constant height of a scan-root section header, memoised
// via a throwaway probe.
func (a *App) groupHeaderHeight() float32 {
	if a.groupRowH == 0 {
		probe := a.probeRow()
		probe.Configure(popoverItem{header: true, root: "~/x", count: 1}, rowState{}, rowActions{})
		a.groupRowH = probe.MinSize().Height
	}
	return a.groupRowH
}

// expandedRowHeight measures the height of a repo row with its detail panel open,
// using the same MinSize the list applies in listUpdate. Pulling is forced off so
// the throwaway probe never starts the spinner animation; any marquees it builds are
// cleared immediately.
func (a *App) expandedRowHeight(r monitor.RepoState) float32 {
	st := a.rowStateFor(r.Path, -1)
	st.expanded = true
	key := expandedHeightKey{
		path: r.Path, detail: st.detail, branchGen: a.branchGen[r.Path],
		worktreeGen: a.worktreeGen[r.Path], status: st.status,
	}
	if bl := a.branches[r.Path]; bl != nil {
		key.branchLoadedAt = bl.LoadedAt
	}
	if wl := a.worktrees[r.Path]; wl != nil {
		key.worktreeLoadedAt = wl.LoadedAt
	}
	if a.expandedH > 0 && a.expandedKey == key {
		return a.expandedH
	}

	probe := a.probeRow()
	probe.Configure(popoverItem{repo: r}, st, rowActions{})
	h := probe.MinSize().Height
	probe.repo.clearMarquees()
	a.expandedKey, a.expandedH = key, h
	return h
}

// updateFooter refreshes the summary bar: total repos and behind count on the left,
// the number of scan-root sections shown on the right (option 3a).
func (a *App) updateFooter() {
	if a.footerLeft == nil || a.footerRight == nil {
		return
	}
	total, behind := a.mgr.Counts()
	roots := "roots"
	if a.groupCount == 1 {
		roots = "root"
	}
	a.footerRight.Text = fmt.Sprintf("%d %s", a.groupCount, roots)
	if _, ok := a.selectedRepo(); ok {
		// While a row is keyboard-highlighted, teach the shortcuts in place of the
		// root count — the hint appears exactly when it is actionable.
		a.footerRight.Text = "⏎ open · ⇥ details · esc close"
	}

	// Update-all goes inert while any pull is running, so the button matches the
	// rows: nothing invites a second click on work already in progress.
	if a.updateAllBtn != nil {
		a.updateAllBtn.setDisabled(a.anyPulling())
	}

	// The left slot narrates background work. Truncate it: an HBox gives each
	// child its MinSize, so a long repo name would otherwise push the right-hand
	// text off the popover's edge. The counter leads, so it survives truncation.
	line := statusLine(a.act, a.transient, total, behind)
	rightW := fyne.MeasureText(a.footerRight.Text, a.footerRight.TextSize, a.footerRight.TextStyle).Width
	avail := popoverWidth - 4*theme.Padding() - rightW - 12
	a.footerLeft.Text = truncateToWidth(line, avail, a.footerLeft.TextSize, a.footerLeft.TextStyle)
	if a.transient != "" && strings.Contains(a.transient, "fail") {
		a.footerLeft.Color = a.pal.statusError
	} else {
		a.footerLeft.Color = a.pal.faint
	}
	a.footerLeft.Refresh()
	a.footerRight.Refresh()
}

// toggleOptions shows or hides the inline filter/sort panel below the search row.
// The panel is part of the header, so growing/shrinking the popover to fit it is
// just the usual content resize; a full content refresh reflows the list around it.
func (a *App) toggleOptions() {
	if a.optionsPanel == nil {
		return
	}
	if a.optionsPanel.Visible() {
		a.optionsPanel.Hide()
	} else {
		a.optionsPanel.Show()
	}
	if c := a.win.Content(); c != nil {
		c.Refresh() // reflow so the list yields/reclaims the panel's space
	}
	a.resizePopoverToContent()
}

// buildOptionsPanel builds the compact filter/sort strip: a "Show" row of All /
// Updatable / Dirty toggles and a "Sort" row of Name / Outdated toggles, closed off
// with a thin rule that separates it from the repo list. The segment order matches
// the filterMode / sortMode iota values.
func (a *App) buildOptionsPanel() *fyne.Container {
	filter := a.segGroup([]optionSegment{
		{label: "All"},
		{label: "Updatable"},
		{label: "Dirty"},
	}, int(a.filter), func(i int) { a.filter = filterMode(i); a.applyFilter() })

	sortG := a.segGroup([]optionSegment{
		{label: "Name", icon: theme.ListIcon()},
		{label: "Outdated", icon: theme.HistoryIcon()},
	}, int(a.sort), func(i int) { a.sort = sortMode(i); a.applyFilter() })
	// These are segChips, not widget.Buttons. A stock button carries the theme's
	// full text size and padding, which made this row tower over the filter chips
	// directly above it — the same panel in two different scales.
	repoActions := container.New(&actionClusterLayout{},
		newActionChip(a.pal, "Refresh", theme.ViewRefreshIcon(), func() { a.mgr.Refresh() }),
		newActionChip(a.pal, "Git Console", theme.ComputerIcon(), a.showGitConsole),
		newActionChip(a.pal, "Open Config", theme.DocumentIcon(), a.openConfigInEditor),
	)
	appActions := container.New(&actionClusterLayout{},
		newActionChip(a.pal, "Reload", theme.HistoryIcon(), a.reloadConfig),
		newActionChip(a.pal, "Quit", theme.LogoutIcon(), a.quit),
	)

	return container.NewVBox(
		container.NewHBox(a.optLabel("Show"), filter),
		container.NewHBox(a.optLabel("Sort"), sortG),
		container.NewHBox(a.optLabel("Actions"), repoActions),
		container.NewHBox(a.optLabel("App"), appActions),
		widget.NewSeparator(), // thin rule between the filtering header and the repos
	)
}

// showGitConsole opens the process-local Git command trace. The monitor owns the
// bounded buffer; this window only formats and filters its current snapshot.
func (a *App) showGitConsole() {
	if a.gitConsoleWin != nil {
		a.refreshGitConsole()
		a.gitConsoleWin.Show()
		a.gitConsoleWin.RequestFocus()
		return
	}

	w := a.fyneApp.NewWindow("git-repo-tracker — Git Console")
	a.gitConsoleWin = w
	a.gitConsoleAuto = true

	search := widget.NewEntry()
	search.SetPlaceHolder("Filter repository, command, or error…")
	search.OnChanged = func(query string) {
		a.gitConsoleQuery = query
		a.refreshGitConsole()
	}

	a.gitConsoleText = widget.NewTextGrid()
	a.gitConsoleText.ShowLineNumbers = false
	a.gitConsoleCount = widget.NewLabel("")

	copyAll := widget.NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), func() {
		if a.gitConsoleText == nil {
			return
		}
		w.Clipboard().SetContent(formatGitCommands(filterGitCommands(a.mgr.GitCommands(), a.gitConsoleQuery)))
	})
	clearAll := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		a.mgr.ClearGitCommands()
		a.refreshGitConsole()
	})
	autoScroll := widget.NewCheck("Auto-scroll", func(enabled bool) {
		a.gitConsoleAuto = enabled
		if enabled && a.gitConsoleText != nil {
			a.gitConsoleText.ScrollToBottom()
		}
	})
	autoScroll.SetChecked(true)

	toolbar := container.NewBorder(nil, nil,
		container.NewHBox(copyAll, clearAll, autoScroll), a.gitConsoleCount, search)
	w.SetContent(container.NewBorder(toolbar, nil, nil, nil, a.gitConsoleText))
	w.Resize(fyne.NewSize(780, 520))
	w.SetOnClosed(func() {
		a.gitConsoleWin = nil
		a.gitConsoleText = nil
		a.gitConsoleCount = nil
		a.gitConsoleQuery = ""
	})
	a.refreshGitConsole()
	w.Show()
	w.RequestFocus()
}

// refreshGitConsole runs only on Fyne's main thread. A fresh immutable snapshot
// makes filtering independent from concurrent Git activity.
func (a *App) refreshGitConsole() {
	if a.gitConsoleText == nil {
		return
	}
	commands := a.mgr.GitCommands()
	filtered := filterGitCommands(commands, a.gitConsoleQuery)
	a.gitConsoleText.SetText(formatGitCommands(filtered))
	if a.gitConsoleCount != nil {
		a.gitConsoleCount.SetText(fmt.Sprintf("%d shown · %d total", len(filtered), len(commands)))
	}
	if a.gitConsoleAuto {
		a.gitConsoleText.ScrollToBottom()
	}
}

// optLabel is a small bold caption ("Show"/"Sort"), vertically centred so it lines
// up with the chips beside it.
func (a *App) optLabel(s string) fyne.CanvasObject {
	t := canvas.NewText(s, a.pal.rowSub)
	t.TextSize = chipTextSize
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewCenter(t)
}

// optionSegment is one choice in a segmented toggle control.
type optionSegment struct {
	label string
	icon  fyne.Resource // optional
}

// segGroup builds a horizontal group of compact toggle chips where exactly one is
// active. selected is the initial index; onSelect fires with the chosen index and
// the group re-highlights so the active chip is always the accent-filled one.
func (a *App) segGroup(segs []optionSegment, selected int, onSelect func(int)) *fyne.Container {
	chips := make([]*segChip, len(segs))
	var choose func(int)
	for i, s := range segs {
		i := i
		chips[i] = newSegChip(s.label, s.icon, a.pal, func() { onSelect(i); choose(i) })
	}
	choose = func(sel int) {
		for i, c := range chips {
			c.setSelected(i == sel)
		}
	}
	choose(selected)
	objs := make([]fyne.CanvasObject, len(chips))
	for i := range chips {
		objs[i] = chips[i]
	}
	return container.NewHBox(objs...)
}
