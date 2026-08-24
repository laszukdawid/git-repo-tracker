package ui

import (
	"fmt"
	"image/color"
	"runtime"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

// assertRenders builds a widget's renderer and runs a layout+refresh pass at a
// realistic width, failing on any panic — a headless smoke test for the custom
// canvas widgets that can't be screenshotted from a non-interactive shell.
func assertRenders(t *testing.T, w fyne.Widget, width float32) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("render panicked: %v", r)
		}
	}()
	r := test.WidgetRenderer(w)
	ms := w.MinSize()
	if ms.Width < 0 || ms.Height < 0 {
		t.Fatalf("negative MinSize %v", ms)
	}
	if width < ms.Width {
		width = ms.Width
	}
	r.Layout(fyne.NewSize(width, ms.Height))
	r.Refresh()
}

// noopActions wires every row callback to the same no-op, for render tests that
// only care that a configured row lays out.
func noopActions(fn func(monitor.RepoState)) rowActions {
	return rowActions{onExpand: fn, onPull: fn, onFresh: fn, onOpen: fn}
}

func TestPopoverRowRenders(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	tips := newTooltipLayer(pal)
	noop := func(monitor.RepoState) {}
	row := newPopoverRow(tips, pal, func(string) {})

	// Behind repo, collapsed.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "dash", Branch: "main", Behind: 20}},
		rowState{}, noopActions(noop))
	assertRenders(t, row, 360)

	// Dirty repo, expanded with detail.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "api", Branch: "main", Dirty: true, Behind: 1}},
		rowState{expanded: true, detail: &monitor.Details{Path: "~/w/api", OriginRef: "origin/main", LocalHash: "a1b2c3", LocalMsg: "fix"}},
		noopActions(noop))
	assertRenders(t, row, 360)

	// Synced repo (dimmed).
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "lib", Branch: "main"}},
		rowState{}, noopActions(noop))
	assertRenders(t, row, 360)

	// Errored repo, expanded — the detail panel surfaces the message.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "broken", Branch: "main", FetchErr: "could not read from remote"}},
		rowState{expanded: true}, noopActions(noop))
	assertRenders(t, row, 360)

	// Group header with a behind pill.
	row.Configure(popoverItem{header: true, root: "~/projects", count: 32, behind: 10},
		rowState{}, rowActions{})
	assertRenders(t, row, 360)
}

func TestRepoRowReusesUnchangedDetailWidgets(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	row := newRepoRow(nil, paletteFor(familySlate, theme.VariantDark))
	repo := monitor.RepoState{Path: "/work/api", Name: "api", Branch: "main", Behind: 1}
	detail := &monitor.Details{Path: repo.Path, OriginRef: "origin/main", LocalHash: "a1", LocalMsg: "first"}

	row.Configure(repo, rowState{expanded: true, detail: detail}, rowActions{})
	first := row.detailBox.Objects[0]
	repo.LastLocal = time.Now()
	row.Configure(repo, rowState{expanded: true, detail: detail}, rowActions{})
	if row.detailBox.Objects[0] != first {
		t.Fatal("unchanged detail was rebuilt")
	}

	changed := *detail
	changed.LocalMsg = "second"
	row.Configure(repo, rowState{expanded: true, detail: &changed}, rowActions{})
	if row.detailBox.Objects[0] == first {
		t.Fatal("changed detail was not rebuilt")
	}
}

func TestRepoRowShowsKeepFreshOnlyWhenSynced(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	repo := monitor.RepoState{Path: "/work/api", Name: "api", Branch: "main"}
	row.Configure(repo, rowState{}, rowActions{})
	row.hovered = true
	row.updateActions()
	if !row.freshBtn.Visible() || row.pullBtn.Visible() || !row.openBtn.Visible() {
		t.Fatalf("synced actions: fresh=%v pull=%v open=%v", row.freshBtn.Visible(), row.pullBtn.Visible(), row.openBtn.Visible())
	}

	repo.Behind = 1
	row.Configure(repo, rowState{}, rowActions{})
	if row.freshBtn.Visible() || !row.pullBtn.Visible() || !row.openBtn.Visible() {
		t.Fatalf("behind actions: fresh=%v pull=%v open=%v", row.freshBtn.Visible(), row.pullBtn.Visible(), row.openBtn.Visible())
	}
}

