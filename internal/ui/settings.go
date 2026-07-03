package ui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/loginitem"
)

// Settings layout metrics (option 1e).
const (
	settingsWidth  = 520
	settingsHeight = 560
	cardRadius     = 11
	cardPadX       = 15
	cardHeadPadY   = 12
	cardRowPadY    = 11
	bodyPad        = 18
	depthFieldW    = 56
	labelColW      = 120
)

// showSettings opens the settings window (option 1e): grouped, titled cards with
// aligned label→control rows, a segmented theme switch, and a sticky save bar. It
// edits which directories to scan, the refresh cadences, the click action, the
// theme and launch-at-login; saving persists the config and triggers a rescan.
func (a *App) showSettings() {
	if a.settingsWin != nil {
		a.settingsWin.Show()
		a.settingsWin.RequestFocus()
		return
	}

	w := a.fyneApp.NewWindow("git-repo-tracker — Settings")
	a.settingsWin = w
	w.SetOnClosed(func() { a.settingsWin = nil })
	w.Resize(fyne.NewSize(settingsWidth, settingsHeight))
	tips := newTooltipLayer(a.pal)

	// Work on a copy of the roots; commit only on Save. Toggles/entries mutate this
	// slice in place through their closures.
	roots := a.cfg.RootList()

	rootsBox := container.New(&tightVBox{gap: 0})
	var rebuildRoots func()
	rebuildRoots = func() {
		rootsBox.RemoveAll()
		for i := range roots {
			if i > 0 {
				rootsBox.Add(a.hairline())
			}
			onRemove := func() {
				roots = append(roots[:i], roots[i+1:]...)
				rebuildRoots()
			}
			rootsBox.Add(a.dirRow(i, roots, tips, onRemove))
		}
		rootsBox.Refresh()
	}
	rebuildRoots()

	addAction := newTextAction("Add directory", theme.ContentAddIcon(), theme.ColorNamePrimary,
		a.pal.rowName, func() {
			roots = append(roots, config.Root{Path: "~/", Depth: 5, AutoFetch: true})
			rebuildRoots()
		})
	dirHeader := container.NewBorder(nil, nil,
		a.cardTitle("Scanned directories"), addAction)
	dirCard := a.cardWithBody(a.inset(dirHeader, cardHeadPadY, cardPadX, cardHeadPadY, cardPadX), rootsBox)

	// Sync & behavior.
	fetchMin := widget.NewEntry()
	fetchMin.SetText(strconv.Itoa(int(a.cfg.FetchInterval().Minutes())))
	localSec := widget.NewEntry()
	localSec.SetText(strconv.Itoa(int(a.cfg.LocalRefresh().Seconds())))

	action, custom := a.cfg.Click()
	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("e.g. code {path}")
	customEntry.SetText(custom)
	customRow := a.formRow("Custom command", customEntry)
	actionSelect := widget.NewSelect(
		[]string{config.ActionOpenFolder, config.ActionTerminal, config.ActionEditor, config.ActionCustom},
		func(s string) {
			if s == config.ActionCustom {
				customRow.Show()
			} else {
				customRow.Hide()
			}
		},
	)
	actionSelect.SetSelected(action) // fires OnChanged → sets customRow visibility

	// Theme: System follows the OS; Light/Dark force a fixed look. The selected
	// index maps to config.ThemeSystem/Light/Dark.
	themeModes := []string{config.ThemeSystem, config.ThemeLight, config.ThemeDark}
	themeSel := indexOf(themeModes, a.cfg.ThemeMode())
	if themeSel < 0 {
		themeSel = 0
	}
	themeBar := a.segmentedBar([]string{"System", "Light", "Dark"}, themeSel, func(i int) { themeSel = i })

	syncBody := a.inset(container.New(&tightVBox{gap: 13},
		a.formRow("Fetch every", a.suffixField(fetchMin, "min")),
		a.formRow("Local refresh", a.suffixField(localSec, "sec")),
		a.formRow("Open with", actionSelect),
		customRow,
		a.formRow("Theme", themeBar),
	), cardRowPadY+2, cardPadX, cardRowPadY+2, cardPadX)
	syncCard := a.cardWithBody(
		a.inset(a.cardTitle("Sync & behavior"), cardHeadPadY, cardPadX, cardHeadPadY, cardPadX),
		syncBody)

	// Launch at login (reflect the actual on-disk state, not just config).
	startupToggle := newToggleSwitch(loginitem.Enabled(), a.pal, nil)
	startupRow := container.NewHBox(
		container.NewCenter(startupToggle),
		container.NewCenter(a.mutedLabel("Start git-repo-tracker at login")),
	)

	body := container.NewVScroll(a.inset(
		container.New(&tightVBox{gap: 16}, dirCard, syncCard, startupRow),
		bodyPad, bodyPad+2, bodyPad, bodyPad+2))

	// Sticky footer save bar.
	save := widget.NewButtonWithIcon("Save", theme.ConfirmIcon(), func() {
		cleaned := roots[:0]
		for _, r := range roots {
			if strings.TrimSpace(r.Path) != "" {
				cleaned = append(cleaned, r)
			}
		}
		fm, _ := strconv.Atoi(strings.TrimSpace(fetchMin.Text))
		ls, _ := strconv.Atoi(strings.TrimSpace(localSec.Text))
		if err := a.cfg.Save(cleaned, fm, ls, actionSelect.Selected, customEntry.Text); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if err := loginitem.Sync(startupToggle.on); err != nil {
			a.logf("launch-at-login: %v", err)
			startupToggle.on = loginitem.Enabled()
			startupToggle.Refresh()
			dialog.ShowError(err, w)
			return
		}
		if err := a.cfg.SetLaunchAtLogin(startupToggle.on); err != nil {
			dialog.ShowError(err, w)
			return
		}
		if err := a.cfg.SetThemeMode(themeModes[themeSel]); err != nil {
			dialog.ShowError(err, w)
			return
		}
		a.applyTheme()          // install the chosen variant as the Fyne theme
		a.buildPopoverContent() // repaint the popover's custom colours for it
		a.mgr.Refresh()
		a.refresh()
		w.Close()
	})
	save.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", w.Close)
	cancel.Importance = widget.LowImportance

	footerBar := container.NewStack(
		a.rect(a.pal.footerBg, 0),
		a.inset(container.NewHBox(layout.NewSpacer(), cancel, save), 10, 16, 10, 16),
	)
	footer := container.New(&tightVBox{gap: 0}, a.hairline(), footerBar)

	w.SetContent(tips.wrap(container.NewBorder(nil, footer, nil, nil, body)))
	w.Show()
	w.RequestFocus()
	// As a menu-bar agent the app isn't auto-activated when Settings is opened from
	// the status-bar menu, so surface the window explicitly (no-op off macOS).
	activateApp()
}

