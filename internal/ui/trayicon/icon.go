// Package trayicon renders the system tray icon resources independently of the
// Fyne app state. The UI package decides which state applies; this package turns
// that state into a themed SVG or composited PNG resource.
package trayicon

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed tray.svg
var trayFS embed.FS

// State describes the visual state of the tray icon.
type State int

const (
	Synced State = iota
	Behind
	Dirty
	Fetching
	Error
)

// Px is the rendered icon size (roughly 22pt menu-bar height at 2x for retina).
const Px = 44

var (
	glyphColor = color.NRGBA{R: 150, G: 150, B: 150, A: 255}
	amber      = color.NRGBA{R: 230, G: 169, B: 77, A: 255}
	coral      = color.NRGBA{R: 232, G: 130, B: 95, A: 255}
	red        = color.NRGBA{R: 229, G: 72, B: 77, A: 255}
	badgeInk   = color.NRGBA{R: 10, G: 13, B: 20, A: 255}
	white      = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
)

// Resource returns the tray resource for a state: the template glyph for
// synced/fetching, or a composited coloured PNG carrying an attention marker.
func Resource(state State, count int) fyne.Resource {
	switch state {
	case Behind:
		return composited(overlay{fill: amber, text: FmtBadge(count), ink: badgeInk})
	case Dirty:
		return composited(overlay{fill: coral, dot: true})
	case Error:
		return composited(overlay{fill: red, text: "!", ink: white})
	default:
		return Base()
	}
}

// Base returns the themed template tray glyph.
func Base() fyne.Resource {
	data, err := trayFS.ReadFile("tray.svg")
	if err != nil {
		return theme.BrokenImageIcon()
	}
	return theme.NewThemedResource(fyne.NewStaticResource("tray.svg", data))
}

func FmtBadge(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

type overlay struct {
	fill color.NRGBA
	dot  bool
	text string
	ink  color.NRGBA
}

func composited(ov overlay) fyne.Resource {
	img := image.NewNRGBA(image.Rect(0, 0, Px, Px))
	drawCommitGraph(img, glyphColor)
	drawOverlay(img, ov)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Base()
	}
	return fyne.NewStaticResource("tray-state.png", buf.Bytes())
}

func drawCommitGraph(img *image.NRGBA, col color.NRGBA) {
	sc := float64(Px) / 24
	sw := 1.8 * sc
	strokeSeg(img, 7*sc, 8.1*sc, 7*sc, 15.9*sc, sw, col)
	strokeSeg(img, 8.9*sc, 6.9*sc, 17*sc, 12*sc, sw, col)
	for _, c := range [][2]float64{{7, 6}, {7, 18}, {17, 12}} {
		fillCircle(img, c[0]*sc, c[1]*sc, 2.1*sc, col)
	}
}

func drawOverlay(img *image.NRGBA, ov overlay) {
	if ov.dot {
		r := 5.0
		fillCircle(img, float64(Px)-r-1, r+1, r, ov.fill)
		return
	}
	const scale = 2.0
	tw := digitsWidth(ov.text, scale)
	th := 5 * scale
	padX, padY := 3.0, 3.0
	bw, bh := tw+padX*2, th+padY*2
	bx := float64(Px) - bw - 0.5
	by := 0.5
	drawCapsule(img, bx, by, bw, bh, ov.fill)
	drawDigits(img, ov.text, bx+(bw-tw)/2, by+(bh-th)/2, scale, ov.ink)
}

func drawCapsule(img *image.NRGBA, x, y, w, h float64, col color.NRGBA) {
	r := h / 2
	fillCircle(img, x+r, y+r, r, col)
	fillCircle(img, x+w-r, y+r, r, col)
	fillRect(img, x+r, y, w-2*r, h, col)
}

func strokeSeg(img *image.NRGBA, x1, y1, x2, y2, width float64, col color.NRGBA) {
	r := width / 2
	forEachPixel(img, math.Min(x1, x2)-r-1, math.Min(y1, y2)-r-1, math.Max(x1, x2)+r+1, math.Max(y1, y2)+r+1,
		func(px, py int, cx, cy float64) {
			d := distToSegment(cx, cy, x1, y1, x2, y2)
			blend(img, px, py, col, clamp01(r+0.5-d))
		})
}

func fillCircle(img *image.NRGBA, cx, cy, r float64, col color.NRGBA) {
	forEachPixel(img, cx-r-1, cy-r-1, cx+r+1, cy+r+1, func(px, py int, x, y float64) {
		d := math.Hypot(x-cx, y-cy)
		blend(img, px, py, col, clamp01(r+0.5-d))
	})
}

func fillRect(img *image.NRGBA, x, y, w, h float64, col color.NRGBA) {
	forEachPixel(img, x, y, x+w, y+h, func(px, py int, _, _ float64) {
		blend(img, px, py, col, 1)
	})
}

func forEachPixel(img *image.NRGBA, x0, y0, x1, y1 float64, fn func(px, py int, cx, cy float64)) {
	b := img.Bounds()
	minX := maxInt(b.Min.X, int(math.Floor(x0)))
	minY := maxInt(b.Min.Y, int(math.Floor(y0)))
	maxX := minInt(b.Max.X, int(math.Ceil(x1)))
	maxY := minInt(b.Max.Y, int(math.Ceil(y1)))
	for py := minY; py < maxY; py++ {
		for px := minX; px < maxX; px++ {
			fn(px, py, float64(px)+0.5, float64(py)+0.5)
		}
	}
}

func blend(img *image.NRGBA, x, y int, c color.NRGBA, cov float64) {
	if cov <= 0 {
		return
	}
	if cov > 1 {
		cov = 1
	}
	sa := cov * float64(c.A) / 255
	dst := img.NRGBAAt(x, y)
	da := float64(dst.A) / 255
	oa := sa + da*(1-sa)
	if oa <= 0 {
		return
	}
	mix := func(cs, cd float64) uint8 {
		return uint8((cs*sa+cd*da*(1-sa))/oa + 0.5)
	}
	img.SetNRGBA(x, y, color.NRGBA{
		R: mix(float64(c.R), float64(dst.R)),
		G: mix(float64(c.G), float64(dst.G)),
		B: mix(float64(c.B), float64(dst.B)),
		A: uint8(oa*255 + 0.5),
	})
}

func distToSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / l2
	t = clamp01(t)
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var glyphs = map[rune][5]uint8{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b001, 0b001, 0b001},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
	'+': {0b000, 0b010, 0b111, 0b010, 0b000},
	'!': {0b010, 0b010, 0b010, 0b000, 0b010},
}

func digitsWidth(s string, scale float64) float64 {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return float64(n)*3*scale + float64(n-1)*scale
}

func drawDigits(img *image.NRGBA, s string, x, y, scale float64, col color.NRGBA) {
	cx := x
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			cx += 4 * scale
			continue
		}
		for row := 0; row < 5; row++ {
			for colIdx := 0; colIdx < 3; colIdx++ {
				if g[row]&(1<<(2-colIdx)) != 0 {
					fillRect(img, cx+float64(colIdx)*scale, y+float64(row)*scale, scale, scale, col)
				}
			}
		}
		cx += 4 * scale
	}
}
