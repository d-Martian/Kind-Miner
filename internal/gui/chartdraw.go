package gui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// Low-level drawing helpers for the dashboard chart.
//
// Fyne 2.5.3 ships no chart widget and no path API, so the plot is rasterised
// into an image.RGBA and handed to a canvas.Raster. x/image/vector fills paths
// with anti-aliasing; strokes are built here by turning each segment into a
// quad, which is all a line chart needs and avoids pulling in a full 2D
// graphics library for four polylines.

// point is a position in raster pixels.
type point struct{ X, Y float32 }

// fillPath rasterises a closed polygon in col.
func fillPath(dst *image.RGBA, pts []point, col color.Color) {
	if len(pts) < 3 {
		return
	}
	b := dst.Bounds()
	r := vector.NewRasterizer(b.Dx(), b.Dy())
	r.MoveTo(pts[0].X, pts[0].Y)
	for _, p := range pts[1:] {
		r.LineTo(p.X, p.Y)
	}
	r.ClosePath()
	r.Draw(dst, b, image.NewUniform(col), image.Point{})
}

// fillPathImage is fillPath with an arbitrary source image, used for the
// gradient under the miner line.
func fillPathImage(dst *image.RGBA, pts []point, src image.Image) {
	if len(pts) < 3 {
		return
	}
	b := dst.Bounds()
	r := vector.NewRasterizer(b.Dx(), b.Dy())
	r.MoveTo(pts[0].X, pts[0].Y)
	for _, p := range pts[1:] {
		r.LineTo(p.X, p.Y)
	}
	r.ClosePath()
	r.Draw(dst, b, src, image.Point{})
}

// strokePolyline draws a polyline of the given width.
//
// Each segment becomes a quad offset along the segment normal, and each
// interior vertex gets a small square so the quads meet without a notch. At the
// 1–2px widths this chart uses, that is indistinguishable from a proper round
// join and costs a fraction of the code.
//
// Every subpath must wind the same way. x/image/vector sums signed coverage
// across subpaths, so a square wound against the quads it overlaps subtracts
// itself from them and punches a hole at every vertex — which looks exactly
// like a dashed line and is far from obvious from the code.
func strokePolyline(dst *image.RGBA, pts []point, width float32, col color.Color) {
	if len(pts) < 2 || width <= 0 {
		return
	}
	half := width / 2
	b := dst.Bounds()
	r := vector.NewRasterizer(b.Dx(), b.Dy())

	for i := 0; i+1 < len(pts); i++ {
		a, c := pts[i], pts[i+1]
		dx, dy := c.X-a.X, c.Y-a.Y
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length == 0 {
			continue
		}
		// Unit normal to the segment. Offsetting to +n along the segment and
		// back along -n always winds the same way, whichever way the segment
		// points, so the quads never cancel each other either.
		nx, ny := -dy/length*half, dx/length*half
		r.MoveTo(a.X+nx, a.Y+ny)
		r.LineTo(c.X+nx, c.Y+ny)
		r.LineTo(c.X-nx, c.Y-ny)
		r.LineTo(a.X-nx, a.Y-ny)
		r.ClosePath()
	}
	// Joins, wound to match the quads above: down the left edge, across the
	// bottom, up the right.
	for _, p := range pts[1 : len(pts)-1] {
		r.MoveTo(p.X-half, p.Y+half)
		r.LineTo(p.X+half, p.Y+half)
		r.LineTo(p.X+half, p.Y-half)
		r.LineTo(p.X-half, p.Y-half)
		r.ClosePath()
	}
	r.Draw(dst, b, image.NewUniform(col), image.Point{})
}

// dashPolyline splits a polyline into dashed segments of on/off pixel lengths,
// so the caller can stroke the result.
func dashPolyline(pts []point, on, off float32) [][]point {
	if len(pts) < 2 || on <= 0 || off <= 0 {
		return [][]point{pts}
	}
	var out [][]point
	var current []point
	drawing := true
	remaining := on

	current = append(current, pts[0])
	for i := 0; i+1 < len(pts); i++ {
		a, c := pts[i], pts[i+1]
		segLen := float32(math.Hypot(float64(c.X-a.X), float64(c.Y-a.Y)))
		pos := float32(0)
		for segLen-pos > remaining {
			pos += remaining
			t := pos / segLen
			mid := point{X: a.X + (c.X-a.X)*t, Y: a.Y + (c.Y-a.Y)*t}
			if drawing {
				current = append(current, mid)
				out = append(out, current)
				current = nil
			} else {
				current = []point{mid}
			}
			drawing = !drawing
			if drawing {
				remaining = on
			} else {
				remaining = off
			}
		}
		remaining -= segLen - pos
		if drawing {
			current = append(current, c)
		}
	}
	if drawing && len(current) > 1 {
		out = append(out, current)
	}
	return out
}

// fillRect paints an axis-aligned rectangle, used for the headroom band and the
// backoff ticks. Rectangles are common enough here to be worth not routing
// through the rasteriser.
func fillRect(dst *image.RGBA, x0, y0, x1, y1 float32, col color.Color) {
	r := image.Rect(int(x0), int(y0), int(math.Ceil(float64(x1))), int(math.Ceil(float64(y1))))
	r = r.Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(dst, r, image.NewUniform(col), image.Point{}, draw.Over)
}

// verticalFade is a source image whose alpha ramps from full at top to nothing
// at the bottom. Used through a fill mask it turns the area under the miner
// line into a gradient without a second rasterising pass.
type verticalFade struct {
	col    color.NRGBA
	top    float32
	height float32
	// peak is the alpha at the top of the ramp, 0–255.
	peak float64
}

func (v verticalFade) ColorModel() color.Model { return color.NRGBAModel }
func (v verticalFade) Bounds() image.Rectangle {
	return image.Rect(-1e9, -1e9, 1e9, 1e9)
}

func (v verticalFade) At(_, y int) color.Color {
	if v.height <= 0 {
		return color.NRGBA{}
	}
	t := (float32(y) - v.top) / v.height
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	a := v.peak * float64(1-t)
	return color.NRGBA{R: v.col.R, G: v.col.G, B: v.col.B, A: uint8(a)}
}

// withAlpha returns col at the given opacity, for the many faint washes the
// chart layers.
func withAlpha(col color.NRGBA, a uint8) color.NRGBA {
	col.A = a
	return col
}
