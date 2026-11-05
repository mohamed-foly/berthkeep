package shell

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// TrayIcon draws the 32px app mark as PNG.
func TrayIcon(running bool) []byte {
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	blue := color.RGBA{0x1F, 0x5F, 0xA8, 0xFF}
	paper := color.RGBA{0xEE, 0xF2, 0xF4, 0xFF}
	light := paper
	if running {
		light = color.RGBA{0x7F, 0xD3, 0xA0, 0xFF}
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if inRounded(x, y, 2, 2, 28, 28, 7) {
				img.Set(x, y, blue)
			}
		}
	}
	fill := func(x0, y0, w, h int, c color.RGBA) {
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				img.Set(x, y, c)
			}
		}
	}
	fill(10, 9, 4, 10, paper)
	fill(18, 9, 4, 10, light)
	fill(8, 21, 16, 2, paper)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func inRounded(x, y, x0, y0, w, h, r int) bool {
	if x < x0 || y < y0 || x >= x0+w || y >= y0+h {
		return false
	}
	cx := clamp(x, x0+r, x0+w-1-r)
	cy := clamp(y, y0+r, y0+h-1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
