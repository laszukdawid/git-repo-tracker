package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// interactionState is the visual contract shared by hand-drawn controls.
// Setters report whether a renderer refresh is needed.
type interactionState struct {
	hovered bool
	pressed bool
	focused bool
}

func (s *interactionState) setHovered(v bool) bool { return s.set(&s.hovered, v) }
func (s *interactionState) setPressed(v bool) bool { return s.set(&s.pressed, v) }
func (s *interactionState) setFocused(v bool) bool { return s.set(&s.focused, v) }

func (s *interactionState) set(target *bool, v bool) bool {
	if *target == v {
		return false
	}
	*target = v
	return true
}

func (s interactionState) active() bool { return s.hovered || s.pressed || s.focused }

func (s *interactionState) clear() bool {
	changed := s.active()
	s.hovered, s.pressed, s.focused = false, false, false
	return changed
}

// keyboardActivate keeps the pressed state set while the callback runs and
// releases it on the next UI turn, leaving enough time for a visible pulse.
func keyboardActivate(state *interactionState, refresh, tapped func()) {
	if state.setPressed(true) {
		refresh()
	}
	tapped()
	fyne.Do(func() {
		if state.setPressed(false) {
			refresh()
		}
	})
}

func unfocusCanvasObjects(objects ...fyne.CanvasObject) {
	app := fyne.CurrentApp()
	if app == nil {
		return
	}
	for _, object := range objects {
		canvas := app.Driver().CanvasForObject(object)
		if canvas == nil {
			continue
		}
		focused := canvas.Focused()
		for _, target := range objects {
			focusable, ok := target.(fyne.Focusable)
			if ok && focused == focusable {
				canvas.Unfocus()
				return
			}
		}
	}
}

type sizeOverrideTheme struct {
	fyne.Theme
	name  fyne.ThemeSizeName
	value float32
}

func (t sizeOverrideTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == t.name {
		return t.value
	}
	return t.Theme.Size(name)
}

func searchTheme(base fyne.Theme) fyne.Theme {
	return sizeOverrideTheme{Theme: base, name: theme.SizeNameInnerPadding, value: searchInnerPad}
}

// actionClusterLayout packs compact action targets with the shared sibling gap.
type actionClusterLayout struct{}

func (actionClusterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, object := range objects {
		if !object.Visible() {
			continue
		}
		itemSize := object.MinSize()
		object.Move(fyne.NewPos(x, (size.Height-itemSize.Height)/2))
		object.Resize(itemSize)
		x += itemSize.Width + actionSiblingGap
	}
}

func (actionClusterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var width, height float32
	visible := 0
	for _, object := range objects {
		if !object.Visible() {
			continue
		}
		itemSize := object.MinSize()
		width += itemSize.Width
		height = max(height, itemSize.Height)
		visible++
	}
	if visible > 1 {
		width += actionSiblingGap * float32(visible-1)
	}
	return fyne.NewSize(width, height)
}

// searchToolbarLayout gives the search field explicit breathing room before
// the fixed-size action cluster.
type searchToolbarLayout struct{}

func (searchToolbarLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	field, toolbar := objects[0], objects[1]
	toolbarSize := toolbar.MinSize()
	toolbarX := max(float32(0), size.Width-toolbarSize.Width)
	toolbar.Move(fyne.NewPos(toolbarX, (size.Height-toolbarSize.Height)/2))
	toolbar.Resize(toolbarSize)
	fieldWidth := max(float32(0), toolbarX-actionClusterGap)
	field.Move(fyne.NewPos(0, 0))
	field.Resize(fyne.NewSize(fieldWidth, size.Height))
}

func (searchToolbarLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 {
		return fyne.NewSize(0, 0)
	}
	field, toolbar := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(field.Width+actionClusterGap+toolbar.Width, max(field.Height, toolbar.Height))
}

// glassTheme is a "glassy" RepoZ-like theme with a dark and a light variant. True
// OS translucency isn't reachable through stock Fyne, so we approximate it with
// deep translucent darks (or soft whites), an accent blue, and tight padding; the
// popover layers these over a gradient backdrop (see buildPopoverContent).
//
// When forced is set the theme ignores the OS variant Fyne passes and always
// renders the chosen variant, so "Light"/"Dark" settings override the system
// appearance; with forced unset it follows the OS ("System").
type glassTheme struct {
	family  paletteFamily
	variant fyne.ThemeVariant
	forced  bool
}

var _ fyne.Theme = glassTheme{}

// Custom theme colour names for the filtering-header chips. Fyne colourises an SVG
// icon by theme-colour name, so exposing the chip label colours here lets an icon
// be tinted to exactly the text beside it (see segChip): colorNameChipIcon matches
// the muted label, colorNameChipIconOn matches the selected (accent-filled) label.
const (
	colorNameChipIcon   fyne.ThemeColorName = "grtChipIcon"
	colorNameChipIconOn fyne.ThemeColorName = "grtChipIconOn"

	// Status-glyph colour names. Fyne tints an SVG resource by theme-colour name,
	// so exposing the semantic status colours here lets the repo-row glyphs
	// (down-arrow / check) be painted amber/green without baking the hue into each
	// SVG. They repaint on a theme flip like any other themed resource.
	colorNameBehind fyne.ThemeColorName = "grtBehind"
	colorNameSynced fyne.ThemeColorName = "grtSynced"
	// Muted foreground for flat header icons (search/update-all/menu).
	colorNameMuted fyne.ThemeColorName = "grtMuted"
)

