package ui

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/dawidlaszuk/git-repo-tracker/internal/config"
	"github.com/dawidlaszuk/git-repo-tracker/internal/loginitem"
)

// showSettings opens a window to edit the configuration: which directories to
// scan, the refresh cadences, the click action, and launch-at-login. Saving
// persists the config and triggers an immediate rescan.
func (a *App) showSettings() {
	w := a.fyneApp.NewWindow("git-repo-tracker — Settings")
	w.Resize(fyne.NewSize(560, 470))
	tips := newTooltipLayer(a.pal)

	// Work on a copy of the roots; commit only on Save.
	roots := a.cfg.RootList()

	rootsBox := container.NewVBox()
	var rebuildRoots func()
	rebuildRoots = func() {
		rootsBox.RemoveAll()
		for i := range roots {
			i := i
			path := widget.NewEntry()
			path.SetText(roots[i].Path)
			path.SetPlaceHolder("~/projects")
			path.OnChanged = func(s string) { roots[i].Path = s }

			depth := widget.NewEntry()
			depth.SetText(strconv.Itoa(roots[i].Depth))
			depth.OnChanged = func(s string) {
				if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
					roots[i].Depth = n
				}
			}

			fetch := widget.NewCheck("auto-fetch", func(b bool) { roots[i].AutoFetch = b })
			fetch.SetChecked(roots[i].AutoFetch)

			remove := newTipButton(tips, theme.DeleteIcon(), "Remove this directory", func() {
				roots = append(roots[:i], roots[i+1:]...)
				rebuildRoots()
			})

			depthBox := container.NewHBox(widget.NewLabel("depth"), depth, fetch, remove)
			rootsBox.Add(container.NewBorder(nil, nil, nil, depthBox, path))
		}
		rootsBox.Refresh()
	}
	rebuildRoots()

	addBtn := widget.NewButtonWithIcon("Add directory", theme.ContentAddIcon(), func() {
		roots = append(roots, config.Root{Path: "~/", Depth: 5, AutoFetch: true})
		rebuildRoots()
	})

	// Intervals.
	fetchMin := widget.NewEntry()
	fetchMin.SetText(strconv.Itoa(int(a.cfg.FetchInterval().Minutes())))
	localSec := widget.NewEntry()
	localSec.SetText(strconv.Itoa(int(a.cfg.LocalRefresh().Seconds())))

	// Open-with action. The custom-command field is its own form so it can be
	// shown only when the "custom" action is selected.
	action, custom := a.cfg.Click()
	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("e.g. code {path}")
	customEntry.SetText(custom)
	customForm := widget.NewForm(widget.NewFormItem("Custom command", customEntry))
	actionSelect := widget.NewSelect(
		[]string{config.ActionOpenFolder, config.ActionTerminal, config.ActionEditor, config.ActionCustom},
		func(s string) {
			if s == config.ActionCustom {
				customForm.Show()
			} else {
				customForm.Hide()
			}
		},
	)
	actionSelect.SetSelected(action) // fires OnChanged → sets customForm visibility

	// Appearance: System follows the OS; Light/Dark force a fixed look.
	themeLabels := map[string]string{
		config.ThemeSystem: "System", config.ThemeLight: "Light", config.ThemeDark: "Dark",
	}
	themeValues := map[string]string{
		"System": config.ThemeSystem, "Light": config.ThemeLight, "Dark": config.ThemeDark,
	}
	themeSelect := widget.NewSelect([]string{"System", "Light", "Dark"}, nil)
	themeSelect.SetSelected(themeLabels[a.cfg.ThemeMode()])

	// Launch at login (reflect the actual on-disk state, not just config).
	launch := widget.NewCheck("Start git-repo-tracker at login", nil)
	launch.SetChecked(loginitem.Enabled())

	form := widget.NewForm(
		widget.NewFormItem("Fetch every (min)", fetchMin),
		widget.NewFormItem("Local refresh (s)", localSec),
		widget.NewFormItem("Open with", actionSelect),
		widget.NewFormItem("Theme", themeSelect),
	)

	save := widget.NewButtonWithIcon("Save", theme.ConfirmIcon(), func() {
		// Drop blank-path roots.
		cleaned := roots[:0]
		for _, r := range roots {
			if strings.TrimSpace(r.Path) != "" {
				cleaned = append(cleaned, r)
			}
		}
		fm, _ := strconv.Atoi(strings.TrimSpace(fetchMin.Text))
		ls, _ := strconv.Atoi(strings.TrimSpace(localSec.Text))
		// Persist in one atomic write; on failure the config is rolled back and the
		// window stays open instead of closing as if everything saved.
		if err := a.cfg.Save(cleaned, fm, ls, actionSelect.Selected, customEntry.Text); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if err := loginitem.Sync(launch.Checked); err != nil {
			a.logf("launch-at-login: %v", err)
			launch.SetChecked(loginitem.Enabled())
			dialog.ShowError(err, w)
			return
		}
		if err := a.cfg.SetLaunchAtLogin(launch.Checked); err != nil {
			dialog.ShowError(err, w)
			return
		}
		mode := themeValues[themeSelect.Selected]
		if mode == "" {
			mode = config.ThemeSystem
		}
		if err := a.cfg.SetThemeMode(mode); err != nil {
			dialog.ShowError(err, w)
			return
		}
		a.applyTheme()          // install the chosen variant as the Fyne theme
		a.buildPopoverContent() // repaint the popover's custom colours for it

		a.mgr.Refresh()
		a.refresh()
		w.Close()
	})
	cancel := widget.NewButton("Cancel", w.Close)
	buttons := container.NewHBox(layout.NewSpacer(), cancel, save)

	rootsHeader := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Scanned directories", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), addBtn)

	// Keep the directories area compact — a few rows tall, then it scrolls —
	// rather than letting it consume the whole window.
	dirs := container.NewVScroll(rootsBox)
	dirs.SetMinSize(fyne.NewSize(0, 120))

	content := container.NewVBox(
		rootsHeader,
		dirs,
		widget.NewSeparator(),
		form,
		customForm,
		launch,
		buttons,
	)
	w.SetContent(tips.wrap(container.NewPadded(content)))
	w.Show()
	w.RequestFocus()
}
