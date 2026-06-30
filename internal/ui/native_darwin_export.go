//go:build darwin

package ui

// This file is intentionally separate from native_darwin.go: a cgo file using
// //export may only contain declarations in its preamble, not the C function
// definitions that live in native_darwin.go.

/*
 */
import "C"

//export grtAppResignedActive
func grtAppResignedActive() {
	if popoverAutoHide != nil {
		popoverAutoHide()
	}
}
