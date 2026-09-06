package gui

import (
	"image"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// The dashboard chart: everything else's CPU against the miner's, with the
// kindness ceiling drawn in.
//
// This is the most important thing in the window. kind-miner is meant to be
// set-and-forget, and the failure mode that costs a user the most is deciding
// some unrelated slowdown is the miner's fault and uninstalling it. Seeing the
// miner's line dive the moment the grey one climbs answers that in a way no
// status text can.

// chartGutter is the width reserved for the percentage labels, in logical
// pixels.
const chartGutter = 26

// chart is a live plot of the scheduler's rolling history.
type chart struct {
	mu      sync.Mutex
	samples []stats.Sample
	ceiling float64
	opts    config.ChartOptions

	raster *canvas.Raster
	labels []*canvas.Text
	object fyne.CanvasObject
}

// newChart builds the plot and its axis labels.
func newChart() *chart {
	c := &chart{
		ceiling: 1,
		opts:    config.Defaults().Chart,
	}
	c.raster = canvas.NewRaster(c.draw)

	for _, pct := range []string{"100%", "50%", "0%"} {
		t := canvas.NewText(pct, withAlpha(colorForeground, 0x5c))
		t.TextSize = 9
		c.labels = append(c.labels, t)
	}

	objects := []fyne.CanvasObject{c.raster}
	for _, l := range c.labels {
		objects = append(objects, l)
	}
	c.object = container.New(&chartLayout{}, objects...)
	return c
}

// Object returns the canvas object to place in a layout.
func (c *chart) Object() fyne.CanvasObject { return c.object }

// Set replaces the plotted data and redraws. ceiling is the kindness ceiling as
// a fraction of the whole machine.
func (c *chart) Set(samples []stats.Sample, ceiling float64, opts config.ChartOptions) {
	c.mu.Lock()
	c.samples = samples
	c.ceiling = ceiling
	c.opts = opts
	c.mu.Unlock()
	c.raster.Refresh()
}

// chartLayout puts the raster to the right of a fixed gutter and pins the three
// percentage labels to the gridlines they name.
type chartLayout struct{}

func (l *chartLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(160, 96)
}

func (l *chartLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	raster := objects[0]
	raster.Move(fyne.NewPos(chartGutter, 0))
	raster.Resize(fyne.NewSize(size.Width-chartGutter, size.Height))

	// The labels sit at the same fractions the raster draws gridlines at, so
	// the two must use one definition of where a value lands.
	fractions := []float32{1, 0.5, 0}
	for i, obj := range objects[1:] {
		if i >= len(fractions) {
			break
		}
		min := obj.MinSize()
		y := plotY(fractions[i], size.Height) - min.Height/2
		obj.Move(fyne.NewPos(0, y))
		obj.Resize(min)
	}
}

// plotY maps a 0–1 value to a vertical position, leaving a 2px margin top and
// bottom so a line at 0% or 100% is not clipped in half.
func plotY(v float32, height float32) float32 {
	return height - 2 - v*(height-4)
}

// draw renders the plot. Fyne calls it with pixel dimensions, which on a HiDPI
// display are larger than the logical size — everything measured in logical
// pixels is scaled by the ratio between them.
func (c *chart) draw(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if w <= 0 || h <= 0 {
		return img
	}

	c.mu.Lock()
	samples := c.samples
	ceiling := c.ceiling
	opts := c.opts
	c.mu.Unlock()

	scale := float32(1)
	if lw := c.raster.Size().Width; lw > 0 {
		scale = float32(w) / lw
	}

	fh := float32(h)
	y := func(v float64) float32 { return plotY(float32(v), fh) }

	// Gridlines first, so everything else sits on top of them.
	for _, v := range []float64{0, 0.25, 0.5, 0.75, 1} {
		gy := y(v)
		fillRect(img, 0, gy, float32(w), gy+scale, withAlpha(colorForeground, 0x14))
	}

	// The headroom band: the part of the machine mining will not take. Drawing
	// it is the difference between a chart that shows two lines and one that
	// shows a promise being kept.
	if opts.ShadeHeadroom && ceiling < 1 {
		fillRect(img, 0, y(1), float32(w), y(ceiling), withAlpha(colorForeground, 0x0e))
		ceilingLine := []point{{X: 0, Y: y(ceiling)}, {X: float32(w), Y: y(ceiling)}}
		for _, dash := range dashPolyline(ceilingLine, 3*scale, 3*scale) {
			strokePolyline(img, dash, scale, withAlpha(colorMuted, 0x80))
		}
	}

	window := time.Duration(opts.WindowSeconds) * time.Second
	if window <= 0 {
		window = 30 * time.Second
	}
	visible := stats.Since(samples, window)
	if len(visible) < 2 {
		return img
	}

	// Position by timestamp rather than index so a missed tick leaves a gap in
	// the right place instead of quietly compressing the timeline.
	end := visible[len(visible)-1].At
	start := end.Add(-window)
	x := func(t time.Time) float32 {
		f := float64(t.Sub(start)) / float64(window)
		if f < 0 {
			f = 0
		}
		return float32(f) * float32(w)
	}

	series := func(value func(stats.Sample) float64) []point {
		pts := make([]point, 0, len(visible))
		for _, s := range visible {
			pts = append(pts, point{X: x(s.At), Y: y(value(s))})
		}
		return pts
	}

	if opts.DrawTemp {
		// Temperature shares the axis on a 0–100 °C scale. It is dashed and
		// faint because it is a different unit borrowing the same space.
		temp := series(func(s stats.Sample) float64 { return clamp01(s.TempC / 100) })
		for _, dash := range dashPolyline(temp, 2*scale, 4*scale) {
			strokePolyline(img, dash, scale, withAlpha(colorMuted, 0xaa))
		}
	}

	other := series(func(s stats.Sample) float64 { return clamp01(s.OtherCPU) })
	strokePolyline(img, other, 1.25*scale, withAlpha(colorMuted, 0xdd))

	miner := series(func(s stats.Sample) float64 { return clamp01(s.MinerCPU) })
	if opts.FillMiner {
		area := append([]point(nil), miner...)
		area = append(area,
			point{X: miner[len(miner)-1].X, Y: fh},
			point{X: miner[0].X, Y: fh},
		)
		fillPathImage(img, area, verticalFade{
			col:    colorAccent,
			top:    0,
			height: fh,
			peak:   0x47,
		})
	}
	strokePolyline(img, miner, 1.75*scale, colorAccent)

	if opts.MarkBackoff {
		for _, s := range visible {
			if !s.BackedOff {
				continue
			}
			px := x(s.At)
			fillRect(img, px-scale, fh-3*scale, px+scale, fh, colorAccent)
		}
	}

	return img
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

// legendSwatch is the small coloured line in front of each legend entry.
func legendSwatch(col color.Color) fyne.CanvasObject {
	r := canvas.NewRectangle(col)
	r.SetMinSize(fyne.NewSize(10, 2))
	return container.NewCenter(r)
}