func TestRepoRowActionsKeepLargeTargetsWithCompactChrome(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	row := newRepoRow(nil, paletteFor(familySlate, theme.VariantDark))

	for name, button := range map[string]*iconButton{
		"pull": row.pullBtn, "refresh": row.freshBtn, "folder": row.openBtn, "info": row.infoBtn,
	} {
		t.Run(name, func(t *testing.T) {
			renderer := test.WidgetRenderer(button).(*iconButtonRenderer)
			renderer.Layout(button.MinSize())
			if got := button.MinSize(); got != fyne.NewSize(28, 28) {
				t.Fatalf("hit target = %v, want 28x28", got)
			}
			if got := renderer.bg.Size(); got != fyne.NewSize(22, 22) {
				t.Errorf("visible surface = %v, want 22x22", got)
			}
			if got := renderer.bg.Position(); got != fyne.NewPos(3, 3) {
				t.Errorf("visible surface position = %v, want centered at (3,3)", got)
			}
			if got := renderer.img.Size(); got != fyne.NewSize(13, 13) {
				t.Errorf("icon = %v, want 13x13", got)
			}
		})
	}

	renderer := test.WidgetRenderer(row.ideBtn).(*ideButtonRenderer)
	renderer.Layout(row.ideBtn.MinSize())
	if got := row.ideBtn.MinSize(); got != fyne.NewSize(28, 28) {
		t.Fatalf("IDE hit target = %v, want 28x28", got)
	}
	if got := renderer.bg.Size(); got != fyne.NewSize(22, 22) {
		t.Errorf("IDE visible surface = %v, want 22x22", got)
	}
	if got := renderer.img.Size(); got != fyne.NewSize(13, 13) {
		t.Errorf("IDE icon = %v, want 13x13", got)
	}
}

func TestRepoInfoActionOpensConfigAndDisablesWhilePulling(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	row := newRepoRow(nil, paletteFor(familySlate, theme.VariantDark))
	repo := monitor.RepoState{Path: "/work/api", Name: "api"}
	opened := 0
	actions := rowActions{onOpenConfig: func(got monitor.RepoState) {
		if got.Path != repo.Path {
			t.Errorf("opened config for %q, want %q", got.Path, repo.Path)
		}
		opened++
	}}
	row.Configure(repo, rowState{}, actions)
	row.hovered = true
	row.updateActions()
	if !row.infoBtn.Visible() {
		t.Fatal("repository Info action is not visible with the other row actions")
	}
	row.infoBtn.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if opened != 1 {
		t.Fatalf("Info action count = %d, want 1", opened)
	}

	row.Configure(repo, rowState{status: &rowStatus{phase: rowPulling, msg: "Pulling…"}}, actions)
	if row.infoBtn.Visible() {
		t.Fatal("Info action remained visible while the repository was pulling")
	}
	row.infoBtn.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if opened != 1 {
		t.Fatal("disabled Info action still opened the config")
	}
}

func TestGitConsoleFormatsAndFiltersStructuredCommands(t *testing.T) {
	commands := []monitor.GitCommand{
		{
			StartedAt: time.Date(2026, 8, 24, 10, 30, 0, 0, time.UTC), Duration: 125 * time.Millisecond,
			RepoPath: "/work/api", Executable: "/usr/bin/git",
			Args: []string{"-C", "/work/api", "status", "--short"}, ExitCode: 0,
		},
		{
			StartedAt: time.Date(2026, 8, 24, 10, 31, 2, 0, time.UTC), Duration: 2 * time.Second,
			RepoPath: "/work/web", Executable: "/usr/bin/git",
			Args: []string{"-C", "/work/web", "fetch", "origin"}, ExitCode: 128,
			Stderr: "fatal: authentication failed",
		},
	}

	got := formatGitCommands(commands)
	for _, want := range []string{
		"10:30:00.000  ✓ exit 0  125ms",
		"repo: /work/api",
		"/usr/bin/git -C /work/api status --short",
		"10:31:02.000  ✕ exit 128  2s",
		"fatal: authentication failed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted console missing %q:\n%s", want, got)
		}
	}

	filtered := filterGitCommands(commands, "authentication")
	if len(filtered) != 1 || filtered[0].RepoPath != "/work/web" {
		t.Fatalf("filtered commands = %+v", filtered)
	}
	if empty := formatGitCommands(nil); empty != "No Git commands yet." {
		t.Fatalf("empty console = %q", empty)
	}
}

func TestOptionsPanelActionsFitPopoverWithGitConsole(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	a := &App{pal: paletteFor(familySlate, theme.VariantDark)}
	a.initState()
	panel := a.buildOptionsPanel()
	if got, maxWidth := panel.MinSize().Width, float32(popoverWidth)-2*theme.Padding(); got > maxWidth {
		t.Fatalf("options panel width = %v, exceeds available popover width %v", got, maxWidth)
	}
}

