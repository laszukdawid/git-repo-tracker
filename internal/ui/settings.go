package ui

import (
	"fmt"
	"image/color"
	"os"
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
	// Deliberately the same width as the popover: the two windows are the same
	// object seen two ways, and matching their width makes that read.
	settingsWidth  = popoverWidth
	settingsHeight = 560
	cardRadius     = radiusMd
	cardPadX       = spaceLg
	cardHeadPadY   = spaceMd
	cardRowPadY    = spaceMd
	bodyPad        = spaceLg
	depthFieldW    = 64  // one digit plus the validator icon and entry padding
	numFieldW      = 80  // fixed width for the small min/sec number fields
	controlW       = 220 // fixed width for the Open-with select and Theme switch
	labelColW      = 120
)

// showSettings opens the settings window (option 1e): grouped, titled cards with
// aligned label→control rows, a segmented theme switch, and immediate-save
// controls. Any change updates config and refreshes live state without a final save.
func (a *App) showSettings() {
	// The popover floats at status-window level so it sits over other apps like a
	// real menu-bar dropdown — which would also keep it above Settings. Dismiss it so
	// Settings opens unobstructed.
	if a.popVisible {
		a.hidePopover()
	}

	if a.settingsWin != nil {
		a.settingsWin.Show()
		a.settingsWin.RequestFocus()
		activateApp()
		return
	}

	w := a.fyneApp.NewWindow("git-repo-tracker — Settings")
	a.settingsWin = w
	w.SetOnClosed(func() { a.settingsWin = nil })
	w.Resize(fyne.NewSize(settingsWidth, settingsHeight))
	w.SetContent(a.settingsContent(w))
	w.Show()
	w.RequestFocus()
	// As a menu-bar agent the app isn't auto-activated when Settings is opened from
	// the status-bar menu, so surface the window explicitly (no-op off macOS).
	activateApp()
}

