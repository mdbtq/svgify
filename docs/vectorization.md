# Vectorization engine: decision and trade-offs

## Decision

`svgify` uses **[github.com/dennwc/gotrace](https://github.com/dennwc/gotrace)**,
a pure-Go port of the Potrace algorithm, linked as a library.

## Why not VTracer

VTracer is Rust. Reaching it from Go means one of:

| Path | Cost |
| --- | --- |
| cgo + Rust staticlib | Needs a Rust toolchain to build, and cgo makes cross-compilation require a C toolchain per target. `go install` stops working for users without Rust. |
| WASM via wazero | Keeps `CGO_ENABLED=0`, but embeds a multi-MB module, adds a runtime, and pays interpretation cost. Adds a build step producing a checked-in binary artefact. |
| Subprocess | Requires users to install VTracer separately. Fails the "single distributable binary" goal outright. |

None of these is worth it for the primary use case. VTracer's real strength is
*colour* tracing via hierarchical clustering; for monochrome logos, icons, line
art and signatures — what this tool targets — Potrace's polygon-optimization and
curve-fitting pipeline is the stronger algorithm and the industry reference
(it is what Inkscape's "Trace Bitmap" uses for monochrome).

## Why gotrace specifically

It is a faithful port of the full Potrace pipeline, not a naive contour tracer:
path decomposition, `calcLon` (longest straight subpaths), `bestpolygon`
(optimal polygon by dynamic programming), `adjustVertices`, `smooth` (corner
detection and Bézier fitting) and `optiCurve` (curve joining) are all present.
That is what produces smooth curves instead of a staircase of pixel edges.

Verified properties:

- **Pure Go.** cgo appears only in the optional `bindings/` subpackage, which
  wraps the C libpotrace and which this project does not import. The root
  package builds with `CGO_ENABLED=0`.
- **Cross-compiles** to darwin/arm64, darwin/amd64, linux/amd64, linux/arm64
  and windows/amd64 with no C toolchain.
- **Preserves holes**: `Trace` returns a hierarchy where `Path.Childs` holds
  inner contours with the opposite `Sign`, which maps directly onto SVG's
  `fill-rule="evenodd"`.

## Licensing consequence

Potrace is GPLv2, and so is this port. Linking it makes the resulting binary a
derivative work, so **`svgify` is distributed under GPLv2**. This is the same
position Inkscape and every other Potrace-based tool is in.

If a permissive licence is ever required, the replacement options are to
re-implement the curve-fitting stage from the published paper under a
different licence, or to move the tracer behind an interface and ship a
separate engine. The `internal/trace` package exists partly to keep that
boundary: nothing outside it imports gotrace, and its `Path`/`Curve` types are
engine-neutral.

## Colour

The internal representation carries a `Fill` per path and the SVG writer groups
paths by fill, so multi-colour tracing (colour quantization, then one Potrace
pass per colour layer) can be added without changing the SVG or CLI layers.
Monochrome is what is implemented today.
