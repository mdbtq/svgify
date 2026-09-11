package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdbtq/svgify/internal/preprocess"
	"github.com/mdbtq/svgify/internal/raster"
	"github.com/mdbtq/svgify/internal/svg"
	"github.com/mdbtq/svgify/internal/trace"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Exit codes follow the conventional split between misuse and runtime failure.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

var (
	errUsage = errors.New("usage")
	// errNoArtwork reports an image that thresholded to nothing, which almost
	// always means the wrong threshold rather than an empty input.
	errNoArtwork = errors.New("no artwork found: the image thresholded to nothing (try --threshold or --invert)")
)

func exitCode(err error) int {
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		return exitUsage
	default:
		return exitFailure
	}
}

type config struct {
	output     string
	threshold  int
	invert     bool
	padding    float64
	simplify   float64
	foreground string
	background string
	noCrop     bool
	precision  int
	force      bool
	quiet      bool
	showVer    bool
}

func run(args []string, stdout, stderr io.Writer) error {
	var cfg config

	fs := flag.NewFlagSet("svgify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.output, "o", "", "output file, or - for stdout (default: input with .svg extension)")
	fs.IntVar(&cfg.threshold, "threshold", -1, "brightness threshold 0-255 (default: automatic, via Otsu's method)")
	fs.BoolVar(&cfg.invert, "invert", false, "swap foreground and background")
	fs.Float64Var(&cfg.padding, "padding", 0, "padding around the artwork, in source pixels")
	fs.Float64Var(&cfg.simplify, "simplify", 1, "curve simplification; >1 is smoother, <1 follows the source more closely")
	fs.StringVar(&cfg.foreground, "foreground", "#000", "fill colour of the traced artwork")
	fs.StringVar(&cfg.background, "background", "", "background colour (default: transparent)")
	fs.BoolVar(&cfg.noCrop, "no-crop", false, "keep the original canvas instead of cropping to the artwork")
	fs.IntVar(&cfg.precision, "precision", 2, "decimal places in path coordinates")
	fs.BoolVar(&cfg.force, "f", false, "overwrite the output file if it exists")
	fs.BoolVar(&cfg.quiet, "quiet", false, "suppress warnings")
	fs.BoolVar(&cfg.showVer, "version", false, "print the version and exit")
	fs.Usage = func() { usage(stderr) }

	if err := fs.Parse(reorder(args)); err != nil {
		// flag already reported the problem, including for -h.
		if errors.Is(err, flag.ErrHelp) {
			return errUsage
		}
		return fmt.Errorf("%w: %v", errUsage, err)
	}

	if cfg.showVer {
		fmt.Fprintf(stdout, "svgify %s\n", version)
		return nil
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr)
		return errUsage
	}
	if len(rest) > 1 {
		return fmt.Errorf("%w: expected one input file, got %d", errUsage, len(rest))
	}
	input := rest[0]

	if cfg.threshold != -1 && (cfg.threshold < 0 || cfg.threshold > 255) {
		return fmt.Errorf("%w: --threshold must be between 0 and 255", errUsage)
	}
	if cfg.simplify < 0 {
		return fmt.Errorf("%w: --simplify must not be negative", errUsage)
	}
	if cfg.precision < 0 || cfg.precision > 10 {
		return fmt.Errorf("%w: --precision must be between 0 and 10", errUsage)
	}

	out, err := resolveOutput(input, cfg.output)
	if err != nil {
		return err
	}

	doc, warn, err := convert(input, cfg)
	if err != nil {
		return err
	}
	if warn != "" && !cfg.quiet {
		fmt.Fprintln(stderr, "svgify: warning:", warn)
	}

	return write(doc, out, cfg.force, stdout)
}

