package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

func TestBranchLineText(t *testing.T) {
	base := time.Now().Add(-2 * time.Hour)
	cases := []struct {
		name string
		info monitor.BranchInfo
		want string
	}{
		{"in sync", monitor.BranchInfo{Name: "main", Time: base}, "main · 2h"},
		{"behind", monitor.BranchInfo{Name: "main", Behind: 3, Time: base}, "main  ↓3 · 2h"},
		{"ahead", monitor.BranchInfo{Name: "main", Ahead: 2, Time: base}, "main  ↑2 · 2h"},
		{"diverged", monitor.BranchInfo{Name: "wip", Ahead: 1, Behind: 2, Time: base}, "wip  ↑1 ↓2 · 2h"},
		{"gone", monitor.BranchInfo{Name: "old", Gone: true, Time: base}, "old  ⨯ gone · 2h"},
		// Ahead/behind mean nothing for a branch that has no local counterpart.
		{"remote only", monitor.BranchInfo{Name: "spike", RemoteOnly: true, Behind: 9, Time: base}, "spike · 2h"},
		{"no time", monitor.BranchInfo{Name: "main"}, "main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := branchLineText(tc.info); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBranchRowUsesReadableThemeAwareGeometry(t *testing.T) {
	app := test.NewApp()
	families := []paletteFamily{familySlate, familyInk, familySignal}
	variants := []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark}

	for _, family := range families {
		for _, variant := range variants {
			pal := paletteFor(family, variant)
			app.Settings().SetTheme(glassTheme{family: family, variant: variant, forced: true})
			row := newBranchRow(nil, pal, monitor.BranchInfo{Name: "feature/readable", Upstream: "origin/feature/readable"}, nil, nil, nil, nil, nil, false)

			if got := row.MinSize().Height; got != 36 {
				t.Errorf("family %d variant %d: row height = %v, want 36", family, variant, got)
			}
			if got := row.label.TextSize; got != 12 {
				t.Errorf("family %d variant %d: text size = %v, want 12", family, variant, got)
			}
			if !sameColour(row.label.Color, pal.rowSub) {
				t.Errorf("family %d variant %d: branch label does not use the palette rowSub colour", family, variant)
			}

			renderer := test.WidgetRenderer(row.act).(*iconButtonRenderer)
			renderer.Layout(row.act.MinSize())
			if got := row.act.MinSize(); got != fyne.NewSize(28, 28) {
				t.Errorf("family %d variant %d: hit target = %v, want 28x28", family, variant, got)
			}
			if got := renderer.bg.Size(); got != fyne.NewSize(22, 22) {
				t.Errorf("family %d variant %d: visible surface = %v, want 22x22", family, variant, got)
			}
			if got := renderer.img.Size(); got != fyne.NewSize(13, 13) {
				t.Errorf("family %d variant %d: icon = %v, want 13x13", family, variant, got)
			}
		}
	}
}

func TestShortAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"seconds", now.Add(-10 * time.Second), "now"},
		{"minutes", now.Add(-30 * time.Minute), "30m"},
		{"hours", now.Add(-5 * time.Hour), "5h"},
		{"days", now.Add(-3 * 24 * time.Hour), "3d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortAge(tc.when); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	// Beyond a month the exact date is more useful than a large day count.
	old := now.AddDate(0, -3, 0)
	if got := shortAge(old); got != old.Local().Format("2006-01-02") {
		t.Errorf("three months ago = %q, want an absolute date", got)
	}
}

func TestBranchTooltipExplainsWhyABranchCannotMove(t *testing.T) {
	cases := []struct {
		name string
		info monitor.BranchInfo
		want string
	}{
		{"remote only", monitor.BranchInfo{Name: "spike", RemoteOnly: true}, "create a local branch"},
		{"gone", monitor.BranchInfo{Name: "old", Gone: true}, "no longer exists"},
		{"checked out", monitor.BranchInfo{Name: "wt", Worktree: "/tmp/wt"}, "Checked out at"},
		{"no upstream", monitor.BranchInfo{Name: "orphan"}, "No upstream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := branchTooltip(tc.info); !strings.Contains(got, tc.want) {
				t.Errorf("tooltip %q should explain %q", got, tc.want)
			}
		})
	}
}

