// Package svg renders traced paths into a compact, standards-compliant SVG.
package svg

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mdbtq/trace/internal/trace"
)

// Options controls SVG serialization.
type Options struct {
	// Padding in source pixels added around the artwork bounds.
	Padding float64
	// Precision is the number of decimals kept in path data.
	Precision int
	// Fill is the CSS colour for the traced artwork.
	Fill string
	// Background, when non-empty, paints a rectangle behind the artwork.
	Background string
	// Crop tightens the viewBox to the artwork. When false the viewBox covers
	// the full source canvas.
	Crop bool
	// CanvasW and CanvasH are the source image dimensions, used when Crop is
	// false.
	CanvasW, CanvasH float64
}

// Render serializes paths into an SVG document.
func Render(paths []trace.Path, opt Options) string {
	if opt.Precision <= 0 {
		opt.Precision = 2
	}
	if opt.Fill == "" {
		opt.Fill = "#000"
	}

	minX, minY, w, h := viewBox(paths, opt)

	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="`)
	b.WriteString(strings.Join([]string{
		num(minX, opt.Precision), num(minY, opt.Precision),
		num(w, opt.Precision), num(h, opt.Precision),
	}, " "))
	b.WriteString(`">`)

	if opt.Background != "" {
		fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`,
			num(minX, opt.Precision), num(minY, opt.Precision),
			num(w, opt.Precision), num(h, opt.Precision), opt.Background)
	}

	// One path element per fill colour keeps the document flat: no groups, no
	// transforms. Monochrome output collapses to a single <path>.
	for _, group := range groupByFill(paths, opt.Fill) {
		d := pathData(group.paths, opt.Precision)
		if d == "" {
			continue
		}
		b.WriteString(`<path fill="`)
		b.WriteString(group.fill)
		// Even-odd is what makes a contour inside a contour read as a hole.
		b.WriteString(`" fill-rule="evenodd" d="`)
		b.WriteString(d)
		b.WriteString(`"/>`)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

type fillGroup struct {
	fill  string
	paths []trace.Path
}

// groupByFill collects paths sharing a colour, preserving first-seen order so
// output is deterministic.
func groupByFill(paths []trace.Path, defaultFill string) []fillGroup {
	var groups []fillGroup
	index := map[string]int{}
	for _, p := range paths {
		fill := p.Fill
		if fill == "" {
			fill = defaultFill
		}
		i, ok := index[fill]
		if !ok {
			index[fill] = len(groups)
			groups = append(groups, fillGroup{fill: fill})
			i = len(groups) - 1
		}
		groups[i].paths = append(groups[i].paths, p)
	}
	return groups
}

// viewBox computes the document viewport: the artwork bounds plus padding when
// cropping, otherwise the full source canvas.
func viewBox(paths []trace.Path, opt Options) (x, y, w, h float64) {
	if !opt.Crop {
		return 0, 0, opt.CanvasW, opt.CanvasH
	}

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range paths {
		for _, c := range p.Curves {
			x0, y0, x1, y1 := c.Bounds()
			minX, minY = math.Min(minX, x0), math.Min(minY, y0)
			maxX, maxY = math.Max(maxX, x1), math.Max(maxY, y1)
		}
	}
	if math.IsInf(minX, 1) {
		return 0, 0, opt.CanvasW, opt.CanvasH
	}

	minX -= opt.Padding
	minY -= opt.Padding
	maxX += opt.Padding
	maxY += opt.Padding
	return minX, minY, maxX - minX, maxY - minY
}

// pathData serializes contours into a single `d` attribute, using relative
// commands and dropping redundant separators.
func pathData(paths []trace.Path, prec int) string {
	var b strings.Builder
	for _, p := range paths {
		for _, c := range p.Curves {
			writeContour(&b, c, prec)
		}
	}
	return b.String()
}

func writeContour(b *strings.Builder, c trace.Curve, prec int) {
	if len(c.Segments) == 0 {
		return
	}
	start := c.Start
	b.WriteString("M")
	b.WriteString(num(start.X, prec))
	b.WriteString(" ")
	b.WriteString(num(start.Y, prec))

	cur := start
	var lastCmd byte
	for _, seg := range c.Segments {
		switch seg.Kind {
		case trace.SegLine:
			// Axis-aligned lines compress to H/V.
			switch {
			case eq(seg.To.Y, cur.Y, prec):
				lastCmd = writeCmd(b, 'h', lastCmd, num(seg.To.X-cur.X, prec))
			case eq(seg.To.X, cur.X, prec):
				lastCmd = writeCmd(b, 'v', lastCmd, num(seg.To.Y-cur.Y, prec))
			default:
				lastCmd = writeCmd(b, 'l', lastCmd,
					num(seg.To.X-cur.X, prec), num(seg.To.Y-cur.Y, prec))
			}
		case trace.SegCubic:
			lastCmd = writeCmd(b, 'c', lastCmd,
				num(seg.C1.X-cur.X, prec), num(seg.C1.Y-cur.Y, prec),
				num(seg.C2.X-cur.X, prec), num(seg.C2.Y-cur.Y, prec),
				num(seg.To.X-cur.X, prec), num(seg.To.Y-cur.Y, prec))
		}
		cur = seg.To
	}
	b.WriteString("z")
}

// writeCmd emits a command letter only when it differs from the previous one;
// SVG allows repeating a command by simply supplying more coordinates.
func writeCmd(b *strings.Builder, cmd byte, last byte, args ...string) byte {
	if cmd != last {
		b.WriteByte(cmd)
	} else {
		b.WriteByte(' ')
	}
	for i, a := range args {
		if i > 0 && needsSep(args[i-1], a) {
			b.WriteByte(' ')
		}
		b.WriteString(a)
	}
	return cmd
}

// needsSep reports whether two adjacent path numbers require a separator.
// Only a leading "-" is self-delimiting; a leading "." is not, because the
// previous number would simply absorb it ("0" then ".91" must not become
// "0.91").
func needsSep(_, next string) bool {
	return !strings.HasPrefix(next, "-")
}

func eq(a, b float64, prec int) bool {
	return num(a, prec) == num(b, prec)
}

// num formats a coordinate at the requested precision, trimming trailing zeros
// and the leading zero of a fraction ("0.5" -> ".5").
func num(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	switch {
	case s == "" || s == "-" || s == "-0":
		return "0"
	case strings.HasPrefix(s, "0."):
		return s[1:]
	case strings.HasPrefix(s, "-0."):
		return "-" + s[2:]
	}
	return s
}
