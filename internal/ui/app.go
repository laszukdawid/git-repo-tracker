// Package ui builds the Fyne system-tray application: a tray menu summarising
// repositories that are behind their remote, and a searchable browser window
// listing every tracked repo with its status. State comes from the monitor,
// which pushes changes through onChange (marshalled onto Fyne's main thread).
package ui

import (
	"fmt"
	"io"
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

	"github.com/laszukdawid/git-repo-tracker/internal/backend"
	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

const appID = "com.github.laszukdawid.git-repo-tracker"

// maxTrayRepos caps how many updatable repos appear directly in the tray menu;
// the rest are reachable through the browser window.
const maxTrayRepos = 12

// Popover window identity and size. The title is how the macOS native helper
// locates the underlying NSWindow to position it (see native_darwin.go). The
// height is dynamic: the window shrinks to fit its rows and grows only up to
// popoverMaxHeight, after which the list scrolls (see desiredPopoverHeight).
const (
	popoverTitle = "GitRepoTrackerPopover"
	// Wider than it was: two-line rows plus the branch list need the room, and
	// long repository names were being truncated at 380.
	popoverWidth = 430
	// Two-line rows are taller than the old adaptive ones, so the cap rises to
	// keep roughly the same number of repositories visible at once.
	popoverMaxHeight = 560
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
	mgr     *backend.Service

	// win is the borderless search popover, toggled by left-clicking the tray
	// icon (see SetSystemTrayWindow in Run). It stays hidden until tapped.
	win fyne.Window

	// settingsWin is the single settings window instance, reused and focused when
	// the user clicks Settings while it is already open.
	settingsWin fyne.Window

	// Appearance: pal holds the popover's custom colours and variant records which
	// light/dark variant they were built for, so we can tell when the OS flipped.
	pal     palette
	family  paletteFamily
	variant fyne.ThemeVariant

	// Browser widgets and view state — only ever touched on the main thread.
	search       *searchEntry
	header       *fyne.Container // search row + (collapsible) options panel; measured when sizing
	optionsPanel *fyne.Container // inline filter/sort toggles, hidden until the ☰ button
	list         *widget.List
	moreBtn      *headerButton
	updateAllBtn *headerButton
	footer       *fyne.Container // "N repos · N behind" / "N roots" summary bar
	footerLeft   *canvas.Text
	footerRight  *canvas.Text
	tips         *tooltipLayer
	// ideIcons caches editor artwork by editor id. Reading an application's icon
	// means opening its bundle, which is not something to redo per row per paint.
	ideIcons map[string]fyne.Resource
	// The editor chip's resolved appearance, shared by every row. ideGen is
	// bumped whenever the choice changes so the cache is rebuilt exactly then.
	ideCached   rowState
	ideGen      uint64
	ideCacheGen uint64

	all          []monitor.RepoState // latest full snapshot
	visible      []popoverItem       // grouped headers + repo rows currently shown
	query        string
	filter       filterMode
	sort         sortMode
	groupCount   int               // distinct scan-root sections currently shown (footer "N roots")
	collapsedRow float32           // memoised height of a repo row (identical for every repo)
	expandedKey  expandedHeightKey // what the memoised expanded height was measured from
	expandedH    float32           // memoised height of the one expanded row
	groupRowH    float32           // memoised height of a group-header row
	collapsedGrp map[string]bool   // scan roots the user has folded closed

	// Keyboard selection in the popover list: selPath is the repo highlighted by
	// Up/Down (empty = none) and selIdx its index in visible (-1 = none). The path
	// is the source of truth so the highlight survives a re-filter; the index is
	// re-derived in applyFilter.
	selPath string
	selIdx  int

	// Background-activity mirror, main thread only. act is the last progress
	// snapshot pulled from the monitor; tally accumulates completions so a batch
	// of pulls reports one sentence rather than a stream; transient is that
	// sentence while it is on screen.
	act            monitor.Activity
	tally          pullTally
	transient      string
	transientUntil time.Time
	expiryTimer    *time.Timer // single sweeper for every transient state

	popVisible   bool                        // whether the popover is currently shown (for tray toggle)
	lastResign   time.Time                   // when the popover last auto-hid on focus loss
	expandedPath string                      // repo path expanded inline ("" = none)
	rowStatus    map[string]*rowStatus       // transient per-repo pull state, keyed by path
	details      map[string]*monitor.Details // cached commit details for expanded repos

	// Branch state, all keyed by repo path and all main-thread only. It is
	// deliberately here rather than in the monitor: a branch listing is large and
	// goes stale on any commit, so it is read on demand and never persisted.
	// branchGen is bumped on every change and is what makes the section state
	// comparable — see branchSectionState.
	branches      map[string]*monitor.BranchList
	branchOpen    map[string]bool
	branchLoading map[string]bool
	branchBusy    map[branchKey]bool
	branchErr     map[branchKey]string
	branchGen     map[string]uint64
	// branchLimit is how many of a repository's branches are rendered; it grows
	// a page at a time when the user asks for more.
	branchLimit map[string]int
	// branchFetching marks repositories with an explicit fetch in flight.
	branchFetching map[string]bool
	// matchHint records, per repository, the branch that a search matched when
	// the repository's own name and path did not.
	matchHint map[string]string
	// bindex is the branch-name index the search box consults, so a branch can
	// be found without having opened the repository that holds it.
	bindex *branchIndex
	// lastSeenFetch remembers each repository's fetch stamp so the search index
	// can be invalidated even for repositories whose branches were never opened.
	lastSeenFetch map[string]time.Time

	trayKey       string      // memoised tray icon state key (skip redundant re-encodes)
	trayOpen      bool        // native tray menu is probably open; avoid replacing it under the cursor
	trayDirty     bool        // a tray rebuild was requested while the menu was open
	trayIconDirty bool        // a tray icon update was requested while the menu was open
	trayBuilt     bool        // Linux native menus are snapshot-based to avoid AppIndicator flicker
	trayTimer     *time.Timer // best-effort tray-open timeout; systray has no close event
	remoteCloser  io.Closer   // Linux D-Bus single-instance listener; nil on other platforms

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
	a := &App{fyneApp: fyneApp, desk: desk, cfg: cfg, filter: filterAll, sort: sortBehind}
	a.initState()
	a.applyTheme() // resolve the configured appearance before any UI is built
	a.mgr = backend.New(cfg, a.onChange, a.logf)
	a.mgr.SetOnActivity(a.onActivity)
	a.installEditorResolver()
	return a, nil
}

// initState allocates every map the UI mutates and sets the fields whose zero
// value is wrong. It is separate from NewApp so tests that build an App directly
// get the same footing — a missing map here is a nil-map panic on the first
// interaction, not a compile error.
func (a *App) initState() {
	a.selIdx = -1
	a.rowStatus = map[string]*rowStatus{}
	a.details = map[string]*monitor.Details{}
	a.collapsedGrp = map[string]bool{}
	a.branches = map[string]*monitor.BranchList{}
	a.branchOpen = map[string]bool{}
	a.branchLoading = map[string]bool{}
	a.branchBusy = map[branchKey]bool{}
	a.branchErr = map[branchKey]string{}
	a.branchGen = map[string]uint64{}
	a.branchLimit = map[string]int{}
	a.branchFetching = map[string]bool{}
	a.matchHint = map[string]string{}
	a.ideIcons = map[string]fyne.Resource{}
	a.bindex = newBranchIndex()
	a.lastSeenFetch = map[string]time.Time{}
	a.ideGen, a.ideCacheGen = 1, 0 // force the first resolve
}

// onActivity is called (debounced) from a monitor goroutine when background
// progress changes. It only hops threads; everything else happens on the main
// thread in applyActivity.
func (a *App) onActivity() { fyne.Do(a.applyActivity) }

// applyActivity pulls the monitor's current progress and any queued completions,
// and repaints the status line. Main thread only.
func (a *App) applyActivity() {
	// Always drain, even with the popover closed. Otherwise a keep-fresh pull
	// that happened an hour ago would be announced the moment the popover next
	// opens, as though it had just occurred.
	events := a.mgr.DrainActivityEvents()
	if !a.popVisible {
		return
	}
	a.tally = foldEvents(a.tally, events)
	a.act = a.mgr.Activity()
	// A batch is over once nothing is pulling any more; that is when its one
	// summary sentence is emitted.
	if a.act.Kind != monitor.ActivityPulling {
		a.flushTally()
	}
	a.updateFooter()
}

// flushTally turns an accumulated batch into a single transient message.
func (a *App) flushTally() {
	msg := tallyMessage(a.tally)
	a.tally = pullTally{}
	if msg == "" {
		return
	}
	a.transient = msg
	a.transientUntil = time.Now().Add(transientHold)
	a.rearmExpiry()
}

// setRowStatus records a repository's transient pull state and repaints.
func (a *App) setRowStatus(path string, st *rowStatus) {
	if st == nil {
		delete(a.rowStatus, path)
	} else {
		a.rowStatus[path] = st
	}
	a.rearmExpiry()
	a.applyFilter()
}

// rearmExpiry points the single sweeper timer at the soonest transient state
// due to lapse. One timer, not one per row: a dozen pulls finishing together
// would otherwise trigger a dozen separate full refreshes.
func (a *App) rearmExpiry() {
	if a.expiryTimer != nil {
		a.expiryTimer.Stop()
		a.expiryTimer = nil
	}
	d, ok := nextExpiry(time.Now(), a.rowStatus, a.transientUntil)
	if !ok {
		return
	}
	a.expiryTimer = time.AfterFunc(d, func() { fyne.Do(a.sweepExpired) })
}

// sweepExpired drops lapsed transient state in one pass. It re-derives
// everything from the maps rather than from a captured entry, so a timer that
// fires just as its entry is replaced is harmless.
func (a *App) sweepExpired() {
	now := time.Now()
	changed := false
	for path, st := range a.rowStatus {
		if st != nil && !st.expires.IsZero() && !st.expires.After(now) {
			delete(a.rowStatus, path)
			changed = true
		}
	}
	if !a.transientUntil.IsZero() && !a.transientUntil.After(now) {
		a.transient = ""
		a.transientUntil = time.Time{}
		changed = true
	}
	a.rearmExpiry()
	if changed {
		a.applyFilter()
		a.updateFooter()
	}
}

// anyPulling reports whether a pull is currently running. Rows that merely
// carry a finished message do not count.
func (a *App) anyPulling() bool {
	for _, st := range a.rowStatus {
		if st.pulling() {
			return true
		}
	}
	return false
}

// stopTimers halts every timer that could otherwise fire after the app is gone
// and marshal work onto a stopped Fyne app.
func (a *App) stopTimers() {
	if a.expiryTimer != nil {
		a.expiryTimer.Stop()
		a.expiryTimer = nil
	}
	if a.trayTimer != nil {
		a.trayTimer.Stop()
		a.trayTimer = nil
	}
	if a.tips != nil {
		a.tips.stopDelay()
	}
}

// applyTheme resolves the configured appearance into a concrete light/dark
// variant, records it (pal + variant) and installs that same concrete variant as
// the Fyne theme so custom-painted surfaces and stock widgets stay in sync. Call
// it before building UI and whenever the theme setting changes.
func (a *App) applyTheme() {
	v, forced := a.resolveVariant()
	a.variant = v
	a.family = familyFromConfig(a.cfg.PaletteName())
	a.pal = paletteFor(a.family, v)
	a.fyneApp.Settings().SetTheme(glassTheme{family: a.family, variant: v, forced: forced})
	// Row heights are memoised from a probe rendered under the theme. They do not
	// vary by variant today, but a theme that ever changed a size would leave the
	// stale measurement driving the popover's height — so invalidate them here
	// rather than relying on that staying true.
	a.collapsedRow, a.groupRowH = 0, 0
}

// resolveVariant maps the configured theme mode to a concrete variant. "System"
// snapshots the current OS appearance into a concrete light/dark variant; the app
// re-runs this when the system flips so both the custom palette and stock widgets
// change together.
func (a *App) resolveVariant() (variant fyne.ThemeVariant, forced bool) {
	switch a.cfg.ThemeMode() {
	case config.ThemeLight:
		return theme.VariantLight, true
	case config.ThemeDark:
		return theme.VariantDark, true
	default:
		return a.fyneApp.Settings().ThemeVariant(), true
	}
}

// RunOptions controls the initial UI shown after startup.
type RunOptions struct {
	ShowSettings bool
}

// Run builds the UI, starts the monitor and blocks until quit.
func (a *App) Run() {
	a.RunWithOptions(RunOptions{})
}

// RunWithOptions builds the UI, starts the monitor and blocks until quit.
func (a *App) RunWithOptions(opts RunOptions) {
	if !a.claimRemote(opts) {
		return
	}
	a.buildPopover()
	a.rebuildTray()
	a.updateTrayIcon()
	if runtime.GOOS == "linux" {
		// Let Ubuntu/AppIndicator own tray clicks and show the native menu. Registering
		// a custom activation handler here fights the host and causes double-click or
		// misplaced-window behaviour on some desktops.
		systray.SetOnTapped(nil)
		systray.SetOnSecondaryTapped(nil)
	} else {
		// Left-click toggles the rich popover; right-click falls through to the menu.
		systray.SetOnTapped(a.togglePopover)
	}
	a.watchTrayOpen()
	// Dismiss the popover when the user clicks outside the app, like a real
	// menu-bar popover.
	watchPopoverAutoHide(a.onPopoverResign)
	a.fyneApp.Lifecycle().SetOnStarted(func() {
		// Drop the Dock icon once the app has started: it's a menu-bar utility, and
		// GLFW forces a Dock tile during init that only a runtime activation-policy
		// switch can undo (see setMenuBarAgent). No-op off macOS.
		setMenuBarAgent()
		if opts.ShowSettings {
			fyne.Do(func() {
				a.showSettings()
			})
		}
	})
	a.mgr.Start()
	a.fyneApp.Run()
}

// watchTrayOpen tracks when the host opens the native tray menu. Linux tray hosts
// can redraw or drop clicks if we replace the menu while it is visible, so menu
// rebuilds are deferred briefly after an open event. systray exposes no close
// event, hence the timeout-based release.
func (a *App) watchTrayOpen() {
	go func() {
		for range systray.TrayOpenedCh {
			fyne.Do(func() {
				a.trayOpen = true
				if a.trayTimer != nil {
					a.trayTimer.Stop()
				}
				a.trayTimer = time.AfterFunc(3*time.Second, func() {
					fyne.Do(a.releaseTray)
				})
			})
		}
	}()
}

func (a *App) releaseTray() {
	if a.trayTimer != nil {
		a.trayTimer.Stop()
		a.trayTimer = nil
	}
	a.trayOpen = false
	if a.trayDirty {
		a.trayDirty = false
		a.rebuildTray()
	}
	if a.trayIconDirty {
		a.trayIconDirty = false
		a.updateTrayIcon()
	}
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
	a.invalidateBranchesFor(a.all)
	a.pruneViewState()
	a.applyFilter()
	a.rebuildTray()
	a.updateTrayIcon()
}

// pruneViewState drops cached UI state whose repository or group no longer
// exists in the latest snapshot.
func (a *App) pruneViewState() {
	livePaths := make(map[string]bool, len(a.all))
	for _, r := range a.all {
		livePaths[r.Path] = true
	}
	for path := range a.details {
		if !livePaths[path] {
			delete(a.details, path)
		}
	}
	for path := range a.rowStatus {
		if !livePaths[path] {
			delete(a.rowStatus, path)
		}
	}
	a.pruneBranchState(livePaths)
	if a.bindex != nil {
		a.bindex.prune(livePaths)
	}
	if a.expandedPath != "" && !livePaths[a.expandedPath] {
		a.expandedPath = ""
	}

	liveGroups := make(map[string]bool)
	items, _ := a.groupItems(a.all)
	for _, item := range items {
		if item.header {
			liveGroups[item.root] = true
		}
	}
	for root := range a.collapsedGrp {
		if !liveGroups[root] {
			delete(a.collapsedGrp, root)
		}
	}
}

// activate runs the configured click action for a repo.
func (a *App) activate(r monitor.RepoState) {
	action, custom := a.cfg.Click()
	if err := actions.Run(action, custom, r.Path); err != nil {
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
	if runtime.GOOS == "linux" {
		a.trayBuilt = false
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
		return
	}
	go func() { _ = cmd.Wait() }() // reap; see actions.Run
}

func (a *App) quit() {
	a.stopTimers()
	a.mgr.Stop()
	if a.remoteCloser != nil {
		_ = a.remoteCloser.Close()
		a.remoteCloser = nil
	}
	a.fyneApp.Quit()
}

// logf prints a timestamped diagnostic line to stderr.
func (a *App) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05")+"  "+format+"\n", args...)
}