// The header must not claim a count before the branches have been read — saying
// a number would mean running git for every repository in the list, which is
// precisely what the lazily-loaded section exists to avoid.
func TestBranchSectionTextCountsOnlyWhenLoaded(t *testing.T) {
	if got := branchSectionText(branchSectionState{}); got != "Branches" {
		t.Errorf("unloaded = %q, want a bare label", got)
	}
	loaded := branchSectionState{list: &monitor.BranchList{
		Branches: []monitor.BranchInfo{{Name: "a"}, {Name: "b"}},
	}}
	if got := branchSectionText(loaded); got != "Branches (2)" {
		t.Errorf("loaded = %q", got)
	}
	failed := branchSectionState{list: &monitor.BranchList{Err: "boom"}}
	if got := branchSectionText(failed); got != "Branches" {
		t.Errorf("failed load = %q, want no count", got)
	}
}

func TestWorktreeSectionTextAndRowStatus(t *testing.T) {
	if got := worktreeSectionText(worktreeSectionState{}); got != "Worktrees" {
		t.Errorf("unloaded = %q", got)
	}
	list := &monitor.WorktreeList{Worktrees: []monitor.WorktreeInfo{
		{Path: "/work/clean", Branch: "main"},
		{Path: "/work/feature", Branch: "feature/x", Modified: 1, Dirty: true, Ahead: 2},
		{Path: "/work/detached", Detached: true, Head: "abc1234", Untracked: 1, Dirty: true},
	}}
	if got, want := worktreeSectionText(worktreeSectionState{list: list}), "Worktrees (3 · 2 dirty)"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	if got, want := worktreeLineText(list.Worktrees[1]), "feature/x  · 1 changes  ↑2"; got != want {
		t.Errorf("branch row = %q, want %q", got, want)
	}
	if got, want := worktreeLineText(list.Worktrees[2]), "detached @ abc1234  · 1 changes"; got != want {
		t.Errorf("detached row = %q, want %q", got, want)
	}
}

func TestWorktreesRenderBeforeBranchesWithLocalOnlyActions(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)
	repo := monitor.RepoState{Path: "/work/api", Name: "api", Branch: "main"}
	worktree := monitor.WorktreeInfo{Path: "/work/api-wt", Branch: "feature/x"}
	ide, folder := 0, 0
	row.Configure(repo, rowState{
		expanded: true,
		detail:   &monitor.Details{Path: repo.Path},
		worktrees: worktreeSectionState{open: true, gen: 1,
			list: &monitor.WorktreeList{Worktrees: []monitor.WorktreeInfo{worktree}}},
		branches: branchSectionState{list: &monitor.BranchList{}},
	}, rowActions{
		onOpenWorktreeIDE:    func(monitor.WorktreeInfo) { ide++ },
		onOpenWorktreeFolder: func(monitor.WorktreeInfo) { folder++ },
	})

	var headers []string
	var child *worktreeRow
	for _, control := range row.detailControls {
		switch control := control.(type) {
		case *sectionHeader:
			headers = append(headers, control.text)
		case *worktreeRow:
			child = control
		}
	}
	if len(headers) != 2 || !strings.HasPrefix(headers[0], "Worktrees") || !strings.HasPrefix(headers[1], "Branches") {
		t.Fatalf("section order = %v", headers)
	}
	if child == nil {
		t.Fatal("linked worktree row was not rendered")
	}
	child.ide.Tapped(nil)
	child.folder.Tapped(nil)
	if ide != 1 || folder != 1 {
		t.Fatalf("local actions = IDE %d, folder %d", ide, folder)
	}
}

