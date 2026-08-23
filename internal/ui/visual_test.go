package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/laszukdawid/git-repo-tracker/internal/backend"
	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/ide"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// This file renders the real popover headlessly and checks the promises the
// redesign makes. It deliberately asserts *invariants* rather than comparing
// against committed reference images: Fyne's software painter resolves runes the
// bundled font lacks (↓ · — … ⏎ ⇥ all appear in this UI) through the host's
// installed fonts, so pixel-identical output between the macOS and Ubuntu CI
// runners is not achievable, and golden files would be disabled within a month.
//
// Set GRT_UI_SNAPSHOT_DIR to also write PNGs for a human — or an agent — to look
// at. Nothing is written during an ordinary `go test`.
//
//	GRT_UI_SNAPSHOT_DIR=/tmp/shots go test ./internal/ui/ -run Visual

// scalable is the test canvas's SetScale, reached without importing internals.
type scalable interface{ SetScale(float32) }

// visualFixture is a set of repositories chosen to exercise every row state at
// once. Paths sit outside any home directory and times are fixed, so the render
// does not depend on who runs it or when.
func visualFixture() []monitor.RepoState {
	base := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	return []monitor.RepoState{
		{Path: "/fixtures/projects/api", Name: "api", Branch: "main", Upstream: "origin/main",
			Behind: 12, LinesAdded: 340, LinesDeleted: 12, LastFetch: base, LastLocal: base},
		{Path: "/fixtures/projects/dashboard", Name: "dashboard", Branch: "PLATFORM-1553-cache-update-check",
			Upstream: "origin/PLATFORM-1553-cache-update-check", Behind: 3, LastFetch: base, LastLocal: base},
		{Path: "/fixtures/projects/lib-core", Name: "lib-core", Branch: "main", Upstream: "origin/main",
			Dirty: true, Modified: 4, Untracked: 2, LastFetch: base, LastLocal: base},
		{Path: "/fixtures/projects/notes", Name: "notes", Branch: "main", Upstream: "origin/main",
			LastFetch: base, LastPull: base, LastLocal: base},
		{Path: "/fixtures/projects/a-very-long-repository-name-that-will-not-fit",
			Name: "a-very-long-repository-name-that-will-not-fit", Branch: "feature/long-branch-name",
			Upstream: "origin/feature/long-branch-name", Behind: 1, LastFetch: base, LastLocal: base},
		{Path: "/fixtures/work/billing", Name: "billing", Branch: "release/24.4",
			FetchErr: "could not read from remote repository", LastLocal: base},
		{Path: "/fixtures/work/frontend", Name: "frontend", Branch: "main", Upstream: "origin/main",
			Behind: 1, KeepFresh: true, LastFetch: base, LastAutoPull: base, LastLocal: base},
		{Path: "/fixtures/work/empty-branch", Name: "unborn", LastLocal: base},
	}
}

// newVisualApp builds a fully wired App on the headless test driver. Nothing
// touches git, the network, or the real machine's config and cache.
func newVisualApp(t *testing.T, variant fyne.ThemeVariant, scale float32) *App {
	return newVisualAppWith(t, familySlate, variant, scale)
}

func newVisualAppWith(t *testing.T, family paletteFamily, variant fyne.ThemeVariant, scale float32) *App {
	t.Helper()
	// No t.Parallel anywhere in this file: test.NewTempApp replaces the global
	// fyne.CurrentApp(), so parallel subtests would fight over the theme.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(dir, "state.json"))
	t.Setenv("GIT_REPO_TRACKER_CONFIG", filepath.Join(dir, "config.yaml"))

	fapp := test.NewTempApp(t)
	fapp.Settings().SetTheme(glassTheme{family: family, variant: variant, forced: true})

	cfg := &config.Config{Roots: []config.Root{
		{Path: "/fixtures/projects", Depth: 3, AutoFetch: true},
		{Path: "/fixtures/work", Depth: 3, AutoFetch: false},
	}}

	win := test.NewTempWindow(t, nil)
	if sc, ok := win.Canvas().(scalable); ok {
		sc.SetScale(scale)
	}

	a := &App{
		fyneApp: fapp, cfg: cfg, win: win,
		pal: paletteFor(family, variant), family: family, variant: variant,
		filter: filterAll, sort: sortBehind,
	}
	a.initState()
	a.mgr = backend.New(cfg, func() {}, func(string, ...any) {})
	a.all = visualFixture()
	a.buildPopoverContent()
	return a
}