// convert runs the pipeline over one input file.
func convert(input string, cfg config) (doc string, warning string, err error) {
	var img *raster.Image
	if input == "-" {
		img, err = raster.Decode(os.Stdin, "")
	} else {
		img, err = raster.Open(input)
	}
	if err != nil {
		return "", "", err
	}

	pre := preprocess.Options{Invert: cfg.invert}
	if cfg.threshold >= 0 {
		t := cfg.threshold
		pre.Threshold = &t
	}
	res := preprocess.Run(img, pre)

	if res.Bitmap.Count() == 0 {
		return "", "", errNoArtwork
	}
	// A bitmap that is almost entirely foreground usually means the background
	// was misdetected; the trace will "work" but produce a filled box.
	if coverage := float64(res.Bitmap.Count()) / float64(res.Bitmap.W*res.Bitmap.H); coverage > 0.95 {
		warning = "the image traced as almost entirely foreground; --invert or --threshold may be needed"
	}

	paths, err := trace.Run(res.Bitmap, trace.Options{Simplify: cfg.simplify})
	if err != nil {
		return "", "", err
	}
	if len(paths) == 0 {
		return "", "", errNoArtwork
	}

	doc = svg.Render(paths, svg.Options{
		Padding:    cfg.padding,
		Precision:  cfg.precision,
		Fill:       cfg.foreground,
		Background: cfg.background,
		Crop:       !cfg.noCrop,
		CanvasW:    float64(img.Bounds().Dx()),
		CanvasH:    float64(img.Bounds().Dy()),
	})
	return doc, warning, nil
}

// resolveOutput derives the output path and refuses to clobber the input.
func resolveOutput(input, explicit string) (string, error) {
	out := explicit
	if out == "" {
		if input == "-" {
			return "-", nil
		}
		out = strings.TrimSuffix(input, filepath.Ext(input)) + ".svg"
	}
	if out == "-" {
		return out, nil
	}
	if input != "-" && sameFile(input, out) {
		return "", fmt.Errorf("refusing to overwrite the input file %s", input)
	}
	return out, nil
}

// sameFile compares paths by identity where possible, falling back to a
// cleaned-path comparison when the output does not exist yet.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err == nil {
		if bi, err := os.Stat(b); err == nil {
			return os.SameFile(ai, bi)
		}
	}
	pa, err1 := filepath.Abs(a)
	pb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && filepath.Clean(pa) == filepath.Clean(pb)
}

func write(doc, out string, force bool, stdout io.Writer) error {
	if out == "-" {
		_, err := io.WriteString(stdout, doc+"\n")
		return err
	}
	if !force {
		if _, err := os.Stat(out); err == nil {
			return fmt.Errorf("%s already exists (use -f to overwrite)", out)
		}
	}
	return os.WriteFile(out, []byte(doc+"\n"), 0o644)
}

// reorder moves operands after flags. Go's flag package stops parsing at the
// first non-flag argument, but "svgify logo.png -o out.svg" is the natural way
// to invoke a one-argument tool, so accept flags on either side of the input.
func reorder(args []string) []string {
	// Flags that consume a following value, and so must keep it adjacent.
	valueFlags := map[string]bool{
		"-o": true, "-threshold": true, "-padding": true, "-simplify": true,
		"-foreground": true, "-background": true, "-precision": true,
	}
	// Accept both -name and --name spellings, as the flag package does.
	name := func(s string) string { return "-" + strings.TrimLeft(s, "-") }

	var flags, operands []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// A flag written as --name=value carries its own value.
			if !strings.Contains(a, "=") && valueFlags[name(a)] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		operands = append(operands, a)
	}
	return append(flags, operands...)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `svgify converts raster images into clean, tightly cropped SVG vector graphics.

Usage:
  svgify [options] <input>

  Reads PNG, JPEG or WebP. Writes an SVG next to the input unless -o is given.
  Use - as the input or output to read stdin or write stdout.

Options:
  -o <file>            output file, or - for stdout (default: input with .svg)
  -f                   overwrite the output file if it exists
  --threshold <0-255>  brightness cutoff (default: automatic, via Otsu's method)
  --invert             swap foreground and background
  --padding <px>       padding around the artwork, in source pixels
  --simplify <n>       curve simplification; >1 smoother, <1 closer to source
  --foreground <col>   fill colour of the artwork (default: #000)
  --background <col>   background colour (default: transparent)
  --no-crop            keep the original canvas instead of cropping
  --precision <n>      decimal places in path coordinates (default: 2)
  --quiet              suppress warnings
  --version            print the version

Examples:
  svgify logo.png
  svgify logo.png -o icon.svg
  svgify scan.jpg --threshold 160 --padding 4
  svgify photo.png --invert --simplify 1.5
`)
}
