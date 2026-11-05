package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

var (
	blue  = color.NRGBA{0x1F, 0x5F, 0xA8, 0xFF}
	paper = color.NRGBA{0xEE, 0xF2, 0xF4, 0xFF}
	green = color.NRGBA{0x7F, 0xD3, 0xA0, 0xFF}
)

// geometry on a 100-unit canvas
type geom struct {
	tileR                 float64 // tile corner radius
	half, radius, stroke  float64 // frame half-size, outer radius, thickness
	gateX, gateW          float64 // gate in the top edge
	pillY, pillLen, pillH float64 // vessel
	endsW                 float64 // frame length lit when running
}

var (
	large = geom{tileR: 22, half: 31, radius: 15, stroke: 8, gateX: 65, gateW: 5, pillY: 52, pillLen: 33, pillH: 15, endsW: 9}
	// snapped to the 16px grid (6.25 units per pixel)
	small = geom{tileR: 21.875, half: 37.5, radius: 18.75, stroke: 12.5, gateX: 68.75, gateW: 12.5, pillY: 53.125, pillLen: 37.5, pillH: 18.75, endsW: 12.5}
)

// TrayIcon is the 32px tray icon as PNG.
func TrayIcon(running bool) []byte { return PNG(32, running) }

// PNG encodes Mark as PNG.
func PNG(n int, running bool) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, Mark(n, running))
	return buf.Bytes()
}

// Mark draws the app icon at n×n pixels.
func Mark(n int, running bool) *image.NRGBA {
	g := large
	if n < 48 {
		g = small
	}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	px := 100 / float64(n) // units per pixel
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			p := [2]float64{(float64(x) + 0.5) * px, (float64(y) + 0.5) * px}
			c := over(color.NRGBA{}, blue, cover(roundBox(p, 50, 50, 50, g.tileR), px))
			frame := g.frame(p)
			c = over(c, paper, cover(frame, px))
			if running {
				ends := math.Max(frame, box(p, g.gateX-g.endsW, 0, g.gateX+g.gateW+g.endsW, 50-g.half+g.stroke))
				c = over(c, green, cover(ends, px))
			}
			c = over(c, paper, cover(g.pill(p), px))
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func (g geom) frame(p [2]float64) float64 {
	ring := math.Max(roundBox(p, 50, 50, g.half, g.radius), -roundBox(p, 50, 50, g.half-g.stroke, math.Max(g.radius-g.stroke, 0)))
	gate := box(p, g.gateX, 0, g.gateX+g.gateW, 50-g.half+g.stroke)
	return math.Max(ring, -gate)
}

func (g geom) pill(p [2]float64) float64 {
	r := g.pillH / 2
	ax, bx := 50-g.pillLen/2+r, 50+g.pillLen/2-r
	dx := p[0] - math.Min(math.Max(p[0], ax), bx)
	return math.Hypot(dx, p[1]-g.pillY) - r
}

// signed distances: negative inside
func roundBox(p [2]float64, cx, cy, half, r float64) float64 {
	qx := math.Abs(p[0]-cx) - (half - r)
	qy := math.Abs(p[1]-cy) - (half - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

func box(p [2]float64, x0, y0, x1, y1 float64) float64 {
	qx := math.Abs(p[0]-(x0+x1)/2) - (x1-x0)/2
	qy := math.Abs(p[1]-(y0+y1)/2) - (y1-y0)/2
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0)
}

// cover turns a distance into pixel coverage.
func cover(d, px float64) float64 {
	return math.Min(math.Max(0.5-d/px, 0), 1)
}

func over(dst, src color.NRGBA, a float64) color.NRGBA {
	sa := a * float64(src.A) / 255
	da := float64(dst.A) / 255
	oa := sa + da*(1-sa)
	if oa == 0 {
		return color.NRGBA{}
	}
	mix := func(s, d uint8) uint8 {
		return uint8(math.Round((float64(s)*sa + float64(d)*da*(1-sa)) / oa))
	}
	return color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), uint8(math.Round(oa * 255))}
}
