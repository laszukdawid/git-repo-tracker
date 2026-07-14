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
)

// searchEntry is the popover's search field. It intercepts Escape so the popover
// can close even while the entry has keyboard focus — Fyne only routes the
// canvas-level key handler when nothing is focused, so a plain Entry would
// otherwise swallow Escape.
type searchEntry struct {
	widget.Entry
	onEscape func()
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
	e.Entry.TypedKey(ev)
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
		a.applyFilter()
	}

	a.tips = newTooltipLayer(a.pal)
	updateAllBtn := newTipButton(a.tips, theme.DownloadIcon(), "Update all (pull every repo that's behind)", a.updateAll)
	settingsBtn := newTipButton(a.tips, theme.SettingsIcon(), "Settings", a.showSettings)
	a.moreBtn = newTipButton(a.tips, theme.MenuIcon(), "Show/hide filtering header", a.toggleOptions)
	right := container.NewHBox(updateAllBtn, settingsBtn, a.moreBtn)
	searchRow := container.NewBorder(nil, nil, nil, right, a.search)

	// The filter/sort toggles live in a panel below the search row that the ☰ button
	// expands. It's part of the header, so the popover's height math (which measures
	// a.header) grows to include it automatically when shown.
	a.optionsPanel = a.buildOptionsPanel()
	a.optionsPanel.Hide()
	a.header = container.NewVBox(searchRow, a.optionsPanel)

	// Footer: repo/behind totals on the left, scan-root count on the right (3a).
	a.footerLeft = canvas.NewText("", a.pal.faint)
	a.footerLeft.TextStyle = fyne.TextStyle{Monospace: true}
	a.footerLeft.TextSize = 11.5
	a.footerRight = canvas.NewText("", a.pal.faint)
	a.footerRight.TextStyle = fyne.TextStyle{Monospace: true}
	a.footerRight.TextSize = 11.5
	a.footer = container.NewPadded(container.NewHBox(a.footerLeft, layout.NewSpacer(), a.footerRight))

	a.list = widget.NewList(a.listLen, a.listCreate, a.listUpdate)
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
	a.win.Hide()
	a.popVisible = false
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
	return newPopoverRow(a.tips, a.pal, a.toggleGroup)
}

func (a *App) listUpdate(id widget.ListItemID, o fyne.CanvasObject) {
	if id < 0 || id >= len(a.visible) {
		return
	}
	it := a.visible[id]
	row := o.(*popoverRow)
	if it.header {
		row.Configure(it, false, false, nil, nil, nil, nil, nil)
	} else {
		p := it.repo.Path
		row.Configure(it, a.expandedPath == p, a.pulling[p], a.details[p],
			a.toggleExpand, a.pullRepo, a.toggleKeepFresh, a.activate)
	}
	a.list.SetItemHeight(id, row.MinSize().Height)
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

// pullRepo fast-forwards a single repo, showing a spinner on its row and a
// dialog if it fails (e.g. a diverged branch).
func (a *App) pullRepo(r monitor.RepoState) {
	if a.pulling[r.Path] {
		return
	}
	a.pulling[r.Path] = true
	a.applyFilter()
	a.startPull(r, true)
}

// updateAll fast-forwards every repo that is behind its origin. Per-repo spinners
// show progress; errors are left as the row's ⚠ marker rather than a dialog flood.
func (a *App) updateAll() {
	var targets []monitor.RepoState
	for _, r := range a.all {
		if r.Behind > 0 && !a.pulling[r.Path] {
			a.pulling[r.Path] = true
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		return
	}
	a.applyFilter() // show all the spinners at once
	for _, r := range targets {
		a.startPull(r, false)
	}
}

// startPull runs the pull in the background (the repo is already marked pulling).
func (a *App) startPull(r monitor.RepoState, showErr bool) {
	go func() {
		err := a.mgr.Pull(r.Path)
		fyne.Do(func() {
			delete(a.pulling, r.Path)
			delete(a.details, r.Path) // stale after a successful pull
			if err != nil && showErr {
				dialog.ShowError(err, a.win)
			}
			if a.expandedPath == r.Path {
				a.fetchDetails(r)
			}
			a.refresh()
		})
	}()
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
		if q != "" && !strings.Contains(strings.ToLower(r.Name), q) &&
			!strings.Contains(strings.ToLower(r.Branch), q) {
			continue
		}
		out = append(out, r)
	}

	a.visible, a.groupCount = a.groupItems(out)
	if a.list != nil {
		a.list.Refresh()
	}
	a.updateFooter()
	a.resizePopoverToContent()
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
		probe.Configure(popoverItem{repo: monitor.RepoState{Name: "Ag"}}, false, false, nil, nil, nil, nil, nil)
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
		probe.Configure(popoverItem{header: true, root: "~/x", count: 1}, false, false, nil, nil, nil, nil, nil)
		a.groupRowH = probe.MinSize().Height
	}
	return a.groupRowH
}

// expandedRowHeight measures the height of a repo row with its detail panel open,
// using the same MinSize the list applies in listUpdate. Pulling is forced off so
// the throwaway probe never starts the spinner animation; any marquees it builds are
// cleared immediately.
func (a *App) expandedRowHeight(r monitor.RepoState) float32 {
	probe := a.probeRow()
	probe.Configure(popoverItem{repo: r}, true, false, a.details[r.Path], nil, nil, nil, nil)
	h := probe.MinSize().Height
	probe.repo.clearMarquees()
	return h
}

// updateFooter refreshes the summary bar: total repos and behind count on the left,
// the number of scan-root sections shown on the right (option 3a).
func (a *App) updateFooter() {
	if a.footerLeft == nil || a.footerRight == nil {
		return
	}
	total, behind := a.mgr.Counts()
	a.footerLeft.Text = fmt.Sprintf("%d repos · %d behind", total, behind)
	roots := "roots"
	if a.groupCount == 1 {
		roots = "root"
	}
	a.footerRight.Text = fmt.Sprintf("%d %s", a.groupCount, roots)
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
	actions := container.NewHBox(
		widget.NewButton("Refresh", func() { a.mgr.Refresh() }),
		widget.NewButton("Open Config", a.openConfigInEditor),
		widget.NewButton("Reload", a.reloadConfig),
		widget.NewButton("Quit", a.quit),
	)

	return container.NewVBox(
		container.NewHBox(a.optLabel("Show"), filter),
		container.NewHBox(a.optLabel("Sort"), sortG),
		container.NewHBox(a.optLabel("Actions"), actions),
		widget.NewSeparator(), // thin rule between the filtering header and the repos
	)
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