func TestCustomControlsSupportKeyboardActivation(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	tests := []struct {
		name    string
		control func(*int) fyne.CanvasObject
	}{
		{"icon button", func(count *int) fyne.CanvasObject {
			return newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open", pal.openBtnBg, pal.btnHover, func() { *count++ })
		}},
		{"header button", func(count *int) fyne.CanvasObject {
			return newHeaderButton(nil, pal, theme.SettingsIcon(), "Settings", func() { *count++ })
		}},
		{"repo row", func(count *int) fyne.CanvasObject {
			row := newRepoRow(nil, pal)
			row.Configure(monitor.RepoState{Path: "/work/api", Name: "api"}, rowState{}, rowActions{
				onExpand: func(monitor.RepoState) { *count++ },
			})
			return row
		}},
		{"group header", func(count *int) fyne.CanvasObject {
			header := newGroupHeaderRow(pal, func(string) { *count++ })
			header.Configure("~/work", 1, 0, false)
			return header
		}},
		{"segment chip", func(count *int) fyne.CanvasObject {
			return newSegChip("All", nil, pal, func() { *count++ })
		}},
		{"toggle", func(count *int) fyne.CanvasObject {
			return newToggleSwitch(false, pal, func(bool) { *count++ })
		}},
		{"text action", func(count *int) fyne.CanvasObject {
			return newTextAction("Add", nil, theme.ColorNamePrimary, pal.accent, func() { *count++ })
		}},
		{"IDE button", func(count *int) fyne.CanvasObject {
			return newIDEButton(pal, theme.ComputerIcon(), "Open", "Choose", false, func() { *count++ }, nil)
		}},
		{"branch section header", func(count *int) fyne.CanvasObject {
			return newSectionHeader(pal, "Branches", false, func() { *count++ })
		}},
		{"show more", func(count *int) fyne.CanvasObject {
			return newMoreRow(pal, "Show 40 more", func() { *count++ })
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			control := tc.control(&count)
			focusable, ok := control.(fyne.Focusable)
			if !ok {
				t.Fatalf("%T is not keyboard focusable", control)
			}
			focusable.FocusGained()
			focusable.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
			focusable.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
			focusable.FocusLost()
			if count != 2 {
				t.Fatalf("activation count = %d, want 2 for Space and Return", count)
			}
		})
	}
}

func TestCustomControlPressedAndFocusFeedback(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	tests := []struct {
		name    string
		control fyne.CanvasObject
		stroke  func() float32
		refresh func()
	}{
		func() struct {
			name    string
			control fyne.CanvasObject
			stroke  func() float32
			refresh func()
		} {
			control := newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open", pal.openBtnBg, pal.btnHover, nil)
			renderer := test.WidgetRenderer(control).(*iconButtonRenderer)
			return struct {
				name    string
				control fyne.CanvasObject
				stroke  func() float32
				refresh func()
			}{"icon button", control, func() float32 { return renderer.bg.StrokeWidth }, renderer.Refresh}
		}(),
		func() struct {
			name    string
			control fyne.CanvasObject
			stroke  func() float32
			refresh func()
		} {
			control := newSegChip("All", nil, pal, nil)
			renderer := test.WidgetRenderer(control).(*segChipRenderer)
			return struct {
				name    string
				control fyne.CanvasObject
				stroke  func() float32
				refresh func()
			}{"segment chip", control, func() float32 { return renderer.bg.StrokeWidth }, renderer.Refresh}
		}(),
		func() struct {
			name    string
			control fyne.CanvasObject
			stroke  func() float32
			refresh func()
		} {
			control := newToggleSwitch(false, pal, nil)
			renderer := test.WidgetRenderer(control).(*toggleRenderer)
			return struct {
				name    string
				control fyne.CanvasObject
				stroke  func() float32
				refresh func()
			}{"toggle", control, func() float32 { return renderer.track.StrokeWidth }, renderer.Refresh}
		}(),
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mouse, ok := tc.control.(interface {
				MouseDown(*desktop.MouseEvent)
				MouseUp(*desktop.MouseEvent)
			})
			if !ok {
				t.Fatalf("%T does not expose pressed interaction", tc.control)
			}
			mouse.MouseDown(&desktop.MouseEvent{})
			tc.refresh()
			if tc.stroke() < 2 {
				t.Fatalf("pressed stroke = %v, want at least 2", tc.stroke())
			}
			mouse.MouseUp(&desktop.MouseEvent{})
			focusable := tc.control.(fyne.Focusable)
			focusable.FocusGained()
			tc.refresh()
			if tc.stroke() <= 0 {
				t.Fatal("focused control has no visible focus stroke")
			}
			focusable.FocusLost()
		})
	}
}

func TestDisablingHeaderButtonClearsOuterFocusAndInteraction(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	window := app.NewWindow("header disable")
	pal := paletteFor(familySlate, theme.VariantDark)
	header := newHeaderButton(nil, pal, theme.SettingsIcon(), "Settings", nil)
	window.SetContent(header)
	window.Show()
	header.MouseIn(&desktop.MouseEvent{})
	header.MouseDown(&desktop.MouseEvent{})
	window.Canvas().Focus(header)
	header.setDisabled(true)
	if focused := window.Canvas().Focused(); focused != nil {
		t.Fatalf("disabled header retained outer focus on %T", focused)
	}
	if header.btn.state.active() || header.btn.state.pressed {
		t.Fatalf("disabled header retained interaction state: %+v", header.btn.state)
	}

	window.Canvas().Focus(header)
	if focused := window.Canvas().Focused(); focused != nil {
		t.Fatalf("disabled header accepted focus again on %T", focused)
	}
}

