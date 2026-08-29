package svg

import (
	"strings"
	"testing"

	"github.com/mdbtq/trace/internal/trace"
)

// square returns a unit-square contour at the given offset.
func square(x, y, size float64) trace.Path {
	return trace.Path{Curves: []trace.Curve{{
		Start: trace.Point{X: x, Y: y},
		Segments: []trace.Segment{
			{Kind: trace.SegLine, To: trace.Point{X: x + size, Y: y}},
			{Kind: trace.SegLine, To: trace.Point{X: x + size, Y: y + size}},
			{Kind: trace.SegLine, To: trace.Point{X: x, Y: y + size}},
			{Kind: trace.SegLine, To: trace.Point{X: x, Y: y}},
		},
	}}}
}

func TestViewBoxIsTight(t *testing.T) {
	got := Render([]trace.Path{square(10, 20, 30)}, Options{Crop: true, CanvasW: 500, CanvasH: 500})
	if !strings.Contains(got, `viewBox="10 20 30 30"`) {
		t.Errorf("viewBox is not tight around the artwork: %s", got)
	}
}

func TestPaddingExpandsViewBox(t *testing.T) {
	got := Render([]trace.Path{square(10, 20, 30)}, Options{Crop: true, Padding: 5})
	if !strings.Contains(got, `viewBox="5 15 40 40"`) {
		t.Errorf("padding not applied: %s", got)
	}
}

func TestNoCropUsesCanvas(t *testing.T) {
	got := Render([]trace.Path{square(10, 20, 30)}, Options{Crop: false, CanvasW: 500, CanvasH: 400})
	if !strings.Contains(got, `viewBox="0 0 500 400"`) {
		t.Errorf("--no-crop did not use the full canvas: %s", got)
	}
}

// A responsive SVG carries a viewBox and no fixed pixel dimensions.
func TestNoFixedDimensions(t *testing.T) {
	got := Render([]trace.Path{square(0, 0, 10)}, Options{Crop: true})
	for _, attr := range []string{"width=", "height="} {
		if strings.Contains(got, attr) {
			t.Errorf("output contains %q, which prevents responsive scaling: %s", attr, got)
		}
	}
}

// Holes depend on even-odd filling; without it nested contours render solid.
func TestEvenOddFillRule(t *testing.T) {
	got := Render([]trace.Path{square(0, 0, 10)}, Options{Crop: true})
	if !strings.Contains(got, `fill-rule="evenodd"`) {
		t.Errorf("missing even-odd fill rule, holes would not render: %s", got)
	}
}

// Flat output: no groups and no transforms.
func TestNoGroupsOrTransforms(t *testing.T) {
	got := Render([]trace.Path{square(0, 0, 10), square(20, 20, 10)}, Options{Crop: true})
	for _, s := range []string{"<g", "transform="} {
		if strings.Contains(got, s) {
			t.Errorf("output contains %q: %s", s, got)
		}
	}
}

// Paths sharing a fill collapse into one element; distinct fills do not.
func TestGroupsByFill(t *testing.T) {
	mono := Render([]trace.Path{square(0, 0, 10), square(20, 20, 10)}, Options{Crop: true})
	if n := strings.Count(mono, "<path"); n != 1 {
		t.Errorf("monochrome output has %d path elements, want 1", n)
	}

	a, b := square(0, 0, 10), square(20, 20, 10)
	a.Fill, b.Fill = "#f00", "#00f"
	color := Render([]trace.Path{a, b}, Options{Crop: true})
	if n := strings.Count(color, "<path"); n != 2 {
		t.Errorf("two-colour output has %d path elements, want 2", n)
	}
}

func TestPrecision(t *testing.T) {
	p := trace.Path{Curves: []trace.Curve{{
		Start: trace.Point{X: 1.23456, Y: 2.34567},
		Segments: []trace.Segment{
			{Kind: trace.SegLine, To: trace.Point{X: 9.87654, Y: 2.34567}},
			{Kind: trace.SegLine, To: trace.Point{X: 1.23456, Y: 2.34567}},
		},
	}}}
	got := Render([]trace.Path{p}, Options{Crop: true, Precision: 1})
	if strings.Contains(got, "1.23456") {
		t.Errorf("precision was not applied: %s", got)
	}
}

func TestBackgroundRect(t *testing.T) {
	got := Render([]trace.Path{square(0, 0, 10)}, Options{Crop: true, Background: "#fff"})
	if !strings.Contains(got, `<rect`) || !strings.Contains(got, `fill="#fff"`) {
		t.Errorf("background not rendered: %s", got)
	}
	// Transparent by default.
	if plain := Render([]trace.Path{square(0, 0, 10)}, Options{Crop: true}); strings.Contains(plain, "<rect") {
		t.Errorf("a background was drawn without being asked for: %s", plain)
	}
}

func TestNumFormatting(t *testing.T) {
	tests := []struct {
		in   float64
		prec int
		want string
	}{
		{0, 2, "0"},
		{1, 2, "1"},
		{1.5, 2, "1.5"},
		{0.5, 2, ".5"},
		{-0.5, 2, "-.5"},
		{1.005, 2, "1"}, // rounds to 1.00, then trims
		{-0.001, 2, "0"},
		{12.34, 1, "12.3"},
	}
	for _, tt := range tests {
		if got := num(tt.in, tt.prec); got != tt.want {
			t.Errorf("num(%v, %d) = %q, want %q", tt.in, tt.prec, got, tt.want)
		}
	}
}

// Adjacent numbers in path data must never merge into one token.
func TestNumberSeparators(t *testing.T) {
	p := trace.Path{Curves: []trace.Curve{{
		Start: trace.Point{X: 0, Y: 0},
		Segments: []trace.Segment{
			// Deltas of 0 then 0.91 must not serialize as "0.91".
			{Kind: trace.SegCubic,
				C1: trace.Point{X: 0, Y: 0}, C2: trace.Point{X: 0.91, Y: 0.91},
				To: trace.Point{X: 5, Y: 5}},
			{Kind: trace.SegLine, To: trace.Point{X: 0, Y: 0}},
		},
	}}}
	got := Render([]trace.Path{p}, Options{Crop: true})
	if strings.Contains(got, "0.91") && !strings.Contains(got, "0 .91") {
		t.Errorf("adjacent numbers merged into one token: %s", got)
	}
}
