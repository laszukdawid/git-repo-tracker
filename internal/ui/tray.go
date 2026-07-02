package ui

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// rebuildTray reconstructs the whole tray menu from the current snapshot. Fyne
// has no incremental menu update, so we replace the menu wholesale; this is
// cheap and keeps labels perfectly in sync with state.
func (a *App) rebuildTray() {
	if runtime.GOOS == "linux" && a.trayBuilt {
		return
	}
	if a.trayOpen {
		a.trayDirty = true
		return
	}

	snap := a.mgr.Snapshot()
	var updatable []monitor.RepoState
	for _, r := range snap {
		if r.Behind > 0 {
			updatable = append(updatable, r)
		}
	}
	sort.SliceStable(updatable, func(i, j int) bool {
		return updatable[i].Behind > updatable[j].Behind
	})

	header := fyne.NewMenuItem(fmt.Sprintf("%d repos · %d behind", len(snap), len(updatable)), nil)
	header.Disabled = true
	search := fyne.NewMenuItem("Search in Browse Repos...", nil)
	search.Icon = theme.SearchIcon()
	search.Disabled = true
	items := []*fyne.MenuItem{header, search, fyne.NewMenuItemSeparator()}

	switch {
	case len(snap) == 0:
		empty := fyne.NewMenuItem("No repositories found — check your roots", nil)
		empty.Disabled = true
		items = append(items, empty)
	case len(updatable) == 0:
		ok := fyne.NewMenuItem("Everything up to date ✓", nil)
		ok.Disabled = true
		items = append(items, ok)
	default:
		shown := append([]monitor.RepoState(nil), snap...)
		sortRepos(shown, sortBehind)
		if len(shown) > maxTrayRepos {
			shown = shown[:maxTrayRepos]
		}
		for _, r := range shown {
			r := r
			item := fyne.NewMenuItem(trayLabel(r), func() { a.activate(r) })
			item.Icon = trayMenuIcon(r)
			items = append(items, item)
		}
		if len(snap) > maxTrayRepos {
			rest := fyne.NewMenuItem(fmt.Sprintf("...and %d more", len(snap)-maxTrayRepos),
				func() { fyne.Do(a.showWindow) })
			rest.Icon = theme.MoreHorizontalIcon()
			items = append(items, rest)
		}
	}

	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Browse Repos...", func() { fyne.Do(a.showWindow) }),
		fyne.NewMenuItem("Refresh Now", a.refreshFromTray),
		fyne.NewMenuItem("Settings...", func() { fyne.Do(a.showSettings) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open Config File...", a.openConfigInEditor),
		fyne.NewMenuItem("Reload Config", func() { fyne.Do(a.reloadConfig) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { fyne.Do(a.quit) }),
	)

	a.desk.SetSystemTrayMenu(fyne.NewMenu("Git Repos", items...))
	if runtime.GOOS == "linux" {
		a.trayBuilt = true
	}
}

func (a *App) refreshFromTray() {
	if runtime.GOOS == "linux" {
		a.trayBuilt = false
	}
	a.mgr.Refresh()
	a.refresh()
}

// trayLabel renders one repo as a compact native-menu row. Native tray menus are
// text-only layouts, so this approximates the rich browser row with name, branch,
// behind count and dirty/error markers.
func trayLabel(r monitor.RepoState) string {
	branch := strings.TrimSpace(r.Branch)
	if branch == "" {
		branch = "-"
	}
	s := fmt.Sprintf("%s  %s", r.Name, branch)
	if r.Behind > 0 {
		s += fmt.Sprintf("  ↓%d", r.Behind)
	}
	if r.Ahead > 0 {
		s += fmt.Sprintf("  ↑%d", r.Ahead)
	}
	if r.Dirty {
		s += "  dirty"
	}
	if r.LinesAdded > 0 || r.LinesDeleted > 0 {
		s += fmt.Sprintf("  +%d/-%d", r.LinesAdded, r.LinesDeleted)
	}
	return s
}

func trayMenuIcon(r monitor.RepoState) fyne.Resource {
	switch {
	case r.Err != "" || r.FetchErr != "":
		return theme.ErrorIcon()
	case r.Dirty:
		return theme.WarningIcon()
	case r.Behind > 0:
		return theme.DownloadIcon()
	default:
		return theme.ConfirmIcon()
	}
}