func TestRepoPullingHidesAndDisablesEveryAction(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	called := 0
	row.Configure(monitor.RepoState{Path: "/work/api", Name: "api"}, rowState{
		status:  &rowStatus{phase: rowPulling, msg: "Pulling…"},
		ideIcon: theme.ComputerIcon(),
	}, rowActions{
		onPull:       func(monitor.RepoState) { called++ },
		onFresh:      func(monitor.RepoState) { called++ },
		onOpen:       func(monitor.RepoState) { called++ },
		onOpenConfig: func(monitor.RepoState) { called++ },
		onOpenIDE:    func(monitor.RepoState) { called++ },
		onPickIDE:    func(monitor.RepoState) { called++ },
	})
	row.FocusGained()
	for name, control := range map[string]fyne.CanvasObject{
		"pull": row.pullBtn, "fresh": row.freshBtn, "IDE": row.ideBtn, "open": row.openBtn, "info": row.infoBtn,
	} {
		if control.Visible() {
			t.Errorf("%s action visible while pulling", name)
		}
		if focusable, ok := control.(fyne.Focusable); ok {
			focusable.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
		}
	}
	if called != 0 {
		t.Fatalf("pulling row actions invoked %d callbacks, want 0", called)
	}
}

func TestRepoPullingMakesExpandedBranchControlsInert(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	called := 0
	row.Configure(monitor.RepoState{Path: "/work/api", Name: "api"}, rowState{
		expanded: true,
		status:   &rowStatus{phase: rowPulling, msg: "Pulling…"},
		branches: branchSectionState{
			open:  true,
			gen:   1,
			limit: 1,
			list: &monitor.BranchList{Branches: []monitor.BranchInfo{
				{Name: "main", Upstream: "origin/main"},
				{Name: "topic", Upstream: "origin/topic"},
			}},
			errs: map[string]string{"main": "failed"},
		},
	}, rowActions{
		onToggleBranches: func(monitor.RepoState) { called++ },
		onPullBranch:     func(monitor.RepoState, monitor.BranchInfo) { called++ },
		onDismissBranch:  func(monitor.RepoState, string) { called++ },
		onFetchRepo:      func(monitor.RepoState) { called++ },
		onMoreBranches:   func(monitor.RepoState) { called++ },
		onCopyBranch:     func(string) { called++ },
	})
	for _, control := range row.detailControls {
		switch control := control.(type) {
		case *sectionHeader:
			control.Tapped(nil)
			if control.action != nil {
				control.action.Tapped(nil)
			}
		case *branchRow:
			control.act.Tapped(nil)
			control.copyBtn.Tapped(nil)
		case *dismissRow:
			control.close.Tapped(nil)
		case *moreRow:
			control.Tapped(nil)
		}
	}
	if called != 0 {
		t.Fatalf("expanded controls invoked %d callbacks while the repo was pulling", called)
	}
}

func TestRebindingPopoverRowClearsFocusAndCallbacks(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	window := app.NewWindow("row recycle")
	pal := paletteFor(familySlate, theme.VariantDark)
	called := 0
	row := newPopoverRow(newTooltipLayer(pal), pal, func(string) { called++ })
	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/old", Name: "old"}}, rowState{
		expanded: true,
		branches: branchSectionState{open: true, gen: 1, list: &monitor.BranchList{Branches: []monitor.BranchInfo{{Name: "main", Upstream: "origin/main"}}}},
	}, rowActions{
		onOpen:       func(monitor.RepoState) { called++ },
		onPullBranch: func(monitor.RepoState, monitor.BranchInfo) { called++ },
	})
	window.SetContent(row)
	window.Show()
	oldBranch := row.repo.branchRows[0]
	window.Canvas().Focus(oldBranch.act)
	row.Configure(popoverItem{header: true, root: "~/work", count: 1}, rowState{}, rowActions{})
	if window.Canvas().Focused() != nil {
		t.Fatal("recycled row retained focus on a hidden repo child")
	}
	row.repo.openBtn.Tapped(nil)
	oldBranch.act.Tapped(nil)
	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/new", Name: "new"}}, rowState{}, rowActions{})
	row.header.Tapped(nil)
	if called != 0 {
		t.Fatalf("recycled controls invoked %d stale callbacks", called)
	}
}

func TestDestroyingPopoverRowClearsChildCallbacks(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	called := 0
	row := newPopoverRow(nil, pal, func(string) { called++ })
	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/api", Name: "api"}}, rowState{
		expanded: true,
		branches: branchSectionState{open: true, gen: 1, list: &monitor.BranchList{
			Branches: []monitor.BranchInfo{{Name: "main", Upstream: "origin/main"}},
		}},
	}, rowActions{
		onOpen:       func(monitor.RepoState) { called++ },
		onPullBranch: func(monitor.RepoState, monitor.BranchInfo) { called++ },
	})
	oldBranch := row.repo.branchRows[0]
	test.WidgetRenderer(row).Destroy()
	row.repo.openBtn.Tapped(nil)
	oldBranch.act.Tapped(nil)
	row.header.Tapped(nil)
	if called != 0 {
		t.Fatalf("destroyed row children invoked %d stale callbacks", called)
	}
}

