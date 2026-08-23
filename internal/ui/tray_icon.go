package ui

import (
	"strconv"

	"github.com/laszukdawid/git-repo-tracker/internal/ui/trayicon"
)

// Tray icon states (option 1f). The base is the commit-graph glyph in tray.svg;
// overlays communicate status before the popover is even opened. Precedence when
// several apply: error > behind > dirty > fetching > synced.
// trayStateNow derives the current tray state (and behind-repo count) from the
// latest snapshot, applying the option-1f precedence.
func (a *App) trayStateNow() (trayicon.State, int) {
	behind, anyErr, anyDirty := 0, false, false
	for _, r := range a.all {
		if r.Behind > 0 {
			behind++
		}
		if r.Err != "" || r.FetchErr != "" {
			anyErr = true
		}
		if r.Dirty {
			anyDirty = true
		}
	}
	switch {
	case anyErr:
		return trayicon.Error, 0
	case behind > 0:
		return trayicon.Behind, behind
	case anyDirty:
		return trayicon.Dirty, 0
	case a.anyPulling():
		return trayicon.Fetching, 0
	default:
		return trayicon.Synced, 0
	}
}

// updateTrayIcon recomputes and installs the menu-bar icon for the current state.
// The (state,count) pair is memoised so an unchanged icon isn't re-encoded on every
// debounced refresh.
func (a *App) updateTrayIcon() {
	if a.trayOpen {
		a.trayIconDirty = true
		return
	}

	state, count := a.trayStateNow()
	key := strconv.Itoa(int(state)) + ":" + strconv.Itoa(count)
	if key == a.trayKey {
		return
	}
	a.trayKey = key
	a.desk.SetSystemTrayIcon(trayicon.Resource(state, count))
}
