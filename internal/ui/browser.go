package ui

import (
	"fmt"
	"image/color"
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

	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

// Row text colours: accent blue for the repo name, muted grey for the status line.
var (
	rowNameColor = color.NRGBA{R: 91, G: 157, B: 255, A: 255}
	rowSubColor  = color.NRGBA{R: 168, G: 178, B: 192, A: 235}
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
// support we fall back to a regular window.
func (a *App) buildPopover() {
	var w fyne.Window
	if drv, ok := a.fyneApp.Driver().(desktop.Driver); ok {
		w = drv.CreateSplashWindow()
	} else {
		w = a.fyneApp.NewWindow("Repositories")
	}
	a.win = w
	w.SetTitle(popoverTitle) // how the macOS native helper finds this NSWindow
	w.Resize(fyne.NewSize(popoverWidth, popoverHeight))
	w.SetCloseIntercept(a.hidePopover) // hide instead of quitting the app

	a.search = newSearchEntry(func(s string) {
		a.query = s
		a.applyFilter()
	}, a.hidePopover)

	a.tips = newTooltipLayer()
	a.moreBtn = newTipButton(a.tips, theme.MenuIcon(), "View options", a.showMoreMenu)
	updateAllBtn := newTipButton(a.tips, theme.DownloadIcon(), "Update all (pull every repo that's behind)", a.updateAll)
	right := container.NewHBox(updateAllBtn, a.moreBtn)
	header := container.NewBorder(nil, nil, nil, right, a.search)

	a.statusLbl = widget.NewLabel("")
	a.list = widget.NewList(a.listLen, a.listCreate, a.listUpdate)
	// No list.OnSelected: rows handle their own tap (expand) + hover buttons.

	content := container.NewBorder(header, a.statusLbl, nil, nil, a.list)

	// Glassy backdrop: a dark gradient under the content. On macOS the window
	// itself is rounded + shadowed natively (see placePopover); the gradient is
	// clipped to those rounded corners. True OS translucency isn't reachable via
	// stock Fyne, so this evokes the look instead (see glassTheme).
	bg := canvas.NewLinearGradient(
		color.NRGBA{R: 26, G: 31, B: 41, A: 255},
		color.NRGBA{R: 10, G: 13, B: 20, A: 255},
		135,
	)
	w.SetContent(a.tips.wrap(container.NewStack(bg, container.NewPadded(content))))

	// Escape hides the window without quitting.
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			a.hidePopover()
		}
	})

	// Focus the search up front so keystrokes land there when the popover is
	// toggled open via the tray icon (Fyne routes typed runes to the canvas's
	// focused object).
	w.Canvas().Focus(a.search)
}

// hidePopover hides the window and clears the visibility flag the tray toggle
// reads.
func (a *App) hidePopover() {
	a.win.Hide()
	a.popVisible = false
}

// showWindow brings the popover to the front, positioned next to the tray icon,
// with fresh data and the search field focused so the user can type immediately.
func (a *App) showWindow() {
	a.refresh()
	a.win.Resize(fyne.NewSize(popoverWidth, popoverHeight))
	a.win.Show()
	a.popVisible = true
	placePopover(popoverTitle, popoverWidth, popoverHeight)
	a.win.RequestFocus()
	if a.search != nil {
		a.win.Canvas().Focus(a.search)
	}
}

func (a *App) listLen() int { return len(a.visible) }

func (a *App) listCreate() fyne.CanvasObject { return newRepoRow(a.tips) }

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
}

func (a *App) updateStatusLabel() {
	if a.statusLbl == nil {
		return
	}
	total, behind := a.mgr.Counts()
	a.statusLbl.SetText(fmt.Sprintf("%d repos · %d behind · showing %d", total, behind, len(a.visible)))
}

// showMoreMenu pops up the view-options menu beneath the "more" button.
func (a *App) showMoreMenu() {
	filterItem := func(label string, mode filterMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { a.filter = mode; a.applyFilter() })
		it.Checked = a.filter == mode
		return it
	}
	sortItem := func(label string, mode sortMode) *fyne.MenuItem {
		it := fyne.NewMenuItem(label, func() { a.sort = mode; a.applyFilter() })
		it.Checked = a.sort == mode
		return it
	}
	menu := fyne.NewMenu("",
		filterItem("Show all", filterAll),
		filterItem("Only updatable", filterUpdatable),
		filterItem("Only dirty", filterDirty),
		fyne.NewMenuItemSeparator(),
		sortItem("Sort by name", sortName),
		sortItem("Sort by most behind", sortBehind),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Refresh now", func() { a.mgr.Refresh() }),
		fyne.NewMenuItem("Settings…", a.showSettings),
	)
	pop := widget.NewPopUpMenu(menu, a.win.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(a.moreBtn)
	pop.ShowAtPosition(fyne.NewPos(pos.X, pos.Y+a.moreBtn.Size().Height))
}
