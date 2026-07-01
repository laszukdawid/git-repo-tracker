package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/dawidlaszuk/git-repo-tracker/internal/config"
	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
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
	if drv, ok := a.fyneApp.Driver().(desktop.Driver); ok {
		w = drv.CreateSplashWindow()
	} else {
		w = a.fyneApp.NewWindow("Repositories")
	}
	a.win = w
	w.SetTitle(popoverTitle) // how the macOS native helper finds this NSWindow
	w.Resize(fyne.NewSize(popoverWidth, popoverMaxHeight))
	w.SetCloseIntercept(a.hidePopover) // hide instead of quitting the app
	a.buildPopoverContent()
}

// buildPopoverContent builds (or rebuilds) the popover's widgets and sets them as
// the window content, using the current palette. It's safe to call again after a
// theme change: the custom colours below are baked into canvas objects, so they
// only pick up a new variant when the content is recreated here.
func (a *App) buildPopoverContent() {
	a.search = newSearchEntry(func(s string) {
		a.query = s
		a.applyFilter()
	}, a.hidePopover)
	if a.query != "" {
		a.search.SetText(a.query) // preserve the active filter across a content rebuild
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

	a.statusLbl = widget.NewLabel("")
	a.list = widget.NewList(a.listLen, a.listCreate, a.listUpdate)
	// No list.OnSelected: rows handle their own tap (expand) + hover buttons.

	content := container.NewBorder(a.header, a.statusLbl, nil, nil, a.list)

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
	a.refresh()
	h := a.desiredPopoverHeight()
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

func (a *App) listCreate() fyne.CanvasObject { return newRepoRow(a.tips, a.pal) }

func (a *App) listUpdate(id widget.ListItemID, o fyne.CanvasObject) {
	if id < 0 || id >= len(a.visible) {
		return
	}
	r := a.visible[id]
	row := o.(*repoRow)
	row.Configure(r, a.expandedPath == r.Path, a.pulling[r.Path], a.details[r.Path],
		a.toggleExpand, a.pullRepo, a.activate)
	a.list.SetItemHeight(id, row.MinSize().Height)
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

// commitMeta renders the short hash and committed time, e.g.
// "908e320 · 2026-06-23 18:39". The commit message is shown on its own line.
func commitMeta(hash string, t time.Time) string {
	when := "unknown"
	if !t.IsZero() {
		when = t.Local().Format("2006-01-02 15:04")
	}
	return hash + " · " + when
}

// rowSubtitle renders the branch plus the status glyphs and, for updatable
// repos, how many lines behind they are.
func rowSubtitle(r monitor.RepoState) string {
	branch := r.Branch
	if branch == "" {
		branch = "—"
	}
	s := fmt.Sprintf("%-22s %s", truncate(branch, 22), statusGlyphs(r))
	if r.Behind > 0 && (r.LinesAdded > 0 || r.LinesDeleted > 0) {
		s += fmt.Sprintf("  Δ+%d/-%d", r.LinesAdded, r.LinesDeleted)
	}
	if r.Err != "" || r.FetchErr != "" {
		s += "  ⚠"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// applyFilter recomputes the visible rows from the latest snapshot using the
// current query, filter and sort, then refreshes the list and summary.
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
	switch a.sort {
	case sortBehind:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Behind > out[j].Behind })
	case sortName:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
	}
	a.visible = out
	if a.list != nil {
		a.list.Refresh()
	}
	a.updateStatusLabel()
	a.resizePopoverToContent()
}

// resizePopoverToContent shrinks or grows the open popover so it fits its rows
// (up to popoverMaxHeight). It keeps the window's top-left corner fixed rather
// than re-anchoring to the cursor, so resizing while the user types in the search
// field doesn't make the window hop to the pointer. No-op while hidden.
func (a *App) resizePopoverToContent() {
	if !a.popVisible || a.win == nil {
		return
	}
	h := a.desiredPopoverHeight()
	a.win.Resize(fyne.NewSize(popoverWidth, h))
	resizePopover(popoverTitle, popoverWidth, h)
}

// desiredPopoverHeight is the height needed to show every visible row without
// scrolling, clamped to popoverMaxHeight. It mirrors the popover's layout: the
// padded content (top + bottom) plus the two border gaps around the list make up
// a fixed chrome (4 paddings), to which the header, status label and the summed
// row heights are added.
func (a *App) desiredPopoverHeight() float32 {
	pad := theme.Padding()
	chrome := pad * 4
	var headerH, statusH float32
	if a.header != nil {
		headerH = a.header.MinSize().Height
	}
	if a.statusLbl != nil {
		statusH = a.statusLbl.MinSize().Height
	}
	body := a.listContentHeight()
	if min := a.collapsedRowHeight(); body < min {
		body = min // keep room for at least one row when empty/over-filtered
	}
	h := chrome + headerH + statusH + body
	if h > popoverMaxHeight {
		h = popoverMaxHeight
	}
	return h
}

// listContentHeight is the height the list needs to show every visible row without
// scrolling, matching widget.List.contentMinSize: summed item heights plus one
// inter-row separator (theme.Padding()) between each pair. A collapsed row's height
// is constant (it depends only on theme sizes, not content), so we use the memoised
// value for all rows and probe only the single expanded row — whose detail panel
// makes it taller — for its extra height. This keeps applyFilter (one call per
// keystroke) at one widget probe at most, instead of one per visible repo.
func (a *App) listContentHeight() float32 {
	n := len(a.visible)
	if n == 0 {
		return 0
	}
	collapsed := a.collapsedRowHeight()
	total := collapsed * float32(n)
	if a.expandedPath != "" {
		for _, r := range a.visible {
			if r.Path == a.expandedPath {
				total += a.expandedRowHeight(r) - collapsed
				break
			}
		}
	}
	return total + theme.Padding()*float32(n-1)
}

// collapsedRowHeight is the height of a single collapsed row. It's constant for the
// app's theme sizes, so it's measured once via a throwaway probe and memoised; it
// also serves as the minimum body height so an empty/over-filtered list still shows
// a row's worth of space.
func (a *App) collapsedRowHeight() float32 {
	if a.collapsedRow == 0 {
		probe := newRepoRow(a.tips, a.pal)
		probe.Configure(monitor.RepoState{Name: "Ag"}, false, false, nil, nil, nil, nil)
		a.collapsedRow = probe.MinSize().Height
		probe.clearMarquees()
	}
	return a.collapsedRow
}

// expandedRowHeight measures the height of a row with its detail panel open, using
// the same repoRow.MinSize the list applies in listUpdate. Pulling is forced off so
// the throwaway probe never starts the spinner animation; any marquees it builds are
// cleared immediately.
func (a *App) expandedRowHeight(r monitor.RepoState) float32 {
	probe := newRepoRow(a.tips, a.pal)
	probe.Configure(r, true, false, a.details[r.Path], nil, nil, nil)
	h := probe.MinSize().Height
	probe.clearMarquees()
	return h
}

func (a *App) updateStatusLabel() {
	if a.statusLbl == nil {
		return
	}
	total, behind := a.mgr.Counts()
	a.statusLbl.SetText(fmt.Sprintf("%d repos · %d behind · showing %d", total, behind, len(a.visible)))
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

	return container.NewVBox(
		container.NewHBox(a.optLabel("Show"), filter),
		container.NewHBox(a.optLabel("Sort"), sortG),
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
