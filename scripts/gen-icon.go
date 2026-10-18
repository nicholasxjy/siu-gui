//go:build ignore

// Regenerates resources/icon.png: a flat iris-blue square with a white
// flowing "S", rasterized with a tapered stroke and rounded caps. The
// geometry matches resources/icon.svg (two half-circle arcs of radius 135
// in a 0-1000 space, sharing the center point, bulging left on top and
// right on the bottom), just with the taper and anti-aliasing a plain SVG
// stroke does not have.
//
// Run with: go run scripts/gen-icon.go
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const (
	canvasSize = 1024.0
	outPath    = "resources/icon.png"
)

// Geometry in the same 0-1000 design space as resources/icon.svg, then
// scaled to canvasSize.
const (
	designSize    = 1000.0
	xMidDesign    = 500.0
	yTopDesign    = 230.0
	yMidDesign    = 500.0
	yBottomDesign = 770.0
	radiusDesign  = 135.0

	// Stroke half-widths, in design units: full width at the body of the
	// S, tapering to a thinner rounded tip over the first/last fraction
	// (tipFraction) of the curve's length.
	baseHalfWidthDesign = 58.0
	tipHalfWidthDesign  = 56.0
	tipFraction         = 0.06
	// antiAliasDesign is the width of the soft edge between the S and
	// the background, in design units.
	antiAliasDesign = 3.0
)

func main() {
	scale := canvasSize / designSize
	xMid := xMidDesign * scale
	yTop := yTopDesign * scale
	yMid := yMidDesign * scale
	yBottom := yBottomDesign * scale
	radius := radiusDesign * scale
	baseHalfWidth := baseHalfWidthDesign * scale
	tipHalfWidth := tipHalfWidthDesign * scale
	antiAlias := antiAliasDesign * scale

	bg := color.NRGBA{R: 0x4b, G: 0x4b, B: 0xc2, A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	size := int(canvasSize)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for py := 0; py < size; py++ {
		y := float64(py) + 0.5
		for px := 0; px < size; px++ {
			x := float64(px) + 0.5
			alpha := sCoverage(x, y, xMid, yTop, yMid, yBottom, radius, baseHalfWidth, tipHalfWidth, antiAlias)
			img.SetNRGBA(px, py, blend(bg, white, alpha))
		}
	}

	if err := os.MkdirAll("resources", 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%dx%d)", outPath, size, size)
}

// sCoverage returns how much of the pixel at (x,y) the white S covers, in
// [0,1]: 1 fully inside the stroke, 0 fully in the background, with a
// smooth edge of width antiAlias in between.
func sCoverage(x, y, xMid, yTop, yMid, yBottom, radius, baseHalfWidth, tipHalfWidth, antiAlias float64) float64 {
	totalHeight := yBottom - yTop

	if y < yTop {
		// Rounded cap above the top tip.
		dist := math.Hypot(x-xMid, y-yTop)
		return edgeCoverage(dist, tipHalfWidth, antiAlias)
	}
	if y > yBottom {
		// Rounded cap below the bottom tip.
		dist := math.Hypot(x-xMid, y-yBottom)
		return edgeCoverage(dist, tipHalfWidth, antiAlias)
	}

	var centerX float64
	if y <= yMid {
		// Top half-circle: center directly below yTop by one quarter of
		// the S's height, bulging left (the center point of this arc's
		// circle sits to the right of the leftmost point it traces).
		cy := (yTop + yMid) / 2
		r := (yMid - yTop) / 2
		dy := y - cy
		inside := math.Max(0, r*r-dy*dy)
		centerX = xMid - math.Sqrt(inside)
	} else {
		cy := (yMid + yBottom) / 2
		r := (yBottom - yMid) / 2
		dy := y - cy
		inside := math.Max(0, r*r-dy*dy)
		centerX = xMid + math.Sqrt(inside)
	}
	_ = radius // kept for documentation: both arcs above use this same radius

	t := (y - yTop) / totalHeight // 0 at the top tip, 1 at the bottom tip
	halfWidth := taperedHalfWidth(t, baseHalfWidth, tipHalfWidth, tipFraction)

	dist := math.Abs(x - centerX)
	return edgeCoverage(dist, halfWidth, antiAlias)
}

// taperedHalfWidth narrows the stroke to tipHalfWidth over the first and
// last tipFraction of the curve (by vertical position t in [0,1]),
// keeping it at baseHalfWidth over the body in between.
func taperedHalfWidth(t, baseHalfWidth, tipHalfWidth, tipFraction float64) float64 {
	switch {
	case t < tipFraction:
		return lerp(tipHalfWidth, baseHalfWidth, smoothstep(t/tipFraction))
	case t > 1-tipFraction:
		return lerp(tipHalfWidth, baseHalfWidth, smoothstep((1-t)/tipFraction))
	default:
		return baseHalfWidth
	}
}

// edgeCoverage turns a signed distance from a stroke's centerline (or a
// cap's center) into the [0,1] coverage of a stroke half-width
// halfWidth, with a soft edge of width aa.
func edgeCoverage(dist, halfWidth, aa float64) float64 {
	return 1 - smoothstep(clamp((dist-(halfWidth-aa/2))/aa, 0, 1))
}

func smoothstep(f float64) float64 {
	f = clamp(f, 0, 1)
	return f * f * (3 - 2*f)
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func lerp(a, b, f float64) float64 { return a + (b-a)*f }

func blend(bg, fg color.NRGBA, alpha float64) color.NRGBA {
	blendCh := func(a, b uint8) uint8 {
		return uint8(float64(a)*(1-alpha) + float64(b)*alpha)
	}
	return color.NRGBA{
		R: blendCh(bg.R, fg.R),
		G: blendCh(bg.G, fg.G),
		B: blendCh(bg.B, fg.B),
		A: 0xff,
	}
}
