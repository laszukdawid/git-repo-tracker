package ui

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"runtime"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/trayicon"
)

// The status glyph answers one question — where does this repository stand
// against its remote — and answers it specifically.
//
// "Dirty" used to outrank "behind", so on a machine where most working trees
// carry local edits every row showed the same dot and the column said nothing at
// all. Uncommitted changes are now a separate mark beside the name, and the
// glyph is free to say pull, push, both, or stop.
func TestRepoGlyph(t *testing.T) {
	pal := paletteFor(familySlate, theme.VariantDark)
	cases := []struct {
		name     string
		repo     monitor.RepoState
		wantKind glyphKind
		wantDim  bool
	}{
		{"error beats all", monitor.RepoState{FetchErr: "boom", Dirty: true, Behind: 3}, glyphError, false},
		{"status error", monitor.RepoState{Err: "bad"}, glyphError, false},
		{"a half-finished rebase outranks everything but an error",
			monitor.RepoState{Operation: "rebase", Behind: 3, Ahead: 1, Dirty: true}, glyphConflict, false},
		{"unresolved conflicts say the same thing",
			monitor.RepoState{Conflicts: 2, Dirty: true}, glyphConflict, false},
		{"behind and ahead is neither — it has diverged",
			monitor.RepoState{Behind: 3, Ahead: 1}, glyphDiverged, false},
		{"behind, even with local edits", monitor.RepoState{Dirty: true, Behind: 3}, glyphBehind, false},
		{"ahead is its own state", monitor.RepoState{Ahead: 2}, glyphAhead, false},
		{"ahead means nothing on a detached HEAD",
			monitor.RepoState{Ahead: 2, Detached: true, Dirty: true}, glyphDirty, false},
		{"dirty, when the branch itself is settled", monitor.RepoState{Dirty: true}, glyphDirty, false},
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

// Every state the glyph can show must be a distinct shape — the point of the
// change is that "there is something" became "there is THIS".
func TestGlyphKindsAreDistinct(t *testing.T) {
	seen := map[string]glyphKind{}
	for _, k := range []glyphKind{glyphBehind, glyphAhead, glyphDiverged, glyphConflict, glyphSynced} {
		g := newGlyph(k, chipSelectedText, 15, 2)
		key := fmt.Sprint(g.segs())
		if len(g.segs()) == 0 {
			t.Errorf("glyph %d draws no strokes", k)
		}
		if other, dup := seen[key]; dup {
			t.Errorf("glyphs %d and %d draw the same shape", other, k)
		}
		seen[key] = k
	}
}

// The right-hand counter carries both directions.
func TestSyncCount(t *testing.T) {
	cases := []struct {
		repo monitor.RepoState
		want string
	}{
		{monitor.RepoState{}, ""},
		{monitor.RepoState{Behind: 12}, "\u219312"},
		{monitor.RepoState{Ahead: 3}, "\u21913"},
		{monitor.RepoState{Behind: 12, Ahead: 3}, "\u219312 \u21913"},
		{monitor.RepoState{Ahead: 3, Detached: true}, ""},
	}
	for _, tc := range cases {
		if got := syncCount(tc.repo); got != tc.want {
			t.Errorf("syncCount(%+v) = %q, want %q", tc.repo, got, tc.want)
		}
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

func TestNewScanRootRequiresDirectorySelection(t *testing.T) {
	root := newScanRoot()
	if root.Path != "" {
		t.Errorf("new root path = %q, want empty", root.Path)
	}
	if root.Depth != 5 || !root.AutoFetch {
		t.Errorf("new root = %+v, want depth 5 with auto-fetch enabled", root)
	}
}

func TestPruneViewState(t *testing.T) {
	a := &App{
		cfg:          &config.Config{},
		all:          []monitor.RepoState{{Path: "/work/live", Name: "live"}},
		details:      map[string]*monitor.Details{"/work/live": {}, "/work/gone": {}},
		rowStatus:    map[string]*rowStatus{"/work/live": {phase: rowPulling}, "/work/gone": {phase: rowPulling}},
		expandedPath: "/work/gone",
		collapsedGrp: map[string]bool{"/work": true, "/gone": true},
	}

	a.pruneViewState()
	if _, ok := a.details["/work/gone"]; ok {
		t.Fatal("stale detail cache entry was retained")
	}
	if _, ok := a.rowStatus["/work/gone"]; ok {
		t.Fatal("stale pull-status entry was retained")
	}
	if a.expandedPath != "" {
		t.Errorf("expandedPath = %q, want empty", a.expandedPath)
	}
	if !a.collapsedGrp["/work"] || a.collapsedGrp["/gone"] {
		t.Errorf("collapsed groups were not reconciled: %v", a.collapsedGrp)
	}
	if a.details["/work/live"] == nil || !a.rowStatus["/work/live"].pulling() {
		t.Fatal("live repository state was pruned")
	}
}

func TestTrayIconResources(t *testing.T) {
	testDarwinTrayIconResources(t)
	testColoredTrayIconResources(t)
}

func testDarwinTrayIconResources(t *testing.T) {
	t.Helper()
	resources := []struct {
		name string
		res  fyne.Resource
	}{
		{"synced", trayicon.ResourceForPlatform("darwin", trayicon.Synced, 0)},
		{"fetching", trayicon.ResourceForPlatform("darwin", trayicon.Fetching, 0)},
		{"behind-1", trayicon.ResourceForPlatform("darwin", trayicon.Behind, 1)},
		{"behind-99", trayicon.ResourceForPlatform("darwin", trayicon.Behind, 99)},
		{"behind-100", trayicon.ResourceForPlatform("darwin", trayicon.Behind, 100)},
		{"dirty", trayicon.ResourceForPlatform("darwin", trayicon.Dirty, 0)},
		{"error", trayicon.ResourceForPlatform("darwin", trayicon.Error, 0)},
	}
	for i, left := range resources[2:] {
		if bytes.Equal(left.res.Content(), resources[0].res.Content()) {
			t.Fatalf("%s SVG content does not differ from the base graph", left.name)
		}
		for _, right := range resources[i+3:] {
			if bytes.Equal(left.res.Content(), right.res.Content()) {
				t.Fatalf("SVG content is shared by %s and %s", left.name, right.name)
			}
		}
	}

	names := make(map[string]string, len(resources))
	images := make(map[string]image.Image, len(resources))
	for _, tc := range resources {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.res.(*theme.ThemedResource); !ok {
				t.Fatalf("%s tray icon is %T, want *theme.ThemedResource", tc.name, tc.res)
			}
			if previous, exists := names[tc.res.Name()]; exists {
				t.Fatalf("resource name %q is shared by %s and %s", tc.res.Name(), previous, tc.name)
			}
			names[tc.res.Name()] = tc.name

			img := renderTraySVG(t, tc.res)
			images[tc.name] = img
			minX, minY, maxX, maxY, found := alphaBounds(img, 16)
			if !found {
				t.Fatal("rendered SVG has no visible pixels")
			}
			if minX < 2 || minY < 2 || maxX > trayicon.Px-3 || maxY > trayicon.Px-3 {
				t.Errorf("visible bounds = (%d,%d)-(%d,%d), want at least 2px transparent edge margin", minX, minY, maxX, maxY)
			}
			assertMonochrome(t, trayicon.DarwinSVG(stateForTrayIconName(tc.name), countForTrayIconName(tc.name)))
		})
	}

	base := images["synced"]
	markerROI := image.Rect(trayicon.Px/2, 0, trayicon.Px, trayicon.Px/2)
	markers := []string{"behind-1", "behind-99", "behind-100", "dirty", "error"}
	for _, name := range markers {
		minX, minY, maxX, maxY, found := alphaDifferenceBounds(base, images[name], markerROI, 16)
		if !found {
			t.Errorf("%s marker is not visible in the top-right ROI", name)
			continue
		}
		if maxX-minX+1 < 3 || maxY-minY+1 < 3 {
			t.Errorf("%s marker bounds = (%d,%d)-(%d,%d), want a readable marker", name, minX, minY, maxX, maxY)
		}
	}
	for i, left := range markers {
		for _, right := range markers[i+1:] {
			if sameAlphaMask(images[left], images[right], 16) {
				t.Errorf("%s and %s render the same alpha mask", left, right)
			}
		}
	}
}

func testColoredTrayIconResources(t *testing.T) {
	t.Helper()
	for _, state := range []trayicon.State{trayicon.Synced, trayicon.Fetching} {
		if _, ok := trayicon.ResourceForPlatform("linux", state, 0).(*theme.ThemedResource); !ok {
			t.Errorf("state %d tray icon is not a ThemedResource", state)
		}
	}
	states := []struct {
		name      string
		state     trayicon.State
		count     int
		fill, ink color.NRGBA
	}{
		{"behind", trayicon.Behind, 14, color.NRGBA{R: 230, G: 169, B: 77, A: 255}, color.NRGBA{R: 10, G: 13, B: 20, A: 255}},
		{"dirty", trayicon.Dirty, 0, color.NRGBA{R: 232, G: 130, B: 95, A: 255}, color.NRGBA{}},
		{"error", trayicon.Error, 0, color.NRGBA{R: 229, G: 72, B: 77, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	}
	images := make(map[string]image.Image, len(states))
	for _, tc := range states {
		res := trayicon.ResourceForPlatform("linux", tc.state, tc.count)
		if _, ok := res.(*theme.ThemedResource); ok {
			t.Errorf("%s must be a coloured PNG resource, got ThemedResource", tc.name)
		}
		img, err := png.Decode(bytes.NewReader(res.Content()))
		if err != nil {
			t.Fatalf("%s tray icon is not valid PNG: %v", tc.name, err)
		}
		if bounds := img.Bounds(); bounds.Dx() != trayicon.Px || bounds.Dy() != trayicon.Px {
			t.Errorf("%s tray icon size = %dx%d, want %dx%d", tc.name, bounds.Dx(), bounds.Dy(), trayicon.Px, trayicon.Px)
		}
		if !containsColor(img, tc.fill) {
			t.Errorf("%s image does not contain overlay fill %#v", tc.name, tc.fill)
		}
		if tc.ink.A != 0 && !containsColor(img, tc.ink) {
			t.Errorf("%s image does not contain overlay ink %#v", tc.name, tc.ink)
		}
		images[tc.name] = img
	}
	colored := []string{"behind", "dirty", "error"}
	for i, left := range colored {
		for _, right := range colored[i+1:] {
			if sameImage(images[left], images[right]) {
				t.Errorf("%s and %s render the same image", left, right)
			}
		}
	}
	if !containsColorInRect(images["behind"], color.NRGBA{R: 230, G: 169, B: 77, A: 255}, image.Rect(trayicon.Px/2, 0, trayicon.Px, trayicon.Px/2)) {
		t.Error("expected a visible badge pixel in the top-right corner")
	}
}

func renderTraySVG(t *testing.T, res fyne.Resource) image.Image {
	t.Helper()
	icon, err := oksvg.ReadIconStream(bytes.NewReader(res.Content()))
	if err != nil {
		t.Fatalf("tray icon SVG does not render: %v", err)
	}
	icon.SetTarget(0, 0, trayicon.Px, trayicon.Px)
	img := image.NewRGBA(image.Rect(0, 0, trayicon.Px, trayicon.Px))
	scanner := rasterx.NewScannerGV(trayicon.Px, trayicon.Px, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(trayicon.Px, trayicon.Px, scanner), 1)
	return img
}

func alphaBounds(img image.Image, minAlpha uint8) (minX, minY, maxX, maxY int, found bool) {
	bounds := img.Bounds()
	minX, minY = bounds.Max.X, bounds.Max.Y
	maxX, maxY = bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if alphaAt(img, x, y) < minAlpha {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
			found = true
		}
	}
	return minX, minY, maxX, maxY, found
}

func alphaDifferenceBounds(base, candidate image.Image, roi image.Rectangle, minDifference uint8) (minX, minY, maxX, maxY int, found bool) {
	minX, minY = roi.Max.X, roi.Max.Y
	maxX, maxY = roi.Min.X, roi.Min.Y
	for y := roi.Min.Y; y < roi.Max.Y; y++ {
		for x := roi.Min.X; x < roi.Max.X; x++ {
			if absoluteDifference(alphaAt(base, x, y), alphaAt(candidate, x, y)) < minDifference {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
			found = true
		}
	}
	return minX, minY, maxX, maxY, found
}

func sameAlphaMask(left, right image.Image, minAlpha uint8) bool {
	if !left.Bounds().Eq(right.Bounds()) {
		return false
	}
	for y := left.Bounds().Min.Y; y < left.Bounds().Max.Y; y++ {
		for x := left.Bounds().Min.X; x < left.Bounds().Max.X; x++ {
			if (alphaAt(left, x, y) >= minAlpha) != (alphaAt(right, x, y) >= minAlpha) {
				return false
			}
		}
	}
	return true
}

func alphaAt(img image.Image, x, y int) uint8 {
	_, _, _, alpha := img.At(x, y).RGBA()
	return uint8(alpha >> 8)
}

func absoluteDifference(left, right uint8) uint8 {
	if left > right {
		return left - right
	}
	return right - left
}

func sameImage(left, right image.Image) bool {
	if !left.Bounds().Eq(right.Bounds()) {
		return false
	}
	for y := left.Bounds().Min.Y; y < left.Bounds().Max.Y; y++ {
		for x := left.Bounds().Min.X; x < left.Bounds().Max.X; x++ {
			if left.At(x, y) != right.At(x, y) {
				return false
			}
		}
	}
	return true
}

func containsColor(img image.Image, want color.NRGBA) bool {
	return containsColorInRect(img, want, img.Bounds())
}

func containsColorInRect(img image.Image, want color.NRGBA, roi image.Rectangle) bool {
	for y := roi.Min.Y; y < roi.Max.Y; y++ {
		for x := roi.Min.X; x < roi.Max.X; x++ {
			if got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); got == want {
				return true
			}
		}
	}
	return false
}

func assertMonochrome(t *testing.T, content []byte) {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(content))
	colors := make(map[string]struct{})
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("could not parse SVG colors: %v", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range start.Attr {
			if (attr.Name.Local == "fill" || attr.Name.Local == "stroke") && attr.Value != "none" && attr.Value != "" {
				colors[attr.Value] = struct{}{}
			}
		}
	}
	if len(colors) != 1 {
		t.Fatalf("SVG uses %d foreground colors, want one: %v", len(colors), colors)
	}
}

func stateForTrayIconName(name string) trayicon.State {
	switch name {
	case "synced":
		return trayicon.Synced
	case "fetching":
		return trayicon.Fetching
	case "behind-1", "behind-99", "behind-100":
		return trayicon.Behind
	case "dirty":
		return trayicon.Dirty
	default:
		return trayicon.Error
	}
}

func countForTrayIconName(name string) int {
	switch name {
	case "behind-1":
		return 1
	case "behind-99":
		return 99
	case "behind-100":
		return 100
	default:
		return 0
	}
}

// ensure the resource interface is what we think (compile-time guard for the test).
var _ fyne.Resource = (*theme.ThemedResource)(nil)