// dirRow builds one scanned-directory row: a path entry (flex), a fixed-width depth
// entry, an auto-fetch pill toggle and a remove button. The path/depth/fetch
// closures mutate roots[i] in place (shared backing array); onRemove — bound to
// this index by the caller — drops the entry and rebuilds the list.
func (a *App) dirRow(i int, roots []config.Root, tips *tooltipLayer, onRemove func()) fyne.CanvasObject {
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
	depthCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(depthFieldW, depth.MinSize().Height)), depth)

	fetch := newToggleSwitch(roots[i].AutoFetch, a.pal, func(b bool) { roots[i].AutoFetch = b })
	remove := newTipButton(tips, theme.DeleteIcon(), "Remove this directory", onRemove)

	right := container.NewHBox(
		container.NewCenter(a.mutedLabel("depth")),
		container.NewCenter(depthCell),
		container.NewCenter(fetch),
		remove,
	)
	row := container.NewBorder(nil, nil, nil, right, path)
	return a.inset(row, cardRowPadY, cardPadX, cardRowPadY, cardPadX)
}

// segmentedBar renders a segmented control (option 1e theme switch) reusing the
// segChip look: equal-width chips inside a bordered track, the selected one filled
// accent. onSelect fires with the chosen index and the bar re-highlights.
func (a *App) segmentedBar(labels []string, selected int, onSelect func(int)) fyne.CanvasObject {
	chips := make([]*segChip, len(labels))
	var choose func(int)
	for i, l := range labels {
		i := i
		chips[i] = newSegChip(l, nil, a.pal, func() { onSelect(i); choose(i) })
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
	grid := container.New(layout.NewGridLayout(len(chips)), objs...)
	track := a.rect(a.pal.fieldBg, 8)
	track.StrokeColor = a.pal.fieldBorder
	track.StrokeWidth = 1
	return container.NewStack(track, container.NewPadded(grid))
}

// formRow lays out a fixed-width, right-aligned label beside a flexing control.
func (a *App) formRow(label string, control fyne.CanvasObject) *fyne.Container {
	l := widget.NewLabelWithStyle(label, fyne.TextAlignTrailing, fyne.TextStyle{})
	cell := container.New(layout.NewGridWrapLayout(fyne.NewSize(labelColW, control.MinSize().Height)), l)
	return container.NewBorder(nil, nil, cell, nil, control)
}

// suffixField pairs a number entry with a faint unit suffix (min / sec).
func (a *App) suffixField(entry *widget.Entry, suffix string) fyne.CanvasObject {
	s := canvas.NewText(suffix, a.pal.faint)
	s.TextSize = 12
	return container.NewBorder(nil, nil, nil, container.NewCenter(s), entry)
}

// cardWithBody wraps a header and body in a rounded, bordered card with a hairline
// under the header (option 1e / 1d card chrome).
func (a *App) cardWithBody(header, body fyne.CanvasObject) fyne.CanvasObject {
	inner := container.New(&tightVBox{gap: 0}, header, a.hairline(), body)
	bg := a.rect(a.pal.cardBg, cardRadius)
	bg.StrokeColor = a.pal.cardBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, inner)
}

