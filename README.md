# trace

Convert raster images into clean, tightly cropped SVG vector graphics.

```console
$ trace logo.png
```

writes `logo.svg`: a single flat path, smooth Bézier curves, holes preserved,
a `viewBox` fitted to the artwork and no surrounding whitespace.

Built for logos, icons, line art and signatures — the cases where you want one
command and a good result, not a dialog full of sliders.

## Install

```console
go install github.com/mdbtq/trace/cmd/trace@latest
```

Or from a clone:

```console
make build/install
```

Pre-built binaries can be produced for every supported platform with
`make build/dist` (macOS arm64/amd64, Linux amd64/arm64, Windows amd64).

There are **no native or runtime dependencies**. The tracer is pure Go, so the
result is a single static binary (~2.3 MB) that needs no C toolchain to build
and no shared libraries to run.

## Examples

```console
trace logo.png                      # -> logo.svg
trace logo.png -o icon.svg          # explicit output
trace logo.png -o -                 # write to stdout
cat logo.png | trace - -o out.svg   # read from stdin

trace scan.jpg --threshold 160      # override automatic thresholding
trace photo.png --invert            # swap foreground and background
trace mark.png --padding 4          # breathing room around the artwork
trace rough.png --simplify 1.5      # smoother, fewer segments
trace art.png --no-crop             # keep the original canvas
trace logo.png --foreground '#e11'  # colour the output
```

## Options

| Option | Default | Meaning |
| --- | --- | --- |
| `-o <file>` | input with `.svg` | Output path, or `-` for stdout |
| `-f` | off | Overwrite the output if it exists |
| `--threshold <0-255>` | automatic | Brightness cutoff; pixels darker than this are artwork |
| `--invert` | off | Swap foreground and background |
| `--padding <px>` | `0` | Padding around the artwork, in source pixels |
| `--simplify <n>` | `1` | Curve simplification; `>1` smoother, `<1` closer to the source |
| `--foreground <col>` | `#000` | Fill colour of the artwork |
| `--background <col>` | transparent | Background colour |
| `--no-crop` | off | Keep the original canvas instead of cropping |
| `--precision <n>` | `2` | Decimal places in path coordinates |
| `--quiet` | off | Suppress warnings |
| `--version`, `--help` | | |

Flags may appear before or after the input file.

## Supported formats

PNG, JPEG and WebP in; SVG out. Alpha is read and used where present.

## How the automatic tracing works

The aim is that `trace logo.png` is right often enough that the flags stay
unused. The pipeline is:

1. **Decode** to straight (non-premultiplied) RGBA.
2. **Separate artwork from background.** If more than 5% of the image is
   transparent, alpha defines the artwork — a logo on a transparent canvas is
   traced by its coverage, whatever colour it is. Otherwise the image is
   converted to Rec. 601 luma (composited onto white so transparent pixels do
   not read as black) and thresholded.
3. **Threshold automatically** using [Otsu's method][otsu], which picks the
   cutoff maximising between-class variance. This adapts to the image, so
   low-contrast grey-on-grey artwork works without tuning.
4. **Detect inversion.** The border ring of the image is sampled; whichever
   side of the threshold dominates it is treated as background. That is what
   makes white-on-black trace the same shape as black-on-white, instead of
   tracing the canvas.
5. **Remove speckles.** Connected components below 1/20000 of the image area
   are dropped, and interior gaps that small are filled. Background regions
   touching the border are never filled — that is the canvas, not a hole.
6. **Vectorize** with the Potrace algorithm: the contour is decomposed into
   the optimal polygon, then fitted with Bézier curves. This is why the output
   is smooth rather than a staircase of pixel edges, and why antialiased source
   edges survive.
7. **Emit SVG**: one `<path>` per fill colour, `fill-rule="evenodd"` so nested
   contours read as holes, relative commands, `H`/`V` shorthand, trimmed
   numbers, and a `viewBox` computed from the true curve extrema — not the
   control-point hull, which would leave a sliver of margin.

The output has **no `width`/`height` attributes**, so it scales to whatever box
you put it in. There are no groups, no transforms and no metadata.

### When to reach for a flag

- The result is a filled box → the background was misread. Try `--invert`.
  `trace` warns on stderr when the trace comes out almost entirely solid.
- Detail is lost or noise is kept → set `--threshold` explicitly.
- Edges are wobbly → raise `--simplify`. Fine detail is being lost → lower it.

## Behaviour

Conventional Unix behaviour: silent on success, errors on stderr, exit `0` on
success, `1` on failure, `2` on misuse. `trace` will not overwrite its own
input, and will not overwrite an existing output without `-f`.

## Colour

Monochrome tracing is what is implemented. The internal representation carries
a fill per path and the SVG writer groups by fill, so colour tracing can be
added without touching the SVG or CLI layers. See
[docs/vectorization.md](docs/vectorization.md).

## Development

```console
go build ./...
go test ./...
```

or `make build`, `make test`, `make lint`, `make build/dist`.

Test fixtures are generated in code rather than checked in as binaries; see
`internal/testfixtures`. They cover black-on-white, white-on-black, transparent
PNGs, large surrounding whitespace, internal holes, antialiased edges and
low-contrast grey artwork.

## Licence

GPL-2.0. `trace` links [dennwc/gotrace][gotrace], a Go port of Potrace, which
is GPL-2.0; a binary linking it is a derivative work. The dependency is
vendored under `vendor/`. See [LICENSE](LICENSE), [NOTICE](NOTICE) and
[docs/vectorization.md](docs/vectorization.md) for the reasoning.

[otsu]: https://en.wikipedia.org/wiki/Otsu%27s_method
[gotrace]: https://github.com/dennwc/gotrace