// settle forces a real layout pass at the wanted size.
//
// The test canvas's Refresh(obj) is a no-op, and BaseWidget.Resize returns early
// when the size has not changed — so a same-size pass would leave rows holding
// the geometry they had before listUpdate filled in their text, and the branch
// would draw on top of the name. Nudging the WIDTH is what forces the relayout;
// nudging only the height does nothing.
func settle(w fyne.Window, size fyne.Size) {
	w.Resize(size.AddWidthHeight(1, 1))
	w.Resize(size)
	if c := w.Content(); c != nil {
		c.Refresh()
	}
}

// snapshot renders the popover and, when GRT_UI_SNAPSHOT_DIR is set, writes it
// to a PNG for review.
func snapshot(t *testing.T, a *App, name string) image.Image {
	t.Helper()
	settle(a.win, fyne.NewSize(popoverWidth, a.desiredPopoverHeight()))
	img := a.win.Canvas().Capture()

	dir := os.Getenv("GRT_UI_SNAPSHOT_DIR")
	if dir == "" {
		return img
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	t.Logf("wrote %s (%dx%d px)", path, b.Dx(), b.Dy())
	return img
}

func TestVisualPopoverRenders(t *testing.T) {
	families := []struct {
		name   string
		family paletteFamily
	}{
		{"slate", familySlate},
		{"ink", familyInk},
		{"signal", familySignal},
	}
	variants := []struct {
		name    string
		variant fyne.ThemeVariant
	}{
		{"dark", theme.VariantDark},
		{"light", theme.VariantLight},
	}
	type shot struct {
		name    string
		family  paletteFamily
		variant fyne.ThemeVariant
	}
	var cases []shot
	for _, f := range families {
		for _, v := range variants {
			cases = append(cases, shot{"popover-" + f.name + "-" + v.name, f.family, v.variant})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newVisualAppWith(t, tc.family, tc.variant, 2)
			img := snapshot(t, a, tc.name)

			b := img.Bounds()
			if b.Dx() == 0 || b.Dy() == 0 {
				t.Fatal("captured an empty image")
			}
			// A canvas that never laid out renders as one flat colour. Counting
			// distinct colours is a cheap guard against exactly that.
			if n := distinctColours(img); n < 20 {
				t.Errorf("only %d distinct colours — the popover probably never laid out", n)
			}
		})
	}
}

func TestVisualPopoverStates(t *testing.T) {
	a := newVisualApp(t, theme.VariantDark, 2)
	a.optionsPanel.Show()
	a.collapsedGrp["/fixtures/work"] = true
	a.expandedPath = a.all[0].Path
	a.details[a.all[0].Path] = &monitor.Details{
		Path: a.all[0].Path, OriginRef: "origin/main",
		LocalHash: "a1b2c3d", LocalMsg: "fix: keep the popover from hopping",
		OriginHash: "9f8e7d6", OriginMsg: "feat: batch the fetch pass",
	}
	// The Branches section, open with a mix of states and one refused pull.
	a.branchOpen[a.all[0].Path] = true
	a.branches[a.all[0].Path] = &monitor.BranchList{
		Path:     a.all[0].Path,
		Branches: manyBranches(),
	}
	a.branchErr[branchKey{a.all[0].Path, "release/24.4"}] = "diverged — cannot fast-forward"
	a.branchGen[a.all[0].Path] = 1

	a.rowStatus[a.all[2].Path] = &rowStatus{phase: rowPulling, msg: "Pulling…"}
	a.rowStatus[a.all[3].Path] = &rowStatus{phase: rowPulled, msg: "Pulled · just now"}
	a.transient = "Updated 2 repos"

	a.applyFilter()

	snapshot(t, a, "popover-dark-states-2x")
}

