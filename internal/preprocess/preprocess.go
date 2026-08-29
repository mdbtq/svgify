// Package preprocess turns a decoded raster image into a binary bitmap
// suitable for tracing, deciding foreground/background automatically.
package preprocess

import (
	"image"

	"github.com/mdbtq/trace/internal/raster"
)

// Bitmap is a binary image where true means foreground (ink).
type Bitmap struct {
	W, H int
	Bits []bool
}

// NewBitmap allocates a blank bitmap.
func NewBitmap(w, h int) *Bitmap {
	return &Bitmap{W: w, H: h, Bits: make([]bool, w*h)}
}

// At reports whether (x, y) is foreground. Out-of-bounds is background, which
// makes the tracer's neighbourhood tests safe at the edges.
func (b *Bitmap) At(x, y int) bool {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return false
	}
	return b.Bits[y*b.W+x]
}

// Set marks (x, y) as foreground or background.
func (b *Bitmap) Set(x, y int, v bool) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return
	}
	b.Bits[y*b.W+x] = v
}

// Count returns the number of foreground pixels.
func (b *Bitmap) Count() int {
	n := 0
	for _, v := range b.Bits {
		if v {
			n++
		}
	}
	return n
}

// Options controls the preprocessing stage. The zero value means "decide
// everything automatically", which is the intended default.
type Options struct {
	// Threshold, when non-nil, overrides automatic thresholding. Values are
	// 0-255 on the intensity scale; pixels darker than this become foreground.
	Threshold *int
	// Invert flips the foreground/background decision after it is made.
	Invert bool
	// NoiseRemoval, when > 0, discards connected components smaller than this
	// many pixels. Negative disables removal entirely.
	NoiseRemoval int
}

// Result carries the binary bitmap plus what the automatic stages decided,
// so the CLI can report it and tests can assert on it.
type Result struct {
	Bitmap *Bitmap
	// Threshold actually used on the intensity scale, or -1 when the image was
	// separated by alpha rather than by luminance.
	Threshold int
	// UsedAlpha reports that transparency, not luminance, defined the artwork.
	UsedAlpha bool
	// Inverted reports that the automatic pass decided the artwork was light
	// on a dark background (before any explicit --invert).
	Inverted bool
}

// defaultNoiseRemoval scales with image size: components below this fraction of
// the total pixel count are speckles rather than artwork.
const defaultNoiseFraction = 1.0 / 20000.0

// Run converts img to a binary bitmap.
func Run(img *raster.Image, opt Options) *Result {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	res := &Result{Threshold: -1}

	gray, alpha := decompose(img, w, h)

	// A meaningfully transparent image defines its artwork by coverage, not by
	// colour: the visible pixels are the artwork whatever colour they are.
	if significantAlpha(alpha) {
		res.UsedAlpha = true
		res.Bitmap = thresholdChannel(alpha, w, h, 128, false)
	} else {
		if opt.Threshold != nil {
			// An explicit threshold is taken literally: pixels darker than it
			// are foreground. Auto-inversion would silently contradict the
			// value the user asked for, so it only applies to the automatic
			// path below.
			res.Threshold = clamp(*opt.Threshold, 0, 255)
			res.Bitmap = thresholdChannel(gray, w, h, res.Threshold, true)
		} else {
			res.Threshold = otsu(histogram(gray))
			// Foreground is the darker side by default; flip when the border
			// says the background is the dark one.
			res.Inverted = backgroundIsDark(gray, w, h, res.Threshold)
			res.Bitmap = thresholdChannel(gray, w, h, res.Threshold, !res.Inverted)
		}
	}

	if opt.Invert {
		for i := range res.Bitmap.Bits {
			res.Bitmap.Bits[i] = !res.Bitmap.Bits[i]
		}
		res.Inverted = !res.Inverted
	}

	minArea := opt.NoiseRemoval
	if minArea == 0 {
		minArea = int(float64(w*h) * defaultNoiseFraction)
		if minArea < 2 {
			minArea = 2
		}
	}
	if minArea > 0 {
		removeSpeckles(res.Bitmap, minArea)
	}

	return res
}

// decompose splits the image into luminance and alpha planes. Luminance is
// computed over the image composited onto its own dominant background so that
// transparent regions do not skew the histogram toward black.
func decompose(img *raster.Image, w, h int) (gray, alpha []uint8) {
	gray = make([]uint8, w*h)
	alpha = make([]uint8, w*h)
	for i, p := 0, 0; i < w*h; i, p = i+1, p+4 {
		r, g, b, a := img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3]
		alpha[i] = a
		// Rec. 601 luma, the conventional perceptual weighting.
		y := (299*int(r) + 587*int(g) + 114*int(b)) / 1000
		// Composite onto white so that transparent areas read as background
		// for the luminance path.
		if a != 0xff {
			y = (y*int(a) + 255*(255-int(a))) / 255
		}
		gray[i] = uint8(y)
	}
	return gray, alpha
}

