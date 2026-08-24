//go:build !darwin

package ui

// placePopover is a no-op on platforms where we can't position the window. Stock
// Fyne centers splash windows; on Wayland in particular, forced positioning is
// unreliable, so we accept the default placement.
func placePopover(title string, width, height float32) {}

// resizePopover is a no-op off macOS; the Fyne-level Resize already adjusts the
// window, and native top-anchored resizing isn't available here.
func resizePopover(title string, width, height float32) {}

// watchPopoverAutoHide is a no-op off macOS.
func watchPopoverAutoHide(hide func()) {}

// setMenuBarAgent is a no-op off macOS. Hiding the Dock icon is a macOS concept;
// Linux/Windows tray behaviour is left to the host.
func setMenuBarAgent() {}

// activateApp is a no-op off macOS; the host window manager handles focusing a
// newly shown window.
func activateApp() {}

// chooseFolderNative is unavailable off macOS; the false return tells callers to
// use the Fyne in-app folder dialog instead.
func chooseFolderNative(initialDir string) (string, bool) { return "", false }

// chooseApplicationNative is unavailable off macOS; callers fall back to the
// Fyne in-app file dialog.
func chooseApplicationNative() (string, bool) { return "", false }
