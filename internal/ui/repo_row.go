package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/dawidlaszuk/git-repo-tracker/internal/monitor"
)

var (
	rowHoverColor  = color.NRGBA{R: 255, G: 255, B: 255, A: 14}
	btnHoverColor  = color.NRGBA{R: 255, G: 255, B: 255, A: 38}
	detailKeyColor = color.NRGBA{R: 210, G: 216, B: 226, A: 255}
)

const rightReserve = 70 // space kept on the title's right edge for the action icons

// iconButton is a minimal tappable icon with its own hover highlight. It is NOT
// Hoverable: the row below tracks the pointer itself (via MouseMoved) and toggles
// each button's highlight, so the icons never steal hover from the row (a
// widget.Button would, making the hover buttons vanish as you reach for them).
type iconButton struct {
	widget.BaseWidget
	res     fyne.Resource
	tip     string
	onTap   func()
	hovered bool
}

func newIconButton(res fyne.Resource, tip string, onTap func()) *iconButton {
	b := &iconButton{res: res, tip: tip, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *iconButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *iconButton) setHovered(h bool) {
	if b.hovered != h {
		b.hovered = h
		b.Refresh()
	}
}

func (b *iconButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 6
	img := canvas.NewImageFromResource(theme.NewThemedResource(b.res))
	img.FillMode = canvas.ImageFillContain
	return &iconButtonRenderer{b: b, bg: bg, img: img, objects: []fyne.CanvasObject{bg, img}}
}

type iconButtonRenderer struct {
	b       *iconButton
	bg      *canvas.Rectangle
	img     *canvas.Image
	objects []fyne.CanvasObject
}

func (r *iconButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	inset := float32(4)
	r.img.Move(fyne.NewPos(inset, inset))
	r.img.Resize(fyne.NewSize(size.Width-inset*2, size.Height-inset*2))
}
func (r *iconButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(26, 26) }
func (r *iconButtonRenderer) Refresh() {
	if r.b.hovered {
		r.bg.FillColor = btnHoverColor
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	r.img.Refresh()
}
func (r *iconButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *iconButtonRenderer) Destroy()                     {}

// repoRow is one list row. Collapsed it shows the repo title + status; clicking
// the body expands an inline detail panel beneath the title (the row grows via
// list.SetItemHeight). Hovering reveals action icons in the top-right corner.
type repoRow struct {
	widget.BaseWidget

	repo     monitor.RepoState
	expanded bool
	pulling  bool

	name      *canvas.Text
	sub       *canvas.Text
	pullBtn   *iconButton
	openBtn   *iconButton
	spinner   *widget.Activity
	rightBox  *fyne.Container
	detailBox *fyne.Container
	marquees  []*marqueeText // scrollable detail lines (path + commit messages)
	tips      *tooltipLayer

	hovered bool

	onExpand func(monitor.RepoState)
	onPull   func(monitor.RepoState)
	onOpen   func(monitor.RepoState)
}

func newRepoRow(tips *tooltipLayer) *repoRow {
	r := &repoRow{tips: tips}
	r.name = canvas.NewText("", rowNameColor)
	r.name.TextStyle = fyne.TextStyle{Bold: true}
	r.name.TextSize = 14
	r.sub = canvas.NewText("", rowSubColor)
	r.sub.TextStyle = fyne.TextStyle{Monospace: true}
	r.sub.TextSize = 12

	r.pullBtn = newIconButton(theme.DownloadIcon(), "Pull (fast-forward)", func() {
		if r.onPull != nil {
			r.onPull(r.repo)
		}
	})
	r.openBtn = newIconButton(theme.FolderOpenIcon(), "Open folder", func() {
		if r.onOpen != nil {
			r.onOpen(r.repo)
		}
	})
	r.spinner = widget.NewActivity()
	r.spinner.Hide()
	r.rightBox = container.New(&rowButtonsLayout{}, r.pullBtn, r.openBtn, r.spinner)
	r.detailBox = container.New(&tightVBox{gap: 4})
	r.detailBox.Hide()

	r.ExtendBaseWidget(r)
	return r
}

// Configure rebinds the row to a repo and its state/callbacks. widget.List
// recycles row objects during scroll, so this runs on every listUpdate.
func (r *repoRow) Configure(repo monitor.RepoState, expanded, pulling bool, detail *monitor.Details,
	onExpand, onPull, onOpen func(monitor.RepoState)) {

	if r.repo.Path != repo.Path {
		r.setHovered(false)
	}
	r.repo = repo
	r.expanded = expanded
	r.pulling = pulling
	r.onExpand, r.onPull, r.onOpen = onExpand, onPull, onOpen

	r.name.Text = repo.Name
	r.sub.Text = rowSubtitle(repo)

	if expanded {
		r.rebuildDetail(detail)
		r.detailBox.Show()
	} else {
		r.clearMarquees()
		r.detailBox.Hide()
	}

	r.updateActions()
	r.Refresh()
}

func (r *repoRow) clearMarquees() {
	for _, m := range r.marquees {
		m.stopAnim()
	}
	r.marquees = nil
}

func (r *repoRow) rebuildDetail(d *monitor.Details) {
	r.clearMarquees()
	r.detailBox.RemoveAll()
	if d == nil {
		r.detailBox.Add(r.line("Loading…", rowSubColor, 12, false, false, false))
		r.detailBox.Refresh()
		return
	}
	branch := r.repo.Branch
	if branch == "" {
		branch = "—"
	}
	// Path needs no label; commit message gets its own line (it can be long, and
	// scrolls on hover). Long lines truncate to fit otherwise.
	r.detailBox.Add(r.line(d.Path, rowSubColor, 12, false, true, true))
	r.detailBox.Add(r.line("Local "+branch, detailKeyColor, 12, true, false, false))
	r.addCommit(d.LocalHash, d.LocalTime, d.LocalMsg)
	r.detailBox.Add(r.line("Origin "+d.OriginRef, detailKeyColor, 12, true, false, false))
	r.addCommit(d.OriginHash, d.OriginTime, d.OriginMsg)

	if r.hovered { // already hovered when (re)built: scroll right away
		for _, m := range r.marquees {
			m.SetActive(true)
		}
	}
	r.detailBox.Refresh()
}

func (r *repoRow) addCommit(hash string, t time.Time, msg string) {
	if hash == "" {
		r.detailBox.Add(r.line("—", rowSubColor, 11, false, true, false))
		return
	}
	r.detailBox.Add(r.line(commitMeta(hash, t), rowSubColor, 11, false, true, false))
	if msg != "" {
		r.detailBox.Add(r.line(msg, rowSubColor, 11, false, true, true))
	}
}

// line builds a detail line; scrollable ones are tracked so hover can animate them.
func (r *repoRow) line(s string, col color.Color, size float32, bold, mono, scrollable bool) *marqueeText {
	m := newMarquee(s, col, size, bold, mono)
	if scrollable {
		r.marquees = append(r.marquees, m)
	}
	return m
}

// updateActions sets which right-side widgets are visible: a spinner while
// pulling, otherwise the action icons (only on hover).
func (r *repoRow) updateActions() {
	if r.pulling {
		r.pullBtn.Hide()
		r.openBtn.Hide()
		r.spinner.Show()
		r.spinner.Start()
	} else {
		r.spinner.Stop()
		r.spinner.Hide()
		if r.hovered {
			r.pullBtn.Show()
			r.openBtn.Show()
		} else {
			r.pullBtn.Hide()
			r.openBtn.Hide()
		}
	}
	r.rightBox.Refresh()
}

func (r *repoRow) Tapped(*fyne.PointEvent) {
	if r.onExpand != nil {
		r.onExpand(r.repo)
	}
}

func (r *repoRow) MouseIn(ev *desktop.MouseEvent) {
	r.setHovered(true)
	r.MouseMoved(ev)
}

func (r *repoRow) MouseMoved(ev *desktop.MouseEvent) {
	if !r.hovered || r.pulling {
		return
	}
	var hovered *iconButton
	for _, b := range []*iconButton{r.pullBtn, r.openBtn} {
		in := false
		if b.Visible() {
			origin := r.rightBox.Position().Add(b.Position())
			in = ev.Position.X >= origin.X && ev.Position.X <= origin.X+b.Size().Width &&
				ev.Position.Y >= origin.Y && ev.Position.Y <= origin.Y+b.Size().Height
		}
		b.setHovered(in)
		if in {
			hovered = b
		}
	}
	if r.tips != nil {
		if hovered != nil {
			r.tips.show(hovered.tip, hovered)
		} else {
			r.tips.hide()
		}
	}
}

func (r *repoRow) MouseOut() {
	r.pullBtn.setHovered(false)
	r.openBtn.setHovered(false)
	if r.tips != nil {
		r.tips.hide()
	}
	r.setHovered(false)
}

func (r *repoRow) setHovered(h bool) {
	if r.hovered == h {
		return
	}
	r.hovered = h
	for _, m := range r.marquees { // scroll path/messages only while hovered
		m.SetActive(h)
	}
	r.updateActions()
	r.Refresh()
}

func (r *repoRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	return &repoRowRenderer{
		row:     r,
		bg:      bg,
		objects: []fyne.CanvasObject{bg, r.name, r.sub, r.rightBox, r.detailBox},
	}
}

type repoRowRenderer struct {
	row     *repoRow
	bg      *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (rr *repoRowRenderer) titleHeight() float32 {
	pad := theme.Padding()
	return pad + rr.row.name.MinSize().Height + theme.InnerPadding() + rr.row.sub.MinSize().Height + pad
}

func (rr *repoRowRenderer) Layout(size fyne.Size) {
	pad := theme.Padding()
	gap := theme.InnerPadding()
	rr.bg.Resize(size)

	rb := rr.row.rightBox.MinSize()
	rr.row.rightBox.Resize(rb)
	rr.row.rightBox.Move(fyne.NewPos(size.Width-pad-rb.Width, pad))

	nameH := rr.row.name.MinSize().Height
	subH := rr.row.sub.MinSize().Height
	nameW := size.Width - pad*2 - rightReserve
	if nameW < 0 {
		nameW = 0
	}
	rr.row.name.Move(fyne.NewPos(pad, pad))
	rr.row.name.Resize(fyne.NewSize(nameW, nameH))
	rr.row.sub.Move(fyne.NewPos(pad, pad+nameH+gap))
	rr.row.sub.Resize(fyne.NewSize(size.Width-pad*2, subH))

	if rr.row.expanded {
		ty := rr.titleHeight()
		rr.row.detailBox.Move(fyne.NewPos(pad, ty))
		rr.row.detailBox.Resize(fyne.NewSize(size.Width-pad*2, size.Height-ty-pad))
	}
}

func (rr *repoRowRenderer) MinSize() fyne.Size {
	h := rr.titleHeight()
	if rr.row.expanded {
		h += rr.row.detailBox.MinSize().Height + theme.Padding()
	}
	w := rr.row.name.MinSize().Width + rightReserve + theme.Padding()*2
	return fyne.NewSize(w, h)
}

func (rr *repoRowRenderer) Refresh() {
	if rr.row.hovered {
		rr.bg.FillColor = rowHoverColor
	} else {
		rr.bg.FillColor = color.Transparent
	}
	rr.bg.Refresh()
	rr.row.name.Refresh()
	rr.row.sub.Refresh()
}

func (rr *repoRowRenderer) Objects() []fyne.CanvasObject { return rr.objects }

// Destroy stops any running animations so a row torn down mid-pull (or with the
// detail expanded) doesn't leak the spinner/marquee animations.
func (rr *repoRowRenderer) Destroy() {
	rr.row.spinner.Stop()
	rr.row.clearMarquees()
}

// tightVBox stacks objects vertically with a small fixed gap and no per-item
// padding (unlike container.NewVBox / widget.Label), keeping the detail dense.
type tightVBox struct{ gap float32 }

func (l *tightVBox) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + l.gap
	}
}

func (l *tightVBox) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		w = max(w, s.Width)
		h += s.Height
		n++
	}
	if n > 1 {
		h += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

// rowButtonsLayout packs the visible right-side widgets left-to-right.
type rowButtonsLayout struct{}

func (rowButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gap := theme.InnerPadding()
	x := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		o.Move(fyne.NewPos(x, (size.Height-s.Height)/2))
		o.Resize(s)
		x += s.Width + gap
	}
}

func (rowButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	gap := theme.InnerPadding()
	var w, h float32
	n := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		s := o.MinSize()
		w += s.Width
		h = max(h, s.Height)
		n++
	}
	if n > 1 {
		w += gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}