func (a *App) settingsContent(w fyne.Window) fyne.CanvasObject {
	tips := newTooltipLayer(a.pal)

	// Work on a copy of the roots; toggles/entries mutate this slice in place.
	roots := a.cfg.RootList()
	var reloadLive func()

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
				if reloadLive != nil {
					reloadLive()
				}
			}
			rootsBox.Add(a.dirRow(i, roots, tips, w, func() {
				if reloadLive != nil {
					reloadLive()
				}
			}, onRemove))
		}
		rootsBox.Refresh()
	}
	rebuildRoots()

	addAction := newTextAction("Add directory", theme.ContentAddIcon(), theme.ColorNamePrimary,
		a.pal.accent, func() {
			roots = append(roots, newScanRoot())
			rebuildRoots()
			if reloadLive != nil {
				reloadLive()
			}
		})
	dirHeader := container.NewBorder(nil, nil,
		a.cardTitle("Scanned directories"), addAction)
	dirCard := a.cardWithBody(a.inset(dirHeader, cardHeadPadY, cardPadX, cardHeadPadY, cardPadX), rootsBox)
	// Sync & behavior.
	fetchMin := widget.NewEntry()
	fetchMin.Validator = positiveInt
	fetchMin.SetText(strconv.Itoa(int(a.cfg.FetchInterval().Minutes())))
	localSec := widget.NewEntry()
	localSec.Validator = positiveInt
	localSec.SetText(strconv.Itoa(int(a.cfg.LocalRefresh().Seconds())))
	fetchMin.OnChanged = func(_ string) { reloadLive() }
	localSec.OnChanged = func(_ string) { reloadLive() }

	action, custom := a.cfg.Click()
	customEntry := widget.NewEntry()
	customEntry.SetPlaceHolder("e.g. code {path}")
	customEntry.SetText(custom)
	customEntry.OnChanged = func(_ string) { reloadLive() }
	customRow := a.formRow("Custom command", customEntry)
	actionSelect := widget.NewSelect(
		[]string{config.ActionOpenFolder, config.ActionTerminal, config.ActionEditor, config.ActionCustom},
		nil,
	)
	actionSelect.SetSelected(action)
	onActionChanged := func(s string) {
		if s == config.ActionCustom {
			customRow.Show()
		} else {
			customRow.Hide()
		}
		reloadLive()
	}
	actionSelect.OnChanged = onActionChanged
	if action == config.ActionCustom {
		customRow.Show()
	} else {
		customRow.Hide()
	}
	// Refresh and persist the parts of settings controlled in this pane.
	cleanRoots := func() []config.Root {
		cleaned := roots[:0]
		for _, r := range roots {
			if strings.TrimSpace(r.Path) != "" {
				cleaned = append(cleaned, r)
			}
		}
		return cleaned
	}
	saveBehavior := func() error {
		fm, fmErr := strconv.Atoi(strings.TrimSpace(fetchMin.Text))
		if fmErr != nil || fm <= 0 {
			return nil
		}
		ls, lsErr := strconv.Atoi(strings.TrimSpace(localSec.Text))
		if lsErr != nil || ls <= 0 {
			return nil
		}
		if err := customCommandError(actionSelect.Selected, customEntry.Text); err != nil {
			return nil
		}
		return a.cfg.Save(cleanRoots(), fm, ls, actionSelect.Selected, customEntry.Text)
	}
	reloadLive = func() {
		if err := saveBehavior(); err != nil {
			dialog.ShowError(err, w)
			return
		}
		a.mgr.Refresh()
		a.refresh()
	}

	// Theme family and mode apply instantly and are visible without Save.
	themeModes := []string{config.ThemeSystem, config.ThemeLight, config.ThemeDark}
	palettes := []string{config.PaletteSlate, config.PaletteInk, config.PaletteSignal}
	themeSel := indexOf(themeModes, a.cfg.ThemeMode())
	if themeSel < 0 {
		themeSel = 0
	}
	paletteSel := indexOf(palettes, a.cfg.PaletteName())
	if paletteSel < 0 {
		paletteSel = 0
	}
	applyThemeChoice := func() {
		a.applyTheme()
		a.buildPopoverContent()
		a.refresh()
		tips.stopDelay()
		w.SetContent(a.settingsContent(w))
	}
	onSetThemeMode := func(i int) {
		if err := a.cfg.SetThemeMode(themeModes[i]); err != nil {
			dialog.ShowError(err, w)
			themeSel = indexOf(themeModes, a.cfg.ThemeMode())
			if themeSel < 0 {
				themeSel = 0
			}
			return
		}
		applyThemeChoice()
		reloadLive()
	}
	onSetPalette := func(i int) {
		if err := a.cfg.SetPalette(palettes[i]); err != nil {
			dialog.ShowError(err, w)
			paletteSel = indexOf(palettes, a.cfg.PaletteName())
			if paletteSel < 0 {
				paletteSel = 0
			}
			return
		}
		applyThemeChoice()
		reloadLive()
	}

	// Theme: System follows the OS; Light/Dark force a fixed look. The selected
	// index maps to config.ThemeSystem/Light/Dark.
	themeBar := a.segmentedBar([]string{"System", "Light", "Dark"}, themeSel, onSetThemeMode)

	// Colour family. Three families times the light/dark variant above give the
	// six themes; the family decides what colour *means* in the list, not just
	// which hue is used. See internal/ui/palettes.go.
	paletteBar := a.segmentedBar([]string{"Slate", "Ink", "Signal"}, paletteSel, onSetPalette)

	syncBody := a.inset(container.New(&tightVBox{gap: spaceMd},
		a.formRow("Fetch every", a.suffixField(fetchMin, "min")),
		a.formRow("Local refresh", a.suffixField(localSec, "sec")),
		a.formRow("Open with", a.fixedWidth(actionSelect, controlW)),
		customRow,
		a.formRow("Theme", a.fixedWidth(themeBar, controlW)),
		a.formRow("Colours", a.fixedWidth(paletteBar, controlW)),
	), cardRowPadY, cardPadX, cardRowPadY, cardPadX)
	syncCard := a.cardWithBody(
		a.inset(a.cardTitle("Sync & behavior"), cardHeadPadY, cardPadX, cardHeadPadY, cardPadX),
		syncBody)

	// Launch at login (reflect the actual on-disk state, not just config).
	var startupToggle *toggleSwitch
	startupToggle = newToggleSwitch(loginitem.Enabled(), a.pal, func(on bool) {
		if err := loginitem.Sync(on); err != nil {
			dialog.ShowError(err, w)
			startupToggle.on = loginitem.Enabled()
			startupToggle.Refresh()
			return
		}
		if err := a.cfg.SetLaunchAtLogin(on); err != nil {
			dialog.ShowError(err, w)
			startupToggle.on = !on
			startupToggle.Refresh()
			return
		}
	})
	startupRow := container.NewHBox(
		container.NewCenter(startupToggle),
		container.NewCenter(a.mutedLabel("Start git-repo-tracker at login")),
	)

	body := container.NewVScroll(a.inset(
		container.New(&tightVBox{gap: spaceLg}, dirCard, syncCard, startupRow),
		bodyPad, bodyPad, bodyPad, bodyPad))

	// Settings updates are live; footer closes the pane only.
	done := widget.NewButton("Done", w.Close)
	done.Importance = widget.HighImportance

	footerBar := container.NewStack(
		a.rect(a.pal.footerBg, 0),
		a.inset(container.NewHBox(layout.NewSpacer(), done), spaceSm, spaceLg, spaceSm, spaceLg),
	)
	footer := container.New(&tightVBox{gap: 0}, a.hairline(), footerBar)

	return tips.wrap(container.NewBorder(nil, footer, nil, nil, body))
}

