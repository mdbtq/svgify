// Package testfixtures generates the synthetic raster images used across the
// test suite. Generating them in code keeps the repository free of binary
// blobs and makes each fixture's intent explicit.
package testfixtures

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// Fixture is a generated test image together with what it is meant to exercise.
type Fixture struct {
	Name string
	Img  *image.NRGBA
}

// PNG encodes the fixture.
func (f Fixture) PNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, f.Img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func newCanvas(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

var (
	white       = color.NRGBA{255, 255, 255, 255}
	black       = color.NRGBA{0, 0, 0, 255}
	transparent = color.NRGBA{0, 0, 0, 0}
)

// disc draws a filled circle with antialiased edges when aa is true.
func disc(img *image.NRGBA, cx, cy, r float64, c color.NRGBA, aa bool) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			cov := coverage(r-d, aa)
			if cov > 0 {
				blend(img, x, y, c, cov)
			}
		}
	}
}

// ring draws an annulus: the hole in the middle is what makes holes testable.
func ring(img *image.NRGBA, cx, cy, outer, inner float64, c color.NRGBA, aa bool) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			cov := math.Min(coverage(outer-d, aa), coverage(d-inner, aa))
			if cov > 0 {
				blend(img, x, y, c, cov)
			}
		}
	}
}

func rect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{c}, image.Point{}, draw.Src)
}

// coverage maps a signed distance (positive inside) to pixel coverage,
// producing a one-pixel antialiased ramp when aa is true.
func coverage(sd float64, aa bool) float64 {
	if !aa {
		if sd >= 0 {
			return 1
		}
		return 0
	}
	return math.Max(0, math.Min(1, sd+0.5))
}

func blend(img *image.NRGBA, x, y int, c color.NRGBA, a float64) {
	i := img.PixOffset(x, y)
	dst := color.NRGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	mix := func(s, d uint8) uint8 {
		return uint8(math.Round(float64(s)*a + float64(d)*(1-a)))
	}
	img.Pix[i] = mix(c.R, dst.R)
	img.Pix[i+1] = mix(c.G, dst.G)
	img.Pix[i+2] = mix(c.B, dst.B)
	img.Pix[i+3] = mix(c.A, dst.A)
}

// BlackOnWhite is a solid black disc on a white canvas: the baseline case.
func BlackOnWhite() Fixture {
	img := newCanvas(120, 120, white)
	disc(img, 60, 60, 40, black, false)
	return Fixture{"black-on-white", img}
}

// WhiteOnBlack inverts the baseline; automatic detection must still produce
// the same shape rather than tracing the canvas.
func WhiteOnBlack() Fixture {
	img := newCanvas(120, 120, black)
	disc(img, 60, 60, 40, white, false)
	return Fixture{"white-on-black", img}
}

// TransparentPNG has no background at all; the artwork is defined by alpha.
func TransparentPNG() Fixture {
	img := newCanvas(120, 120, transparent)
	disc(img, 60, 60, 40, black, false)
	return Fixture{"transparent", img}
}

// Whitespace places a small mark in the corner of a large canvas, so cropping
// has something substantial to remove.
func Whitespace() Fixture {
	img := newCanvas(400, 400, white)
	rect(img, 20, 30, 60, 90, black)
	return Fixture{"whitespace", img}
}

// Holes is a ring: a filled shape with an interior cutout.
func Holes() Fixture {
	img := newCanvas(160, 160, white)
	ring(img, 80, 80, 60, 30, black, false)
	return Fixture{"holes", img}
}

// Antialiased is the baseline disc with soft edges, which must not be
// destroyed into a jagged polygon.
func Antialiased() Fixture {
	img := newCanvas(120, 120, white)
	disc(img, 60, 60, 40, black, true)
	return Fixture{"antialiased", img}
}

// LowContrast uses mid greys close together, where a hardcoded threshold would
// fail but Otsu should not.
func LowContrast() Fixture {
	bg := color.NRGBA{170, 170, 170, 255}
	fg := color.NRGBA{110, 110, 110, 255}
	img := newCanvas(120, 120, bg)
	disc(img, 60, 60, 40, fg, false)
	return Fixture{"low-contrast", img}
}

// Logo approximates the real-world reference fixture: a headphone band over a
// face with two eye cutouts and a smile, on a transparent background. It
// exercises holes, smooth curves and alpha together.
//
// The shape is composed as a signed-distance field rather than by painting
// overlapping primitives, so the cutouts are exact and the edges stay cleanly
// antialiased.
func Logo() Fixture {
	img := newCanvas(200, 200, transparent)

	// Signed distances, positive inside the shape.
	circle := func(x, y, cx, cy, r float64) float64 {
		return r - math.Hypot(x-cx, y-cy)
	}
	annulus := func(x, y, cx, cy, outer, inner float64) float64 {
		d := math.Hypot(x-cx, y-cy)
		return math.Min(outer-d, d-inner)
	}

	shape := func(x, y float64) float64 {
		// Head plus the ear cups.
		s := circle(x, y, 100, 108, 56)
		s = math.Max(s, circle(x, y, 24, 100, 15))
		s = math.Max(s, circle(x, y, 176, 100, 15))
		// Headphone band: upper half of an annulus.
		band := math.Min(annulus(x, y, 100, 100, 80, 68), 100-y)
		s = math.Max(s, band)
		// Eyes: subtract.
		s = math.Min(s, -circle(x, y, 82, 98, 8))
		s = math.Min(s, -circle(x, y, 118, 98, 8))
		// Smile: subtract the lower half of a thin annulus.
		smile := math.Min(annulus(x, y, 100, 108, 34, 26), y-118)
		s = math.Min(s, -smile)
		return s
	}

	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if cov := coverage(shape(float64(x)+0.5, float64(y)+0.5), true); cov > 0 {
				blend(img, x, y, black, cov)
			}
		}
	}
	return Fixture{"logo", img}
}

// BlankWhite is an empty canvas, used to check that "nothing to trace" is
// reported as an error rather than producing an empty document.
func BlankWhite() *image.NRGBA { return newCanvas(60, 60, white) }

// All returns every fixture, in a stable order.
func All() []Fixture {
	return []Fixture{
		BlackOnWhite(), WhiteOnBlack(), TransparentPNG(), Whitespace(),
		Holes(), Antialiased(), LowContrast(), Logo(),
	}
}
