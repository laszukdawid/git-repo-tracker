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
		return paletteFor(v).rowSub // same muted colour as an unselected chip's label
	case colorNameChipIconOn:
		return chipSelectedText // same colour as a selected chip's label
	case colorNameBehind:
		return paletteFor(v).statusBehind
	case colorNameSynced:
		return paletteFor(v).statusSynced
	case colorNameMuted:
		return paletteFor(v).muted
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

	// Status semantics (option 3a) — used sparingly so the list still reads calm:
	// amber = behind, coral = dirty, green = synced, red = error. Each has a soft
	// tinted background for the icon chip variants.
	statusBehind color.Color
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
	openBtnBg     color.Color // hover action chip: open (neutral tint)
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

			// Light equivalents share the status hues at higher chroma / lower
			// lightness so they read on a pale surface.
			statusBehind: color.NRGBA{R: 176, G: 122, B: 26, A: 255},
			statusDirty:  color.NRGBA{R: 184, G: 90, B: 61, A: 255},
			statusSynced: color.NRGBA{R: 63, G: 138, B: 92, A: 255},
			statusError:  color.NRGBA{R: 192, G: 53, B: 58, A: 255},
			behindTint:   color.NRGBA{R: 176, G: 122, B: 26, A: 34},
			dirtyTint:    color.NRGBA{R: 184, G: 90, B: 61, A: 30},
			syncedTint:   color.NRGBA{R: 63, G: 138, B: 92, A: 28},

			muted:         color.NRGBA{R: 92, G: 100, B: 112, A: 255},
			faint:         color.NRGBA{R: 120, G: 128, B: 140, A: 255},
			syncedName:    color.NRGBA{R: 90, G: 110, B: 150, A: 255},
			groupHeaderBg: color.NRGBA{R: 0, G: 0, B: 0, A: 8},
			rowExpandedBg: color.NRGBA{R: 0, G: 0, B: 0, A: 10},
			pillBehindBg:  color.NRGBA{R: 176, G: 122, B: 26, A: 34},
			cardBg:        color.NRGBA{R: 255, G: 255, B: 255, A: 235},
			cardBorder:    color.NRGBA{R: 0, G: 0, B: 0, A: 20},
			fieldBg:       color.NRGBA{R: 255, G: 255, B: 255, A: 235},
			fieldBorder:   color.NRGBA{R: 0, G: 0, B: 0, A: 30},
			titlebarBg:    color.NRGBA{R: 236, G: 239, B: 244, A: 255},
			footerBg:      color.NRGBA{R: 240, G: 242, B: 246, A: 255},
			hairline:      color.NRGBA{R: 0, G: 0, B: 0, A: 15},
			toggleOffBg:   color.NRGBA{R: 0, G: 0, B: 0, A: 36},
			toggleKnobOff: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
			pullBtnBg:     color.NRGBA{R: 25, G: 103, B: 224, A: 36},
			openBtnBg:     color.NRGBA{R: 0, G: 0, B: 0, A: 15},
		}
	}
	return palette{
		rowName:    color.NRGBA{R: 91, G: 157, B: 255, A: 255},
		rowSub:     color.NRGBA{R: 151, G: 161, B: 176, A: 255},
		rowHover:   color.NRGBA{R: 255, G: 255, B: 255, A: 14},
		btnHover:   color.NRGBA{R: 255, G: 255, B: 255, A: 38},
		detailKey:  color.NRGBA{R: 210, G: 216, B: 226, A: 255},
		gradTop:    color.NRGBA{R: 26, G: 31, B: 41, A: 255},
		gradBottom: color.NRGBA{R: 10, G: 13, B: 20, A: 255},
		tipBg:      color.NRGBA{R: 38, G: 42, B: 51, A: 250},
		tipBorder:  color.NRGBA{R: 255, G: 255, B: 255, A: 40},
		tipText:    color.NRGBA{R: 230, G: 235, B: 242, A: 255},

		// Dark variant — the design's default glass look (all tokens are the dark
		// values from the handoff).
		statusBehind: color.NRGBA{R: 230, G: 169, B: 77, A: 255},  // #e6a94d amber
		statusDirty:  color.NRGBA{R: 232, G: 130, B: 95, A: 255},  // #e8825f coral
		statusSynced: color.NRGBA{R: 108, G: 195, B: 138, A: 255}, // #6cc38a green
		statusError:  color.NRGBA{R: 229, G: 72, B: 77, A: 255},   // #e5484d red
		behindTint:   color.NRGBA{R: 230, G: 169, B: 77, A: 41},   // rgba(...,.16)
		dirtyTint:    color.NRGBA{R: 232, G: 130, B: 95, A: 36},   // rgba(...,.14)
		syncedTint:   color.NRGBA{R: 108, G: 195, B: 138, A: 33},  // rgba(...,.13)

		muted:         color.NRGBA{R: 151, G: 161, B: 176, A: 255}, // #97a1b0
		faint:         color.NRGBA{R: 103, G: 113, B: 127, A: 255}, // #67717f
		syncedName:    color.NRGBA{R: 127, G: 151, B: 196, A: 255}, // #7f97c4
		groupHeaderBg: color.NRGBA{R: 255, G: 255, B: 255, A: 5},   // rgba(255,255,255,.02)
		rowExpandedBg: color.NRGBA{R: 255, G: 255, B: 255, A: 8},   // rgba(255,255,255,.03)
		pillBehindBg:  color.NRGBA{R: 230, G: 169, B: 77, A: 41},   // amber @ .16
		cardBg:        color.NRGBA{R: 27, G: 33, B: 44, A: 255},    // #1b212c
		cardBorder:    color.NRGBA{R: 255, G: 255, B: 255, A: 18},  // rgba(255,255,255,.07)
		fieldBg:       color.NRGBA{R: 18, G: 21, B: 27, A: 255},    // #12151b
		fieldBorder:   color.NRGBA{R: 255, G: 255, B: 255, A: 26},  // rgba(255,255,255,.1)
		titlebarBg:    color.NRGBA{R: 23, G: 28, B: 37, A: 255},    // #171c25
		footerBg:      color.NRGBA{R: 14, G: 17, B: 23, A: 255},    // #0e1117
		hairline:      color.NRGBA{R: 255, G: 255, B: 255, A: 15},  // rgba(255,255,255,.06)
		toggleOffBg:   color.NRGBA{R: 255, G: 255, B: 255, A: 36},  // rgba(255,255,255,.14)
		toggleKnobOff: color.NRGBA{R: 195, G: 203, B: 214, A: 255}, // #c3cbd6
		pullBtnBg:     color.NRGBA{R: 91, G: 157, B: 255, A: 41},   // rgba(91,157,255,.16)
		openBtnBg:     color.NRGBA{R: 255, G: 255, B: 255, A: 15},  // rgba(255,255,255,.06)
	}
}
