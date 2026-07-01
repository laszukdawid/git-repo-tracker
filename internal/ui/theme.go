package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// glassTheme is a "glassy" RepoZ-like theme with a dark and a light variant. True
// OS translucency isn't reachable through stock Fyne, so we approximate it with
// deep translucent darks (or soft whites), an accent blue, and tight padding; the
// popover layers these over a gradient backdrop (see buildPopoverContent).
//
// When forced is set the theme ignores the OS variant Fyne passes and always
// renders the chosen variant, so "Light"/"Dark" settings override the system
// appearance; with forced unset it follows the OS ("System").
type glassTheme struct {
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
)

func (g glassTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if g.forced {
		v = g.variant
	}
	switch name {
	case colorNameChipIcon:
		return paletteFor(v).rowSub // same muted colour as an unselected chip's label
	case colorNameChipIconOn:
		return chipSelectedText // same colour as a selected chip's label
	}
	if v == theme.VariantLight {
		return lightColor(name)
	}
	return darkColor(name)
}

func darkColor(name fyne.ThemeColorName) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 18, G: 21, B: 27, A: 250}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 236, G: 240, B: 246, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 31, G: 36, B: 46, A: 235}
	case theme.ColorNameButton:
		return color.NRGBA{R: 35, G: 41, B: 52, A: 0}
	case theme.ColorNameHover:
		return color.NRGBA{R: 74, G: 144, B: 255, A: 45}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 74, G: 144, B: 255, A: 70}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 28}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 130, G: 138, B: 150, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 120, G: 128, B: 140, A: 200}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 91, G: 157, B: 255, A: 255}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 40}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func lightColor(name fyne.ThemeColorName) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 244, G: 246, B: 250, A: 252}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 28, G: 33, B: 42, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 235}
	case theme.ColorNameButton:
		return color.NRGBA{R: 230, G: 234, B: 240, A: 0}
	case theme.ColorNameHover:
		return color.NRGBA{R: 30, G: 110, B: 230, A: 38}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 30, G: 110, B: 230, A: 60}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 30}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 120, G: 128, B: 140, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 150, G: 156, B: 166, A: 220}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 25, G: 103, B: 224, A: 255}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 55}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantLight)
	}
}

func (glassTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }

func (glassTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (glassTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 7
	case theme.SizeNameInnerPadding:
		return 6
	default:
		return theme.DefaultTheme().Size(name)
	}
}

// palette holds the popover's hand-tuned colours that aren't part of Fyne's theme
// palette — repo row text, hover tints, the glass gradient and tooltip chrome.
// These are baked into canvas objects at build time (Fyne won't repaint them on a
// theme change), so the popover content is rebuilt when the variant changes.
type palette struct {
	rowName    color.Color // repo title (accent blue)
	rowSub     color.Color // status / detail lines (muted)
	rowHover   color.Color // row background on hover
	btnHover   color.Color // action-icon background on hover
	detailKey  color.Color // "Local"/"Origin" labels in the detail panel
	gradTop    color.Color // popover backdrop gradient, top
	gradBottom color.Color // popover backdrop gradient, bottom
	tipBg      color.Color
	tipBorder  color.Color
	tipText    color.Color
}

// paletteFor returns the custom popover colours for the given appearance variant.
func paletteFor(v fyne.ThemeVariant) palette {
	if v == theme.VariantLight {
		return palette{
			rowName:    color.NRGBA{R: 25, G: 103, B: 224, A: 255},
			rowSub:     color.NRGBA{R: 92, G: 100, B: 112, A: 255},
			rowHover:   color.NRGBA{R: 0, G: 0, B: 0, A: 12},
			btnHover:   color.NRGBA{R: 0, G: 0, B: 0, A: 26},
			detailKey:  color.NRGBA{R: 52, G: 58, B: 70, A: 255},
			gradTop:    color.NRGBA{R: 250, G: 251, B: 253, A: 255},
			gradBottom: color.NRGBA{R: 232, G: 236, B: 242, A: 255},
			tipBg:      color.NRGBA{R: 250, G: 251, B: 253, A: 252},
			tipBorder:  color.NRGBA{R: 0, G: 0, B: 0, A: 40},
			tipText:    color.NRGBA{R: 28, G: 33, B: 42, A: 255},
		}
	}
	return palette{
		rowName:    color.NRGBA{R: 91, G: 157, B: 255, A: 255},
		rowSub:     color.NRGBA{R: 168, G: 178, B: 192, A: 235},
		rowHover:   color.NRGBA{R: 255, G: 255, B: 255, A: 14},
		btnHover:   color.NRGBA{R: 255, G: 255, B: 255, A: 38},
		detailKey:  color.NRGBA{R: 210, G: 216, B: 226, A: 255},
		gradTop:    color.NRGBA{R: 26, G: 31, B: 41, A: 255},
		gradBottom: color.NRGBA{R: 10, G: 13, B: 20, A: 255},
		tipBg:      color.NRGBA{R: 38, G: 42, B: 51, A: 250},
		tipBorder:  color.NRGBA{R: 255, G: 255, B: 255, A: 40},
		tipText:    color.NRGBA{R: 230, G: 235, B: 242, A: 255},
	}
}
