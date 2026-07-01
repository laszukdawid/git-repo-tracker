// Package ui builds the Fyne system-tray application: a tray menu summarising
// repositories that are behind their remote, and a searchable browser window
// listing every tracked repo with its status. State comes from the monitor,
// which pushes changes through onChange (marshalled onto Fyne's main thread).
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"

	"github.com/dawidlaszuk/git-repo-tracker/internal/config"
	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

const appID = "com.github.dawidlaszuk.git-repo-tracker"

// maxTrayRepos caps how many updatable repos appear directly in the tray menu;
// the rest are reachable through the browser window.
const maxTrayRepos = 12

// Popover window identity and size. The title is how the macOS native helper
// locates the underlying NSWindow to position it (see native_darwin.go). The
// height is dynamic: the window shrinks to fit its rows and grows only up to
// popoverMaxHeight, after which the list scrolls (see desiredPopoverHeight).
const (
	popoverTitle     = "GitRepoTrackerPopover"
	popoverWidth     = 380
	popoverMaxHeight = 480
)

type filterMode int

const (
	filterAll filterMode = iota
	filterUpdatable
	filterDirty
)

type sortMode int

const (
	sortName sortMode = iota
	sortBehind
)

// App holds the shared state wired into the tray and the browser window.
type App struct {
	fyneApp fyne.App
	desk    desktop.App
	cfg     *config.Config
	mgr     *monitor.Manager

	// win is the borderless search popover, toggled by left-clicking the tray
	// icon (see SetSystemTrayWindow in Run). It stays hidden until tapped.
	win fyne.Window

	// Appearance: pal holds the popover's custom colours and variant records which
	// light/dark variant they were built for, so we can tell when the OS flipped.
	pal     palette
	variant fyne.ThemeVariant

	// Browser widgets and view state — only ever touched on the main thread.
	search       *searchEntry
	header       *fyne.Container // search row + (collapsible) options panel; measured when sizing
	optionsPanel *fyne.Container // inline filter/sort toggles, hidden until the ☰ button
	list         *widget.List
	moreBtn      *tipButton
	footer       *fyne.Container // "N repos · N behind" / "N roots" summary bar
	footerLeft   *canvas.Text
	footerRight  *canvas.Text
	tips         *tooltipLayer

	all          []monitor.RepoState // latest full snapshot
	visible      []popoverItem       // grouped headers + repo rows currently shown
	query        string
	filter       filterMode
	sort         sortMode
	groupCount   int             // distinct scan-root sections currently shown (footer "N roots")
	collapsedRow float32         // memoised height of a collapsed repo row
	groupRowH    float32         // memoised height of a group-header row
	collapsedGrp map[string]bool // scan roots the user has folded closed

	popVisible   bool                        // whether the popover is currently shown (for tray toggle)
	lastResign   time.Time                   // when the popover last auto-hid on focus loss
	expandedPath string                      // repo path expanded inline ("" = none)
	pulling      map[string]bool             // repos with a pull in progress
	details      map[string]*monitor.Details // cached commit details for expanded repos
	trayKey      string                      // memoised tray icon state key (skip redundant re-encodes)

	// Popover height animation state (main thread only). popoverH is the last
	// applied height; resizeAnim animates a group collapse/expand smoothly; while
	// suppressAutoResize is set the per-render instant resize is skipped so the
	// animation owns the height.
	popoverH           float32
	resizeAnim         *fyne.Animation
	suppressAutoResize bool
}

// NewApp constructs the application around an already-loaded config.
func NewApp(cfg *config.Config) (*App, error) {
	fyneApp := app.NewWithID(appID)
	desk, ok := fyneApp.(desktop.App)
	if !ok {
		return nil, fmt.Errorf("system tray is not supported on this platform")
	}
	a := &App{
		fyneApp: fyneApp, desk: desk, cfg: cfg, filter: filterAll, sort: sortBehind,
		pulling: map[string]bool{}, details: map[string]*monitor.Details{},
		collapsedGrp: map[string]bool{},
	}
	a.applyTheme() // resolve the configured appearance before any UI is built
	a.mgr = monitor.New(cfg, a.onChange, a.logf)
	return a, nil
}