// The popover measures the expanded row with a throwaway copy. If the two ever
// disagree the window ends up the wrong size, so this asserts they are handed
// the same inputs and produce the same height.
func TestExpandedRowHeightMatchesRenderedRow(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	repo := monitor.RepoState{Path: "/w/api", Name: "api", Branch: "main"}
	detail := &monitor.Details{Path: repo.Path, OriginRef: "origin/main", LocalHash: "a1", LocalMsg: "x"}
	sec := branchSectionState{
		open: true, gen: 7,
		list: &monitor.BranchList{Branches: []monitor.BranchInfo{
			{Name: "main", Current: true, Behind: 2},
			{Name: "feature/x", Behind: 1},
			{Name: "spike", RemoteOnly: true},
		}},
		errs: map[string]string{"feature/x": "diverged — cannot fast-forward"},
	}
	st := rowState{expanded: true, detail: detail, branches: sec}

	real := newPopoverRow(nil, pal, nil)
	real.Configure(popoverItem{repo: repo}, st, rowActions{})

	probe := newPopoverRow(nil, pal, nil)
	probe.Configure(popoverItem{repo: repo}, st, rowActions{})

	if real.MinSize().Height != probe.MinSize().Height {
		t.Errorf("rendered row is %v tall but the probe measured %v",
			real.MinSize().Height, probe.MinSize().Height)
	}
	// An open section with three branches must be taller than a closed one.
	closed := newPopoverRow(nil, pal, nil)
	closed.Configure(popoverItem{repo: repo},
		rowState{expanded: true, detail: detail, branches: branchSectionState{gen: 7}}, rowActions{})
	if closed.MinSize().Height >= real.MinSize().Height {
		t.Errorf("closed section (%v) should be shorter than an open one (%v)",
			closed.MinSize().Height, real.MinSize().Height)
	}
}

// The section is compared by a generation counter because its real contents are
// a map and a pointer, which Go cannot compare. If that broke, the row would
// either rebuild on every background tick — restarting every marquee — or never
// rebuild at all.
func TestBranchSectionRebuildsOnlyWhenItChanges(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	row := newRepoRow(nil, pal)

	repo := monitor.RepoState{Path: "/w/api", Name: "api", Branch: "main"}
	detail := &monitor.Details{Path: repo.Path, OriginRef: "origin/main", LocalHash: "a1", LocalMsg: "x"}
	list := &monitor.BranchList{Branches: []monitor.BranchInfo{{Name: "main", Current: true}}}
	st := rowState{expanded: true, detail: detail,
		branches: branchSectionState{open: true, gen: 1, list: list}}

	row.Configure(repo, st, rowActions{})
	first := row.detailBox.Objects[0]

	// An idle refresh: same generation, so nothing should be rebuilt.
	repo.LastLocal = time.Now()
	row.Configure(repo, st, rowActions{})
	if row.detailBox.Objects[0] != first {
		t.Error("an unchanged section was rebuilt on an idle refresh")
	}

	// A real change bumps the generation.
	st.branches.gen = 2
	row.Configure(repo, st, rowActions{})
	if row.detailBox.Objects[0] == first {
		t.Error("a changed section was not rebuilt")
	}
}

func TestBranchSectionForCollectsOnlyItsOwnRepo(t *testing.T) {
	a := &App{
		branches:      map[string]*monitor.BranchList{"/a": {Branches: []monitor.BranchInfo{{Name: "main"}}}},
		branchOpen:    map[string]bool{"/a": true},
		branchLoading: map[string]bool{},
		branchGen:     map[string]uint64{"/a": 3},
		branchBusy:    map[branchKey]bool{{"/a", "main"}: true, {"/b", "main"}: true},
		branchErr:     map[branchKey]string{{"/a", "x"}: "boom", {"/b", "x"}: "other"},
	}

	sec := a.branchSectionFor("/a")
	if !sec.open || sec.gen != 3 || sec.list == nil {
		t.Fatalf("section = %+v", sec)
	}
	if len(sec.busy) != 1 || !sec.busy["main"] {
		t.Errorf("busy = %v, want only this repo's branch", sec.busy)
	}
	if len(sec.errs) != 1 || sec.errs["x"] != "boom" {
		t.Errorf("errs = %v, want only this repo's message", sec.errs)
	}

	// A closed section renders one line and needs neither map.
	a.branchOpen["/a"] = false
	if sec := a.branchSectionFor("/a"); sec.busy != nil || sec.errs != nil {
		t.Error("a closed section should not collect per-branch state")
	}
}

