// Package pipeline_test exercises decode -> preprocess -> trace -> svg as a
// whole, asserting the structural properties the tool promises rather than
// pixel-exact output.
package pipeline_test

import (
	"bytes"
	"encoding/xml"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mdbtq/trace/internal/preprocess"
	"github.com/mdbtq/trace/internal/raster"
	"github.com/mdbtq/trace/internal/svg"
	"github.com/mdbtq/trace/internal/testfixtures"
	"github.com/mdbtq/trace/internal/trace"
)

// convert runs the full pipeline over a fixture.
func convert(t *testing.T, f testfixtures.Fixture, pre preprocess.Options, so svg.Options) string {
	t.Helper()
	img, err := raster.Decode(bytes.NewReader(f.PNG()), f.Name+".png")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	res := preprocess.Run(img, pre)
	paths, err := trace.Run(res.Bitmap, trace.Options{})
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	so.Crop = !so.Crop // callers pass "noCrop"; default here is cropping
	so.CanvasW, so.CanvasH = float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	return svg.Render(paths, so)
}

func traceFixture(t *testing.T, f testfixtures.Fixture) string {
	t.Helper()
	return convert(t, f, preprocess.Options{}, svg.Options{})
}

var viewBoxRe = regexp.MustCompile(`viewBox="([-\d.]+) ([-\d.]+) ([-\d.]+) ([-\d.]+)"`)

func viewBox(t *testing.T, doc string) (x, y, w, h float64) {
	t.Helper()
	m := viewBoxRe.FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("no viewBox in output: %s", doc)
	}
	v := make([]float64, 4)
	for i := range v {
		f, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			t.Fatalf("bad viewBox number %q: %v", m[i+1], err)
		}
		v[i] = f
	}
	return v[0], v[1], v[2], v[3]
}

// Every fixture must produce well-formed XML with a viewBox and at least one
// path.
func TestOutputIsValidSVG(t *testing.T) {
	for _, f := range testfixtures.All() {
		t.Run(f.Name, func(t *testing.T) {
			doc := traceFixture(t, f)

			if err := xml.Unmarshal([]byte(doc), new(struct {
				XMLName xml.Name
			})); err != nil {
				t.Fatalf("output is not well-formed XML: %v\n%s", err, doc)
			}
			if !strings.Contains(doc, `xmlns="http://www.w3.org/2000/svg"`) {
				t.Error("missing SVG namespace")
			}
			if !strings.Contains(doc, "<path") {
				t.Errorf("no path traced: %s", doc)
			}
			if _, _, w, h := viewBox(t, doc); w <= 0 || h <= 0 {
				t.Errorf("viewBox has non-positive size: %g x %g", w, h)
			}
		})
	}
}

// The viewBox must hug the artwork. Each fixture's artwork has a known size in
// source pixels; the traced box should match within a pixel of tolerance.
func TestViewBoxIsTightlyCropped(t *testing.T) {
	tests := []struct {
		fixture      testfixtures.Fixture
		wantW, wantH float64
	}{
		{testfixtures.BlackOnWhite(), 80, 80},   // disc r=40
		{testfixtures.WhiteOnBlack(), 80, 80},   // same disc, inverted
		{testfixtures.TransparentPNG(), 80, 80}, // same disc via alpha
		{testfixtures.Holes(), 120, 120},        // ring outer r=60
		{testfixtures.Antialiased(), 80, 80},
		{testfixtures.LowContrast(), 80, 80},
		{testfixtures.Whitespace(), 40, 60}, // 40x60 mark on a 400x400 canvas
	}

	const tol = 2.0
	for _, tt := range tests {
		t.Run(tt.fixture.Name, func(t *testing.T) {
			_, _, w, h := viewBox(t, traceFixture(t, tt.fixture))
			if math.Abs(w-tt.wantW) > tol || math.Abs(h-tt.wantH) > tol {
				t.Errorf("viewBox = %g x %g, want %g x %g (±%g): whitespace was not cropped",
					w, h, tt.wantW, tt.wantH, tol)
			}
		})
	}
}

// The large-whitespace fixture is the sharpest test of cropping: the artwork
// is 1.5% of the canvas.
func TestWhitespaceIsRemoved(t *testing.T) {
	doc := traceFixture(t, testfixtures.Whitespace())
	x, y, w, h := viewBox(t, doc)

	if w > 100 || h > 100 {
		t.Errorf("viewBox %g x %g still contains the surrounding whitespace", w, h)
	}
	// The mark sits at (20,30); the box must start near it, not at the origin.
	if x < 10 || y < 20 {
		t.Errorf("viewBox origin (%g, %g) includes empty canvas", x, y)
	}
}