// significantAlpha reports whether transparency carries the shape. A handful of
// soft pixels on an otherwise opaque image is antialiasing, not a cutout.
func significantAlpha(alpha []uint8) bool {
	transparent := 0
	for _, a := range alpha {
		if a < 128 {
			transparent++
		}
	}
	// At least 5% of the canvas must be see-through before alpha is treated as
	// the defining channel.
	return transparent*20 > len(alpha)
}

func thresholdChannel(ch []uint8, w, h int, t int, below bool) *Bitmap {
	bm := NewBitmap(w, h)
	for i, v := range ch {
		on := int(v) >= t
		if below {
			on = int(v) < t
		}
		bm.Bits[i] = on
	}
	return bm
}

func histogram(gray []uint8) [256]int {
	var hist [256]int
	for _, v := range gray {
		hist[v]++
	}
	return hist
}

// otsu finds the intensity threshold maximizing between-class variance.
// The returned value is the first level of the upper class: pixels with an
// intensity below it belong to the darker class.
func otsu(hist [256]int) int {
	total := 0
	sum := 0.0
	for i, c := range hist {
		total += c
		sum += float64(i) * float64(c)
	}
	if total == 0 {
		return 128
	}

	var (
		wB, sumB float64
		best     = -1.0
		// Track the plateau of equally good thresholds and take its midpoint,
		// which is the conventional tie-break and keeps results stable.
		lo, hi int
	)
	for t := 0; t < 256; t++ {
		wB += float64(hist[t])
		if wB == 0 {
			continue
		}
		wF := float64(total) - wB
		if wF == 0 {
			break
		}
		sumB += float64(t) * float64(hist[t])
		mB := sumB / wB
		mF := (sum - sumB) / wF
		between := wB * wF * (mB - mF) * (mB - mF)

		switch {
		case between > best:
			best, lo, hi = between, t, t
		case between == best:
			hi = t
		}
	}
	if best < 0 {
		return 128
	}
	// t is the last level of the lower class, so the cutoff sits just above it.
	return (lo+hi)/2 + 1
}

// backgroundIsDark inspects the border ring to decide which side of the
// threshold is background. Logos sit inside their canvas, so the outermost
// pixels are background in practice.
func backgroundIsDark(gray []uint8, w, h, t int) bool {
	var dark, light int
	sample := func(x, y int) {
		if int(gray[y*w+x]) < t {
			dark++
		} else {
			light++
		}
	}
	for x := 0; x < w; x++ {
		sample(x, 0)
		sample(x, h-1)
	}
	for y := 1; y < h-1; y++ {
		sample(0, y)
		sample(w-1, y)
	}
	return dark > light
}

// removeSpeckles clears connected foreground components smaller than minArea,
// and fills background components (holes) smaller than minArea. Both are
// scanning artefacts rather than artwork.
func removeSpeckles(bm *Bitmap, minArea int) {
	clearSmallComponents(bm, minArea, true)
	clearSmallComponents(bm, minArea, false)
}

// clearSmallComponents flips components of the given value that are smaller
// than minArea. Background components touching the border are never filled:
// that is the canvas, not a hole.
func clearSmallComponents(bm *Bitmap, minArea int, value bool) {
	seen := make([]bool, len(bm.Bits))
	stack := make([]int, 0, 64)
	comp := make([]int, 0, 64)

	for start := range bm.Bits {
		if seen[start] || bm.Bits[start] != value {
			continue
		}
		comp = comp[:0]
		stack = append(stack[:0], start)
		seen[start] = true
		touchesBorder := false

		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, i)
			x, y := i%bm.W, i/bm.W
			if x == 0 || y == 0 || x == bm.W-1 || y == bm.H-1 {
				touchesBorder = true
			}
			// 4-connectivity, matching the tracer's edge-following topology.
			if x > 0 && !seen[i-1] && bm.Bits[i-1] == value {
				seen[i-1] = true
				stack = append(stack, i-1)
			}
			if x < bm.W-1 && !seen[i+1] && bm.Bits[i+1] == value {
				seen[i+1] = true
				stack = append(stack, i+1)
			}
			if y > 0 && !seen[i-bm.W] && bm.Bits[i-bm.W] == value {
				seen[i-bm.W] = true
				stack = append(stack, i-bm.W)
			}
			if y < bm.H-1 && !seen[i+bm.W] && bm.Bits[i+bm.W] == value {
				seen[i+bm.W] = true
				stack = append(stack, i+bm.W)
			}
		}

		if len(comp) >= minArea || (!value && touchesBorder) {
			continue
		}
		for _, i := range comp {
			bm.Bits[i] = !value
		}
	}
}

// Bounds returns the tight bounding box of the foreground, or the empty
// rectangle when the bitmap has no foreground at all.
func (b *Bitmap) Bounds() image.Rectangle {
	minX, minY, maxX, maxY := b.W, b.H, -1, -1
	for y := 0; y < b.H; y++ {
		row := y * b.W
		for x := 0; x < b.W; x++ {
			if !b.Bits[row+x] {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < 0 {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
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
