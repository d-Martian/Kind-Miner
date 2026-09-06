package gui

import (
	"image"
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// The chart is the dashboard's central claim — "the miner is not what is
// slowing your machine" — so what it draws has to be checked, not assumed.

// syntheticHistory builds a history where the two CPU series are flat and
// distinct, so a rendered line can be located by the row it occupies.
func syntheticHistory(other, miner float64, n int) []stats.Sample {
	now := time.Now()
	out := make([]stats.Sample, n)
	for i := range out {
		out[i] = stats.Sample{
			At:       now.Add(time.Duration(i-n+1) * time.Second),
			OtherCPU: other,
			MinerCPU: miner,
			TempC:    50,
		}
	}
	return out
}

// alphaAt reports how strongly a pixel is painted, which is enough to find a
// line without asserting on anti-aliased colour values.
func alphaAt(img image.Image, x, y int) uint32 {
	_, _, _, a := img.At(x, y).RGBA()
	return a >> 8
}

// rowOfDarkestPaint returns the row in a column with the most paint on it.
func paintedRows(img image.Image, x int) []int {
	b := img.Bounds()
	var rows []int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if alphaAt(img, x, y) > 0x60 {
			rows = append(rows, y)
		}
	}
	return rows
}

func newTestChart(t *testing.T, samples []stats.Sample, ceiling float64, opts config.ChartOptions) *chart {
	t.Helper()
	c := newChart()
	// Layout normally sets this; the raster reads it to work out the device
	// scale, and a zero would leave every line one pixel wide on a HiDPI screen.
	c.raster.Resize(chartSize)
	c.Set(samples, ceiling, opts)
	return c
}

var chartSize = fyne.NewSize(400, 200)

// The miner and other-CPU series must land at the heights they claim, or the
// chart is decorative rather than informative.
func TestChartPlotsSeriesAtTheirValues(t *testing.T) {
	opts := config.ChartOptions{WindowSeconds: 30}
	c := newTestChart(t, syntheticHistory(0.25, 0.50, 30), 0.78, opts)

	img := c.draw(400, 200)
	const midX = 200

	rows := paintedRows(img, midX)
	if len(rows) < 2 {
		t.Fatalf("column %d has %d painted rows, want at least the two series", midX, len(rows))
	}

	// plotY is the single definition of where a value sits; the assertion is
	// that the ink actually landed there.
	wantMiner := int(plotY(0.50, 200))
	wantOther := int(plotY(0.25, 200))

	if !nearAnyRow(rows, wantMiner, 2) {
		t.Errorf("no line within 2px of the miner's 50%% (y=%d); painted rows: %v", wantMiner, rows)
	}
	if !nearAnyRow(rows, wantOther, 2) {
		t.Errorf("no line within 2px of other-CPU's 25%% (y=%d); painted rows: %v", wantOther, rows)
	}
	// The miner is the higher value, so it must sit above the other series.
	if wantMiner >= wantOther {
		t.Fatalf("test setup is wrong: miner y=%d is not above other y=%d", wantMiner, wantOther)
	}
}

// The axis labels and the gridlines have to agree. They are laid out by
// different code paths — Fyne widgets and a rasteriser — so this is exactly
// the kind of thing that drifts.
func TestChartLabelsShareThePlotGeometry(t *testing.T) {
	const height = 200
	for _, fraction := range []float32{0, 0.5, 1} {
		labelY := plotY(fraction, height)
		if labelY < 0 || labelY > height {
			t.Errorf("plotY(%.1f) = %.1f, outside the plot", fraction, labelY)
		}
	}
	// Top and bottom keep a margin so a line at 0% or 100% is not clipped in
	// half by the edge of the raster.
	if got := plotY(1, height); got < 1 {
		t.Errorf("plotY(1) = %.1f, want a margin below the top edge", got)
	}
	if got := plotY(0, height); got > height-1 {
		t.Errorf("plotY(0) = %.1f, want a margin above the bottom edge", got)
	}
	// The scale must be linear, or a value halfway up would not read as half.
	mid := plotY(0.5, height)
	if math.Abs(float64(mid-(plotY(0, height)+plotY(1, height))/2)) > 0.01 {
		t.Errorf("plotY is not linear: 0.5 maps to %.2f", mid)
	}
}

// Each option must actually change the picture — a checkbox that paints nothing
// different is worse than no checkbox.
func TestChartOptionsChangeTheDrawing(t *testing.T) {
	base := config.ChartOptions{WindowSeconds: 30}
	history := syntheticHistory(0.25, 0.50, 30)

	variants := map[string]config.ChartOptions{
		"shade headroom":       {WindowSeconds: 30, ShadeHeadroom: true},
		"fill under the miner": {WindowSeconds: 30, FillMiner: true},
		"draw temperature":     {WindowSeconds: 30, DrawTemp: true},
	}

	plain := newTestChart(t, history, 0.78, base).draw(400, 200)
	for name, opts := range variants {
		t.Run(name, func(t *testing.T) {
			got := newTestChart(t, history, 0.78, opts).draw(400, 200)
			if imagesEqual(plain, got) {
				t.Errorf("%q drew an identical image", name)
			}
		})
	}
}