func TestPopoverRowSurfaceBridgesListSpacing(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newPopoverRow(nil, pal, nil)
	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/api", Name: "api"}}, rowState{}, rowActions{})
	renderer := test.WidgetRenderer(row).(*popoverRowRenderer)
	renderer.Layout(fyne.NewSize(360, row.MinSize().Height))
	if got, want := row.repo.Position().Y, -theme.Padding()/2; got != want {
		t.Fatalf("repo surface y = %v, want %v", got, want)
	}
}

func TestActionGeometryUsesSharedTokens(t *testing.T) {
	field := canvas.NewRectangle(color.Transparent)
	field.SetMinSize(fyne.NewSize(100, actionBox))
	toolbar := canvas.NewRectangle(color.Transparent)
	toolbar.SetMinSize(fyne.NewSize(3*actionBox+2*actionSiblingGap, actionBox))
	search := container.New(&searchToolbarLayout{}, field, toolbar)
	search.Resize(fyne.NewSize(240, actionBox))
	if got := toolbar.Position().X - field.Size().Width; got != actionClusterGap {
		t.Fatalf("search toolbar breathing = %v, want %v", got, actionClusterGap)
	}

	rects := []*canvas.Rectangle{
		canvas.NewRectangle(color.Transparent),
		canvas.NewRectangle(color.Transparent),
		canvas.NewRectangle(color.Transparent),
	}
	objects := make([]fyne.CanvasObject, len(rects))
	for i, object := range rects {
		object.SetMinSize(fyne.NewSize(actionBox, actionBox))
		objects[i] = object
	}
	cluster := container.New(&actionClusterLayout{}, objects...)
	cluster.Resize(cluster.MinSize())
	for i := 1; i < len(objects); i++ {
		if got := objects[i].Position().X - objects[i-1].Position().X - actionBox; got != actionSiblingGap {
			t.Fatalf("action gap %d = %v, want %v", i, got, actionSiblingGap)
		}
	}
	if got, want := fmt.Sprint(actionBox, actionIcon, actionLabelPad, actionLabelGap), "28 16 8 6"; got != want {
		t.Fatalf("action geometry = %s, want %s", got, want)
	}
	branchButton := newIconButton(theme.DownloadIcon(), theme.ColorNamePrimary, "", color.Transparent, color.Transparent, nil)
	branchButton.size = branchActionBox
	if got, want := branchButton.box()-2*branchButton.inset(), float32(actionIcon); got != want {
		t.Fatalf("branch action glyph = %v, want %v", got, want)
	}
}

func TestSearchFieldUsesLocalInnerPadding(t *testing.T) {
	app := test.NewApp()
	base := glassTheme{family: familySlate, variant: theme.VariantDark, forced: true}
	app.Settings().SetTheme(base)
	entry := newSearchEntry(nil, nil)
	field := newPopoverSearchField(entry)
	override, ok := field.(*container.ThemeOverride)
	if !ok {
		t.Fatalf("search field = %T, want *container.ThemeOverride", field)
	}
	if got := override.Theme.Size(theme.SizeNameInnerPadding); got != searchInnerPad {
		t.Fatalf("search inner padding = %v, want %v", got, searchInnerPad)
	}
	if got := base.Size(theme.SizeNameInnerPadding); got != spaceSm {
		t.Fatalf("global inner padding = %v, want token %v", got, spaceSm)
	}
}

func TestRepoOpenPresentationMatchesConfiguredAction(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	actions.SetEditorResolver(nil)
	t.Cleanup(func() { actions.SetEditorResolver(nil) })
	folderTip := "Open folder"
	terminalTip := "Open in Terminal"
	switch runtime.GOOS {
	case "darwin":
		folderTip = "Open in Finder"
	case "windows":
		folderTip = "Open in File Explorer"
		terminalTip = "Open in Command Prompt"
	}
	tests := []struct {
		action string
		tip    string
		icon   fyne.Resource
	}{
		{config.ActionOpenFolder, folderTip, theme.FolderOpenIcon()},
		{config.ActionTerminal, terminalTip, theme.ComputerIcon()},
		{config.ActionEditor, "Open with code (fallback)", theme.FileApplicationIcon()},
		{config.ActionCustom, "Run custom action", theme.FileApplicationIcon()},
		{"unknown", folderTip, theme.FolderOpenIcon()},
	}
	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			view := &App{cfg: &config.Config{ClickAction: tc.action}}
			icon, tip := view.repoOpenPresentation()
			if tip != tc.tip || icon.Name() != tc.icon.Name() {
				t.Fatalf("presentation = (%q, %q), want (%q, %q)", tip, icon.Name(), tc.tip, tc.icon.Name())
			}

			pal := paletteFor(familySlate, theme.VariantDark)
			button := newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open", pal.openBtnBg, pal.btnHover, nil)
			renderer := test.WidgetRenderer(button).(*iconButtonRenderer)
			button.setPresentation(icon, tip)
			renderer.Refresh()
			if button.tip != tc.tip || renderer.img.Resource.Name() != theme.NewColoredResource(tc.icon, colorNameMuted).Name() {
				t.Fatalf("button did not refresh its configured presentation")
			}
		})
	}
}