// The complaint that started the redesign: the window has a soft 20px corner
// while the search field was nearly square. This asserts the mechanism that
// fixes it end-to-end, so a Fyne upgrade that stopped honouring the theme's
// input radius would fail here rather than silently regress the look.
func TestVisualSearchFieldIsRounded(t *testing.T) {
	a := newVisualApp(t, theme.VariantDark, 1)
	settle(a.win, fyne.NewSize(popoverWidth, a.desiredPopoverHeight()))
	img := a.win.Canvas().Capture()

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(a.search)
	corner := img.At(int(pos.X)+1, int(pos.Y)+1)
	inside := img.At(int(pos.X)+radiusMd, int(pos.Y)+radiusMd)

	if sameColour(corner, inside) {
		t.Errorf("the search field's corner pixel matches its interior — the field is square, "+
			"not rounded (theme.SizeNameInputRadius = %v)", glassTheme{}.Size(theme.SizeNameInputRadius))
	}
}

// The popover shrinks to its content and stops growing at the cap. Both halves
// matter: a short list should not leave dead space, and a long one must not run
// off the screen.
func TestVisualPopoverHeightFitsContent(t *testing.T) {
	a := newVisualApp(t, theme.VariantDark, 1)

	a.all = visualFixture()[:2]
	a.applyFilter()
	short := a.desiredPopoverHeight()
	if short >= popoverMaxHeight {
		t.Errorf("two repos want the full %v — the popover is not shrinking to fit", popoverMaxHeight)
	}

	many := make([]monitor.RepoState, 0, 100)
	for i := 0; i < 100; i++ {
		many = append(many, monitor.RepoState{
			Path:   filepath.Join("/fixtures/projects", string(rune('a'+i%26))+"-repo"),
			Name:   string(rune('a'+i%26)) + "-repo",
			Branch: "main",
		})
	}
	a.all = many
	a.applyFilter()
	if got := a.desiredPopoverHeight(); got != popoverMaxHeight {
		t.Errorf("100 repos give a height of %v, want it capped at %v", got, popoverMaxHeight)
	}
}

// TestVisualHoveredRow renders one row with its action chips revealed, which is
// the only state where the editor control is visible.
func TestVisualHoveredRow(t *testing.T) {
	a := newVisualApp(t, theme.VariantDark, 2)
	pal := a.pal

	rows := container.New(&tightVBox{gap: 0})
	for _, repo := range []monitor.RepoState{
		{Path: "/fixtures/projects/api", Name: "api", Branch: "main", Behind: 12},
		{Path: "/fixtures/work/frontend", Name: "frontend", Branch: "main", KeepFresh: true},
	} {
		row := newRepoRow(a.tips, pal)
		st := a.rowStateFor(repo.Path, -1)
		st.ideIcon = a.ideCached.ideIcon
		st.ideTip, st.ideAltTip = a.ideCached.ideTip, a.ideCached.ideAltTip
		row.Configure(repo, st, a.rowActions())
		row.hovered = true
		row.updateActions()
		row.Refresh()
		rows.Add(row)
	}

	bg := canvas.NewLinearGradient(pal.gradTop, pal.gradBottom, 135)
	content := container.NewStack(bg, container.NewPadded(rows))
	win := test.NewTempWindow(t, content)
	if sc, ok := win.Canvas().(scalable); ok {
		sc.SetScale(2)
	}
	settle(win, fyne.NewSize(popoverWidth, content.MinSize().Height+2*theme.Padding()))

	img := win.Canvas().Capture()
	if dir := os.Getenv("GRT_UI_SNAPSHOT_DIR"); dir != "" {
		writeImage(t, filepath.Join(dir, "row-hovered-2x.png"), img)
	}
}

// TestVisualRealEditorIcon draws the chip with an actual application icon when
// this machine has one. The PNG-inside-icns path is verified by a unit test; the
// seam this covers is handing those bytes to Fyne as a drawable resource.
func TestVisualRealEditorIcon(t *testing.T) {
	editors := ide.Discover(nil)
	if len(editors) == 0 {
		t.Skip("no editors installed on this machine")
	}
	a := newVisualApp(t, theme.VariantDark, 2)

	rows := container.New(&tightVBox{gap: 0})
	for _, editor := range editors {
		if len(rows.Objects) >= 4 {
			break
		}
		row := newRepoRow(a.tips, a.pal)
		st := a.rowStateFor("/fixtures/projects/api", -1)
		st.ideIcon = ideIconResource(editor)
		st.ideTip, st.ideAltTip = "Open with "+editor.Name, "Open with a different editor"
		row.Configure(monitor.RepoState{
			Path: "/fixtures/projects/api", Name: editor.Name, Branch: "main", Behind: 1,
		}, st, a.rowActions())
		row.hovered = true
		row.updateActions()
		row.Refresh()
		rows.Add(row)
	}

	bg := canvas.NewLinearGradient(a.pal.gradTop, a.pal.gradBottom, 135)
	content := container.NewStack(bg, container.NewPadded(rows))
	win := test.NewTempWindow(t, content)
	if sc, ok := win.Canvas().(scalable); ok {
		sc.SetScale(2)
	}
	settle(win, fyne.NewSize(popoverWidth, content.MinSize().Height+2*theme.Padding()))

	img := win.Canvas().Capture()
	if n := distinctColours(img); n < 40 {
		t.Errorf("only %d distinct colours — the editor icons probably did not draw", n)
	}
	if dir := os.Getenv("GRT_UI_SNAPSHOT_DIR"); dir != "" {
		writeImage(t, filepath.Join(dir, "row-editor-icons-2x.png"), img)
	}
}

