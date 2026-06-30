package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// glassTheme is a dark, semi-transparent theme that gives the popover a "glassy"
// RepoZ-like look. True OS translucency isn't reachable through stock Fyne, so we
// approximate it with deep translucent darks, an accent blue, and tight padding;
// the popover layers these over a gradient backdrop (see buildPopover).
type glassTheme struct{}

var _ fyne.Theme = glassTheme{}

func (glassTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
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
