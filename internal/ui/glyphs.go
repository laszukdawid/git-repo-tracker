package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// glyphKind selects which status/disclosure mark a glyph draws.
type glyphKind int

const (
	glyphBehind       glyphKind = iota // down-arrow (commits to pull)
	glyphSynced                        // check (even with origin)
	glyphDirty                         // filled dot (uncommitted changes)
	glyphChevron                       // ▾ disclosure, group expanded
	glyphChevronRight                  // ▸ disclosure, group collapsed
	glyphError                         // "!" on a filled disc (fetch/status error)
)

// glyph is a tiny single-colour status/disclosure mark drawn from canvas
// primitives (two strokes, or a filled dot) rather than an SVG. Fyne's SVG
// colouriser only recolours *fill* and skips fill="none", so it can't tint the
// stroke-based line icons the design (option 3a) uses — the arrow, check and
// chevron are therefore drawn directly here and coloured straight from the
// palette, which also lets them repaint on a theme flip like the rest of the row.
//
// Coordinates below are expressed in the design's 24×24 viewBox and scaled to the
// glyph's actual box in Layout, so the on-screen stroke width matches the mock
// (e.g. a stroke of 2 shown at a 15px box renders ~1.25px, exactly as the HTML).
type glyph struct {
	widget.BaseWidget
	kind   glyphKind
	col    color.Color
	box    float32 // width/height of the square glyph
	stroke float32 // stroke width in viewBox (24) units; 0 for the dot
}

func newGlyph(kind glyphKind, col color.Color, box, stroke float32) *glyph {
	g := &glyph{kind: kind, col: col, box: box, stroke: stroke}
	g.ExtendBaseWidget(g)
	return g
}

// segs returns the line segments (x1,y1,x2,y2) making up a stroked glyph, in
// 24×24 viewBox space. The dot glyph has none (it's a circle).
func (g *glyph) segs() [][4]float32 {
	switch g.kind {
	case glyphBehind:
		return [][4]float32{{12, 4, 12, 17}, {6, 11, 12, 17}, {12, 17, 18, 11}}
	case glyphSynced:
		return [][4]float32{{5, 13, 9, 17}, {9, 17, 19, 6}}
	case glyphChevron:
		return [][4]float32{{6, 9, 12, 15}, {12, 15, 18, 9}}
	case glyphChevronRight:
		return [][4]float32{{9, 6, 15, 12}, {15, 12, 9, 18}}
	default:
		return nil
	}
}

func (g *glyph) CreateRenderer() fyne.WidgetRenderer {
	r := &glyphRenderer{g: g}
	switch g.kind {
	case glyphDirty:
		r.dot = canvas.NewCircle(g.col)
		r.objects = []fyne.CanvasObject{r.dot}
	case glyphError:
		// A red disc with a white exclamation, echoing the tray's error badge.
		r.disc = canvas.NewCircle(g.col)
		r.exBar = canvas.NewLine(chipSelectedText)
		r.exDot = canvas.NewCircle(chipSelectedText)
		r.objects = []fyne.CanvasObject{r.disc, r.exBar, r.exDot}
	default:
		for _, s := range g.segs() {
			ln := canvas.NewLine(g.col)
			ln.StrokeWidth = g.stroke // rescaled to box units in Layout
			r.lines = append(r.lines, lineSeg{line: ln, pts: s})
			r.objects = append(r.objects, ln)
		}
	}
	return r
}

type lineSeg struct {
	line *canvas.Line
	pts  [4]float32
}

type glyphRenderer struct {
	g       *glyph
	dot     *canvas.Circle // dirty
	disc    *canvas.Circle // error badge background
	exBar   *canvas.Line   // error exclamation stem
	exDot   *canvas.Circle // error exclamation dot
	lines   []lineSeg
	objects []fyne.CanvasObject
}

func (r *glyphRenderer) MinSize() fyne.Size { return fyne.NewSize(r.g.box, r.g.box) }

// Layout draws the glyph at its intended box size, centred in whatever space it's
// given, scaled from the 24-unit viewBox — so a glyph placed in a taller/wider slot
// (e.g. the 18px status column) stays its true size rather than stretching to fill.
func (r *glyphRenderer) Layout(size fyne.Size) {
	side := r.g.box
	if m := min(size.Width, size.Height); side > m {
		side = m
	}
	offX := (size.Width - side) / 2
	offY := (size.Height - side) / 2
	scale := side / 24

	if r.dot != nil {
		r.dot.Move(fyne.NewPos(offX, offY))
		r.dot.Resize(fyne.NewSize(side, side))
		return
	}
	if r.disc != nil {
		r.disc.Move(fyne.NewPos(offX, offY))
		r.disc.Resize(fyne.NewSize(side, side))
		r.exBar.Position1 = fyne.NewPos(offX+12*scale, offY+6.5*scale)
		r.exBar.Position2 = fyne.NewPos(offX+12*scale, offY+13*scale)
		r.exBar.StrokeWidth = r.g.stroke * scale
		dr := 1.5 * scale
		r.exDot.Move(fyne.NewPos(offX+12*scale-dr, offY+17*scale-dr))
		r.exDot.Resize(fyne.NewSize(2*dr, 2*dr))
		return
	}
	for _, s := range r.lines {
		s.line.Position1 = fyne.NewPos(offX+s.pts[0]*scale, offY+s.pts[1]*scale)
		s.line.Position2 = fyne.NewPos(offX+s.pts[2]*scale, offY+s.pts[3]*scale)
		s.line.StrokeWidth = r.g.stroke * scale
		s.line.Refresh()
	}
}

func (r *glyphRenderer) Refresh() {
	if r.dot != nil {
		r.dot.FillColor = r.g.col
		r.dot.Refresh()
		return
	}
	if r.disc != nil {
		r.disc.FillColor = r.g.col
		r.exBar.StrokeColor = chipSelectedText
		r.exDot.FillColor = chipSelectedText
		r.disc.Refresh()
		r.exBar.Refresh()
		r.exDot.Refresh()
		return
	}
	for _, s := range r.lines {
		s.line.StrokeColor = r.g.col
		s.line.Refresh()
	}
}

func (r *glyphRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *glyphRenderer) Destroy()                     {}
