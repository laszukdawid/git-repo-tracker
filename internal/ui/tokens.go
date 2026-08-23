package ui

// Design tokens: every radius, gap and text size in the app resolves to one of
// the constants below.
//
// They live apart from theme.go on purpose. That file is entirely about colour,
// and geometry is an independent axis — mixing the two is how the interface
// drifted out of alignment in the first place. The window had a soft 20px corner
// while the search field inside it was nearly square; group headers started
// their text 8px left of the repo names beneath them; eight different text sizes
// were in use between 10.5 and 14. None of that was decided, it accumulated.
//
// glassTheme.Size() (theme.go) is a thin adapter over these: it publishes the
// subset that Fyne's own widgets read, so a stock Entry, Button and Select pick
// up the same scale as the hand-drawn widgets without being told twice.

// Corner radii, anchored to the window. The macOS popover is drawn with
// radiusWindow; a surface inset by spaceSm inside it geometrically wants
// radiusWindow - spaceSm, which is radiusMd. Everything smaller steps down once.
const (
	radiusSm     = 8  // chips, action buttons, tooltips, row highlights
	radiusMd     = 12 // input fields, buttons, cards — anything inset in the window
	radiusWindow = 20 // the popover itself; mirrored in native_darwin.go
)

// Spacing, on a 4px rhythm with 2 and 6 as deliberate half-steps. space3xs and
// spaceXs are load-bearing: without them a two-line row grows past 56px and the
// popover shows noticeably fewer repositories.
const (
	space3xs = 2
	space2xs = 4
	spaceXs  = 6
	spaceSm  = 8
	spaceMd  = 12
	spaceLg  = 16
)

// Text sizes. Three, down from eight.
const (
	textXs = 11 // branches, counters, captions, the footer
	textSm = 12 // group-header paths, tooltips, secondary labels
	textMd = 13 // repo names, and Fyne's own SizeNameText for stock widgets
)

// Row geometry. Both lines of a repo row are fixed-height, so a row's total
// height is the same for every repository in the list.
const (
	rowPadX   = spaceMd  // row horizontal padding
	rowPadY   = spaceXs  // row vertical padding
	glyphColW = 18       // status-glyph gutter
	glyphGap  = spaceMd  // gutter to name
	branchGap = spaceXs  // name to inline branch
	lineGap   = space3xs // between the name line and the branch line

	// chevColW is the disclosure gutter, shared by scan-root headers and repo
	// rows. Repo rows did not used to have one, and nothing said a row could be
	// opened at all — you had to discover it by clicking. Now the same mark that
	// folds a section folds a row, in the same column.
	chevColW = 13
	chevGap  = space2xs

	// nameX is the single left rail: repo names, group-header paths and the
	// expanded detail panel all start here, so the list reads as one column.
	nameX        = rowPadX + chevColW + chevGap + glyphColW + glyphGap
	detailIndent = nameX

	// Minimum height of the name line, so a row keeps its shape even with a
	// short glyph and no descenders.
	lineMinH = 18
)

// Hover action chips at the right edge of a row.
const (
	actionBox   = 28
	actionIcon  = 16
	actionInset = (actionBox - actionIcon) / 2

	// Row actions keep the full target above, but draw quieter chrome inside it.
	// Header and settings controls retain the standard surface and icon sizes.
	rowActionSurface = 22
	rowActionIcon    = 13

	// Every compact action uses the same target, glyph and spacing contract.
	// Keeping these beside actionBox prevents header, settings and row controls
	// from growing separate geometry systems.
	actionSiblingGap = space2xs
	actionLabelPad   = spaceSm
	actionLabelGap   = spaceXs
	actionClusterGap = spaceSm
	searchInnerPad   = 10

	// rightReserve is the width kept clear on the title line for the behind
	// count, or for the three chips that replace it on hover — pull or keep-fresh,
	// open in editor, open folder. Derived rather than written as a literal so it
	// cannot drift when the chip size or the number of chips changes.
	rightReserve = 3*actionBox + 2*actionSiblingGap + spaceXs
)

// The always-visible keep-fresh marker, kept under lineMinH so it can never
// make a row taller.
const (
	keepBadgeSize = 11
	keepBadgeGap  = 5

	// The uncommitted-changes mark beside the name. Smaller than the keep-fresh
	// badge: it is a note, not a call to action.
	dirtyDotSize = 7
)

// hairlineW is the width of every 1px rule and border in the app.
const hairlineW = 1
