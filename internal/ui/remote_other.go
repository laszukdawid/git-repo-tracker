//go:build !linux

package ui

func TryHandleRemote(RunOptions) bool {
	return false
}

func (a *App) claimRemote(RunOptions) bool {
	return true
}