func TestRepoOpenPresentationFollowsMissingEditorFallback(t *testing.T) {
	actions.SetEditorResolver(nil)
	t.Cleanup(func() { actions.SetEditorResolver(nil) })
	view := &App{cfg: &config.Config{
		ClickAction: config.ActionEditor,
		IDE:         "cmd:git-repo-tracker-editor-that-does-not-exist",
	}}
	view.installEditorResolver()
	cmd, err := actions.Command(config.ActionEditor, "", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "code" || cmd.Args[1] != "/repo" {
		t.Fatalf("editor action = %v, want code fallback", cmd.Args)
	}
	icon, tip := view.repoOpenPresentation()
	if tip != "Open with code (fallback)" || icon.Name() != theme.FileApplicationIcon().Name() {
		t.Fatalf("presentation = (%q, %q), want truthful code fallback", tip, icon.Name())
	}
}

func TestRepoRendererInteractionPriority(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	row.Configure(monitor.RepoState{Path: "/work/api", Name: "api"}, rowState{expanded: true, selected: true}, rowActions{})
	renderer := test.WidgetRenderer(row).(*repoRowRenderer)
	renderer.Refresh()
	if renderer.bg.FillColor != theme.Color(theme.ColorNameSelection) {
		t.Fatal("keyboard selection should outrank expanded and hover state")
	}
	row.MouseDown(&desktop.MouseEvent{})
	renderer.Refresh()
	if renderer.bg.FillColor != pal.btnHover {
		t.Fatal("pressed state should outrank keyboard selection")
	}
}

func TestIconButtonRemainsNonHoverable(t *testing.T) {
	pal := paletteFor(familySlate, theme.VariantDark)
	button := newIconButton(theme.FolderOpenIcon(), colorNameMuted, "Open", pal.openBtnBg, pal.btnHover, nil)
	if _, hoverable := any(button).(desktop.Hoverable); hoverable {
		t.Fatal("iconButton must not become Hoverable; parent row hit-testing owns hover")
	}
}

func TestDifferentRepoAndRootRebindClearFocus(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	window := app.NewWindow("rebind")
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newPopoverRow(nil, pal, nil)
	window.SetContent(row)
	window.Show()

	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/one", Name: "one"}}, rowState{}, rowActions{})
	window.Canvas().Focus(row.repo.openBtn)
	row.Configure(popoverItem{repo: monitor.RepoState{Path: "/work/two", Name: "two"}}, rowState{}, rowActions{})
	if window.Canvas().Focused() != nil {
		t.Fatal("different-repo rebind retained child focus")
	}

	row.Configure(popoverItem{header: true, root: "~/one", count: 1}, rowState{}, rowActions{})
	window.Canvas().Focus(row.header)
	row.Configure(popoverItem{header: true, root: "~/two", count: 1}, rowState{}, rowActions{})
	if window.Canvas().Focused() != nil {
		t.Fatal("different-root rebind retained header focus")
	}
}

func TestBranchAndInlineActionsSupportKeyboardActivation(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	count := 0
	branch := newBranchRow(nil, pal,
		monitor.BranchInfo{Name: "main", Upstream: "origin/main"}, nil,
		func(monitor.BranchInfo) { count++ }, nil, nil, func(string) { count++ }, false)
	branch.act.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	branch.copyBtn.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})

	header := newSectionHeader(pal, "Branches", false, nil)
	fetch := newIconButton(theme.ViewRefreshIcon(), colorNameMuted, "Fetch", color.Transparent, pal.btnHover, func() { count++ })
	header.setAction(fetch)
	fetch.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})

	dismiss := newDismissRow(pal, "failed", func() { count++ })
	dismiss.close.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if count != 4 {
		t.Fatalf("branch/copy/fetch/dismiss activation count = %d, want 4", count)
	}
}