func (g glassTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if g.forced {
		v = g.variant
	}
	switch name {
	case colorNameChipIcon:
		return paletteFor(g.family, v).rowSub // same muted colour as an unselected chip's label
	case colorNameChipIconOn:
		return chipSelectedText // same colour as a selected chip's label
	case colorNameBehind:
		return paletteFor(g.family, v).statusBehind
	case colorNameSynced:
		return paletteFor(g.family, v).statusSynced
	case colorNameMuted:
		return paletteFor(g.family, v).muted
	}
	return stockColor(name, paletteFor(g.family, v), v)
}

// stockColor answers Fyne's own widgets from the active palette, so a stock
// Entry, Button or Select is coloured by the same decisions as the hand-drawn
// rows beside it. Previously these were a separate hard-coded pair of light and
// dark sets, which is how the two drifted apart.
func stockColor(name fyne.ThemeColorName, p palette, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return p.gradTop
	case theme.ColorNameForeground:
		return p.rowName
	case theme.ColorNameInputBackground:
		return p.fieldBg
	case theme.ColorNameButton:
		return color.Transparent
	case theme.ColorNameHover:
		return p.btnHover
	case theme.ColorNameSelection:
		return p.pullBtnBg
	case theme.ColorNameFocus:
		return p.pullBtnBg
	case theme.ColorNameSeparator:
		return p.hairline
	case theme.ColorNamePlaceHolder:
		return p.faint
	case theme.ColorNameDisabled:
		return p.faint
	case theme.ColorNamePrimary:
		return p.accent
	case theme.ColorNameScrollBar:
		return p.toggleOffBg
	default:
		return theme.DefaultTheme().Color(name, v)
	}
}

func (glassTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }

func (glassTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

// Size publishes the design tokens (tokens.go) that Fyne's own widgets read, so
// a stock Entry, Button or Select picks up the same scale as the hand-drawn
// widgets beside it.
//
// SizeNameInputRadius is the important one. Fyne's default is 5, which is why
// the search field looked nearly rectangular inside a window with a 20px corner
// — the two were simply never reconciled. widget.Entry reads this name for both
// its box and its border on every Refresh (entry.go:181,185 and 1691-1692 in
// v2.7.4), and widget.Button and widget.Select read it too, so one line here
// rounds every input in the app consistently.
func (glassTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return spaceSm
	case theme.SizeNameInnerPadding:
		return spaceSm
	case theme.SizeNameText:
		return textMd
	case theme.SizeNameInputRadius:
		return radiusMd
	case theme.SizeNameSelectionRadius:
		return radiusSm
	case theme.SizeNameInputBorder:
		return hairlineW
	default:
		return theme.DefaultTheme().Size(name)
	}
}

// palette holds the popover's hand-tuned colours that aren't part of Fyne's theme
// palette — repo row text, hover tints, the glass gradient and tooltip chrome.
// These are baked into canvas objects at build time (Fyne won't repaint them on a
// theme change), so the popover content is rebuilt when the variant changes.
type palette struct {
	// accent is the interactive colour: a selected chip, a switch that is on, the
	// pull affordance. It is deliberately separate from rowName — repository
	// names are neutral in every palette now, and the two were the same value
	// only back when names were painted accent blue.
	accent     color.Color
	rowName    color.Color // repo title
	rowSub     color.Color // status / detail lines (muted)
	rowHover   color.Color // row background on hover
	btnHover   color.Color // action-icon background on hover
	detailKey  color.Color // "Local"/"Origin" labels in the detail panel
	gradTop    color.Color // popover backdrop gradient, top
	gradBottom color.Color // popover backdrop gradient, bottom
	tipBg      color.Color
	tipBorder  color.Color
	tipText    color.Color

	// How this palette draws status. Colour is not the only channel available:
	// Signal says "clean" by drawing nothing and "dirty" with an outline rather
	// than a fill, which is quieter than any hue could be.
	syncedGlyphHidden bool
	dirtyOutlined     bool

	// Status semantics — used sparingly so the list still reads calm. Each has a
	// soft tinted background for the icon chip variants.
	statusBehind color.Color
	// statusAhead is deliberately a different hue from statusBehind: incoming
	// work and outgoing work are different jobs, and the same colour for both
	// would leave the shape of the arrow doing all the work.
	statusAhead  color.Color
	statusDirty  color.Color
	statusSynced color.Color
	statusError  color.Color
	behindTint   color.Color
	dirtyTint    color.Color
	syncedTint   color.Color

	// Surfaces & text steps shared by the popover (3a) and settings (1e).
	muted         color.Color // #97a1b0 — secondary text, flat header icons
	faint         color.Color // #67717f — branch, meta, footer
	syncedName    color.Color // dimmed repo name on a synced (up-to-date) row
	groupHeaderBg color.Color // scan-root section header background
	rowExpandedBg color.Color // raised background behind an expanded row
	pillBehindBg  color.Color // amber "N behind" group pill fill
	cardBg        color.Color // grouped settings cards / dir cards
	cardBorder    color.Color // 1px hairline around cards
	fieldBg       color.Color // settings input fields
	fieldBorder   color.Color // settings input borders
	titlebarBg    color.Color // raised titlebar band
	footerBg      color.Color // settings footer / save bar
	hairline      color.Color // thin white separators inside surfaces
	toggleOffBg   color.Color // pill switch track, off
	toggleKnobOff color.Color // pill switch knob, off
	pullBtnBg     color.Color // hover action chip: pull (accent tint)
	pushBtnBg     color.Color // branch chip: push (ahead tint, so it is visibly not a pull)
	openBtnBg     color.Color // hover action chip: open (neutral tint)
}
