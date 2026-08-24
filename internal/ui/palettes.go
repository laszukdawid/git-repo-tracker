package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
)

// Three colour families, each with a light and a dark variant — six palettes in
// all, chosen in Settings.
//
// They exist because the original single palette put every repository name in
// saturated accent blue and then added four status hues and an amber pill on
// top, so five colour families competed inside one small list. Each family below
// takes a different position on that problem rather than being a shade of the
// same answer:
//
//   - Slate  — names go neutral; colour is spent only on status and on things
//     you can click.
//   - Ink    — a warm ground, paper and charcoal rather than grey, with one gold
//     doing duty as both accent and "behind", cutting the palette to three hues.
//   - Signal — greyscale with exactly one colour, used only where you must act;
//     a clean repository draws no glyph at all.
//
// Only the decisions below differ per palette. Everything else — hovers, tints,
// card surfaces, tooltip chrome — is derived from them, so a new family is a
// dozen colours rather than thirty-four.

// paletteSeed is the set of real decisions behind one palette.
type paletteSeed struct {
	dark bool

	gradTop    color.NRGBA
	gradBottom color.NRGBA
	fieldBg    color.NRGBA

	name       color.NRGBA // repository title
	syncedName color.NRGBA // title of a repo with nothing to do
	detailKey  color.NRGBA // "Local"/"Origin" labels
	muted      color.NRGBA // secondary text and flat icons
	faint      color.NRGBA // branch, meta, footer

	accent color.NRGBA // interactive: selection, pull chip, keep-fresh badge
	behind color.NRGBA
	// ahead is the "you have work to push" hue. It has to differ from behind:
	// two families deliberately reuse the accent as `behind`, so deriving ahead
	// from the accent would have made incoming and outgoing work the same colour
	// in two of the three palettes. Signal is the exception on purpose — it says
	// everything with shape and keeps one hue.
	ahead  color.NRGBA
	dirty  color.NRGBA
	synced color.NRGBA
	failed color.NRGBA

	// Status told by shape rather than hue.
	syncedGlyphHidden bool
	dirtyOutlined     bool
}

// paletteFamily identifies one of the three colour families.
type paletteFamily int

const (
	familySlate paletteFamily = iota
	familyInk
	familySignal
)

// familyFromConfig maps the persisted name onto a family, defaulting to Slate.
func familyFromConfig(name string) paletteFamily {
	switch name {
	case config.PaletteInk:
		return familyInk
	case config.PaletteSignal:
		return familySignal
	default:
		return familySlate
	}
}

func seedFor(f paletteFamily, v fyne.ThemeVariant) paletteSeed {
	dark := v != theme.VariantLight
	switch f {
	case familyInk:
		if dark {
			return paletteSeed{
				dark:    true,
				gradTop: hex(0x1E1B18), gradBottom: hex(0x131110), fieldBg: hex(0x161311),
				name: hex(0xF0EBE4), syncedName: hex(0x8B8175), detailKey: hex(0xD4CCC2),
				muted: hex(0xA39A8F), faint: hex(0x776F66),
				accent: hex(0xD9A441),
				behind: hex(0xD9A441), ahead: hex(0x7FA8B4), dirty: hex(0xC4785A),
				synced: hex(0x7E8B6F), failed: hex(0xC85A4A),
			}
		}
		return paletteSeed{
			gradTop: hex(0xFCFAF6), gradBottom: hex(0xF1EDE5), fieldBg: hex(0xFFFDF9),
			name: hex(0x23201C), syncedName: hex(0x8E8477), detailKey: hex(0x3E382F),
			muted: hex(0x6E665C), faint: hex(0x938A7C),
			accent: hex(0x8A6318),
			behind: hex(0x8A6318), ahead: hex(0x35707F), dirty: hex(0x9C5334),
			synced: hex(0x6B7A58), failed: hex(0xA94433),
		}

	case familySignal:
		if dark {
			return paletteSeed{
				dark:    true,
				gradTop: hex(0x0C0D0F), gradBottom: hex(0x0C0D0F), fieldBg: hex(0x141619),
				name: hex(0xF2F3F5), syncedName: hex(0x6A6E75), detailKey: hex(0xC6C9CE),
				muted: hex(0x9498A0), faint: hex(0x5F636A),
				accent: hex(0xE0533D),
				behind: hex(0xE0533D), ahead: hex(0xE0533D), dirty: hex(0x8A8F97),
				synced: hex(0x2C2F34), failed: hex(0xE0533D),
				syncedGlyphHidden: true, dirtyOutlined: true,
			}
		}
		return paletteSeed{
			gradTop: hex(0xFFFFFF), gradBottom: hex(0xFFFFFF), fieldBg: hex(0xFFFFFF),
			name: hex(0x15171A), syncedName: hex(0x90959C), detailKey: hex(0x33383F),
			muted: hex(0x5A5F67), faint: hex(0x8B9098),
			accent: hex(0xC43B22),
			behind: hex(0xC43B22), ahead: hex(0xC43B22), dirty: hex(0x8B9098),
			synced: hex(0xDEE1E5), failed: hex(0xC43B22),
			syncedGlyphHidden: true, dirtyOutlined: true,
		}

	default: // familySlate
		if dark {
			return paletteSeed{
				dark:    true,
				gradTop: hex(0x17191D), gradBottom: hex(0x0E1013), fieldBg: hex(0x111317),
				name: hex(0xE7E9EC), syncedName: hex(0x7C828B), detailKey: hex(0xC9CED4),
				muted: hex(0x9AA0A8), faint: hex(0x666C74),
				accent: hex(0x6E93D6),
				behind: hex(0xE9B168), ahead: hex(0x6E93D6), dirty: hex(0xC98A6A),
				synced: hex(0x6E7681), failed: hex(0xE05A5A),
			}
		}
		return paletteSeed{
			gradTop: hex(0xFCFCFD), gradBottom: hex(0xF1F2F4), fieldBg: hex(0xFFFFFF),
			name: hex(0x1B1E23), syncedName: hex(0x868C95), detailKey: hex(0x3A4048),
			muted: hex(0x5B616A), faint: hex(0x878D96),
			accent: hex(0x3B6FC4),
			behind: hex(0x9A6A22), ahead: hex(0x3B6FC4), dirty: hex(0x9C5F42),
			synced: hex(0xA0A6AE), failed: hex(0xB4383C),
		}
	}
}