func TestDetachedListRowCannotActivateFromKeyboard(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	window := app.NewWindow("list pool")
	pal := paletteFor(familySlate, theme.VariantDark)
	view := &App{pal: pal, tips: newTooltipLayer(pal)}
	var first *popoverRow
	activated := 0
	list := widget.NewList(
		func() int { return 40 },
		view.listCreate,
		func(id widget.ListItemID, object fyne.CanvasObject) {
			row := object.(*popoverRow)
			row.Configure(popoverItem{repo: monitor.RepoState{Path: fmt.Sprintf("/work/%d", id), Name: fmt.Sprintf("repo-%d", id)}}, rowState{}, rowActions{
				onOpen: func(monitor.RepoState) { activated++ },
			})
			if id == 0 {
				first = row
			}
		},
	)
	view.list = list
	window.SetContent(container.NewBorder(widget.NewLabel("Repositories"), nil, nil, nil, list))
	window.Resize(fyne.NewSize(360, 110))
	window.Show()
	if first == nil {
		t.Fatal("list did not bind its first visible row")
	}
	first.repo.FocusGained()
	window.Canvas().Focus(first.repo.openBtn)
	list.ScrollToBottom()
	if focused := window.Canvas().Focused(); focused != nil {
		focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	}
	if activated != 0 {
		t.Fatal("detached or recycled row action activated from a keyboard event")
	}
	if window.Canvas().Focused() != nil {
		t.Fatal("detached or recycled row retained keyboard focus")
	}
}

// Rows are always two lines and always the same height. This test replaces an
// earlier one that asserted the opposite — that a row wrapped only when name and
// branch could not share a line — which is exactly what made the list ragged.
func TestRepoRowAlwaysTwoLinesEqualHeight(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	rr := test.WidgetRenderer(row).(*repoRowRenderer)
	width := float32(popoverWidth)

	// Deliberately pathological inputs: this is the set that used to produce three
	// different row heights.
	repos := []monitor.RepoState{
		{Path: "/w/api", Name: "api", Branch: "main"},
		{Path: "/w/hoot", Name: "hoot", Branch: "PLATFORM-1553-cache-update-check"},
		{Path: "/w/x", Name: "a-very-long-repository-name-that-cannot-fit-on-one-line", Branch: "feature/some-long-branch"},
		{Path: "/w/empty", Name: "empty"}, // no branch at all
		{Path: "/w/fresh", Name: "fresh", Branch: "main", KeepFresh: true},
		{Path: "/w/behind", Name: "behind", Branch: "main", Behind: 128},
		{Path: "/w/err", Name: "err", Branch: "main", FetchErr: "could not read from remote"},
	}

	var want float32
	for i, repo := range repos {
		row.Configure(repo, rowState{}, rowActions{})
		h := row.MinSize().Height
		if i == 0 {
			want = h
			continue
		}
		if h != want {
			t.Errorf("%s: row height %v, want %v — every collapsed row must be identical", repo.Name, h, want)
		}
	}

	// A transient pull message replaces the branch text but must not change the
	// height, or a batch update would shove every row below it.
	row.Configure(repos[0], rowState{status: &rowStatus{phase: rowPulling, msg: "Pulling…"}}, rowActions{})
	if h := row.MinSize().Height; h != want {
		t.Errorf("row with a pull message is %v tall, want %v", h, want)
	}

	// Line 2 always sits below line 1, and the name is never squeezed to make
	// room for the branch.
	row.Configure(repos[1], rowState{}, rowActions{})
	rr.Layout(fyne.NewSize(width, want))
	if row.name.Text != "hoot" {
		t.Errorf("name was truncated to %q despite having a whole line to itself", row.name.Text)
	}
	if row.branch.Position().Y <= row.name.Position().Y {
		t.Errorf("branch should sit below the name: nameY=%v branchY=%v",
			row.name.Position().Y, row.branch.Position().Y)
	}
	if row.branch.Position().X != row.name.Position().X {
		t.Errorf("name and branch should share the left rail: %v vs %v",
			row.name.Position().X, row.branch.Position().X)
	}

	// An overlong name is truncated with an ellipsis rather than overflowing.
	row.Configure(repos[2], rowState{}, rowActions{})
	rr.Layout(fyne.NewSize(width, want))
	if r := []rune(row.name.Text); len(r) == 0 || r[len(r)-1] != '\u2026' {
		t.Errorf("overlong name should be truncated with an ellipsis, got %q", row.name.Text)
	}
}

// Group headers and repo names must start at the same x, or every section
// boundary looks like a misalignment.
func TestGroupHeaderSharesLeftRailWithRows(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	row := newRepoRow(nil, pal)
	row.Configure(monitor.RepoState{Path: "/w/api", Name: "api", Branch: "main"}, rowState{}, rowActions{})
	rowR := test.WidgetRenderer(row).(*repoRowRenderer)
	rowR.Layout(fyne.NewSize(popoverWidth, row.MinSize().Height))

	head := newGroupHeaderRow(pal, nil)
	head.Configure("~/projects", 12, 3, false)
	headR := test.WidgetRenderer(head).(*groupHeaderRenderer)
	headR.Layout(fyne.NewSize(popoverWidth, head.MinSize().Height))

	if head.path.Position().X != row.name.Position().X {
		t.Errorf("header path starts at %v but repo names at %v — they must share one rail",
			head.path.Position().X, row.name.Position().X)
	}
}

