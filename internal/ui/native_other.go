//go:build !darwin

package ui

// placePopover is a no-op on platforms where we can't position the window. Stock
// Fyne centers splash windows; on Wayland in particular, forced positioning is
// unreliable, so we accept the default placement.
func placePopover(title string, width, height float32) {}

// watchPopoverAutoHide is a no-op off macOS.
func watchPopoverAutoHide(hide func()) {}