// applyTheme resolves the configured appearance into a concrete light/dark
// variant, records it (pal + variant) and installs it as the Fyne theme. Call it
// before building UI and whenever the theme setting changes.
func (a *App) applyTheme() {
	v, forced := a.resolveVariant()
	a.variant = v
	a.pal = paletteFor(v)
	a.fyneApp.Settings().SetTheme(glassTheme{variant: v, forced: forced})
}

// resolveVariant maps the configured theme mode to a concrete variant. "System"
// follows the OS appearance Fyne reports; light/dark force a fixed variant.
func (a *App) resolveVariant() (variant fyne.ThemeVariant, forced bool) {
	switch a.cfg.ThemeMode() {
	case config.ThemeLight:
		return theme.VariantLight, true
	case config.ThemeDark:
		return theme.VariantDark, true
	default:
		return a.fyneApp.Settings().ThemeVariant(), false
	}
}

// Run builds the UI, starts the monitor and blocks until quit.
func (a *App) Run() {
	a.buildPopover()
	a.rebuildTray()
	a.updateTrayIcon()
	// Left-click the tray icon toggles the search popover; with no secondary
	// handler set, right-click falls through to the menu above. We register the
	// handler ourselves (rather than desk.SetSystemTrayWindow) so we can also
	// position and focus the window on each open.
	systray.SetOnTapped(a.togglePopover)
	// Dismiss the popover when the user clicks outside the app, like a real
	// menu-bar popover.
	watchPopoverAutoHide(a.onPopoverResign)
	a.mgr.Start()
	a.fyneApp.Run()
}

// togglePopover shows the popover (positioned + focused) or hides it if already
// visible. Invoked from the systray click handler, so it marshals onto the main
// thread.
func (a *App) togglePopover() {
	fyne.Do(func() {
		if a.popVisible {
			a.hidePopover()
			return
		}
		// If the popover was just auto-hidden because this same click moved focus
		// away from it, don't immediately reopen it — treat the click as a close.
		if time.Since(a.lastResign) < 300*time.Millisecond {
			return
		}
		a.showWindow()
	})
}

// onPopoverResign hides the popover when the app loses active status (the user
// clicked elsewhere). Invoked from the native observer on the main thread.
func (a *App) onPopoverResign() {
	fyne.Do(func() {
		a.lastResign = time.Now()
		if a.popVisible {
			a.hidePopover()
		}
	})
}

// onChange is invoked from monitor goroutines; marshal UI work onto the main
// thread, which Fyne requires.
func (a *App) onChange() {
	fyne.Do(a.refresh)
}

// refresh pulls the latest snapshot and updates the list, the tray menu, and the
// menu-bar icon state.
func (a *App) refresh() {
	a.all = a.mgr.Snapshot()
	a.applyFilter()
	a.rebuildTray()
	a.updateTrayIcon()
}

// activate runs the configured click action for a repo.
func (a *App) activate(r monitor.RepoState) {
	action, custom := a.cfg.Click()
	if err := runAction(action, custom, r.Path); err != nil {
		a.logf("open %s: %v", r.Name, err)
	}
}

// reloadConfig re-reads the file from disk so hand-edits take effect, then
// triggers a rescan.
func (a *App) reloadConfig() {
	if err := a.cfg.Reload(); err != nil {
		a.logf("reload failed: %v", err)
		return
	}
	a.logf("config reloaded from %s", a.cfg.Path())
	// A hand-edited theme: takes effect too. The palette depends only on the
	// resolved variant, so rebuild the popover's baked-in colours only if it changed.
	prevVariant := a.variant
	a.applyTheme()
	if a.variant != prevVariant {
		a.buildPopoverContent()
	}
	a.mgr.Refresh()
	a.refresh()
}

// openConfigInEditor reveals the config file using the OS default handler.
func (a *App) openConfigInEditor() {
	path := a.cfg.Path()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// Open with the default handler via ShellExecute, without a shell string
		// that would re-interpret special characters in the path.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		a.logf("open config failed: %v", err)
	}
}

func (a *App) quit() {
	a.mgr.Stop()
	a.fyneApp.Quit()
}

// logf prints a timestamped diagnostic line to stderr.
func (a *App) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05")+"  "+format+"\n", args...)
}