func TestMarqueeOverflowAndText(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	col := paletteFor(familySlate, theme.VariantDark).faint
	m := newMarquee("a fairly long repository path that overflows", col, 12, false, true)
	m.scroll = true

	// Narrow box → overflow → the doubled loop (with separator) is rendered so the
	// scroll wraps seamlessly.
	m.width = 20
	if !m.overflow() {
		t.Fatal("narrow box should overflow")
	}
	m.render()
	if m.text.Text != m.doubled {
		t.Errorf("overflow render should show doubled loop text")
	}

	// Wide box → no overflow → the full text is shown at offset 0, no scrolling.
	m.width = m.textW + 50
	if m.overflow() {
		t.Fatal("wide box should not overflow")
	}
	m.render()
	if m.text.Text != m.full {
		t.Errorf("non-overflow render = %q, want the full text", m.text.Text)
	}
	if m.text.Position().X != 0 {
		t.Errorf("non-overflow text should sit at x=0, got %v", m.text.Position().X)
	}
}

func TestTooltipShowCachesSameTarget(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	w := app.NewWindow("tooltip")
	tips := newTooltipLayer(paletteFor(familySlate, theme.VariantDark))
	target := widget.NewButton("hover", nil)
	w.SetContent(tips.wrap(container.NewStack(target)))
	w.Resize(fyne.NewSize(200, 100))
	w.Show()

	tips.show("Open folder", target)
	if !tips.obj.Visible() {
		t.Fatal("tooltip should be visible after show")
	}
	tips.bg.Move(fyne.NewPos(123, 45))
	tips.show("Open folder", target)
	if got := tips.bg.Position(); got.X != 123 || got.Y != 45 {
		t.Errorf("same tooltip target should be cached, got position %v", got)
	}
	tips.hide()
	if tips.obj.Visible() || tips.shownText != "" || tips.shownFor != nil {
		t.Errorf("hide should clear tooltip state")
	}
}

func TestMarqueeDragClamp(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	col := paletteFor(familySlate, theme.VariantDark).faint
	m := newMarquee("a fairly long repository path that overflows", col, 12, false, true)
	m.width = 60
	maxPx := m.maxDragPx()
	if maxPx <= 0 {
		t.Fatal("expected the text to be draggable (overflowing)")
	}

	// Drag left hard → clamps to maxDragPx (end of text at the right edge), never
	// into the separator/repeat.
	for i := 0; i < 80; i++ {
		m.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-100, 0)})
	}
	if m.px != maxPx {
		t.Errorf("drag-left px = %v, want clamped to %v", m.px, maxPx)
	}
	// Drag right hard → clamps back to 0.
	for i := 0; i < 80; i++ {
		m.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(100, 0)})
	}
	if m.px != 0 {
		t.Errorf("drag-right px = %v, want clamped to 0", m.px)
	}
}

func TestTruncateToWidth(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	style := fyne.TextStyle{Bold: true}
	const size = 13.5

	if got := truncateToWidth("anything", 0, size, style); got != "" {
		t.Errorf("maxW 0 → %q, want empty", got)
	}

	full := "short"
	fullW := fyne.MeasureText(full, size, style).Width
	if got := truncateToWidth(full, fullW+10, size, style); got != full {
		t.Errorf("fitting text should be unchanged, got %q", got)
	}

	long := "a-very-long-repository-name-that-will-not-fit"
	maxW := fyne.MeasureText("a-very-long", size, style).Width
	got := truncateToWidth(long, maxW, size, style)
	if got == long {
		t.Fatal("long text should have been truncated")
	}
	if r := []rune(got); len(r) == 0 || r[len(r)-1] != '…' {
		t.Errorf("truncated text %q should end with an ellipsis", got)
	}
	if w := fyne.MeasureText(got, size, style).Width; w > maxW {
		t.Errorf("truncated width %v exceeds max %v", w, maxW)
	}
}

func TestCustomWidgetsRender(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	assertRenders(t, newToggleSwitch(true, pal, nil), 40)
	assertRenders(t, newToggleSwitch(false, pal, nil), 40)
	assertRenders(t, newGlyph(glyphBehind, pal.statusBehind, 15, 2), 18)
	assertRenders(t, newGlyph(glyphSynced, pal.statusSynced, 14, 2.4), 18)
	assertRenders(t, newGlyph(glyphDirty, pal.statusDirty, 9, 0), 18)
	assertRenders(t, newGlyph(glyphError, pal.statusError, 15, 2.2), 18)
	assertRenders(t, newGlyph(glyphChevron, pal.faint, 13, 1.7), 18)
	assertRenders(t, newGlyph(glyphChevronRight, pal.faint, 13, 1.7), 18)
	assertRenders(t, newTextAction("Add directory", theme.ContentAddIcon(), theme.ColorNamePrimary, pal.rowName, nil), 140)
	assertRenders(t, newGroupHeaderRow(pal, nil), 360)
}
