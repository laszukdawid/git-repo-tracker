package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
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

func TestPopoverRowRenders(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	pal := paletteFor(theme.VariantDark)
	tips := newTooltipLayer(pal)
	noop := func(monitor.RepoState) {}
	row := newPopoverRow(tips, pal, func(string) {})

	// Behind repo, collapsed.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "dash", Branch: "main", Behind: 20}},
		false, false, nil, noop, noop, noop, noop)
	assertRenders(t, row, 360)

	// Dirty repo, expanded with detail.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "api", Branch: "main", Dirty: true, Behind: 1}},
		true, false, &monitor.Details{Path: "~/w/api", OriginRef: "origin/main", LocalHash: "a1b2c3", LocalMsg: "fix"},
		noop, noop, noop, noop)
	assertRenders(t, row, 360)

	// Synced repo (dimmed).
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "lib", Branch: "main"}},
		false, false, nil, noop, noop, noop, noop)
	assertRenders(t, row, 360)

	// Errored repo, expanded — the detail panel surfaces the message.
	row.Configure(popoverItem{repo: monitor.RepoState{Name: "broken", Branch: "main", FetchErr: "could not read from remote"}},
		true, false, nil, noop, noop, noop, noop)
	assertRenders(t, row, 360)

	// Group header with a behind pill.
	row.Configure(popoverItem{header: true, root: "~/projects", count: 32, behind: 10},
		false, false, nil, nil, nil, nil, nil)
	assertRenders(t, row, 360)
}

func TestRepoRowReusesUnchangedDetailWidgets(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	row := newRepoRow(nil, paletteFor(theme.VariantDark))
	repo := monitor.RepoState{Path: "/work/api", Name: "api", Branch: "main", Behind: 1}
	detail := &monitor.Details{Path: repo.Path, OriginRef: "origin/main", LocalHash: "a1", LocalMsg: "first"}

	row.Configure(repo, true, false, detail, nil, nil, nil, nil)
	first := row.detailBox.Objects[0]
	repo.LastLocal = time.Now()
	row.Configure(repo, true, false, detail, nil, nil, nil, nil)
	if row.detailBox.Objects[0] != first {
		t.Fatal("unchanged detail was rebuilt")
	}

	changed := *detail
	changed.LocalMsg = "second"
	row.Configure(repo, true, false, &changed, nil, nil, nil, nil)
	if row.detailBox.Objects[0] == first {
		t.Fatal("changed detail was not rebuilt")
	}
}

func TestRepoRowShowsKeepFreshOnlyWhenSynced(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	pal := paletteFor(theme.VariantDark)
	row := newRepoRow(nil, pal)
	repo := monitor.RepoState{Path: "/work/api", Name: "api", Branch: "main"}
	row.Configure(repo, false, false, nil, nil, nil, nil, nil)
	row.hovered = true
	row.updateActions()
	if !row.freshBtn.Visible() || row.pullBtn.Visible() || !row.openBtn.Visible() {
		t.Fatalf("synced actions: fresh=%v pull=%v open=%v", row.freshBtn.Visible(), row.pullBtn.Visible(), row.openBtn.Visible())
	}

	repo.Behind = 1
	row.Configure(repo, false, false, nil, nil, nil, nil, nil)
	if row.freshBtn.Visible() || !row.pullBtn.Visible() || !row.openBtn.Visible() {
		t.Fatalf("behind actions: fresh=%v pull=%v open=%v", row.freshBtn.Visible(), row.pullBtn.Visible(), row.openBtn.Visible())
	}
}

func TestMarqueeOverflowAndText(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	col := paletteFor(theme.VariantDark).faint
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
	app.Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	w := app.NewWindow("tooltip")
	tips := newTooltipLayer(paletteFor(theme.VariantDark))
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
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	col := paletteFor(theme.VariantDark).faint
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
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
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
	test.NewApp().Settings().SetTheme(glassTheme{variant: theme.VariantDark, forced: true})
	pal := paletteFor(theme.VariantDark)
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