// cardTitle is a card's section heading.
func (a *App) cardTitle(text string) fyne.CanvasObject {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// mutedLabel is a small muted caption drawn as canvas text (13px).
func (a *App) mutedLabel(text string) fyne.CanvasObject {
	t := canvas.NewText(text, a.pal.muted)
	t.TextSize = 13
	return t
}

// hairline is a 1px separator tinted to the palette's subtle white rule.
func (a *App) hairline() *canvas.Rectangle {
	r := canvas.NewRectangle(a.pal.hairline)
	r.SetMinSize(fyne.NewSize(0, 1))
	return r
}

// rect builds a filled rounded rectangle (card/track/footer backgrounds).
func (a *App) rect(fill color.Color, radius float32) *canvas.Rectangle {
	r := canvas.NewRectangle(fill)
	r.CornerRadius = radius
	return r
}

// inset wraps an object with fixed padding on each side (top, right, bottom, left).
func (a *App) inset(o fyne.CanvasObject, top, right, bottom, left float32) *fyne.Container {
	return container.New(insetLayout{top: top, right: right, bottom: bottom, left: left}, o)
}

// insetLayout pads its (single) child by fixed amounts on each side, giving the
// design's exact card/row spacing where theme.Padding would be too tight.
type insetLayout struct{ top, right, bottom, left float32 }

func (l insetLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(l.left, l.top))
		o.Resize(fyne.NewSize(size.Width-l.left-l.right, size.Height-l.top-l.bottom))
	}
}

func (l insetLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objs {
		s := o.MinSize()
		w = max(w, s.Width)
		h = max(h, s.Height)
	}
	return fyne.NewSize(w+l.left+l.right, h+l.top+l.bottom)
}

// indexOf returns the index of s in list, or -1.
func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// textAction is a borderless tappable label with an optional leading icon, tinted
// to a theme colour — the accent "＋ Add directory" affordance from option 1e.
type textAction struct {
	widget.BaseWidget
	label    string
	icon     fyne.Resource
	iconName fyne.ThemeColorName
	col      color.Color
	onTap    func()
	hovered  bool
}

func newTextAction(label string, icon fyne.Resource, iconName fyne.ThemeColorName, col color.Color, onTap func()) *textAction {
	t := &textAction{label: label, icon: icon, iconName: iconName, col: col, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *textAction) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}
func (t *textAction) MouseIn(*desktop.MouseEvent)    { t.setHovered(true) }
func (t *textAction) MouseMoved(*desktop.MouseEvent) {}
func (t *textAction) MouseOut()                      { t.setHovered(false) }
func (t *textAction) setHovered(h bool) {
	if t.hovered != h {
		t.hovered = h
		t.Refresh()
	}
}

func (t *textAction) CreateRenderer() fyne.WidgetRenderer {
	txt := canvas.NewText(t.label, t.col)
	txt.TextSize = 12.5
	txt.TextStyle = fyne.TextStyle{Bold: true}
	objs := []fyne.CanvasObject{txt}
	var img *canvas.Image
	if t.icon != nil {
		img = canvas.NewImageFromResource(theme.NewColoredResource(t.icon, t.iconName))
		img.FillMode = canvas.ImageFillContain
		objs = append(objs, img)
	}
	return &textActionRenderer{t: t, txt: txt, img: img, objects: objs}
}

type textActionRenderer struct {
	t       *textAction
	txt     *canvas.Text
	img     *canvas.Image
	objects []fyne.CanvasObject
}

const textActionIcon = 14

func (r *textActionRenderer) MinSize() fyne.Size {
	ts := r.txt.MinSize()
	w := ts.Width
	h := ts.Height
	if r.img != nil {
		w += textActionIcon + 5
		if textActionIcon > h {
			h = textActionIcon
		}
	}
	return fyne.NewSize(w+4, h+4)
}

func (r *textActionRenderer) Layout(size fyne.Size) {
	x := float32(2)
	if r.img != nil {
		r.img.Move(fyne.NewPos(x, (size.Height-textActionIcon)/2))
		r.img.Resize(fyne.NewSize(textActionIcon, textActionIcon))
		x += textActionIcon + 5
	}
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(x, (size.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *textActionRenderer) Refresh() {
	r.txt.Color = r.t.col
	r.txt.Refresh()
	if r.img != nil {
		r.img.Refresh()
	}
}
func (r *textActionRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *textActionRenderer) Destroy()                     {}