// manyBranches is a realistic listing: local branches first, then the ones that
// only exist on the remote.
func manyBranches() []monitor.BranchInfo {
	base := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	out := []monitor.BranchInfo{
		{Name: "main", Current: true, Upstream: "origin/main", Behind: 12,
			Time: base, Subject: "feat: batch the fetch pass"},
		{Name: "release/24.4", Upstream: "origin/release/24.4", Ahead: 1, Behind: 2,
			Time: base.Add(-27 * time.Hour)},
		{Name: "old-experiment", Gone: true, Time: base.AddDate(0, -2, 0)},
	}
	for i := 0; i < 6; i++ {
		out = append(out, monitor.BranchInfo{
			Name: "feature/ticket-" + string(rune('A'+i)), Upstream: "origin/feature",
			Behind: i, Time: base.Add(-time.Duration(i+2) * time.Hour),
		})
	}
	for i := 0; i < 4; i++ {
		out = append(out, monitor.BranchInfo{
			Name: "colleague/work-" + string(rune('a'+i)), RemoteOnly: true,
			Upstream: "origin/colleague", Time: base.Add(-time.Duration(i) * time.Hour),
		})
	}
	return out
}

func writeImage(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

func distinctColours(img image.Image) int {
	seen := map[uint64]struct{}{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, al := img.At(x, y).RGBA()
			key := uint64(r)<<48 | uint64(g)<<32 | uint64(bl)<<16 | uint64(al)
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

func sameColour(a, b interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// A branch row's controls must be PAINTED, not merely present.
//
// They used to appear on hover, and inside the expanded row they never appeared
// at all: a chip that is hidden when its row is first laid out has no size, and
// showing it later left it drawing nothing — the geometry was right, the pixels
// were empty, and the section looked like it offered no actions whatsoever.
// This samples the chip's own rectangle, so "visible" has to mean visible.
func TestVisualBranchChipsAreDrawn(t *testing.T) {
	a := newVisualApp(t, theme.VariantDark, 1)
	pal := a.pal
	row := newBranchRow(nil, pal, monitor.BranchInfo{
		Name: "release/24.4", Upstream: "origin/release/24.4", Behind: 3,
		Time: time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC),
	}, nil, nil, nil, nil, func(string) {}, false)

	bg := canvas.NewRectangle(pal.gradTop)
	content := container.NewStack(bg, container.NewPadded(row))
	win := test.NewTempWindow(t, content)
	settle(win, fyne.NewSize(300, 40))
	img := win.Canvas().Capture()

	for _, chip := range []struct {
		name string
		btn  *iconButton
	}{{"sync", row.act}, {"editor", row.openBtn}, {"copy", row.copyBtn}} {
		if !chip.btn.Visible() {
			t.Errorf("%s chip is not visible without hovering", chip.name)
			continue
		}
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(chip.btn)
		if !hasInk(img, int(pos.X), int(pos.Y), branchActionBox, pal.gradTop) {
			t.Errorf("%s chip occupies %v but painted nothing there", chip.name, pos)
		}
	}
}

// hasInk reports whether any pixel in the square at (x, y) differs from the
// background colour.
func hasInk(img image.Image, x, y, size int, bg color.Color) bool {
	br, bgg, bb, _ := bg.RGBA()
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			r, g, b, _ := img.At(x+dx, y+dy).RGBA()
			if absDiff(r, br)+absDiff(g, bgg)+absDiff(b, bb) > 3000 {
				return true
			}
		}
	}
	return false
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