// paletteFor builds the full palette for one family and variant.
func paletteFor(f paletteFamily, v fyne.ThemeVariant) palette {
	s := seedFor(f, v)

	// Overlays run white-on-dark and black-on-light, so one set of alphas gives
	// the same perceived step in both variants.
	over := func(a uint8) color.NRGBA {
		if s.dark {
			return color.NRGBA{R: 255, G: 255, B: 255, A: a}
		}
		return color.NRGBA{A: a}
	}

	p := palette{
		accent:     s.accent,
		rowName:    s.name,
		rowSub:     s.muted,
		rowHover:   over(14),
		btnHover:   over(30),
		detailKey:  s.detailKey,
		gradTop:    s.gradTop,
		gradBottom: s.gradBottom,

		syncedGlyphHidden: s.syncedGlyphHidden,
		dirtyOutlined:     s.dirtyOutlined,

		statusBehind: s.behind,
		statusAhead:  s.ahead,
		statusDirty:  s.dirty,
		statusSynced: s.synced,
		statusError:  s.failed,
		behindTint:   tint(s.behind, 38),
		dirtyTint:    tint(s.dirty, 34),
		syncedTint:   tint(s.synced, 30),

		muted:         s.muted,
		faint:         s.faint,
		syncedName:    s.syncedName,
		groupHeaderBg: over(10),
		rowExpandedBg: over(10),
		pillBehindBg:  tint(s.behind, 38),
		hairline:      over(18),
		toggleOffBg:   over(36),
		pullBtnBg:     tint(s.accent, 38),
		pushBtnBg:     tint(s.ahead, 38),
		openBtnBg:     over(16),

		fieldBg:     s.fieldBg,
		fieldBorder: over(32),
		cardBg:      surface(s, 6),
		cardBorder:  over(20),
		titlebarBg:  surface(s, 10),
		footerBg:    surface(s, -8),
	}

	// Tooltips sit above everything, so they take a raised surface and the
	// brightest text step rather than a derived overlay.
	p.tipBg = surface(s, 16)
	p.tipBorder = over(40)
	p.tipText = s.name
	p.toggleKnobOff = s.faint
	if !s.dark {
		p.toggleKnobOff = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return p
}

// hex expands 0xRRGGBB into an opaque colour.
func hex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

// tint is a colour at low alpha, for the soft chip and pill fills.
func tint(c color.NRGBA, a uint8) color.NRGBA {
	return color.NRGBA{R: c.R, G: c.G, B: c.B, A: a}
}

// surface lightens (dark themes) or darkens (light themes) the ground by step,
// so cards, footers and tooltips sit at a consistent distance from it. A
// negative step goes the other way — the footer recedes rather than lifts.
func surface(s paletteSeed, step int) color.NRGBA {
	base := s.gradTop
	if !s.dark {
		step = -step
	}
	adj := func(v uint8) uint8 {
		n := int(v) + step
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return uint8(n)
	}
	return color.NRGBA{R: adj(base.R), G: adj(base.G), B: adj(base.B), A: 255}
}