// A listing overtaken by a background fetch or an automatic pull must be
// dropped, or the panel would keep showing counts that have already moved.
func TestInvalidateBranchesForStaleListings(t *testing.T) {
	fetched := time.Now().Add(-time.Hour)
	a := &App{
		branches: map[string]*monitor.BranchList{
			"/fresh": {FetchedAt: fetched, PulledAt: fetched},
			"/stale": {FetchedAt: fetched, PulledAt: fetched},
		},
		branchOpen:    map[string]bool{},
		branchLoading: map[string]bool{},
		branchGen:     map[string]uint64{},
	}
	a.invalidateBranchesFor([]monitor.RepoState{
		{Path: "/fresh", LastFetch: fetched, LastPull: fetched},
		{Path: "/stale", LastFetch: time.Now()},
	})

	if _, ok := a.branches["/fresh"]; !ok {
		t.Error("an untouched repo's listing was discarded")
	}
	if _, ok := a.branches["/stale"]; ok {
		t.Error("a listing overtaken by a fetch should have been dropped")
	}
}

func TestPruneBranchState(t *testing.T) {
	a := &App{
		branches:      map[string]*monitor.BranchList{"/live": {}, "/gone": {}},
		branchOpen:    map[string]bool{"/live": true, "/gone": true},
		branchLoading: map[string]bool{"/gone": true},
		branchGen:     map[string]uint64{"/live": 1, "/gone": 1},
		branchBusy:    map[branchKey]bool{{"/live", "main"}: true, {"/gone", "main"}: true},
		branchErr:     map[branchKey]string{{"/gone", "main"}: "boom"},
		worktrees:     map[string]*monitor.WorktreeList{"/live": {}, "/gone": {}},
		worktreeOpen:  map[string]bool{"/live": true, "/gone": true},
		worktreeGen:   map[string]uint64{"/live": 1, "/gone": 1},
	}
	a.pruneBranchState(map[string]bool{"/live": true})

	if _, ok := a.branches["/gone"]; ok {
		t.Error("listing for a vanished repo was kept")
	}
	if _, ok := a.branchErr[branchKey{"/gone", "main"}]; ok {
		t.Error("a dismissible error outlived the repository it belonged to")
	}
	if _, ok := a.branches["/live"]; !ok {
		t.Error("live state was pruned")
	}
	if _, ok := a.worktrees["/gone"]; ok {
		t.Error("worktrees for a vanished repo were kept")
	}
	if _, ok := a.worktrees["/live"]; !ok {
		t.Error("live worktree state was pruned")
	}
}

// A repository with hundreds of branches must render a bounded section, and the
// rest must be reachable. The "+N more" line used to be plain text at the foot
// of the scrolled area: clicking it did nothing, and reaching it meant scrolling
// past every branch above it.
func TestBranchSectionPagesLongLists(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	repo := monitor.RepoState{Path: "/w/api", Name: "api", Branch: "main"}
	detail := &monitor.Details{Path: repo.Path, OriginRef: "origin/main"}
	many := make([]monitor.BranchInfo, 200)
	for i := range many {
		many[i] = monitor.BranchInfo{Name: fmt.Sprintf("topic/%03d", i)}
	}
	build := func(limit int, branches []monitor.BranchInfo) *popoverRow {
		row := newPopoverRow(nil, pal, nil)
		row.Configure(popoverItem{repo: repo}, rowState{
			expanded: true, detail: detail,
			branches: branchSectionState{open: true, gen: 1, limit: limit,
				list: &monitor.BranchList{Branches: branches}},
		}, rowActions{onMoreBranches: func(monitor.RepoState) {}})
		return row
	}

	first := build(0, many)
	if !hasMoreRow(first) {
		t.Fatal("a truncated list must offer a way to see the rest")
	}
	// Paging further must not make the row taller — the list scrolls inside a
	// bounded box, so the window height is independent of the branch count.
	deeper := build(160, many)
	if first.MinSize().Height != deeper.MinSize().Height {
		t.Errorf("row grew from %v to %v when more branches were shown",
			first.MinSize().Height, deeper.MinSize().Height)
	}
	if !hasMoreRow(deeper) {
		t.Error("40 branches are still hidden and must still be offered")
	}
	if hasMoreRow(build(0, many[:5])) {
		t.Error("a short list must not offer to show more")
	}
}

func hasMoreRow(row *popoverRow) bool {
	for _, o := range row.repo.detailBox.Objects {
		if _, ok := o.(*moreRow); ok {
			return true
		}
	}
	return false
}

