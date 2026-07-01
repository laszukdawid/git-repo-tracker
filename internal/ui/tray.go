package ui

import (
	"fmt"
	"sort"

	"fyne.io/fyne/v2"

	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

// rebuildTray reconstructs the whole tray menu from the current snapshot. Fyne
// has no incremental menu update, so we replace the menu wholesale; this is
// cheap and keeps labels perfectly in sync with state.
func (a *App) rebuildTray() {
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
	items := []*fyne.MenuItem{header, fyne.NewMenuItemSeparator()}

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
		shown := updatable
		if len(shown) > maxTrayRepos {
			shown = shown[:maxTrayRepos]
		}
		for _, r := range shown {
			r := r
			items = append(items, fyne.NewMenuItem(trayLabel(r), func() { a.activate(r) }))
		}
		if len(updatable) > maxTrayRepos {
			rest := fyne.NewMenuItem(fmt.Sprintf("…and %d more", len(updatable)-maxTrayRepos),
				func() { a.showWindow() })
			items = append(items, rest)
		}
	}

	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Browse Repos…", func() { a.showWindow() }),
		fyne.NewMenuItem("Refresh Now", func() { a.mgr.Refresh() }),
		fyne.NewMenuItem("Settings…", a.showSettings),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open Config File…", a.openConfigInEditor),
		fyne.NewMenuItem("Reload Config", a.reloadConfig),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", a.quit),
	)

	a.desk.SetSystemTrayMenu(fyne.NewMenu("Git Repos", items...))
}

// trayLabel renders an updatable repo as "name  ↓behind  +adds/-dels".
func trayLabel(r monitor.RepoState) string {
	s := fmt.Sprintf("%s  ↓%d", r.Name, r.Behind)
	if r.LinesAdded > 0 || r.LinesDeleted > 0 {
		s += fmt.Sprintf("  +%d/-%d", r.LinesAdded, r.LinesDeleted)
	}
	return s
}
