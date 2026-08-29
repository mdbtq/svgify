// Package trace converts binary bitmaps into smooth vector contours.
//
// It is the only package that knows which vectorization engine is in use; its
// types are engine-neutral so the rest of the pipeline is insulated from that
// choice. See docs/vectorization.md.
package trace

import (
	"fmt"
	"math"

	"github.com/dennwc/gotrace"

	"github.com/mdbtq/trace/internal/preprocess"
)

// Point is a coordinate in source-image pixel space.
type Point struct{ X, Y float64 }

// SegKind distinguishes the two segment shapes a contour can contain.
type SegKind int

const (
	// SegLine is a straight line to To.
	SegLine SegKind = iota
	// SegCubic is a cubic Bézier through control points C1 and C2 to To.
	SegCubic
)

// Segment is one step of a closed contour.
type Segment struct {
	Kind   SegKind
	C1, C2 Point
	To     Point
}

// Curve is a single closed contour.
type Curve struct {
	Start    Point
	Segments []Segment
}

// Path is a filled region: one or more contours sharing a fill, where nested
// contours act as holes under the even-odd fill rule.
type Path struct {
	Curves []Curve
	// Fill is the CSS colour, empty meaning "use the document default".
	Fill string
}

// Options controls the vectorizer.
type Options struct {
	// Simplify scales curve optimization tolerance. Higher yields fewer, more
	// approximate segments; 1.0 is the engine default.
	Simplify float64
	// TurdSize suppresses traced components smaller than this area.
	TurdSize int
}

// Run vectorizes a binary bitmap into contours.
func Run(bm *preprocess.Bitmap, opt Options) ([]Path, error) {
	params := gotrace.Defaults
	if opt.TurdSize > 0 {
		params.TurdSize = opt.TurdSize
	}
	if opt.Simplify > 0 {
		// The engine's tolerance is an absolute distance; scaling the default
		// keeps --simplify a relative, intuitive dial.
		params.OptTolerance = gotrace.Defaults.OptTolerance * opt.Simplify
	}

	paths, err := gotrace.Trace(toBitmap(bm), &params)
	if err != nil {
		return nil, fmt.Errorf("vectorize: %w", err)
	}

	// Each top-level path plus its descendants becomes one even-odd filled
	// region, so holes stay attached to the shape that contains them.
	out := make([]Path, 0, len(paths))
	for _, p := range paths {
		var curves []Curve
		collect(p, &curves)
		if len(curves) > 0 {
			out = append(out, Path{Curves: curves})
		}
	}
	return out, nil
}

func collect(p gotrace.Path, dst *[]Curve) {
	if c, ok := convert(p.Curve); ok {
		*dst = append(*dst, c)
	}
	for _, child := range p.Childs {
		collect(child, dst)
	}
}

// convert maps engine segments onto our representation. The engine's contours
// are closed, so the start point is the end point of the final segment.
func convert(segs []gotrace.Segment) (Curve, bool) {
	if len(segs) == 0 {
		return Curve{}, false
	}
	last := segs[len(segs)-1]
	c := Curve{
		Start:    Point{last.Pnt[2].X, last.Pnt[2].Y},
		Segments: make([]Segment, 0, len(segs)+1),
	}
	for _, s := range segs {
		switch s.Type {
		case gotrace.TypeCorner:
			// A corner is two straight segments through the corner vertex.
			c.Segments = append(c.Segments,
				Segment{Kind: SegLine, To: Point{s.Pnt[1].X, s.Pnt[1].Y}},
				Segment{Kind: SegLine, To: Point{s.Pnt[2].X, s.Pnt[2].Y}})
		case gotrace.TypeBezier:
			c.Segments = append(c.Segments, Segment{
				Kind: SegCubic,
				C1:   Point{s.Pnt[0].X, s.Pnt[0].Y},
				C2:   Point{s.Pnt[1].X, s.Pnt[1].Y},
				To:   Point{s.Pnt[2].X, s.Pnt[2].Y},
			})
		}
	}
	return c, true
}

func toBitmap(bm *preprocess.Bitmap) *gotrace.Bitmap {
	out := gotrace.NewBitmap(bm.W, bm.H)
	for y := 0; y < bm.H; y++ {
		for x := 0; x < bm.W; x++ {
			out.Set(x, y, bm.At(x, y))
		}
	}
	return out
}

// Extremes returns points bounding the segment. For a cubic this is the true
// extrema of the curve, not its control polygon, so the viewBox fits the ink
// rather than the (larger) hull of the control points.
func (s Segment) Extremes(from Point) []Point {
	if s.Kind == SegLine {
		return []Point{s.To}
	}
	pts := []Point{s.To}
	for _, axis := range []int{0, 1} {
		p0, p1, p2, p3 := coord(from, axis), coord(s.C1, axis), coord(s.C2, axis), coord(s.To, axis)
		for _, t := range cubicExtremaT(p0, p1, p2, p3) {
			pts = append(pts, Point{
				X: cubicAt(t, from.X, s.C1.X, s.C2.X, s.To.X),
				Y: cubicAt(t, from.Y, s.C1.Y, s.C2.Y, s.To.Y),
			})
		}
	}
	return pts
}

func coord(p Point, axis int) float64 {
	if axis == 0 {
		return p.X
	}
	return p.Y
}

func cubicAt(t, p0, p1, p2, p3 float64) float64 {
	u := 1 - t
	return u*u*u*p0 + 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t*p3
}

// cubicExtremaT solves B'(t)=0 in [0,1] for one axis.
func cubicExtremaT(p0, p1, p2, p3 float64) []float64 {
	// B'(t) = 3[ a t^2 + b t + c ]
	a := -p0 + 3*p1 - 3*p2 + p3
	b := 2 * (p0 - 2*p1 + p2)
	c := p1 - p0

	var ts []float64
	add := func(t float64) {
		if t > 0 && t < 1 {
			ts = append(ts, t)
		}
	}
	if math.Abs(a) < 1e-12 {
		if math.Abs(b) > 1e-12 {
			add(-c / b)
		}
		return ts
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return ts
	}
	sq := math.Sqrt(disc)
	add((-b + sq) / (2 * a))
	add((-b - sq) / (2 * a))
	return ts
}

// Bounds returns the tight bounding box of the curve's ink.
func (c Curve) Bounds() (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	acc := func(p Point) {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	acc(c.Start)
	cur := c.Start
	for _, s := range c.Segments {
		for _, p := range s.Extremes(cur) {
			acc(p)
		}
		cur = s.To
	}
	return minX, minY, maxX, maxY
}