// A long branch name must not widen the scrolled section.
//
// container.Scroll lays its content out at max(content.MinSize(), viewport) in
// both axes, so a row that asks for its full label width drags every row in the
// section past the right edge — taking the action chips with it. That is what
// made the branch list look like it had no controls at all, while long names
// were cut off without an ellipsis.
func TestLongBranchNameDoesNotWidenTheSection(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	long := "feature/" + strings.Repeat("a-very-long-segment-", 8) + "end"
	short := newBranchRow(nil, pal, monitor.BranchInfo{Name: "main"}, nil, nil, nil, nil, nil, false)
	wide := newBranchRow(nil, pal, monitor.BranchInfo{Name: long}, nil, nil, nil, nil, nil, false)

	if wide.MinSize().Width != short.MinSize().Width {
		t.Errorf("a %d-character branch asks for %v of width, a short one %v — the section will "+
			"be laid out that wide and the chips will fall outside it",
			len(long), wide.MinSize().Width, short.MinSize().Width)
	}
	if w := wide.MinSize().Width; w > popoverWidth/2 {
		t.Errorf("a branch row asks for %v, which is too much of a %v popover", w, popoverWidth)
	}

	// The same trap in the inline error line under a branch.
	msg := newDismissRow(pal, strings.Repeat("cannot fast-forward: ", 12), nil)
	if w := msg.MinSize().Width; w > popoverWidth/2 {
		t.Errorf("an error line asks for %v of width", w)
	}
}

