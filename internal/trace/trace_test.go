package trace

import (
	"math"
	"testing"

	"github.com/mdbtq/trace/internal/preprocess"
)

func TestRunTracesASquare(t *testing.T) {
	bm := preprocess.NewBitmap(40, 40)
	for y := 10; y < 30; y++ {
		for x := 10; x < 30; x++ {
			bm.Set(x, y, true)
		}
	}

	paths, err := Run(bm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1", len(paths))
	}

	minX, minY, maxX, maxY := paths[0].Curves[0].Bounds()
	if math.Abs(minX-10) > 1 || math.Abs(minY-10) > 1 ||
		math.Abs(maxX-30) > 1 || math.Abs(maxY-30) > 1 {
		t.Errorf("bounds = (%g,%g)-(%g,%g), want ~(10,10)-(30,30)", minX, minY, maxX, maxY)
	}
}

// A shape with a hole must yield two contours in one path, so that even-odd
// filling renders the hole.
func TestRunPreservesHoles(t *testing.T) {
	bm := preprocess.NewBitmap(60, 60)
	for y := 10; y < 50; y++ {
		for x := 10; x < 50; x++ {
			bm.Set(x, y, true)
		}
	}
	for y := 25; y < 35; y++ {
		for x := 25; x < 35; x++ {
			bm.Set(x, y, false)
		}
	}

	paths, err := Run(bm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1", len(paths))
	}
	if n := len(paths[0].Curves); n != 2 {
		t.Errorf("got %d contours, want 2 (outer edge and hole)", n)
	}
}

func TestRunEmptyBitmap(t *testing.T) {
	paths, err := Run(preprocess.NewBitmap(20, 20), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("got %d paths from a blank bitmap, want 0", len(paths))
	}
}

// The bounds of a curve must follow the curve itself, not the control polygon,
// which is generally larger.
func TestCubicBoundsUseTrueExtrema(t *testing.T) {
	// A curve from (0,0) to (10,0) with control points pulled far up. The peak
	// of the curve is at y = 7.5, well below the controls' y = 10.
	c := Curve{
		Start: Point{0, 0},
		Segments: []Segment{{
			Kind: SegCubic,
			C1:   Point{0, 10}, C2: Point{10, 10}, To: Point{10, 0},
		}},
	}
	_, minY, _, maxY := c.Bounds()
	if math.Abs(minY) > 1e-9 {
		t.Errorf("minY = %g, want 0", minY)
	}
	if math.Abs(maxY-7.5) > 1e-6 {
		t.Errorf("maxY = %g, want 7.5 (the curve peak, not the control point at 10)", maxY)
	}
}

func TestCubicExtremaT(t *testing.T) {
	// Symmetric curve: the single extremum is at t = 0.5.
	got := cubicExtremaT(0, 10, 10, 0)
	if len(got) != 1 || math.Abs(got[0]-0.5) > 1e-9 {
		t.Errorf("cubicExtremaT = %v, want [0.5]", got)
	}
	// Monotonic curve: no interior extremum.
	if got := cubicExtremaT(0, 1, 2, 3); len(got) != 0 {
		t.Errorf("cubicExtremaT on a monotonic curve = %v, want none", got)
	}
}

// Simplify must actually affect the number of segments produced.
func TestSimplifyReducesDetail(t *testing.T) {
	bm := preprocess.NewBitmap(80, 80)
	// A rough disc gives the optimizer something to simplify.
	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			if math.Hypot(float64(x)-40, float64(y)-40) < 30 {
				bm.Set(x, y, true)
			}
		}
	}

	count := func(o Options) int {
		paths, err := Run(bm, o)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, p := range paths {
			for _, c := range p.Curves {
				n += len(c.Segments)
			}
		}
		return n
	}

	detailed := count(Options{Simplify: 0.05})
	smoothed := count(Options{Simplify: 20})
	if smoothed > detailed {
		t.Errorf("simplify 20 produced %d segments but simplify 0.05 produced %d; "+
			"higher simplification should not add detail", smoothed, detailed)
	}
}