// positiveInt is the Entry validator for the interval fields.
func positiveInt(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fmt.Errorf("enter a whole number greater than 0")
	}
	return nil
}

// nonNegativeInt is the Entry validator for the scan-depth field (0 = unlimited).
func nonNegativeInt(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return fmt.Errorf("enter 0 (unlimited) or a positive whole number")
	}
	return nil
}

// customCommandError rejects saving the "custom" click action with nothing to run.
func customCommandError(action, custom string) error {
	if action == config.ActionCustom && strings.TrimSpace(custom) == "" {
		return fmt.Errorf("Custom command: enter a command, e.g. code {path}")
	}
	return nil
}

func newScanRoot() config.Root {
	return config.Root{Depth: 5, AutoFetch: true}
}

// dirRow builds one scanned-directory row: a path entry (flex) with a folder-browse
// button tucked inside it, a fixed-width depth entry, an auto-fetch pill toggle and
// a remove button. The depth label and the toggle rely on hover tooltips rather than
// inline text to keep the row uncluttered. The path/depth/fetch closures mutate
// roots[i] in place (shared backing array); onRemove — bound to this index by the
// caller — drops the entry and rebuilds the list.
func (a *App) dirRow(i int, roots []config.Root, tips *tooltipLayer, w fyne.Window, onChange func(), onRemove func()) fyne.CanvasObject {
	path := widget.NewEntry()
	// A path is one horizontal value. Fyne's default scrolls both axes and draws
	// its horizontal scrollbar over the text when the value is wider than the field.
	path.Wrapping = fyne.TextWrapOff
	path.Scroll = fyne.ScrollHorizontalOnly
	// The folder-browse button sits inside the entry (its ActionItem, like a password
	// revealer) so path + browse read as one field. It MUST be assigned before the
	// SetText/SetPlaceHolder calls below, which build the entry's renderer.
	path.ActionItem = newHeaderButton(tips, a.pal, theme.FolderOpenIcon(), "Browse for a folder", func() {
		a.browseForFolder(w, config.ExpandPath(path.Text), func(chosen string) {
			fyne.Do(func() { path.SetText(tildeAbbrev(chosen)) }) // OnChanged updates roots[i]
		})
	})
	path.SetPlaceHolder("~/projects")
	path.SetText(roots[i].Path)
	path.OnChanged = func(s string) {
		roots[i].Path = s
		if onChange != nil {
			onChange()
		}
	}
	depth := widget.NewEntry()
	depth.Validator = nonNegativeInt
	depth.SetText(strconv.Itoa(roots[i].Depth))
	depth.OnChanged = func(s string) {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
			roots[i].Depth = n
		}
		if onChange != nil {
			onChange()
		}
	}
	depthCell := container.New(layout.NewGridWrapLayout(fyne.NewSize(depthFieldW, depth.MinSize().Height)), depth)
	depthField := newTipHover(tips, "Folder scan depth", depthCell) // replaces the inline "depth" label

	fetch := newToggleSwitch(roots[i].AutoFetch, a.pal, func(b bool) {
		roots[i].AutoFetch = b
		if onChange != nil {
			onChange()
		}
	})
	fetch.tips, fetch.tip = tips, "Auto-fetch this directory"
	remove := newHeaderButton(tips, a.pal, theme.DeleteIcon(), "Remove this directory", onRemove)

	// A little air between the depth field and the toggle; the delete button sits
	// right next to the toggle (no wide gap).
	right := container.New(&directoryActionsLayout{}, depthField, fetch, remove)
	pathField := container.NewThemeOverride(path, pathEntryTheme(path.Theme()))
	row := container.NewBorder(nil, nil, nil, right, pathField)
	return a.inset(row, cardRowPadY, cardPadX, cardRowPadY, cardPadX)
}