// Which branches offer a pull.
//
// Every local branch with an upstream does — including ones that report zero
// behind, because that number is only as fresh as the last fetch and pressing
// the chip fetches first. Hiding it when Behind was 0 meant that on a root with
// autoFetch off no branch was ever pullable, which is what the branch list
// looked like: names and nothing else.
func TestBranchActionOfferedWheneverThereIsAnUpstream(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)

	cases := []struct {
		name string
		info monitor.BranchInfo
		want bool
	}{
		{"level with its upstream", monitor.BranchInfo{Name: "main", Upstream: "origin/main"}, true},
		{"behind", monitor.BranchInfo{Name: "main", Upstream: "origin/main", Behind: 3}, true},
		{"ahead", monitor.BranchInfo{Name: "main", Upstream: "origin/main", Ahead: 2}, true},
		{"checked out elsewhere", monitor.BranchInfo{Name: "wt", Upstream: "origin/wt", Worktree: "/w/x"}, true},
		{"remote-only", monitor.BranchInfo{Name: "new", RemoteOnly: true}, true},
		{"no upstream", monitor.BranchInfo{Name: "local-only"}, false},
		{"upstream gone", monitor.BranchInfo{Name: "old", Upstream: "origin/old", Gone: true}, false},
	}
	for _, tc := range cases {
		row := newBranchRow(nil, pal, tc.info, nil, nil, nil, nil, nil, false)
		if got := row.actionable(); got != tc.want {
			t.Errorf("%s: actionable = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A pull already running keeps its control in place, greyed — a chip that
	// vanished on click would read as the click having gone nowhere.
	busy := newBranchRow(nil, pal, monitor.BranchInfo{Name: "main", Upstream: "origin/main"}, nil, nil, nil, nil, nil, true)
	if !busy.actionable() {
		t.Error("a branch being pulled must keep its chip")
	}
}

// Reintroducing state-specific download/upload artwork must make this fail: a
// local branch has one sync command, so every state needs the same two-way mark.
func TestBranchSyncUsesOneBidirectionalIconForPullAndPush(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	behind := newBranchRow(nil, pal,
		monitor.BranchInfo{Name: "behind", Upstream: "origin/behind", Behind: 2},
		nil, nil, nil, nil, nil, false)
	ahead := newBranchRow(nil, pal,
		monitor.BranchInfo{Name: "ahead", Upstream: "origin/ahead", Ahead: 2},
		nil, nil, nil, nil, nil, false)

	if behind.act.res.Name() != "branch-sync.svg" {
		t.Errorf("behind icon = %q, want branch-sync.svg", behind.act.res.Name())
	}
	if ahead.act.res.Name() != behind.act.res.Name() {
		t.Errorf("ahead icon = %q, want the same two-way icon as behind", ahead.act.res.Name())
	}
}

// Removing the editor chip or wiring it to copy/sync instead must make this
// fail: tapping the visible branch-level control reports the exact branch.
func TestBranchEditorActionTargetsItsBranch(t *testing.T) {
	test.NewApp().Settings().SetTheme(glassTheme{family: familySlate, variant: theme.VariantDark, forced: true})
	pal := paletteFor(familySlate, theme.VariantDark)
	var opened string
	row := newBranchRow(nil, pal, monitor.BranchInfo{Name: "feature/login"}, nil,
		nil, nil, func(b monitor.BranchInfo) { opened = b.Name }, nil, false)

	if !row.openBtn.Visible() {
		t.Fatal("branch editor action is hidden")
	}
	row.openBtn.Tapped(nil)
	if opened != "feature/login" {
		t.Errorf("opened branch = %q, want feature/login", opened)
	}
}

// The fetch has to say what it did. A fetch that found nothing and a dead button
// feel identical otherwise, which is exactly how this read.
func TestFetchNote(t *testing.T) {
	cases := []struct {
		moved int
		err   error
		want  string
	}{
		{0, nil, "Fetched v3 · already current"},
		{1, nil, "Fetched v3 · 1 branch updated"},
		{7, nil, "Fetched v3 · 7 branches updated"},
		{3, errors.New("auth"), "Fetch failed: v3"},
	}
	for _, tc := range cases {
		if got := fetchNote("v3", tc.moved, tc.err); got != tc.want {
			t.Errorf("fetchNote(%d, %v) = %q, want %q", tc.moved, tc.err, got, tc.want)
		}
	}
}

// The section header carries the number that decides whether the list is worth
// opening at all.
func TestBranchSectionTextReportsWhatIsPullable(t *testing.T) {
	list := &monitor.BranchList{Branches: []monitor.BranchInfo{
		{Name: "main", Current: true, Behind: 2},
		{Name: "topic", Behind: 1},
		{Name: "level"},
		{Name: "theirs", RemoteOnly: true, Behind: 9}, // no local counterpart: not pullable
	}}
	if got, want := branchSectionText(branchSectionState{list: list}), "Branches (4 · 2 behind)"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	clean := &monitor.BranchList{Branches: []monitor.BranchInfo{{Name: "main", Current: true}}}
	if got, want := branchSectionText(branchSectionState{list: clean}), "Branches (1)"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

// The status bar has to name the verb that actually ran.
func TestSyncNote(t *testing.T) {
	cases := []struct {
		did  monitor.BranchAction
		want string
	}{
		{monitor.BranchPull, "Pulled topic"},
		{monitor.BranchPush, "Pushed topic"},
		{monitor.BranchMerge, "Merged origin/topic into topic — push when you are ready"},
		{monitor.BranchNothing, "topic is already up to date"},
	}
	for _, tc := range cases {
		if got := syncNote(tc.did, "topic", "origin/topic"); got != tc.want {
			t.Errorf("syncNote(%v) = %q, want %q", tc.did, got, tc.want)
		}
	}
}

// The chip's wording is derived from the same plan that decides what it runs.
func TestSyncBranchTipMatchesThePlan(t *testing.T) {
	ahead := monitor.BranchInfo{Name: "otto", Upstream: "origin/otto", Ahead: 34}
	if got := syncBranchTip(ahead); !strings.HasPrefix(got, "Push otto") {
		t.Errorf("an ahead-only branch reads %q", got)
	}
	diverged := monitor.BranchInfo{Name: "claude", Upstream: "origin/claude",
		Ahead: 4, Behind: 142, Worktree: "/w/claude"}
	if got := syncBranchTip(diverged); !strings.HasPrefix(got, "Merge origin/claude into claude") {
		t.Errorf("a diverged branch reads %q", got)
	}
	stuck := monitor.BranchInfo{Name: "claude", Upstream: "origin/claude", Ahead: 4, Behind: 142}
	if got := syncBranchTip(stuck); !strings.Contains(got, "checked out nowhere") {
		t.Errorf("a diverged branch with no checkout reads %q", got)
	}
}
