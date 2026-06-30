package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed tray.svg
var traySVG []byte

// trayIcon returns the menu-bar icon as a themed (template) resource, so macOS
// tints it to match the light/dark menu bar.
func trayIcon() fyne.Resource {
	return theme.NewThemedResource(fyne.NewStaticResource("tray.svg", traySVG))
}
