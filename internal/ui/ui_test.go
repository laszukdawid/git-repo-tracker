package ui

import (
	"bytes"
	"image/png"
	"runtime"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/trayicon"
)

func TestRepoGlyph(t *testing.T) {
	pal := paletteFor(theme.VariantDark)
	cases := []struct {
		name     string
		repo     monitor.RepoState
		wantKind glyphKind
		wantDim  bool
	}{
		{"error beats all", monitor.RepoState{FetchErr: "boom", Dirty: true, Behind: 3}, glyphError, false},
		{"status error", monitor.RepoState{Err: "bad"}, glyphError, false},
		{"dirty beats behind", monitor.RepoState{Dirty: true, Behind: 3}, glyphDirty, false},
		{"behind", monitor.RepoState{Behind: 2}, glyphBehind, false},
		{"synced dims", monitor.RepoState{}, glyphSynced, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, dim := repoGlyph(tc.repo, pal)
			if kind != tc.wantKind {
				t.Errorf("kind = %d, want %d", kind, tc.wantKind)
			}
			if dim != tc.wantDim {
				t.Errorf("dim = %v, want %v", dim, tc.wantDim)
			}
		})
	}
}

func TestRepoErrorMsg(t *testing.T) {
	if got := repoErrorMsg(monitor.RepoState{}); got != "" {
		t.Errorf("no error should give empty, got %q", got)
	}
	if got := repoErrorMsg(monitor.RepoState{Err: "bad"}); got != "Status error: bad" {
		t.Errorf("status error = %q", got)
	}
	// Fetch error is reported ahead of a local status error.
	if got := repoErrorMsg(monitor.RepoState{Err: "bad", FetchErr: "no remote"}); got != "Fetch error: no remote" {
		t.Errorf("fetch error precedence = %q", got)
	}
}

func TestSortRepos(t *testing.T) {
	repos := []monitor.RepoState{
		{Name: "beta", Behind: 1},
		{Name: "alpha", Behind: 5},
		{Name: "gamma", Behind: 5},
	}

	byBehind := append([]monitor.RepoState(nil), repos...)
	sortRepos(byBehind, sortBehind)
	if got := []string{byBehind[0].Name, byBehind[1].Name, byBehind[2].Name}; got[0] != "alpha" || got[1] != "gamma" || got[2] != "beta" {
		t.Errorf("sortBehind order = %v, want [alpha gamma beta]", got)
	}

	byName := append([]monitor.RepoState(nil), repos...)
	sortRepos(byName, sortName)
	if byName[0].Name != "alpha" || byName[1].Name != "beta" || byName[2].Name != "gamma" {
		t.Errorf("sortName order = %v", []string{byName[0].Name, byName[1].Name, byName[2].Name})
	}

	// An errored repo floats to the top regardless of its behind count.
	withErr := []monitor.RepoState{
		{Name: "alpha", Behind: 99},
		{Name: "broken", Behind: 0, FetchErr: "no remote"},
		{Name: "beta", Behind: 50},
	}
	sortRepos(withErr, sortBehind)
	if withErr[0].Name != "broken" {
		t.Errorf("errored repo should sort first, got %q", withErr[0].Name)
	}
}

func TestCollapseHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cases := map[string]string{
		"/home/tester":          "~",
		"/home/tester/projects": "~/projects",
		"/opt/work":             "/opt/work",
	}
	for in, want := range cases {
		if got := collapseHome(in); got != want {
			t.Errorf("collapseHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanizeTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Time{}, "unknown"},
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-2 * 24 * time.Hour), "2d ago"},
	}
	for _, tc := range cases {
		if got := humanizeTime(tc.in); got != tc.want {
			t.Errorf("humanizeTime(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFmtBadge(t *testing.T) {
	for in, want := range map[int]string{0: "0", 7: "7", 99: "99", 100: "99+", 250: "99+"} {
		if got := trayicon.FmtBadge(in); got != want {
			t.Errorf("fmtBadge(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestUseSplashPopoverOnlyOnDarwin(t *testing.T) {
	if got, want := useSplashPopover(), runtime.GOOS == "darwin"; got != want {
		t.Errorf("useSplashPopover() = %v, want %v", got, want)
	}
}

func TestTrayIconForTemplateVsColored(t *testing.T) {
	// Synced/fetching must be a themed (template) resource so macOS tints it to the
	// menu bar; attention states must be a plain (coloured) resource.
	if _, ok := trayicon.Resource(trayicon.Synced, 0).(*theme.ThemedResource); !ok {
		t.Errorf("synced tray icon is not a ThemedResource (would not template on macOS)")
	}
	if _, ok := trayicon.Resource(trayicon.Fetching, 0).(*theme.ThemedResource); !ok {
		t.Errorf("fetching tray icon is not a ThemedResource")
	}
	for _, st := range []trayicon.State{trayicon.Behind, trayicon.Dirty, trayicon.Error} {
		res := trayicon.Resource(st, 14)
		if _, ok := res.(*theme.ThemedResource); ok {
			t.Errorf("state %d must be a coloured (non-template) resource, got ThemedResource", st)
		}
	}
}

func TestCompositedIsValidPNG(t *testing.T) {
	res := trayicon.Resource(trayicon.Behind, 14)
	img, err := png.Decode(bytes.NewReader(res.Content()))
	if err != nil {
		t.Fatalf("tray icon is not valid PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != trayicon.Px || b.Dy() != trayicon.Px {
		t.Errorf("tray icon size = %dx%d, want %dx%d", b.Dx(), b.Dy(), trayicon.Px, trayicon.Px)
	}
	// The amber badge sits in the top-right; that pixel should be opaque.
	if _, _, _, alpha := img.At(trayicon.Px-6, 6).RGBA(); alpha == 0 {
		t.Errorf("expected an opaque badge pixel in the top-right corner")
	}
}

// ensure the resource interface is what we think (compile-time guard for the test).
var _ fyne.Resource = (*theme.ThemedResource)(nil)
