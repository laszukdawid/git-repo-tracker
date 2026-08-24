package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/ide"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

// Choosing and launching an editor.
//
// The remembered editor is validated every time the control is drawn, not once
// at startup: applications are deleted, renamed and moved between launches, and
// one that has quietly disappeared should say so rather than fail on click.

// ideChoice is the resolved state of the editor setting.
type ideChoice struct {
	set     bool    // an editor has been chosen at all
	editor  ide.IDE // the chosen editor, whether or not it still exists
	missing bool    // it was chosen but is no longer installed
}

type ideOpenTarget struct {
	path  string
	label string
}

// installEditorResolver points the legacy "editor" click action at the chosen
// editor, so a configuration that still says `clickAction: editor` opens what
// the user picked instead of assuming VS Code's CLI exists.
func (a *App) installEditorResolver() {
	actions.SetEditorResolver(func(dir string) ([]string, error) {
		c := a.currentIDE()
		if !c.set || c.missing {
			return nil, fmt.Errorf("no editor chosen")
		}
		return c.editor.Args(dir)
	})
}

// currentIDE resolves the configured editor and checks it is still there.
func (a *App) currentIDE() ideChoice {
	id, extra := a.cfg.IDEChoice()
	editor, ok := ide.Lookup(id, extra)
	if !ok {
		return ideChoice{}
	}
	return ideChoice{set: true, editor: editor, missing: !editor.Exists()}
}

// ideTooltips are the two halves of the chip's wording: what a tap does, and
// what the corner does.
func ideTooltips(c ideChoice) (open, change string) {
	switch {
	case !c.set:
		return "Select an editor to open repositories with", "Select an editor"
	case c.missing:
		return c.editor.Name + " (not found) — choose another", "Choose another editor"
	default:
		return "Open with " + c.editor.Name, "Open with a different editor"
	}
}

// ideIcon is the artwork for the chip: the editor's own where there is one.
func (a *App) ideIcon(c ideChoice) fyne.Resource {
	if !c.set {
		return theme.NewColoredResource(theme.ComputerIcon(), colorNameMuted)
	}
	if a.ideIcons == nil {
		a.ideIcons = map[string]fyne.Resource{}
	}
	key := c.editor.ID()
	if res, ok := a.ideIcons[key]; ok {
		return res
	}
	res := ideIconResource(c.editor)
	a.ideIcons[key] = res
	return res
}

// openInIDE launches the remembered editor on a repository, or opens the picker
// when there is nothing to launch.
func (a *App) openInIDE(r monitor.RepoState) {
	a.openIDETarget(ideOpenTarget{path: r.Path, label: r.Name})
}

func (a *App) openIDETarget(target ideOpenTarget) {
	c := a.currentIDE()
	if !c.set || c.missing {
		a.pickIDETarget(target)
		return
	}
	args, err := c.editor.Args(target.path)
	if err != nil {
		a.logf("open in %s: %v", c.editor.Name, err)
		return
	}
	if err := actions.RunArgs(args); err != nil {
		a.logf("open %s in %s: %v", target.label, c.editor.Name, err)
		dialog.ShowError(fmt.Errorf("could not open %s in %s: %w", target.label, c.editor.Name, err), a.win)
	}
}

// openRepoConfigInIDE resolves the config off the main thread because the
// lookup is a Git subprocess. The chosen editor can open the resulting file path
// just as it opens a repository directory.
func (a *App) openRepoConfigInIDE(r monitor.RepoState) {
	go func() {
		path, err := a.mgr.ConfigPath(r.Path)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("could not locate %s Git config: %w", r.Name, err), a.win)
				return
			}
			a.openIDETarget(ideOpenTarget{path: path, label: r.Name + " Git config"})
		})
	}()
}

// pickIDE shows the list of editors found on this machine. Discovery runs here
// rather than being cached for the session, so an editor installed a minute ago
// appears without restarting the app — and one deleted a minute ago disappears.
func (a *App) pickIDE(r monitor.RepoState) {
	a.pickIDETarget(ideOpenTarget{path: r.Path, label: r.Name})
}

func (a *App) pickIDETarget(target ideOpenTarget) {
	a.ideGen++ // opening the picker re-checks what is installed
	_, extra := a.cfg.IDEChoice()
	found := ide.Discover(extra)
	current := a.currentIDE()

	var items []*fyne.MenuItem

	// A remembered editor that is gone is listed first, disabled, so the reason
	// the chip stopped working is stated rather than left to be guessed.
	if current.set && current.missing {
		gone := fyne.NewMenuItem(current.editor.Name+" (not found)", nil)
		gone.Disabled = true
		items = append(items, gone, fyne.NewMenuItemSeparator())
	}

	for _, editor := range found {
		editor := editor
		item := fyne.NewMenuItem(editor.Name, func() { a.chooseIDE(editor, target) })
		if current.set && !current.missing && current.editor.ID() == editor.ID() {
			item.Checked = true
		}
		items = append(items, item)
	}
	if len(found) == 0 {
		none := fyne.NewMenuItem("No editors found", nil)
		none.Disabled = true
		items = append(items, none)
	}

	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Browse…", func() { a.browseForIDE(target) }),
	)

	menu := fyne.NewMenu("", items...)
	pop := widget.NewPopUpMenu(menu, a.win.Canvas())
	pop.ShowAtPosition(a.idePopupPos())
}

// idePopupPos places the picker near the middle of the popover rather than at
// the pointer, so it cannot open half off the window's edge.
func (a *App) idePopupPos() fyne.Position {
	size := a.win.Canvas().Size()
	return fyne.NewPos(size.Width/2-90, size.Height/3)
}

// chooseIDE remembers an editor and immediately opens the repository the user
// was pointing at, so choosing is not a separate step from acting.
func (a *App) chooseIDE(editor ide.IDE, target ideOpenTarget) {
	if err := a.cfg.SetIDE(editor.ID()); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	a.ideGen++ // the chip's icon and tooltip change
	a.applyFilter()
	if target.path != "" {
		a.openIDETarget(target)
	}
}

// browseForIDE lets the user point at an editor discovery did not find.
func (a *App) browseForIDE(target ideOpenTarget) {
	a.browseForApplication(func(path string) {
		editor, ok := ide.FromPath(path)
		if !ok {
			dialog.ShowError(fmt.Errorf("%s is not an application", path), a.win)
			return
		}
		if err := a.cfg.AddExtraIDE(path, editor.ID()); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		a.ideGen++
		a.applyFilter()
		if target.path != "" {
			a.openIDETarget(target)
		}
	})
}