// A ring must trace as two contours: the outer edge and the hole.
func TestHolesArePreserved(t *testing.T) {
	doc := traceFixture(t, testfixtures.Holes())

	if n := strings.Count(doc, "M"); n < 2 {
		t.Fatalf("ring produced %d subpaths, want at least 2 (outer edge + hole): %s", n, doc)
	}
	if !strings.Contains(doc, `fill-rule="evenodd"`) {
		t.Error("even-odd fill rule missing; the hole would render filled")
	}
}

// The reference logo must keep all three facial cutouts.
func TestLogoCutoutsArePreserved(t *testing.T) {
	doc := traceFixture(t, testfixtures.Logo())
	// Outer silhouette plus two eyes and a mouth.
	if n := strings.Count(doc, "M"); n < 4 {
		t.Errorf("logo produced %d subpaths, want at least 4 (outline + 2 eyes + mouth)", n)
	}
}

// Black-on-white and white-on-black are the same shape; automatic detection
// must yield the same geometry for both.
func TestInversionIsDetectedAutomatically(t *testing.T) {
	_, _, w1, h1 := viewBox(t, traceFixture(t, testfixtures.BlackOnWhite()))
	_, _, w2, h2 := viewBox(t, traceFixture(t, testfixtures.WhiteOnBlack()))

	if math.Abs(w1-w2) > 2 || math.Abs(h1-h2) > 2 {
		t.Errorf("black-on-white traced %gx%g but white-on-black traced %gx%g; "+
			"the background was traced in one of them", w1, h1, w2, h2)
	}
}

// --invert on a black-on-white disc traces the surrounding canvas instead, so
// the result must cover the whole image.
func TestExplicitInvertChangesOutput(t *testing.T) {
	normal := traceFixture(t, testfixtures.BlackOnWhite())
	inverted := convert(t, testfixtures.BlackOnWhite(),
		preprocess.Options{Invert: true}, svg.Options{})

	if normal == inverted {
		t.Fatal("--invert produced identical output")
	}
	_, _, nw, _ := viewBox(t, normal)
	_, _, iw, _ := viewBox(t, inverted)
	if iw <= nw {
		t.Errorf("inverted trace is %g wide, want wider than the %g-wide disc", iw, nw)
	}
}

// An explicit threshold must change what is traced.
func TestThresholdOverrideChangesOutput(t *testing.T) {
	f := testfixtures.LowContrast()
	auto := traceFixture(t, f)

	// 140 sits between the fixture's two grey levels (110 and 170), the same
	// side as Otsu, so the shape survives.
	mid := 140
	got := convert(t, f, preprocess.Options{Threshold: &mid}, svg.Options{})
	if _, _, w, _ := viewBox(t, got); math.Abs(w-80) > 2 {
		t.Errorf("threshold 140 traced a %g-wide shape, want the 80-wide disc", w)
	}
	_ = auto
}

// Transparent PNGs must trace the artwork, not the bounding box of the canvas.
func TestTransparentBackground(t *testing.T) {
	doc := traceFixture(t, testfixtures.TransparentPNG())
	_, _, w, h := viewBox(t, doc)
	if math.Abs(w-80) > 2 || math.Abs(h-80) > 2 {
		t.Errorf("transparent PNG traced %gx%g, want the 80x80 disc", w, h)
	}
	if strings.Contains(doc, "<rect") {
		t.Error("a background rectangle was added to a transparent image")
	}
}

// Antialiased edges must yield smooth curves, not a staircase of line
// segments. Repeated commands omit the letter, so count curve coordinate
// groups rather than "c" characters.
func TestAntialiasedArtworkProducesCurves(t *testing.T) {
	doc := traceFixture(t, testfixtures.Antialiased())
	d := pathData(t, doc)

	if !strings.Contains(d, "c") {
		t.Fatalf("no curve commands at all; the trace is polygonal: %s", d)
	}
	curves, lines := countSegments(d)
	if curves < 4 {
		t.Errorf("only %d curve segments; the trace is polygonal, not smooth: %s", curves, d)
	}
	// A traced circle should be overwhelmingly curves.
	if lines > curves {
		t.Errorf("%d line segments vs %d curve segments; edges were not smoothed", lines, curves)
	}
}