type directoryActionsLayout struct{}

func (directoryActionsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gaps := []float32{spaceMd, actionSiblingGap}
	x := float32(0)
	for i, object := range objects {
		if !object.Visible() {
			continue
		}
		itemSize := object.MinSize()
		object.Move(fyne.NewPos(x, (size.Height-itemSize.Height)/2))
		object.Resize(itemSize)
		x += itemSize.Width
		if i < len(gaps) {
			x += gaps[i]
		}
	}
}

func (directoryActionsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	gaps := []float32{spaceMd, actionSiblingGap}
	var width, height float32
	for i, object := range objects {
		if !object.Visible() {
			continue
		}
		itemSize := object.MinSize()
		width += itemSize.Width
		height = max(height, itemSize.Height)
		if i < len(gaps) {
			width += gaps[i]
		}
	}
	return fyne.NewSize(width, height)
}

// browseForFolder opens a folder picker starting at startDir and calls onPick with
// the chosen absolute path. It uses the native OS panel where available (macOS) and
// falls back to Fyne's in-app folder dialog elsewhere.
func (a *App) browseForFolder(w fyne.Window, startDir string, onPick func(string)) {
	if chosen, native := chooseFolderNative(startDir); native {
		if chosen != "" {
			onPick(chosen)
		}
		return
	}
	dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
		if err != nil || u == nil {
			return
		}
		onPick(u.Path())
	}, w)
}

// browseForApplication opens the OS picker for an application bundle, falling
// back to Fyne's in-app file dialog where there is no native panel.
func (a *App) browseForApplication(onPick func(string)) {
	if chosen, native := chooseApplicationNative(); native {
		if chosen != "" {
			onPick(chosen)
		}
		return
	}
	dialog.ShowFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		defer rc.Close()
		onPick(rc.URI().Path())
	}, a.win)
}

// tildeAbbrev rewrites an absolute path under the user's home directory back to a
// leading ~, matching how paths are typed and displayed; other paths pass through.
func tildeAbbrev(abs string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return abs
	}
	if abs == home {
		return "~"
	}
	if strings.HasPrefix(abs, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(abs, home)
	}
	return abs
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
	track := a.rect(a.pal.fieldBg, radiusSm)
	track.StrokeColor = a.pal.fieldBorder
	track.StrokeWidth = hairlineW
	return container.NewStack(track, container.NewPadded(grid))
}

// formRow lays out a fixed-width, right-aligned label beside a flexing control.
// The label is kept at its own height and vertically centered, so it lines up with
// the control's centered text even when the control is taller (e.g. the Select).
func (a *App) formRow(label string, control fyne.CanvasObject) *fyne.Container {
	l := widget.NewLabelWithStyle(label, fyne.TextAlignTrailing, fyne.TextStyle{})
	cell := container.New(layout.NewGridWrapLayout(fyne.NewSize(labelColW, l.MinSize().Height)), l)
	return container.NewBorder(nil, nil, container.NewCenter(cell), nil, control)
}