// Backoff marks are the visible evidence of the miner stepping aside, so they
// have to reach the bottom edge where the eye looks for them.
func TestChartMarksBackoffs(t *testing.T) {
	history := syntheticHistory(0.25, 0.50, 30)
	history[15].BackedOff = true

	opts := config.ChartOptions{WindowSeconds: 30, MarkBackoff: true}
	marked := newTestChart(t, history, 0.78, opts).draw(400, 200)

	clean := syntheticHistory(0.25, 0.50, 30)
	unmarked := newTestChart(t, clean, 0.78, opts).draw(400, 200)

	if imagesEqual(marked, unmarked) {
		t.Fatal("a backoff drew nothing; the chart would hide the miner getting out of the way")
	}

	// The mark belongs on the bottom edge.
	found := false
	for x := 0; x < 400 && !found; x++ {
		if alphaAt(marked, x, 199) > 0x60 && alphaAt(unmarked, x, 199) <= 0x60 {
			found = true
		}
	}
	if !found {
		t.Error("no backoff mark on the bottom row of the plot")
	}
}

// Drawing must survive the states it meets on the way up: no history at all,
// and a single sample with nothing to join it to.
func TestChartHandlesEmptyAndSingleSample(t *testing.T) {
	opts := config.ChartOptions{WindowSeconds: 30, ShadeHeadroom: true, FillMiner: true, MarkBackoff: true}
	for name, history := range map[string][]stats.Sample{
		"no history":    nil,
		"one sample":    syntheticHistory(0.25, 0.5, 1),
		"two samples":   syntheticHistory(0.25, 0.5, 2),
		"zero-size win": syntheticHistory(0.25, 0.5, 10),
	} {
		t.Run(name, func(t *testing.T) {
			o := opts
			if name == "zero-size win" {
				o.WindowSeconds = 0 // must fall back rather than divide by nothing
			}
			c := newTestChart(t, history, 0.78, o)
			if img := c.draw(400, 200); img == nil {
				t.Fatal("draw returned nothing")
			}
			// A zero-sized raster happens during layout; it must not panic.
			if img := c.draw(0, 0); img == nil {
				t.Fatal("draw(0,0) returned nothing")
			}
		})
	}
}

// A stroked polyline must be continuous. x/image/vector sums signed coverage
// across subpaths, so a join wound against the segment it overlaps subtracts
// itself and punches a hole at every vertex — which renders as a dashed line
// and is invisible in the code that causes it.
func TestStrokePolylineHasNoGapsAtJoins(t *testing.T) {
	shapes := map[string][]point{}

	// A flat line: every vertex is a place two quads abut exactly.
	flat := make([]point, 0, 30)
	for i := 0; i < 30; i++ {
		flat = append(flat, point{X: float32(i) * 13, Y: 100})
	}
	shapes["flat"] = flat

	// A zig-zag, where consecutive segments point in different directions.
	zigzag := make([]point, 0, 30)
	for i := 0; i < 30; i++ {
		y := float32(60)
		if i%2 == 1 {
			y = 140
		}
		zigzag = append(zigzag, point{X: float32(i) * 13, Y: y})
	}
	shapes["zigzag"] = zigzag

	for name, pts := range shapes {
		t.Run(name, func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, 400, 200))
			strokePolyline(img, pts, 2, colorAccent)

			// Every column the line passes through must carry paint.
			first, last := int(pts[0].X)+1, int(pts[len(pts)-1].X)-1
			for x := first; x <= last; x++ {
				if len(paintedRows(img, x)) == 0 {
					t.Fatalf("column %d is empty; the stroke has a hole at a join", x)
				}
			}
		})
	}
}

// Dashing splits a polyline without moving it: every dash has to stay on the
// original line, or the ceiling would be drawn at the wrong height.
func TestDashPolylineStaysOnTheLine(t *testing.T) {
	line := []point{{X: 0, Y: 50}, {X: 100, Y: 50}}
	dashes := dashPolyline(line, 3, 3)
	if len(dashes) < 2 {
		t.Fatalf("dashPolyline produced %d dashes, want several", len(dashes))
	}
	for i, dash := range dashes {
		if len(dash) < 2 {
			t.Errorf("dash %d has %d points, want a segment", i, len(dash))
		}
		for _, p := range dash {
			if p.Y != 50 {
				t.Errorf("dash %d strayed to y=%.2f, want 50", i, p.Y)
			}
			if p.X < 0 || p.X > 100 {
				t.Errorf("dash %d ran to x=%.2f, outside the line", i, p.X)
			}
		}
	}
	// Degenerate inputs pass straight through rather than looping forever.
	if got := dashPolyline(line, 0, 3); len(got) != 1 {
		t.Errorf("dashPolyline with no dash length returned %d runs, want the line unchanged", len(got))
	}
	if got := dashPolyline([]point{{X: 1, Y: 1}}, 3, 3); len(got) != 1 {
		t.Errorf("dashPolyline of a single point returned %d runs, want it unchanged", len(got))
	}
}

func TestClamp01(t *testing.T) {
	cases := map[float64]float64{-1: 0, 0: 0, 0.5: 0.5, 1: 1, 2: 1}
	for in, want := range cases {
		if got := clamp01(in); got != want {
			t.Errorf("clamp01(%.1f) = %.1f, want %.1f", in, got, want)
		}
	}
}

// ---- helpers ----

func nearAnyRow(rows []int, want, tolerance int) bool {
	for _, r := range rows {
		if r >= want-tolerance && r <= want+tolerance {
			return true
		}
	}
	return false
}

func imagesEqual(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}
