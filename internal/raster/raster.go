// Package raster decodes raster images into a normalized in-memory form.
package raster

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// ErrUnsupported reports an input whose format could not be decoded.
var ErrUnsupported = errors.New("unsupported image format")

// Image is a decoded source image in straight (non-premultiplied) RGBA.
type Image struct {
	*image.NRGBA
	// Format is the decoder name, e.g. "png", "jpeg", "webp".
	Format string
}

// Decode reads an image from r. The name is only used to improve error messages.
func Decode(r io.Reader, name string) (*Image, error) {
	src, format, err := image.Decode(r)
	if err != nil {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
			return nil, fmt.Errorf("%w: %s: %v", ErrUnsupported, ext, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}

	b := src.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil, errors.New("image has zero dimensions")
	}

	// Normalize to NRGBA anchored at the origin so downstream stages can index
	// pixels directly without carrying the source origin around.
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	return &Image{NRGBA: dst, Format: format}, nil
}

// Open decodes the image at path.
func Open(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f, path)
}
