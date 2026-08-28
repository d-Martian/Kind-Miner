package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Small composed pieces the dashboard and settings are built from. Fyne 2.5.3
// has no card, tile, pill or key/value widget, so they are assembled here from
// canvas primitives rather than open-coded at every use.

// card wraps content in a rounded, outlined surface.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colorSurface)
	bg.StrokeColor = colorDivider
	bg.StrokeWidth = 1
	bg.CornerRadius = 8
	return container.NewStack(bg, container.NewPadded(content))
}

// statTile is one of the dashboard's headline numbers: a large value, an
// optional unit, and a quiet label saying what it measures.
type statTile struct {
	value  *canvas.Text
	unit   *canvas.Text
	label  *canvas.Text
	object fyne.CanvasObject
}

func newStatTile(label string) *statTile {
	t := &statTile{
		value: canvas.NewText(emDash, colorForeground),
		unit:  canvas.NewText("", colorMuted),
		label: canvas.NewText(label, colorMuted),
	}
	t.value.TextSize = 22
	t.value.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	t.unit.TextSize = 11
	t.label.TextSize = 11

	// Baseline-align the unit with the value by pinning it to the bottom of the
	// row; an HBox would centre it against the much taller number.
	head := container.NewHBox(t.value, container.NewVBox(layoutSpacer(), t.unit))
	t.object = card(container.NewVBox(head, t.label))
	return t
}

// Set updates the number and its unit. Passing an empty unit leaves the value
// standing alone, which is what the connection tile does.
func (t *statTile) Set(value, unit string) {
	if t.value.Text != value {
		t.value.Text = value
		t.value.Refresh()
	}
	if t.unit.Text != unit {
		t.unit.Text = unit
		t.unit.Refresh()
	}
}

// SetColor tints the value, used to show connection state.
func (t *statTile) SetColor(c color.Color) {
	if t.value.Color != c {
		t.value.Color = c
		t.value.Refresh()
	}
}

func (t *statTile) Object() fyne.CanvasObject { return t.object }

// statusPill is the coloured dot and label saying what the miner is doing.
type statusPill struct {
	dot    *canvas.Circle
	text   *canvas.Text
	object fyne.CanvasObject
}

func newStatusPill() *statusPill {
	p := &statusPill{
		dot:  canvas.NewCircle(colorMuted),
		text: canvas.NewText("Starting…", colorForeground),
	}
	p.dot.Resize(fyne.NewSize(8, 8))
	p.text.TextSize = 13

	dot := container.NewWithoutLayout(p.dot)
	dot.Resize(fyne.NewSize(8, 8))
	p.object = container.NewHBox(container.NewCenter(dot), p.text)
	return p
}

func (p *statusPill) Set(text string, c color.Color) {
	if p.text.Text != text {
		p.text.Text = text
		p.text.Refresh()
	}
	if p.dot.FillColor != c {
		p.dot.FillColor = c
		p.dot.Refresh()
	}
}

func (p *statusPill) Object() fyne.CanvasObject { return p.object }

// kvList is a two-column list of labelled values — the detail panels behind
// each dashboard tile, and the read-only facts in settings.
type kvList struct {
	values map[string]*canvas.Text
	rows   []fyne.CanvasObject
}

func newKVList() *kvList {
	return &kvList{values: map[string]*canvas.Text{}}
}

// Add appends a row and returns the list for chaining.
func (l *kvList) Add(key, value string) *kvList {
	k := canvas.NewText(key, colorMuted)
	k.TextSize = 11
	v := canvas.NewText(shortenMiddle(value, valueWidth), colorForeground)
	v.TextSize = 11
	v.TextStyle = fyne.TextStyle{Monospace: true}
	v.Alignment = fyne.TextAlignTrailing

	l.values[key] = v
	l.rows = append(l.rows, container.NewBorder(nil, nil, k, v))
	return l
}

// Set updates a row added earlier. Unknown keys are ignored so a caller can
// update optimistically without checking which rows a mode built.
//
// Values are elided rather than allowed to set the column's width: canvas.Text
// does not wrap, so a long one becomes a minimum width that propagates all the
// way out to the window.
func (l *kvList) Set(key, value string) {
	v, ok := l.values[key]
	if !ok {
		return
	}
	value = shortenMiddle(value, valueWidth)
	if v.Text == value {
		return
	}
	v.Text = value
	v.Refresh()
}

func (l *kvList) Object() fyne.CanvasObject { return container.NewVBox(l.rows...) }

// sectionLabel is a small heading above a group of controls.
func sectionLabel(text string) fyne.CanvasObject {
	t := canvas.NewText(text, colorMuted)
	t.TextSize = 11
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// note is the small explanatory paragraph under a control. It wraps, which
// canvas.Text cannot do — a non-wrapping line here would stretch the window to
// the width of the sentence.
func note(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	l.TextStyle = fyne.TextStyle{Italic: true}
	return l
}