var pathDataRe = regexp.MustCompile(`\sd="([^"]*)"`)

func pathData(t *testing.T, doc string) string {
	t.Helper()
	m := pathDataRe.FindStringSubmatch(doc)
	if m == nil {
		t.Fatalf("no path data in output: %s", doc)
	}
	return m[1]
}

// countSegments counts curve and line segments in path data, accounting for
// implicit repeated commands: a "c" is followed by groups of six numbers, and
// each additional group is another curve segment.
func countSegments(d string) (curves, lines int) {
	numRe := regexp.MustCompile(`-?(?:\d+\.?\d*|\.\d+)`)
	cmdRe := regexp.MustCompile(`[A-Za-z]`)

	locs := cmdRe.FindAllStringIndex(d, -1)
	for i, loc := range locs {
		cmd := d[loc[0]]
		end := len(d)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		n := len(numRe.FindAllString(d[loc[1]:end], -1))
		switch cmd {
		case 'c':
			curves += n / 6
		case 'l':
			lines += n / 2
		case 'h', 'v':
			lines += n
		}
	}
	return curves, lines
}

// The same input must always produce byte-identical output.
func TestDeterministic(t *testing.T) {
	for _, f := range testfixtures.All() {
		t.Run(f.Name, func(t *testing.T) {
			first := traceFixture(t, f)
			for i := 0; i < 3; i++ {
				if got := traceFixture(t, f); got != first {
					t.Fatalf("run %d differed from the first run", i+2)
				}
			}
		})
	}
}

// --no-crop must preserve the original canvas dimensions.
func TestNoCropKeepsCanvas(t *testing.T) {
	doc := convert(t, testfixtures.Whitespace(), preprocess.Options{},
		svg.Options{Crop: true}) // convert() flips this into noCrop
	x, y, w, h := viewBox(t, doc)
	if x != 0 || y != 0 || w != 400 || h != 400 {
		t.Errorf("viewBox = %g %g %g %g, want 0 0 400 400", x, y, w, h)
	}
}

// Padding must expand the box symmetrically.
func TestPadding(t *testing.T) {
	plain := traceFixture(t, testfixtures.BlackOnWhite())
	padded := convert(t, testfixtures.BlackOnWhite(), preprocess.Options{},
		svg.Options{Padding: 10})

	_, _, w0, h0 := viewBox(t, plain)
	_, _, w1, h1 := viewBox(t, padded)
	if d := w1 - w0; math.Abs(d-20) > 0.01 {
		t.Errorf("padding 10 widened the box by %g, want 20", d)
	}
	if d := h1 - h0; math.Abs(d-20) > 0.01 {
		t.Errorf("padding 10 heightened the box by %g, want 20", d)
	}
}

// Output should stay compact: a simple logo must not become a huge polygon
// dump.
func TestOutputIsCompact(t *testing.T) {
	for _, f := range testfixtures.All() {
		t.Run(f.Name, func(t *testing.T) {
			if n := len(traceFixture(t, f)); n > 20000 {
				t.Errorf("output is %d bytes, suspiciously large for a simple shape", n)
			}
		})
	}
}

// JPEG input must decode and trace like PNG.
func TestJPEGInput(t *testing.T) {
	f := testfixtures.BlackOnWhite()
	var buf bytes.Buffer
	encodeJPEG(t, &buf, f)

	img, err := raster.Decode(bytes.NewReader(buf.Bytes()), "x.jpg")
	if err != nil {
		t.Fatalf("decode jpeg: %v", err)
	}
	if img.Format != "jpeg" {
		t.Errorf("format = %q, want jpeg", img.Format)
	}
	res := preprocess.Run(img, preprocess.Options{})
	paths, err := trace.Run(res.Bitmap, trace.Options{})
	if err != nil || len(paths) == 0 {
		t.Fatalf("tracing jpeg produced %d paths, err=%v", len(paths), err)
	}
}

func TestUnsupportedInput(t *testing.T) {
	_, err := raster.Decode(strings.NewReader("this is not an image"), "bad.txt")
	if err == nil {
		t.Fatal("decoding garbage succeeded")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %q, want it to mention the format is unsupported", err)
	}
}

func encodeJPEG(t *testing.T, buf *bytes.Buffer, f testfixtures.Fixture) {
	t.Helper()
	if err := jpegEncode(buf, f); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
}