// fixedWidth pins a control to width w and left-aligns it within its form row, so
// widening the window stretches the empty background beside it rather than the
// control (the extra cells of the grid-wrap stay empty).
func (a *App) fixedWidth(o fyne.CanvasObject, w float32) fyne.CanvasObject {
	return container.New(layout.NewGridWrapLayout(fyne.NewSize(w, o.MinSize().Height)), o)
}

// suffixField pairs a fixed-width number entry with a faint unit suffix (min / sec),
// left-aligned so a two-digit value doesn't stretch a field across the whole row.
func (a *App) suffixField(entry *widget.Entry, suffix string) fyne.CanvasObject {
	s := canvas.NewText(suffix, a.pal.faint)
	s.TextSize = textSm
	field := container.New(layout.NewGridWrapLayout(fyne.NewSize(numFieldW, entry.MinSize().Height)), entry)
	return container.NewHBox(field, container.NewCenter(s))
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
	t.TextSize = textMd
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
	state    interactionState
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
func (t *textAction) MouseOut() {
	t.setHovered(false)
	t.setPressed(false)
}
func (t *textAction) MouseDown(*desktop.MouseEvent) { t.setPressed(true) }
func (t *textAction) MouseUp(*desktop.MouseEvent)   { t.setPressed(false) }
func (t *textAction) setHovered(h bool) {
	if t.state.setHovered(h) {
		t.Refresh()
	}
}

func (t *textAction) setPressed(v bool) {
	if t.state.setPressed(v) {
		t.Refresh()
	}
}

func (t *textAction) FocusGained() {
	if t.state.setFocused(true) {
		t.Refresh()
	}
}

func (t *textAction) FocusLost() {
	if t.state.setFocused(false) {
		t.Refresh()
	}
}

func (t *textAction) TypedRune(rune) {}

func (t *textAction) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn || ev.Name == fyne.KeyEnter {
		keyboardActivate(&t.state, t.Refresh, func() { t.Tapped(nil) })
	}
}

func (t *textAction) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = radiusSm
	txt := canvas.NewText(t.label, t.col)
	txt.TextSize = textSm
	txt.TextStyle = fyne.TextStyle{Bold: true}
	objs := []fyne.CanvasObject{bg, txt}
	var img *canvas.Image
	if t.icon != nil {
		img = canvas.NewImageFromResource(theme.NewColoredResource(t.icon, t.iconName))
		img.FillMode = canvas.ImageFillContain
		objs = append(objs, img)
	}
	return &textActionRenderer{t: t, bg: bg, txt: txt, img: img, objects: objs}
}

type textActionRenderer struct {
	t       *textAction
	bg      *canvas.Rectangle
	txt     *canvas.Text
	img     *canvas.Image
	objects []fyne.CanvasObject
}

func (r *textActionRenderer) MinSize() fyne.Size {
	ts := r.txt.MinSize()
	w := ts.Width + actionLabelPad*2
	h := float32(actionBox)
	if r.img != nil {
		w += actionIcon + actionLabelGap
	}
	return fyne.NewSize(w, h)
}

func (r *textActionRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	x := float32(actionLabelPad)
	if r.img != nil {
		r.img.Move(fyne.NewPos(x, (size.Height-actionIcon)/2))
		r.img.Resize(fyne.NewSize(actionIcon, actionIcon))
		x += actionIcon + actionLabelGap
	}
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(x, (size.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *textActionRenderer) Refresh() {
	switch {
	case r.t.state.pressed:
		r.bg.FillColor = theme.Color(theme.ColorNameSelection)
		r.bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
		r.bg.StrokeWidth = 2
	case r.t.state.active():
		r.bg.FillColor = theme.Color(theme.ColorNameHover)
		if r.t.state.focused {
			r.bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
			r.bg.StrokeWidth = hairlineW
		} else {
			r.bg.StrokeWidth = 0
		}
	default:
		r.bg.FillColor = color.Transparent
		r.bg.StrokeWidth = 0
	}
	r.bg.Refresh()
	r.txt.Color = r.t.col
	r.txt.Refresh()
	if r.img != nil {
		r.img.Refresh()
	}
}
func (r *textActionRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *textActionRenderer) Destroy()                     {}
