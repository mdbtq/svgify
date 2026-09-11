package preprocess

import (
	"bytes"
	"testing"

	"github.com/mdbtq/svgify/internal/raster"
	"github.com/mdbtq/svgify/internal/testfixtures"
)

func decode(t *testing.T, f testfixtures.Fixture) *raster.Image {
	t.Helper()
	img, err := raster.Decode(bytes.NewReader(f.PNG()), f.Name+".png")
	if err != nil {
		t.Fatalf("decode %s: %v", f.Name, err)
	}
	return img
}

// The artwork in each fixture covers a predictable fraction of the canvas, so
// automatic detection can be checked without pixel-exact comparison: it must
// select the shape, never the background.
func TestAutomaticForegroundDetection(t *testing.T) {
	tests := []struct {
		fixture        testfixtures.Fixture
		minCov, maxCov float64
	}{
		// A disc of r=40 in 120x120 covers pi*40^2/14400 = 35%.
		{testfixtures.BlackOnWhite(), 0.30, 0.40},
		{testfixtures.WhiteOnBlack(), 0.30, 0.40},
		{testfixtures.TransparentPNG(), 0.30, 0.40},
		{testfixtures.Antialiased(), 0.30, 0.40},
		{testfixtures.LowContrast(), 0.30, 0.40},
		// A ring of r=60/30 in 160x160 covers pi*(60^2-30^2)/25600 = 33%.
		{testfixtures.Holes(), 0.28, 0.38},
		// A 40x60 mark on a 400x400 canvas is 1.5%.
		{testfixtures.Whitespace(), 0.01, 0.02},
	}

	for _, tt := range tests {
		t.Run(tt.fixture.Name, func(t *testing.T) {
			res := Run(decode(t, tt.fixture), Options{})
			cov := float64(res.Bitmap.Count()) / float64(res.Bitmap.W*res.Bitmap.H)
			if cov < tt.minCov || cov > tt.maxCov {
				t.Errorf("foreground coverage = %.3f, want within [%.2f, %.2f]; "+
					"the background was probably traced instead of the artwork",
					cov, tt.minCov, tt.maxCov)
			}
		})
	}
}

// White-on-black must be recognized as inverted, and black-on-white must not.
func TestInversionDetection(t *testing.T) {
	if res := Run(decode(t, testfixtures.BlackOnWhite()), Options{}); res.Inverted {
		t.Error("black on white was reported as inverted")
	}
	if res := Run(decode(t, testfixtures.WhiteOnBlack()), Options{}); !res.Inverted {
		t.Error("white on black was not detected as inverted")
	}
}

// --invert must produce the complement of the automatic result.
func TestExplicitInvert(t *testing.T) {
	img := decode(t, testfixtures.BlackOnWhite())
	normal := Run(img, Options{})
	inverted := Run(img, Options{Invert: true})

	if normal.Bitmap.Count() == inverted.Bitmap.Count() {
		t.Fatal("--invert did not change the foreground")
	}
	// Speckle removal cleans up both, so allow a small discrepancy rather than
	// requiring an exact complement.
	total := normal.Bitmap.W * normal.Bitmap.H
	sum := normal.Bitmap.Count() + inverted.Bitmap.Count()
	if diff := abs(sum - total); diff > total/50 {
		t.Errorf("inverted + normal = %d, want ~%d (differ by %d)", sum, total, diff)
	}
}

// An explicit threshold must override Otsu and actually change the result.
func TestThresholdOverride(t *testing.T) {
	img := decode(t, testfixtures.LowContrast())

	auto := Run(img, Options{})
	if auto.Threshold < 110 || auto.Threshold > 170 {
		t.Errorf("Otsu threshold = %d, want between the two grey levels (110, 170)", auto.Threshold)
	}

	// Below both grey levels: nothing is dark enough to be foreground.
	low := 50
	if got := Run(img, Options{Threshold: &low}).Bitmap.Count(); got != 0 {
		t.Errorf("threshold 50 produced %d foreground pixels, want 0", got)
	}

	// Above both: everything is foreground.
	high := 250
	res := Run(img, Options{Threshold: &high})
	if cov := float64(res.Bitmap.Count()) / float64(res.Bitmap.W*res.Bitmap.H); cov < 0.99 {
		t.Errorf("threshold 250 covered %.3f, want ~1.0", cov)
	}
	if res.Threshold != high {
		t.Errorf("reported threshold = %d, want %d", res.Threshold, high)
	}
}

// A transparent PNG must be separated by alpha, not by luminance: its black
// artwork on a "transparent black" canvas is indistinguishable by luma alone.
func TestTransparencyUsesAlpha(t *testing.T) {
	res := Run(decode(t, testfixtures.TransparentPNG()), Options{})
	if !res.UsedAlpha {
		t.Error("transparent PNG was not separated by alpha")
	}
}

func TestOpaqueImageDoesNotUseAlpha(t *testing.T) {
	res := Run(decode(t, testfixtures.BlackOnWhite()), Options{})
	if res.UsedAlpha {
		t.Error("fully opaque image was separated by alpha")
	}
}

// Otsu must land between the two modes of a bimodal histogram.
func TestOtsuSeparatesModes(t *testing.T) {
	var hist [256]int
	hist[40] = 1000
	hist[200] = 1000
	if got := otsu(hist); got <= 40 || got > 200 {
		t.Errorf("otsu = %d, want between 40 and 200", got)
	}
}

// Speckle removal must delete isolated dots but keep real artwork.
func TestNoiseRemoval(t *testing.T) {
	bm := NewBitmap(50, 50)
	for y := 10; y < 30; y++ {
		for x := 10; x < 30; x++ {
			bm.Set(x, y, true)
		}
	}
	bm.Set(45, 45, true) // a lone speckle
	removeSpeckles(bm, 10)

	if bm.At(45, 45) {
		t.Error("isolated speckle survived noise removal")
	}
	if !bm.At(20, 20) {
		t.Error("noise removal deleted the artwork")
	}
}

// The canvas surrounding the artwork must never be mistaken for a hole.
func TestBorderBackgroundIsNotFilled(t *testing.T) {
	bm := NewBitmap(50, 50)
	for y := 10; y < 40; y++ {
		for x := 10; x < 40; x++ {
			bm.Set(x, y, true)
		}
	}
	removeSpeckles(bm, 10000)
	if bm.At(0, 0) {
		t.Error("the surrounding canvas was filled in as a hole")
	}
}

func TestBoundsIsTight(t *testing.T) {
	bm := NewBitmap(100, 100)
	bm.Set(10, 20, true)
	bm.Set(60, 70, true)
	got := bm.Bounds()
	if got.Min.X != 10 || got.Min.Y != 20 || got.Max.X != 61 || got.Max.Y != 71 {
		t.Errorf("Bounds() = %v, want (10,20)-(61,71)", got)
	}
}

func TestBoundsEmpty(t *testing.T) {
	if got := NewBitmap(10, 10).Bounds(); !got.Empty() {
		t.Errorf("Bounds() of blank bitmap = %v, want empty", got)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
